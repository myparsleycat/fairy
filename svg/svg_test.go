package svg

import (
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"
)

func TestLazySize(t *testing.T) {
	for _, tc := range []struct {
		src     string
		w, h    float32
		invalid bool
	}{
		{`<svg viewBox="4 8 48 16"><path d="M4 8L52 24"/></svg>`, 48, 16, false},
		{`<svg><circle cx="12" cy="12" r="6"/></svg>`, 24, 24, false},
		{`<svg><path d="R1 2"/></svg>`, 0, 0, true},
	} {
		v := Lazy(tc.src)
		w, h, err := v.Size()
		if tc.invalid {
			if err == nil || v.Mask(24, 24) != nil {
				t.Fatal("invalid lazy vector did not report its parse error")
			}
			continue
		}
		if err != nil || w != tc.w || h != tc.h {
			t.Fatalf("size = %v, %v, %v; want %v, %v", w, h, err, tc.w, tc.h)
		}
	}
}

func TestVectorMaskStrokeCoverage(t *testing.T) {
	cases := []struct {
		name, body string
		area       float64
	}{
		{"line", `<path d="M5 7L19 17"/>`, 2*math.Hypot(14, 10) + math.Pi},
		{"subdivided line", `<path d="M5 7L8.5 9.5L12 12L15.5 14.5L19 17"/>`, 2*math.Hypot(14, 10) + math.Pi},
		{"duplicate points", `<path d="M5 7L5 7L12 12L12 12L19 17L19 17"/>`, 2*math.Hypot(14, 10) + math.Pi},
		{"butt caps", `<g stroke-linecap="butt"><path d="M5 7L19 17"/></g>`, 2 * math.Hypot(14, 10)},
		{"round dot", `<path d="M12 12h0"/>`, math.Pi},
		{"round corner", `<path d="M6 6H18V18"/>`, 47 + 5*math.Pi/4},
		{"circle", `<circle cx="12" cy="12" r="6"/>`, 24 * math.Pi},
		{"closed square", `<path d="M6 6H18V18H6Z"/>`, 92 + math.Pi},
		{"explicitly closed square", `<path d="M6 6H18V18H6V6Z"/>`, 92 + math.Pi},
		{"reversed square", `<path d="M6 6V18H18V6Z"/>`, 92 + math.Pi},
	}
	for _, tc := range cases {
		for _, size := range []int{24, 30, 48} {
			t.Run(fmt.Sprintf("%s/%d", tc.name, size), func(t *testing.T) {
				v := MustParse(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` + tc.body + `</svg>`)
				mask := v.Mask(size, size)
				area := 0.0
				for _, a := range mask.Pix {
					area += float64(a) / 255
				}
				want := tc.area * math.Pow(float64(size)/24, 2)
				if math.Abs(area-want) > want*0.02 {
					t.Errorf("stroke coverage = %.2f pixels, want %.2f (within 2%%)", area, want)
				}
			})
		}
	}
}

func TestParsePathStopsAtError(t *testing.T) {
	line := [][]vpt{{{X: 0, Y: 0}, {X: 10, Y: 10}}}
	cases := []struct {
		d    string
		want [][]vpt
	}{
		{"M0 0L10 10;", line},
		{"M0 0L10 10Z 5 5", line},
		{"M0 0L10 10-", line},
		{"M0 0L10 10 .", line},
		{"M0 0L10 10\u22125 5", line},
		{"M0 0L10 10L5", line},
		{"M0 0\fL10 10", line},
	}
	for _, tc := range cases {
		done := make(chan error, 1)
		var sh vshape
		go func() { done <- sh.parsePath(tc.d) }()
		select {
		case err := <-done:
			if err != nil || !reflect.DeepEqual(sh.subpaths, tc.want) {
				t.Errorf("%q parsed to %v, %v; want %v", tc.d, sh.subpaths, err, tc.want)
			}
		case <-time.After(5 * time.Second):
			// A looping parse keeps allocating, so stop the whole binary.
			panic(fmt.Sprintf("parsePath(%q) did not return", tc.d))
		}
	}
}

func TestParseSVGSkipsDefinitions(t *testing.T) {
	v := MustParse(`<svg viewBox="0 0 24 24"><g clip-path="url(#a)"><path d="M6 6H18V18H6Z"/></g>` +
		`<defs><clipPath id="a"><rect width="24" height="24" fill="white"/></clipPath></defs></svg>`)
	if len(v.shapes) != 1 {
		t.Fatalf("%d shapes, want only the path outside <defs>", len(v.shapes))
	}
}
