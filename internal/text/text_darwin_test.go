package text

import (
	"math/rand"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/myparsleycat/fairy/internal/render"
)

// Chromium rounds the ascent and descent and floors the half-leading above
// the text. IBM Plex Sans KR at 14px in a 20px line sits on pixel 14, as the
// same text in a Chromium page does.
func TestCSSBaseline(t *testing.T) {
	for _, c := range []struct {
		name                  string
		upem, ascent, descent float64
		size, lineHeight      float64
		want                  float32
	}{
		{"plex 14/20", 1000, 1085, 415, 14, 20, 14},
		{"plex 12/16", 1000, 1085, 415, 12, 16, 12},
		{"plex 16/24", 1000, 1085, 415, 16, 24, 17},
		{"segoe 14/20", 2048, 2210, 514, 14, 20, 15},
	} {
		a, d := c.ascent*c.size/c.upem, c.descent*c.size/c.upem
		if got := cssBaseline(a, d, c.lineHeight); got != c.want {
			t.Errorf("%s: baseline %v, want %v", c.name, got, c.want)
		}
	}
}

// A line height places the baseline from the font's own metrics: SF Pro at
// 14px has an ascent of 13.5 and a descent of 3.0 pixels.
func TestLineHeightBaseline(t *testing.T) {
	s := newTestSystem(t)
	for _, fam := range []string{"", ".AppleSystemUIFont", "Segoe UI"} {
		l := s.Layout("Ag\nAg", Style{Family: fam, Size: 14, LineHeight: 20}, Unbounded)
		lines := l.LineMetrics()
		if len(lines) != 2 {
			t.Fatalf("%q: lines = %d, want 2", fam, len(lines))
		}
		for _, m := range lines {
			if m.Height != 20 || m.Baseline != 15 {
				t.Errorf("%q: line height %v baseline %v, want 20 and 15", fam, m.Height, m.Baseline)
			}
		}
		if _, h := l.Size(); h != 40 {
			t.Errorf("%q: height %v, want 40", fam, h)
		}
		if _, y, h := l.Caret(len("Ag\n")); y != 20 || h != 20 {
			t.Errorf("%q: caret on line 2 at y %v height %v, want 20 and 20", fam, y, h)
		}
	}
}

// Truncated text draws the ellipsis in place of what does not fit.
func TestTruncatedTextDrawsEllipsis(t *testing.T) {
	s := newTestSystem(t)
	st := Style{Size: 14, Weight: 400}
	sign := glyphsOf(s.Layout("…", st, Unbounded))
	if len(sign) != 1 {
		t.Fatalf("ellipsis glyphs = %v, want one", sign)
	}
	full := s.Layout("Select channel", st, Unbounded)
	st.NoWrap, st.Ellipsis = true, true
	l := s.Layout("Select channel", st, 60)
	got := glyphsOf(l)
	if !slices.Contains(got, sign[0]) {
		t.Errorf("truncated glyphs %v do not include the ellipsis glyph %d", got, sign[0])
	}
	if len(got) >= len(glyphsOf(full)) {
		t.Errorf("truncated text draws %d glyphs, the full text %d", len(got), len(glyphsOf(full)))
	}
	if w, _ := l.Size(); w > 60 {
		t.Errorf("truncated width %v, want at most 60", w)
	}
	if m := l.LineMetrics(); len(m) != 1 || m[0].IsTrimmed == 0 || m[0].Length != uint32(len("Select channel")) {
		t.Errorf("trimmed line metrics %+v, want one trimmed line of the whole text", m)
	}
	// Text that fits is left alone.
	if got := glyphsOf(s.Layout("Select", st, 60)); slices.Contains(got, sign[0]) {
		t.Errorf("text that fits drew an ellipsis: %v", got)
	}
}

func TestLetterSpacingWidensEachCharacter(t *testing.T) {
	s := newTestSystem(t)
	plain, _ := s.Measure("ABCD", Style{Size: 14}, Unbounded)
	spaced, _ := s.Measure("ABCD", Style{Size: 14, LetterSpacing: 2}, Unbounded)
	if diff := spaced - plain; diff < 7.9 || diff > 8.1 {
		t.Fatalf("spaced width %v, plain %v: grew by %v, want 8", spaced, plain, diff)
	}
}

// A layout reused at another width must break and place every glyph exactly
// as a layout made at that width would.
func TestReusedLayoutMatchesFreshLayout(t *testing.T) {
	s := newTestSystem(t)
	ref := newTestSystem(t)

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

// Systems on different goroutines, as RenderImage uses off the UI thread,
// must each draw only their own layout's glyphs.
func TestConcurrentDrawLayout(t *testing.T) {
	texts := []string{"Hello world", "ABCDEFGHIJKLMNOP", "0123456789", "Zz Yy Xx Ww 한글"}
	var wg sync.WaitGroup
	for _, str := range texts {
		s := newTestSystem(t)
		l := s.Layout(str, Style{Size: 14}, Unbounded)
		want := glyphsOf(l)
		wg.Go(func() {
			for range 50 {
				fresh := s.Layout(str, Style{Size: 14, Weight: 700}, Unbounded)
				var scene render.Scene
				if !fresh.Draw(&scene, 0, 0, [4]float32{0, 0, 0, 1}, render.NoClip) {
					t.Errorf("%q: atlas full", str)
					return
				}
				if got := glyphsOf(l); !slices.Equal(got, want) {
					t.Errorf("%q drew glyphs %v, want %v", str, got, want)
					return
				}
				for k, l := range s.layouts {
					if k.style.weight == 700 {
						s.drop(k, l)
					}
				}
			}
		})
	}
	wg.Wait()
}

func TestFontCacheBounded(t *testing.T) {
	s := newTestSystem(t)
	st := Style{Size: 14, NoWrap: true, Ellipsis: true}
	l := s.Layout("a long paragraph 한글 😀 that must be trimmed", st, 60)
	want := capture(l)
	for i := range 384 {
		s.Layout("x", Style{Size: 20 + float32(i)/16, Ellipsis: true}, Unbounded)
	}
	if len(s.fonts) > fontCacheLimit {
		t.Errorf("cached fonts = %d, want at most %d", len(s.fonts), fontCacheLimit)
	}
	if got := capture(l); !reflect.DeepEqual(got, want) {
		t.Errorf("layout changed after font eviction:\n got %+v\nwant %+v", got, want)
	}
}

func scanUTF8ToUTF16(str string, b int) uint32 {
	var n uint32
	for i, r := range str {
		if i >= b {
			break
		}
		n++
		if r >= 0x10000 {
			n++
		}
	}
	return n
}

func scanUTF16ToUTF8(str string, u uint32) int {
	var n uint32
	for i, r := range str {
		if n >= u {
			return i
		}
		n++
		if r >= 0x10000 {
			n++
		}
	}
	return len(str)
}

func TestOffsetConversionBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		bytes      []uint32
		units      []int
	}{
		{"empty", "", []uint32{0}, []int{0}},
		{"ASCII and NUL", "a\x00b", []uint32{0, 1, 2, 3}, []int{0, 1, 2, 3}},
		{"multibyte and surrogate", "A한😀B", []uint32{0, 1, 2, 2, 2, 4, 4, 4, 4, 5}, []int{0, 1, 4, 8, 8, 9}},
		{"invalid UTF8", "\xe2\x82X\xff", []uint32{0, 1, 2, 3, 4}, []int{0, 1, 2, 3, 4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := &Layout{text: tc.text}
			for b, want := range tc.bytes {
				if got := l.utf8ToUTF16(b); got != want {
					t.Errorf("byte %d: got %d units, want %d", b, got, want)
				}
			}
			for u, want := range tc.units {
				if got := l.utf16ToUTF8(uint32(u)); got != want {
					t.Errorf("unit %d: got byte %d, want %d", u, got, want)
				}
			}
		})
	}
}

func TestOffsetConversionArbitraryOrder(t *testing.T) {
	for _, tc := range []struct{ name, text string }{
		{"ASCII", strings.Repeat("abcdef", 200)},
		{"Unicode", strings.Repeat("한😀e\u0301\x00世界", 80)},
		{"checkpoint inside emoji", strings.Repeat("a", 254) + "😀" + strings.Repeat("한😀", 100)},
		{"invalid UTF8", strings.Repeat("a\xed\xa0\x80\xf0\x9f\x92\xff한😀", 80)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := &Layout{text: tc.text}
			rng := rand.New(rand.NewSource(1))
			for _, b := range rng.Perm(len(tc.text) + 5) {
				b -= 2
				if got, want := l.utf8ToUTF16(b), scanUTF8ToUTF16(tc.text, b); got != want {
					t.Fatalf("byte %d: got %d units, want %d", b, got, want)
				}
			}
			for _, u := range rng.Perm(int(scanUTF8ToUTF16(tc.text, len(tc.text))) + 3) {
				if got, want := l.utf16ToUTF8(uint32(u)), scanUTF16ToUTF8(tc.text, uint32(u)); got != want {
					t.Fatalf("unit %d: got byte %d, want %d", u, got, want)
				}
			}
			if got := l.utf16ToUTF8(^uint32(0)); got != len(tc.text) {
				t.Errorf("out-of-range unit: got byte %d, want %d", got, len(tc.text))
			}
		})
	}
}

// Rich text with every kind of run, invalid UTF-8 and newlines keeps carets,
// line ranges, hit tests and selections consistent with its line metrics.
func TestRichTextUnicodeLayout(t *testing.T) {
	s := newTestSystem(t)
	str := strings.Repeat("한글 😀 e\u0301 words \xff\xe2\x82\n", 16)
	st := Style{Size: 14, Weight: 400, LineHeight: 22, Runs: []Run{
		{Start: 257, End: 411, Weight: 700, Underline: true},
		{Start: 1, End: 277, Italic: true, Color: 1},
		{Start: -2, End: 12, Size: 18, Strike: true},
		{Start: 400, End: len(str) + 5, Family: "Segoe UI", Weight: 500},
		{Start: 10, End: 9, Size: 30},
	}}
	l := s.Layout(str, st, 170)
	ms := l.LineMetrics()
	var total uint32
	for i, m := range ms {
		total += m.Length
		if m.Height != 22 {
			t.Errorf("line %d height %v, want 22", i, m.Height)
		}
		if m.TrailingWhitespaceLength < m.NewlineLength || m.Length < m.TrailingWhitespaceLength {
			t.Errorf("line %d metrics inconsistent: %+v", i, m)
		}
	}
	if want := scanUTF8ToUTF16(str, len(str)); total != want {
		t.Fatalf("line lengths add up to %d units, want %d", total, want)
	}
	if last := ms[len(ms)-1]; last.Length != 0 {
		t.Errorf("text ending in a newline: last line %+v, want empty", last)
	}
	if _, h := l.Size(); h != float32(22*len(ms)) {
		t.Errorf("height %v, want %v", h, 22*len(ms))
	}

	// Line starts by byte, from the metrics.
	var starts []int
	var u uint32
	for _, m := range ms {
		starts = append(starts, scanUTF16ToUTF8(str, u))
		u += m.Length
	}
	for _, b := range rand.New(rand.NewSource(2)).Perm(len(str) + 3) {
		b--
		cb := min(max(b, 0), len(str))
		li := len(starts) - 1
		for i := range starts {
			if i+1 < len(starts) && cb < starts[i+1] {
				li = i
				break
			}
		}
		gs, ge, _ := l.LineRange(b)
		if gs != starts[li] || ge < gs {
			t.Fatalf("line at byte %d: got %d..%d, want start %d", b, gs, ge, starts[li])
		}
		x, y, h := l.Caret(b)
		if h != 22 || y != float32(22*li) {
			t.Fatalf("caret at byte %d: y %v height %v, want line %d", b, y, h, li)
		}
		if x < 0 || x > 170 {
			t.Fatalf("caret at byte %d: x %v outside the layout", b, x)
		}
		end := min(b+19, len(str)+2)
		for _, r := range l.RangeRects(b, end) {
			if r.W < 0 || r.X < 0 || r.X+r.W > 171 || int(r.Y)%22 != 0 || r.H != 22 {
				t.Fatalf("selection %d..%d has rect %+v", b, end, r)
			}
		}
	}
	// Hit tests land on caret positions of the line hit.
	for y := float32(-10); y <= float32(22*len(ms))+10; y += 17 {
		for x := float32(-10); x <= 190; x += 13 {
			b := l.HitTest(x, y)
			li := min(max(int(y/22), 0), len(ms)-1)
			if y < 0 {
				li = 0
			}
			gs, ge, _ := l.LineRange(b)
			if gs != starts[li] || b < gs || b > ge {
				t.Fatalf("hit at %v,%v: byte %d outside line %d (%d..%d)", x, y, b, li, gs, ge)
			}
			if b != scanUTF16ToUTF8(str, scanUTF8ToUTF16(str, b)) {
				t.Fatalf("hit at %v,%v: byte %d is inside a character", x, y, b)
			}
		}
	}
	var scene render.Scene
	if !l.DrawRuns(&scene, 0, 0, [4]float32{0, 0, 0, 1}, [][4]float32{{1, 0, 0, 1}}, render.NoClip) {
		t.Fatal("atlas full")
	}
	var rects, red int
	for _, p := range scene.Prims {
		if p.Kind == render.KindRect {
			rects++
		}
		if p.Color == [4]float32{1, 0, 0, 1} {
			red++
		}
	}
	if rects == 0 || red == 0 {
		t.Errorf("drew %d decoration rects and %d prims in run color 1, want some of each", rects, red)
	}
}
