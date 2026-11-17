package edit

import (
	"slices"
	"strings"

	"github.com/myparsleycat/fairy/keyboard"
)

// A password shows one dot per grapheme cluster, so the caret, which moves
// by clusters, steps over one dot at a time.

// Mask returns the text a password field shows for a value with the given
// grapheme cluster bounds.
func Mask(bounds []int) string {
	return strings.Repeat("•", len(bounds)-1)
}

// MaskedOffset maps a byte offset in a text with the given cluster bounds to
// the start of its cluster's dot in Mask(bounds).
func MaskedOffset(bounds []int, b int) int {
	i, found := slices.BinarySearch(bounds, b)
	if !found {
		i--
	}
	return i * len("•")
}

// UnmaskedOffset maps a byte offset in the masked text back to the text with
// the given cluster bounds.
func UnmaskedOffset(bounds []int, mb int) int {
	return bounds[min(max(mb/len("•"), 0), len(bounds)-1)]
}

// LineStart returns the start of the line of s holding byte i.
func LineStart(s string, i int) int {
	for i > 0 && s[i-1] != '\n' {
		i--
	}
	return i
}

// LineEnd returns the end, before its line break, of the line of s holding
// byte i.
func LineEnd(s string, i int) int {
	for i < len(s) && s[i] != '\n' {
		i++
	}
	return i
}

// UTF16ToByteOffset maps an offset in UTF-16 code units of s to a byte
// offset. A negative offset maps to the end.
func UTF16ToByteOffset(s string, u int) int {
	if u < 0 {
		return len(s)
	}
	n := 0
	for i, r := range s {
		if n >= u {
			return i
		}
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return len(s)
}

// KeyProcess is VK_PROCESSKEY: the key press an input method is handling.
// Its real key may be Enter confirming a composition.
const KeyProcess keyboard.Key = 0xE5

// HookSees reports whether an Input's OnInputKey handler receives ev.
func HookSees(ev keyboard.KeyEvent, composing bool) bool {
	return !composing && ev.Key != KeyProcess
}

// IsTypingKey reports whether k types a character, which then arrives as
// WM_CHAR.
func IsTypingKey(k keyboard.Key) bool {
	return k == keyboard.KeySpace || (k >= '0' && k <= '9') || (k >= 'A' && k <= 'Z') ||
		(k >= 0x60 && k <= 0x6F) || (k >= 0xBA && k <= 0xE2) || k == KeyProcess
}
