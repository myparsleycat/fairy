package spell

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/com"
	"github.com/myparsleycat/fairy/internal/win"
)

var (
	clsidSpellCheckerFactory = com.MustGUID("7AB36653-1796-484B-BDFA-E74F1DB7C1DC")
	iidISpellCheckerFactory  = com.MustGUID("8E018A9D-2415-4677-BF08-794EA61F94BB")
)

// Vtable slots of the spell checking interfaces (spellcheck.h).
const (
	slotFactoryIsSupported   = 4
	slotFactoryCreateChecker = 5
	slotCheckerCheck         = 4
	slotCheckerSuggest       = 5
	slotCheckerAdd           = 6
	slotCheckerIgnore        = 7
	slotEnumErrorNext        = 3
	slotErrorStart           = 3
	slotErrorLength          = 4
	slotEnumStringNext       = 3
)

const suggestions = 5

// defaultWorker runs the Windows spell checker on one goroutine locked to an
// OS thread in the multithreaded apartment, so checks never wait on the UI
// thread's message loop and the UI thread never runs one. Calls the UI thread
// waits on go through urgent, which the worker serves ahead of queued checks
// and between the misspellings of a running one.
var defaultWorker = worker{initialize: initializeCOM}

type worker struct {
	once       sync.Once
	calls      chan func(*engine)
	urgent     chan func(*engine)
	done       chan struct{}
	initialize func(*engine)
}

type engine struct {
	worker   *worker
	factory  unsafe.Pointer
	checkers map[string]unsafe.Pointer // nil for languages without a dictionary
}

func (w *worker) send(urgent bool, f func(*engine)) {
	w.once.Do(func() {
		w.calls = make(chan func(*engine))
		w.urgent = make(chan func(*engine))
		w.done = make(chan struct{})
		go w.loop()
	})
	ch := w.calls
	if urgent {
		ch = w.urgent
	}
	done := make(chan struct{})
	ch <- func(s *engine) {
		defer close(done)
		f(s)
	}
	<-done
}

func (w *worker) loop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(w.done)
	s := &engine{worker: w, checkers: map[string]unsafe.Pointer{}}
	if w.initialize != nil {
		w.initialize(s)
	}
	for {
		s.runUrgent()
		select {
		case f := <-w.urgent:
			f(s)
		case f, ok := <-w.calls:
			if !ok {
				return
			}
			f(s)
		}
	}
}

func initializeCOM(s *engine) {
	if win.CoInitializeEx(win.COINIT_MULTITHREADED) == nil {
		if f, hr := win.CoCreateInstance(unsafe.Pointer(&clsidSpellCheckerFactory), unsafe.Pointer(&iidISpellCheckerFactory)); hr >= 0 {
			s.factory = f
		}
	}
}

// runUrgent runs the urgent calls already waiting, without blocking. Long
// work on the spell checking thread calls it between steps.
func (s *engine) runUrgent() {
	for {
		select {
		case f := <-s.worker.urgent:
			f(s)
		default:
			return
		}
	}
}

// checker returns the spell checker for lang, or for its language without
// the region, or nil when Windows has no dictionary for either.
func (s *engine) checker(lang string) unsafe.Pointer {
	if c, ok := s.checkers[lang]; ok {
		return c
	}
	var c unsafe.Pointer
	if s.factory != nil {
		tags := []string{lang}
		if i := strings.IndexByte(lang, '-'); i > 0 {
			tags = append(tags, lang[:i])
		}
		for _, tag := range tags {
			p, err := syscall.UTF16PtrFromString(tag)
			if err != nil {
				break
			}
			var ok int32
			if com.HR(s.factory, slotFactoryIsSupported, uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&ok))).Failed() || ok == 0 {
				continue
			}
			if com.HR(s.factory, slotFactoryCreateChecker, uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&c))).Failed() {
				c = nil
			}
			break
		}
	}
	s.checkers[lang] = c
	return c
}

// Check returns the misspelled ranges of text.
func Check(ctx context.Context, lang, text string) []Range {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	p, err := syscall.UTF16FromString(text)
	if err != nil {
		return nil
	}
	var errs []Range
	defaultWorker.send(false, func(s *engine) {
		c := s.checker(lang)
		if c == nil || ctx.Err() != nil {
			return
		}
		var enum unsafe.Pointer
		if com.HR(c, slotCheckerCheck, uintptr(unsafe.Pointer(&p[0])), uintptr(unsafe.Pointer(&enum))).Failed() || enum == nil {
			return
		}
		defer com.Release(enum)
		var units []Range
		for ctx.Err() == nil {
			s.runUrgent()
			var e unsafe.Pointer
			if com.HR(enum, slotEnumErrorNext, uintptr(unsafe.Pointer(&e))) != 0 || e == nil {
				break
			}
			var start, n uint32
			com.HR(e, slotErrorStart, uintptr(unsafe.Pointer(&start)))
			com.HR(e, slotErrorLength, uintptr(unsafe.Pointer(&n)))
			com.Release(e)
			units = append(units, Range{int(start), int(start + n)})
		}
		errs = utf16ToBytes(text, units)
	})
	return errs
}

// utf16ToBytes turns UTF-16 offsets into byte offsets of s.
func utf16ToBytes(s string, rs []Range) []Range {
	if len(rs) == 0 {
		return nil
	}
	pos := map[int]int{}
	for _, r := range rs {
		pos[r.Start], pos[r.End] = -1, -1
	}
	u := 0
	for i, r := range s {
		if _, ok := pos[u]; ok {
			pos[u] = i
		}
		u++
		if r >= 0x10000 {
			u++
		}
	}
	if _, ok := pos[u]; ok {
		pos[u] = len(s)
	}
	out := rs[:0]
	for _, r := range rs {
		if a, b := pos[r.Start], pos[r.End]; a >= 0 && b > a {
			out = append(out, Range{a, b})
		}
	}
	return out
}

func spellWord(lang, word string, slot int) {
	p, err := syscall.UTF16PtrFromString(word)
	if err != nil {
		return
	}
	defaultWorker.send(true, func(s *engine) {
		if c := s.checker(lang); c != nil {
			com.HR(c, slot, uintptr(unsafe.Pointer(p)))
		}
	})
}

// Suggest returns up to five replacements for word.
func Suggest(lang, word string) []string {
	p, err := syscall.UTF16PtrFromString(word)
	if err != nil {
		return nil
	}
	var out []string
	defaultWorker.send(true, func(s *engine) {
		c := s.checker(lang)
		if c == nil {
			return
		}
		var enum unsafe.Pointer
		if com.HR(c, slotCheckerSuggest, uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&enum))).Failed() || enum == nil {
			return
		}
		defer com.Release(enum)
		for len(out) < suggestions {
			var str uintptr
			var n uint32
			if com.HR(enum, slotEnumStringNext, 1, uintptr(unsafe.Pointer(&str)), uintptr(unsafe.Pointer(&n))) != 0 || n == 0 {
				break
			}
			out = append(out, win.CoTaskString(str))
		}
	})
	return out
}

func Add(lang, word string) {
	spellWord(lang, word, slotCheckerAdd)
}

func Ignore(lang, word string) {
	spellWord(lang, word, slotCheckerIgnore)
}
