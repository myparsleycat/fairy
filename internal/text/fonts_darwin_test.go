package text

import (
	"math"
	"strings"
	"testing"

	"github.com/myparsleycat/fairy/internal/darwin"
)

// Text on a 2x screen keeps the optical size and tracking of its point size,
// so it is as wide in points as on a 1x screen and as AppKit draws it. SF at
// 26px would otherwise take the shapes of 26pt, about 9% narrower.
func TestRetinaKeepsPointSizeOpticalSize(t *testing.T) {
	if kCTFontOpticalSizeAttribute == 0 {
		t.Skip("CoreText has no optical size attribute")
	}
	s := newTestSystem(t)
	const pangram = "The quick brown fox jumps over the lazy dog"
	for _, st := range []Style{
		{Size: 13},
		{Size: 13, Weight: 700},
		{Size: 13, Italic: true},
		{Size: 11, Family: "Consolas"},
		{Size: 13, Family: "Helvetica"},
		{Size: 13, Runs: []Run{{Start: 4, End: 9, Weight: 600, Size: 17}}},
	} {
		w1, h1 := s.Measure(pangram, st, Unbounded)
		hi := st
		hi.Size, hi.Scale = st.Size*2, 2
		if len(st.Runs) > 0 {
			hi.Runs = []Run{st.Runs[0]}
			hi.Runs[0].Size *= 2
		}
		w2, h2 := s.Measure(pangram, hi, Unbounded)
		if math.Abs(float64(w2/2-w1)) > 0.002*float64(w1) || math.Abs(float64(h2/2-h1)) > 1 {
			t.Errorf("%+v: %vx%v at 1x, %vx%v points at 2x", st, w1, h1, w2/2, h2/2)
		}
	}
}

// Tabs stop every four font sizes, as DirectWrite's default incremental tab
// stop does, however many there are; CoreText's own stops end at 336 pixels.
func TestTabStops(t *testing.T) {
	s := newTestSystem(t)
	for _, n := range []int{1, 3, 10} {
		str := strings.Repeat("\t", n) + "x"
		l := s.Layout(str, Style{Size: 14}, Unbounded)
		if x, _, _ := l.Caret(n); math.Abs(float64(x)-float64(n*56)) > 0.5 {
			t.Errorf("%d tabs end at %v, want %v", n, x, n*56)
		}
	}
	l := s.Layout("ab\tx", Style{Size: 20}, Unbounded)
	if x, _, _ := l.Caret(3); math.Abs(float64(x)-80) > 0.5 {
		t.Errorf("a tab after text ends at %v, want 80", x)
	}
}

// Paragraphs read left to right, as DirectWrite's reading direction does, so
// right-to-left text starts at the left edge and no caret is left of it.
func TestRightToLeftCaretsInLeftToRightParagraph(t *testing.T) {
	s := newTestSystem(t)
	for _, str := range []string{"שלום ", "שלום עולם", "abc שלום", "مرحبا "} {
		l := s.Layout(str, Style{Size: 14}, Unbounded)
		w, _ := l.Size()
		for _, b := range Clusters(str) {
			if x, _, _ := l.Caret(b); x < 0 || x > w+0.5 {
				t.Errorf("%q: caret at byte %d is at %v, outside [0, %v]", str, b, x, w)
			}
		}
		if rects := l.RangeRects(0, len(str)); len(rects) == 0 || rects[0].X < 0 {
			t.Errorf("%q: selection %v", str, rects)
		}
	}
}

// Bold text that falls back to a SetFallbackFamilies family takes its bold face.
func TestFallbackFamiliesKeepWeight(t *testing.T) {
	s := newTestSystem(t)
	const family = "Apple SD Gothic Neo"
	if len(s.familyFaces(family)) < 2 {
		t.Skip(family + " is not installed")
	}
	SetFallbackFamilies([]string{family})
	defer SetFallbackFamilies(nil)
	for _, weight := range []int{400, 700} {
		l := s.Layout("Hello 한국어", Style{Size: 14, Weight: weight}, Unbounded)
		found := false
		for _, ln := range l.lines {
			forRuns(ln.draw, func(_, font uintptr, _ []uint16, _ []darwin.CGPoint, _ []darwin.CGSize, _ []int) {
				if familyOf(font) != family {
					return
				}
				found = true
				desc := ctFontCopyFontDescriptor(font)
				defer cfRelease(desc)
				if got := describeFace(desc).weight; got != weight {
					t.Errorf("weight %d falls back to %s %d", weight, family, got)
				}
			})
		}
		if !found {
			t.Errorf("weight %d: the Hangul did not fall back to %s", weight, family)
		}
	}
}
