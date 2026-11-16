//go:build windows

package text

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/myparsleycat/fairy/internal/render"
)

// Systems on different goroutines, as RenderImage uses off the UI thread,
// must each receive only their own layout's glyph runs.
func TestConcurrentDrawLayout(t *testing.T) {
	texts := []string{"Hello world", "ABCDEFGHIJKLMNOP", "0123456789", "Zz Yy Xx Ww"}
	var wg sync.WaitGroup
	for _, str := range texts {
		s, err := NewSystem(&render.Atlas{})
		if err != nil {
			t.Fatal(err)
		}
		defer s.Release()
		l := s.Layout(str, Style{Family: "Segoe UI", Size: 14}, Unbounded)
		want := glyphsOf(l)
		wg.Go(func() {
			for range 200 {
				if got := glyphsOf(l); !slices.Equal(got, want) {
					t.Errorf("%q drew glyphs %v, want %v", str, got, want)
					return
				}
			}
		})
	}
	wg.Wait()
}

// longGlyphText is a paragraph repeated over many lines, with larger,
// underlined and struck runs and emoji, so that runs on one line differ in
// height and decorations add their own quads.
func longGlyphText(lines int) (string, []Run) {
	var b strings.Builder
	var runs []Run
	for i := range lines {
		start := b.Len()
		fmt.Fprintf(&b, "%d The quick brown fox 😀 jumps over the lazy dog.\n", i)
		switch i % 3 {
		case 0:
			runs = append(runs, Run{Start: start + 4, End: start + 9, Size: 30})
		case 1:
			runs = append(runs, Run{Start: start, End: start + 15, Underline: true, Strike: true})
		}
	}
	return b.String(), runs
}

// Drawing only the bands a clip covers must keep, in order, every primitive of
// a full draw that reaches the clip, and leave out most of those that do not.
func TestDrawRunsCullsOnlyQuadsOutsideClip(t *testing.T) {
	r, err := render.NewOffscreen(8, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	s, err := NewSystem(r.Atlas)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Release()
	str, runs := longGlyphText(200)
	l := s.Layout(str, Style{Family: "Segoe UI", Size: 16, Runs: runs}, 300)
	_, h := l.Size()
	color := [4]float32{1, 1, 1, 1}
	draw := func(clip [4]float32, cull bool) []render.Prim {
		var scene render.Scene
		if !cull {
			bands := l.bands
			l.bands = nil
			defer func() { l.bands = bands }()
		}
		if !l.DrawRuns(&scene, 10.4, -20.6, color, nil, clip) {
			t.Fatal("atlas ran out of space")
		}
		return scene.Prims
	}
	draw([4]float32{0, 0, 1, 1}, true)
	if len(l.bands) < 200 {
		t.Fatalf("recorded %d bands for 200 lines", len(l.bands))
	}
	for _, clip := range [][4]float32{
		{0, 0, 400, 300}, {0, h / 2, 400, 37}, {0, h - 50, 400, 100}, {0, -500, 400, 400},
		{0, h + 100, 400, 50}, {0, -100, 400, h + 200}, {200, h / 3, 5, 1},
	} {
		full := draw(clip, false)
		got := draw(clip, true)
		next := 0
		for _, p := range full {
			b := p.Bounds
			reaches := b[1] <= clip[1]+clip[3] && b[1]+b[3] >= clip[1]
			if next < len(got) && got[next] == p {
				next++
			} else if reaches {
				t.Fatalf("clip %v left out primitive %v that reaches it", clip, b)
			}
		}
		if next != len(got) {
			t.Fatalf("clip %v drew primitives that a full draw does not, or out of order", clip)
		}
		if clip[3] < 100 && len(got) > len(l.quads)/10 {
			t.Errorf("clip %v drew %d of %d primitives", clip, len(got), len(l.quads))
		}
	}
}

func BenchmarkDrawRunsLongText(b *testing.B) {
	r, err := render.NewOffscreen(8, 8)
	if err != nil {
		b.Fatal(err)
	}
	defer r.Release()
	s, err := NewSystem(r.Atlas)
	if err != nil {
		b.Fatal(err)
	}
	defer s.Release()
	str, runs := longGlyphText(2000)
	l := s.Layout(str, Style{Family: "Segoe UI", Size: 16, Runs: runs}, 600)
	_, h := l.Size()
	var scene render.Scene
	clip := [4]float32{0, h / 2, 600, 800}
	color := [4]float32{1, 1, 1, 1}
	for b.Loop() {
		scene.Prims = scene.Prims[:0]
		l.DrawRuns(&scene, 0, 0, color, nil, clip)
	}
}
