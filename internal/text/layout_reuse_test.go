//go:build windows

package text

import (
	"fmt"
	"reflect"
	"testing"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/dwrite"
	"github.com/myparsleycat/fairy/internal/render"
)

type laidOut struct {
	W, H  float32
	Lines []LineMetrics
	Runs  []string
}

func capture(l *Layout) laidOut {
	w, h := l.Size()
	out := laidOut{W: w, H: h, Lines: l.LineMetrics()}
	dwrite.DrawLayout(l.dl, 0, 0, func(bx, by float32, run *dwrite.GlyphRun, _ *dwrite.GlyphRunDescription, _ unsafe.Pointer) {
		n := int(run.GlyphCount)
		out.Runs = append(out.Runs, fmt.Sprint(bx, by, unsafe.Slice(run.GlyphIndices, n), unsafe.Slice(run.GlyphAdvances, n)))
	}, func(x, y, width, thickness, offset float32, _ unsafe.Pointer) {
		out.Runs = append(out.Runs, fmt.Sprint("deco", x, y, width))
	})
	return out
}

// A layout reused at another width must break and place every glyph exactly
// as a layout made at that width would.
func TestReusedLayoutMatchesFreshLayout(t *testing.T) {
	s, err := NewSystem(&render.Atlas{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Release()
	ref, err := NewSystem(&render.Atlas{})
	if err != nil {
		t.Fatal(err)
	}
	defer ref.Release()

	texts := []struct {
		text string
		runs []Run
	}{
		{text: "설정"},
		{text: "Save changes   "},
		{text: "문서 편집기는 제목, 목록, 미리보기 이미지를 한 화면에서 다룬다. The editor keeps them in sync with the document settings."},
		{text: "first line\nsecond line that is a little longer"},
		{text: "averyveryverylongwordwithoutanyspacesthatmustbreakinside and then words"},
		{text: "bold and plain words in one wrapped paragraph of text", runs: []Run{{Start: 0, End: 4, Weight: 700}}},
	}
	// Narrowing, widening and unbounded widths, each in the order a resize sees them.
	var widths []float32
	for w := float32(600); w >= 40; w -= 37 {
		widths = append(widths, w)
	}
	for w := float32(40); w <= 640; w += 53 {
		widths = append(widths, w)
	}
	widths = append(widths, Unbounded, 120, Unbounded, 900)

	reused := 0
	for _, tc := range texts {
		st := Style{Family: "Segoe UI", Size: 18, Weight: 400, Runs: tc.runs}
		made := map[*Layout]bool{}
		for _, w := range widths {
			got := s.Layout(tc.text, st, w)
			if made[got] {
				reused++
			}
			made[got] = true

			for k, l := range ref.layouts {
				ref.drop(k, l)
			}
			want := capture(ref.Layout(tc.text, st, w))
			if g := capture(got); !reflect.DeepEqual(g, want) {
				t.Errorf("%q at width %v:\n got %+v\nwant %+v", tc.text, w, g, want)
			}
		}
	}
	if reused == 0 {
		t.Fatal("no layout was reused; the comparison checked nothing")
	}
}
