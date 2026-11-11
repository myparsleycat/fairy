package text

import (
	"strings"
	"sync"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Grapheme and word boundaries come from the ICU that Windows ships: icu.dll
// since 1903, icuuc.dll since 1703. Without it the callers fall back to
// code point boundaries.

const (
	ubrkCharacter = 0
	ubrkWord      = 1

	ubrkDone          = -1
	ubrkWordNoneLimit = 100 // rule statuses below this mark spaces and punctuation

	// breakContext bounds the text handed to ICU on each side of the offset.
	breakContext = 2048
)

type icuProcs struct {
	open, setText, preceding, following, ruleStatus *windows.LazyProc
}

var icu = sync.OnceValue(func() *icuProcs {
	for _, name := range []string{"icu.dll", "icuuc.dll"} {
		dll := windows.NewLazySystemDLL(name)
		if dll.Load() != nil {
			continue
		}
		p := &icuProcs{
			open:       dll.NewProc("ubrk_open"),
			setText:    dll.NewProc("ubrk_setText"),
			preceding:  dll.NewProc("ubrk_preceding"),
			following:  dll.NewProc("ubrk_following"),
			ruleStatus: dll.NewProc("ubrk_getRuleStatus"),
		}
		ok := true
		for _, f := range []*windows.LazyProc{p.open, p.setText, p.preceding, p.following, p.ruleStatus} {
			if f.Find() != nil {
				ok = false
			}
		}
		if ok {
			return p
		}
	}
	return nil
})

// breaker is one ICU break iterator with the text it was last given. ICU
// keeps a pointer to that text, so the buffer lives as long as the iterator
// may read it.
type breaker struct {
	mu     sync.Mutex
	kind   uintptr
	it     uintptr
	failed bool
	buf    []uint16
	bytes  []int // byte offset in the window of each UTF-16 index, and of the end
	lo     int   // byte offset of the window in the caller's string
}

var (
	charBreaker = &breaker{kind: ubrkCharacter}
	wordBreaker = &breaker{kind: ubrkWord}
)

// iterator returns the ICU iterator, opening it on first use. It must be
// called with mu held.
func (bk *breaker) iterator() (*icuProcs, bool) {
	p := icu()
	if p == nil || bk.failed {
		return nil, false
	}
	if bk.it == 0 {
		var status int32
		locale := [1]byte{}
		r, _, _ := p.open.Call(bk.kind, uintptr(unsafe.Pointer(&locale[0])), 0, 0, uintptr(unsafe.Pointer(&status)))
		if r == 0 || status > 0 {
			bk.failed = true
			return nil, false
		}
		bk.it = r
	}
	return p, true
}

// load hands ICU the part of s around b, from the start of the line before b
// to the end of the line after it, and returns b's UTF-16 index in it.
func (bk *breaker) load(p *icuProcs, s string, b int) (int, bool) {
	lo := 0
	if b > 1 {
		lo = strings.LastIndexByte(s[:b-1], '\n') + 1
	}
	hi := len(s)
	if i := strings.IndexByte(s[b:], '\n'); i >= 0 {
		hi = b + i + 1
	}
	if b-lo > breakContext {
		lo = b - breakContext
		for lo < b && !utf8.RuneStart(s[lo]) {
			lo++
		}
	}
	if hi-b > breakContext {
		hi = b + breakContext
		for hi > b && !utf8.RuneStart(s[hi]) {
			hi--
		}
	}
	bk.buf, bk.bytes, bk.lo = bk.buf[:0], bk.bytes[:0], lo
	at := -1
	for i, r := range s[lo:hi] {
		if lo+i == b {
			at = len(bk.buf)
		}
		bk.bytes = append(bk.bytes, i)
		if r >= 0x10000 {
			r1, r2 := utf16.EncodeRune(r)
			bk.buf = append(bk.buf, uint16(r1), uint16(r2))
			bk.bytes = append(bk.bytes, i)
		} else {
			bk.buf = append(bk.buf, uint16(r))
		}
	}
	if b == hi {
		at = len(bk.buf)
	}
	bk.bytes = append(bk.bytes, hi-lo)
	if at < 0 {
		return 0, false
	}
	var status int32
	ptr := uintptr(0)
	if len(bk.buf) > 0 {
		ptr = uintptr(unsafe.Pointer(&bk.buf[0]))
	}
	p.setText.Call(bk.it, ptr, uintptr(len(bk.buf)), uintptr(unsafe.Pointer(&status)))
	if status > 0 {
		return 0, false
	}
	return at, true
}

func (bk *breaker) byteAt(u int) int { return bk.lo + bk.bytes[u] }

func (bk *breaker) start() int { return bk.lo }

func (bk *breaker) end() int { return bk.lo + bk.bytes[len(bk.bytes)-1] }

func icuInt(p *windows.LazyProc, args ...uintptr) int {
	r, _, _ := p.Call(args...)
	return int(int32(r))
}

// icuPrevBoundary returns the grapheme boundary before b.
func icuPrevBoundary(s string, b int) (int, bool) {
	bk := charBreaker
	bk.mu.Lock()
	defer bk.mu.Unlock()
	p, ok := bk.iterator()
	if !ok {
		return 0, false
	}
	at, ok := bk.load(p, s, b)
	if !ok {
		return 0, false
	}
	u := icuInt(p.preceding, bk.it, uintptr(at))
	if u == ubrkDone {
		return bk.start(), true
	}
	return bk.byteAt(u), true
}

// icuNextBoundary returns the grapheme boundary after b.
func icuNextBoundary(s string, b int) (int, bool) {
	bk := charBreaker
	bk.mu.Lock()
	defer bk.mu.Unlock()
	p, ok := bk.iterator()
	if !ok {
		return 0, false
	}
	at, ok := bk.load(p, s, b)
	if !ok {
		return 0, false
	}
	u := icuInt(p.following, bk.it, uintptr(at))
	if u == ubrkDone {
		return bk.end(), true
	}
	return bk.byteAt(u), true
}

// icuPrevWord returns the start of the word before b, skipping spaces and
// punctuation.
func icuPrevWord(s string, b int) (int, bool) {
	bk := wordBreaker
	bk.mu.Lock()
	defer bk.mu.Unlock()
	p, ok := bk.iterator()
	if !ok {
		return 0, false
	}
	at, ok := bk.load(p, s, b)
	if !ok {
		return 0, false
	}
	for {
		u := icuInt(p.preceding, bk.it, uintptr(at))
		if u == ubrkDone {
			return bk.start(), true
		}
		// The rule status describes the segment that ends at the boundary
		// last returned, so step forward again to classify [u, at).
		icuInt(p.following, bk.it, uintptr(u))
		if icuInt(p.ruleStatus, bk.it) >= ubrkWordNoneLimit {
			return bk.byteAt(u), true
		}
		at = u
	}
}

// icuNextWord returns the end of the word after b, skipping spaces and
// punctuation.
func icuNextWord(s string, b int) (int, bool) {
	bk := wordBreaker
	bk.mu.Lock()
	defer bk.mu.Unlock()
	p, ok := bk.iterator()
	if !ok {
		return 0, false
	}
	at, ok := bk.load(p, s, b)
	if !ok {
		return 0, false
	}
	for {
		u := icuInt(p.following, bk.it, uintptr(at))
		if u == ubrkDone {
			return bk.end(), true
		}
		if icuInt(p.ruleStatus, bk.it) >= ubrkWordNoneLimit {
			return bk.byteAt(u), true
		}
		at = u
	}
}
