//go:build windows

package text

import (
	"testing"

	"github.com/myparsleycat/fairy/internal/dwrite"
	"github.com/myparsleycat/fairy/internal/render"
)

// Chromium rounds the ascent and descent and floors the half-leading above
// the text. IBM Plex Sans KR at 14px in a 20px line sits on pixel 14, as the
// same text in a Chromium page does.
func TestCSSBaseline(t *testing.T) {
	plex := dwrite.FontMetrics{DesignUnitsPerEm: 1000, Ascent: 1085, Descent: 415}
	segoe := dwrite.FontMetrics{DesignUnitsPerEm: 2048, Ascent: 2210, Descent: 514}
	for _, c := range []struct {
		name             string
		m                dwrite.FontMetrics
		size, lineHeight float32
		want             float32
	}{
		{"plex 14/20", plex, 14, 20, 14},
		{"plex 12/16", plex, 12, 16, 12},
		{"plex 16/24", plex, 16, 24, 17},
		{"segoe 14/20", segoe, 14, 20, 15},
	} {
		if got := cssBaseline(c.m, c.size, c.lineHeight); got != c.want {
			t.Errorf("%s: baseline %v, want %v", c.name, got, c.want)
		}
	}
}

// A line height places the baseline from the font's own metrics, which the
// installed Segoe UI provides on every Windows machine.
func TestLineHeightBaseline(t *testing.T) {
	s, err := NewSystem(&render.Atlas{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Release()

	l := s.Layout("Ag", Style{Family: "Segoe UI", Size: 14, LineHeight: 20}, Unbounded)
	lines := l.LineMetrics()
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
	if lines[0].Height != 20 || lines[0].Baseline != 15 {
		t.Errorf("line height %v baseline %v, want 20 and 15", lines[0].Height, lines[0].Baseline)
	}
}
