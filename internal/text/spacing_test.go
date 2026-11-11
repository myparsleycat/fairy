//go:build windows

package text

import (
	"testing"

	"github.com/myparsleycat/fairy/internal/render"
)

// LetterSpacing goes through IDWriteTextLayout1 by vtable slot; a wrong slot
// would set another property or crash instead of widening the text.
func TestLetterSpacingWidensEachCharacter(t *testing.T) {
	s, err := NewSystem(&render.Atlas{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Release()

	plain, _ := s.Measure("ABCD", Style{Size: 14}, Unbounded)
	spaced, _ := s.Measure("ABCD", Style{Size: 14, LetterSpacing: 2}, Unbounded)
	if diff := spaced - plain; diff < 7.9 || diff > 8.1 {
		t.Fatalf("spaced width %v, plain %v: grew by %v, want 8", spaced, plain, diff)
	}
}
