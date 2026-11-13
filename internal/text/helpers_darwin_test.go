package text

import (
	"fmt"
	"testing"

	"github.com/myparsleycat/fairy/internal/darwin"
	"github.com/myparsleycat/fairy/internal/render"
)

// fakeAtlas packs tiles in rows like render's shelf packer and records the
// masks inserted, so glyphs can be drawn without a GPU.
type fakeAtlas struct {
	size, gen  int
	x, y, rowH int
	inserts    int
	masks      map[render.Tile][]byte
}

func newFakeAtlas(size int) *fakeAtlas {
	return &fakeAtlas{size: size, gen: 1, masks: map[render.Tile][]byte{}}
}

func (a *fakeAtlas) Insert(w, h int, data []byte) (render.Tile, bool) {
	if a.x+w+1 > a.size {
		a.x, a.y, a.rowH = 0, a.y+a.rowH, 0
	}
	if a.y+h+1 > a.size || w+1 > a.size {
		return render.Tile{}, false
	}
	t := render.Tile{X: a.x, Y: a.y, W: w, H: h}
	a.x += w + 1
	a.rowH = max(a.rowH, h+1)
	a.inserts++
	a.masks[t] = data
	return t, true
}

func (a *fakeAtlas) Size() int { return a.size }

func (a *fakeAtlas) generation() int { return a.gen }

// clear empties the atlas as Grow does.
func (a *fakeAtlas) clear() {
	a.x, a.y, a.rowH = 0, 0, 0
	a.gen++
	clear(a.masks)
}

func newTestSystem(t testing.TB) *System {
	t.Helper()
	s := newSystem(newFakeAtlas(2048))
	t.Cleanup(s.Release)
	return s
}

type laidOut struct {
	W, H  float32
	Lines []LineMetrics
	Runs  []string
}

// capture records everything a layout draws: its size, line metrics and the
// glyphs, positions and decorations of each run.
func capture(l *Layout) laidOut {
	w, h := l.Size()
	out := laidOut{W: w, H: h, Lines: l.LineMetrics()}
	for _, ln := range l.lines {
		if ln.draw == 0 {
			continue
		}
		forRuns(ln.draw, func(_, font uintptr, glyphs []uint16, pos []darwin.CGPoint, adv []darwin.CGSize, idx []int) {
			out.Runs = append(out.Runs, fmt.Sprint(ln.x, ln.top, ln.baseline, familyOf(font), ctFontGetSize(font), glyphs, pos, adv, idx))
		})
	}
	out.Runs = append(out.Runs, fmt.Sprint("deco", l.deco))
	return out
}

// glyphsOf returns the glyphs a layout draws.
func glyphsOf(l *Layout) []uint16 {
	var out []uint16
	for _, ln := range l.lines {
		if ln.draw == 0 {
			continue
		}
		forRuns(ln.draw, func(_, _ uintptr, glyphs []uint16, _ []darwin.CGPoint, _ []darwin.CGSize, _ []int) {
			out = append(out, glyphs...)
		})
	}
	return out
}

// runFamilies returns the family of each glyph run of a layout.
func runFamilies(l *Layout) []string {
	var out []string
	for _, ln := range l.lines {
		if ln.draw == 0 {
			continue
		}
		forRuns(ln.draw, func(_, font uintptr, _ []uint16, _ []darwin.CGPoint, _ []darwin.CGSize, _ []int) {
			out = append(out, familyOf(font))
		})
	}
	return out
}
