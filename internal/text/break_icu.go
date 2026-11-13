//go:build windows || darwin

package text

import (
	"strings"
	"sync"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"
)

// Grapheme and word boundaries come from the ICU that the system ships:
// icu.dll or icuuc.dll on Windows, libicucore on macOS. Without it the
// callers fall back to code point boundaries.

const (
	ubrkCharacter = 0
	ubrkWord      = 1

	ubrkDone          = -1
	ubrkWordNoneLimit = 100 // rule statuses below this mark spaces and punctuation

	// breakContext bounds the text handed to ICU on each side of the offset.
	breakContext = 2048
)

// icuAPI holds the ICU break iterator functions of the platform's library.
type icuAPI struct {
	open       func(kind int32, locale *byte, text *uint16, n int32, status *int32) uintptr
	setText    func(it uintptr, text *uint16, n int32, status *int32)
	preceding  func(it uintptr, offset int32) int32
	following  func(it uintptr, offset int32) int32
	ruleStatus func(it uintptr) int32
}

// breaker is one ICU break iterator with the text it was last given. ICU
// keeps a pointer to that text, so the buffer lives as long as the iterator
// may read it.
type breaker struct {
	mu     sync.Mutex
	kind   int32
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
func (bk *breaker) iterator() (*icuAPI, bool) {
	p := icu()
	if p == nil || bk.failed {
		return nil, false
	}
	if bk.it == 0 {
		var status int32
		locale := [1]byte{}
		r := p.open(bk.kind, &locale[0], nil, 0, &status)
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
func (bk *breaker) load(p *icuAPI, s string, b int) (int, bool) {
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
	var ptr *uint16
	if len(bk.buf) > 0 {
		ptr = unsafe.SliceData(bk.buf)
	}
	p.setText(bk.it, ptr, int32(len(bk.buf)), &status)
	if status > 0 {
		return 0, false
	}
	return at, true
}

func (bk *breaker) byteAt(u int) int { return bk.lo + bk.bytes[u] }

func (bk *breaker) start() int { return bk.lo }

func (bk *breaker) end() int { return bk.lo + bk.bytes[len(bk.bytes)-1] }

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
	u := int(p.preceding(bk.it, int32(at)))
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
	u := int(p.following(bk.it, int32(at)))
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
		u := int(p.preceding(bk.it, int32(at)))
		if u == ubrkDone {
			return bk.start(), true
		}
		// The rule status describes the segment that ends at the boundary
		// last returned, so step forward again to classify [u, at).
		p.following(bk.it, int32(u))
		if p.ruleStatus(bk.it) >= ubrkWordNoneLimit {
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
		u := int(p.following(bk.it, int32(at)))
		if u == ubrkDone {
			return bk.end(), true
		}
		if p.ruleStatus(bk.it) >= ubrkWordNoneLimit {
			return bk.byteAt(u), true
		}
		at = u
	}
}
