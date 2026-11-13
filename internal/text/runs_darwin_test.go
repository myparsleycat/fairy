package text

import (
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/myparsleycat/fairy/internal/darwin"
	"github.com/myparsleycat/fairy/internal/render"
)

// segmentsByScan is the plain form of Layout.segments: each piece takes every
// run that covers it, in order.
func segmentsByScan(l *Layout, k formatKey, runs []Run) []runStyle {
	edges := []uint32{0, uint32(len(l.u16))}
	for _, r := range runs {
		if a, b := l.utf8ToUTF16(r.Start), l.utf8ToUTF16(r.End); r.End > r.Start && a < b {
			edges = append(edges, a, b)
		}
	}
	slices.Sort(edges)
	edges = slices.Compact(edges)
	out := []runStyle{}
	for i := 0; i+1 < len(edges); i++ {
		st := runStyle{family: k.family, weight: k.weight, italic: k.italic, size: k.size}
		for _, r := range runs {
			a, b := l.utf8ToUTF16(r.Start), l.utf8ToUTF16(r.End)
			if r.End <= r.Start || a >= b || a > edges[i] || b < edges[i+1] {
				continue
			}
			if r.Weight > 0 {
				st.weight = r.Weight
			}
			st.italic = st.italic || r.Italic
			if r.Size > 0 {
				st.size = r.Size
			}
			if r.Family != "" {
				st.family = r.Family
			}
			st.underline = st.underline || r.Underline
			st.strike = st.strike || r.Strike
			if r.Color > 0 {
				st.color = r.Color
			}
		}
		out = append(out, st)
	}
	return out
}

func randomRuns(rng *rand.Rand, n, length int) []Run {
	runs := make([]Run, n)
	for i := range runs {
		a := rng.Intn(length+4) - 2
		runs[i] = Run{Start: a, End: a + rng.Intn(length/4+1) - 1,
			Weight: []int{0, 300, 700}[rng.Intn(3)], Italic: rng.Intn(3) == 0,
			Size: []float32{0, 12, 18}[rng.Intn(3)], Family: []string{"", "Menlo", "Helvetica"}[rng.Intn(3)],
			Color: rng.Intn(3), Underline: rng.Intn(3) == 0, Strike: rng.Intn(4) == 0}
	}
	return runs
}

// Overlapping runs apply in order, as DirectWrite's range setters do.
func TestSegmentsApplyRunsInOrder(t *testing.T) {
	s := newTestSystem(t)
	rng := rand.New(rand.NewSource(1))
	str := strings.Repeat("한글 😀 e\u0301 words \xff\n", 4)
	k := keyOf(Style{Size: 14})
	for range 300 {
		runs := randomRuns(rng, rng.Intn(12), len(str))
		l := &Layout{sys: s, text: str, u16: utf16.Encode([]rune(str))}
		edges, got := l.segments(k, runs)
		if want := segmentsByScan(l, k, runs); !reflect.DeepEqual(got, want) || len(edges) != len(got)+1 {
			t.Fatalf("runs %+v:\n got %+v\nwant %+v", runs, got, want)
		}
	}
}

// Underlines and strikethroughs, found in one pass over each run's glyphs,
// match those of scanning the glyphs for each decorated span.
func TestDecorationsMatchSpanScan(t *testing.T) {
	s := newTestSystem(t)
	rng := rand.New(rand.NewSource(2))
	str := strings.Repeat("Hello 한글 world 😀 שלום abc ", 6)
	for range 20 {
		l := s.Layout(str, Style{Size: 14, Runs: randomRuns(rng, 30, len(str))}, 300)
		for _, ln := range l.lines {
			by := float32(math.Round(float64(ln.top + ln.baseline)))
			forRuns(ln.draw, func(_, font uintptr, _ []uint16, pos []darwin.CGPoint, adv []darwin.CGSize, idx []int) {
				l.quads = l.quads[:0]
				l.decorate(&ln, font, by, pos, adv, idx)
				var want []glyphQuad
				m := fontDecoMetrics(font)
				for _, d := range l.deco {
					x0, x1 := math.Inf(1), math.Inf(-1)
					for g := range pos {
						if i := uint32(idx[g]); i >= d.start && i < d.end {
							x0, x1 = min(x0, pos[g].X), max(x1, pos[g].X+adv[g].W)
						}
					}
					if x0 >= x1 {
						continue
					}
					q := glyphQuad{x: int32(math.Round(float64(ln.x) + x0)), w: uint16(math.Round(x1 - x0)), kind: render.KindRect, effect: uint32(d.color)}
					if d.underline {
						q.y, q.h = int32(math.Round(float64(by+m.underline))), uint16(max(math.Round(float64(m.underlineThick)), 1))
						want = append(want, q)
					}
					if d.strike {
						q.y, q.h = int32(math.Round(float64(by+m.strike))), uint16(max(math.Round(float64(m.strikeThick)), 1))
						want = append(want, q)
					}
				}
				if got := fmt.Sprint(l.quads); got != fmt.Sprint(want) {
					t.Errorf("decorations %v, want %v", got, want)
				}
			})
		}
		l.quads = nil
	}
}

// A whole line selects as one rectangle over its glyphs, which RangeRects
// takes from the line's width without walking them.
func TestWholeLineRectsMatchGlyphs(t *testing.T) {
	s := newTestSystem(t)
	for _, c := range []struct {
		str string
		st  Style
	}{
		{"Hello world, a few words to wrap here\nnext\n\nend ", Style{Size: 14}},
		{"abc שלום עולם def مرحبا 123\tx", Style{Size: 14}},
		{"한글 😀 é tracked text that wraps", Style{Size: 14, LetterSpacing: 1.5}},
		{"   leading spaces and trailing   ", Style{Size: 14, Align: AlignCenter}},
		{"office ffi fl Ta To AV", Style{Size: 20, Family: "Times New Roman"}},
	} {
		l := s.Layout(c.str, c.st, 120)
		for i, ln := range l.lines {
			if ln.ct == 0 {
				continue
			}
			got := l.lineRects(nil, &ln, ln.start, ln.start+ln.length)
			x0, x1 := math.Inf(1), math.Inf(-1)
			forRuns(ln.ct, func(_, _ uintptr, _ []uint16, pos []darwin.CGPoint, adv []darwin.CGSize, _ []int) {
				for g := range pos {
					x0, x1 = min(x0, pos[g].X), max(x1, pos[g].X+adv[g].W)
				}
			})
			if x0 > x1 {
				x0, x1 = 0, 0
			}
			want := Rect{ln.x + float32(x0), ln.top, float32(x1 - x0), ln.height}
			if len(got) != 1 || math.Abs(float64(got[0].X-want.X)) > 0.01 || math.Abs(float64(got[0].W-want.W)) > 0.01 || got[0].Y != want.Y || got[0].H != want.H {
				t.Errorf("%q line %d: rects %v, glyphs span %v", c.str, i, got, want)
			}
		}
	}
}

// BenchmarkManyRuns lays out and records text with a run per word, as syntax
// coloring makes.
func BenchmarkManyRuns(b *testing.B) {
	s := newTestSystem(b)
	words := strings.Repeat("word ", 6400)
	runs := make([]Run, 0, 6400)
	for i := range 6400 {
		runs = append(runs, Run{Start: i * 5, End: i*5 + 4, Color: 1 + i%3, Underline: i%7 == 0})
	}
	for b.Loop() {
		clear(s.layouts)
		clear(s.shapes)
		l := s.Layout(words, Style{Size: 14, Runs: runs}, 800)
		l.record()
		l.release()
	}
}

// BenchmarkRangeRects measures the selection of a long paragraph, which is
// painted on every frame.
func BenchmarkRangeRects(b *testing.B) {
	s := newTestSystem(b)
	str := strings.Repeat("Hello 한글 world 😀 abc ", 200)
	l := s.Layout(str, Style{Size: 14}, 600)
	b.ReportAllocs()
	for b.Loop() {
		l.RangeRects(3, len(str)-3)
	}
}
