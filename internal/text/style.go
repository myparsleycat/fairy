// Package text lays out and rasterizes text: with DirectWrite on Windows.
// All coordinates are device pixels.
package text

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Align is the horizontal alignment of lines inside the layout width.
type Align uint8

const (
	AlignStart Align = iota
	AlignCenter
	AlignEnd
)

// Style selects the font and paragraph behavior.
type Style struct {
	Family     string
	Size       float32 // device pixels
	Weight     int
	Italic     bool
	LineHeight float32 // device pixels; 0 keeps the font's own spacing
	Align      Align
	NoWrap     bool
	Ellipsis   bool
	// LetterSpacing is added after each character, in device pixels, as CSS letter-spacing.
	LetterSpacing float32
	// Runs override the style for byte ranges of the text.
	Runs []Run
}

// Run formats the bytes [Start, End) of the text. Zero fields keep the
// paragraph style. Color selects an entry of the colors passed to Draw
// (1-based; 0 keeps the paragraph color).
type Run struct {
	Start, End int
	Weight     int
	Italic     bool
	Size       float32 // device pixels
	Family     string
	Color      int
	Underline  bool
	Strike     bool
}

// Unbounded is the layout width used when text may take any width.
const Unbounded = float32(1 << 20)

// Rect is an axis-aligned rectangle in layout coordinates.
type Rect struct{ X, Y, W, H float32 }

// PrevBoundary returns the byte offset of the grapheme cluster boundary before b.
func PrevBoundary(s string, b int) int {
	if b <= 0 {
		return 0
	}
	if p, ok := icuPrevBoundary(s, b); ok {
		return p
	}
	_, size := utf8.DecodeLastRuneInString(s[:b])
	return b - size
}

// NextBoundary returns the byte offset of the grapheme cluster boundary after b.
func NextBoundary(s string, b int) int {
	if b >= len(s) {
		return len(s)
	}
	if n, ok := icuNextBoundary(s, b); ok {
		return n
	}
	_, size := utf8.DecodeRuneInString(s[b:])
	return b + size
}

// PrevDeleteBoundary returns where Backspace at b deletes back to: the whole
// cluster before b, except that a combining mark ending a cluster that is not
// an emoji sequence is deleted on its own, so an accent can be retyped
// without its base letter.
func PrevDeleteBoundary(s string, b int) int {
	p := PrevBoundary(s, b)
	r, size := utf8.DecodeLastRuneInString(s[p:b])
	if b-size <= p || !unicode.In(r, unicode.Mn, unicode.Mc) {
		return p
	}
	for _, c := range s[p:b] {
		if isEmojiJoiner(c) {
			return p
		}
	}
	return b - size
}

func isEmojiJoiner(r rune) bool {
	return r == 0x200D || r == 0xFE0E || r == 0xFE0F || r == 0x20E3 ||
		r >= 0x1F3FB && r <= 0x1F3FF || r >= 0xE0020 && r <= 0xE007F
}

// Clusters returns the byte offsets of the grapheme cluster boundaries of s,
// from 0 to len(s).
func Clusters(s string) []int {
	out := []int{0}
	for b := 0; b < len(s); {
		b = NextBoundary(s, b)
		out = append(out, b)
	}
	return out
}

// PrevWord returns the start of the word before b.
func PrevWord(s string, b int) int {
	if b <= 0 {
		return 0
	}
	if p, ok := icuPrevWord(s, b); ok {
		return p
	}
	i := b
	for i > 0 {
		r, size := utf8.DecodeLastRuneInString(s[:i])
		if !isSpace(r) {
			break
		}
		i -= size
	}
	for i > 0 {
		r, size := utf8.DecodeLastRuneInString(s[:i])
		if isSpace(r) {
			break
		}
		i -= size
	}
	return i
}

// NextWord returns the end of the word after b.
func NextWord(s string, b int) int {
	if b >= len(s) {
		return len(s)
	}
	if n, ok := icuNextWord(s, b); ok {
		return n
	}
	i := b
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !isSpace(r) {
			break
		}
		i += size
	}
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if isSpace(r) {
			break
		}
		i += size
	}
	return i
}

func isSpace(r rune) bool { return strings.ContainsRune(" \t\n\r.,;:/\\()[]{}\"'", r) }

// LineMetrics describes one line of a layout. Lengths count UTF-16 code units.
type LineMetrics struct {
	Length                   uint32
	TrailingWhitespaceLength uint32
	NewlineLength            uint32
	Height                   float32
	Baseline                 float32
	IsTrimmed                int32
}
