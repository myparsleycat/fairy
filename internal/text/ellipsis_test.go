//go:build windows

package text

import (
	"slices"
	"testing"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/dwrite"
	"github.com/myparsleycat/fairy/internal/render"
)

func glyphsOf(l *Layout) []uint16 {
	var glyphs []uint16
	dwrite.DrawLayout(l.dl, 0, 0, func(_, _ float32, run *dwrite.GlyphRun, _ *dwrite.GlyphRunDescription, _ unsafe.Pointer) {
		glyphs = append(glyphs, unsafe.Slice(run.GlyphIndices, int(run.GlyphCount))...)
	}, nil)
	return glyphs
}

// DirectWrite draws the trimming sign as an inline object, so a renderer that
// skips inline objects cuts truncated text without its ellipsis.
func TestTruncatedTextDrawsEllipsis(t *testing.T) {
	s, err := NewSystem(&render.Atlas{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Release()

	st := Style{Family: "Segoe UI", Size: 14, Weight: 400}
	sign := glyphsOf(s.Layout("…", st, Unbounded))
	if len(sign) != 1 {
		t.Fatalf("ellipsis glyphs = %v, want one", sign)
	}

	st.NoWrap, st.Ellipsis = true, true
	if got := glyphsOf(s.Layout("Select channel", st, 60)); !slices.Contains(got, sign[0]) {
		t.Errorf("truncated glyphs %v do not include the ellipsis glyph %d", got, sign[0])
	}
}
