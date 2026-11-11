//go:build windows

package text

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/com"
	"github.com/myparsleycat/fairy/internal/dwrite"
	"github.com/myparsleycat/fairy/internal/render"
)

func TestFormatCacheBounded(t *testing.T) {
	s, err := NewSystem(&render.Atlas{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Release()
	st := Style{Size: 14, NoWrap: true, Ellipsis: true}
	l := s.Layout("a long paragraph 한글 😀 that must be trimmed", st, 60)
	want := capture(l)
	for i := range 384 {
		if _, err := s.format(keyOf(Style{Size: 20 + float32(i)/16, Ellipsis: true})); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.formats) > 128 {
		t.Errorf("cached formats = %d, want at most 128", len(s.formats))
	}
	if got := capture(l); !reflect.DeepEqual(got, want) {
		t.Errorf("layout changed after format eviction:\n got %+v\nwant %+v", got, want)
	}
}

func trimmingSign(t *testing.T, l *Layout) unsafe.Pointer {
	t.Helper()
	var trimming dwrite.Trimming
	var sign unsafe.Pointer
	if err := com.HR(l.dl.P, 17, uintptr(unsafe.Pointer(&trimming)), uintptr(unsafe.Pointer(&sign))).Err("GetTrimming"); err != nil {
		t.Fatal(err)
	}
	if sign == nil {
		t.Fatal("layout has no ellipsis sign")
	}
	return sign
}

func TestFormatResourcesReleased(t *testing.T) {
	for _, action := range []string{"eviction", "font reset", "system release"} {
		t.Run(action, func(t *testing.T) {
			s, err := NewSystem(&render.Atlas{})
			if err != nil {
				t.Fatal(err)
			}
			released := false
			defer func() {
				if !released {
					s.Release()
				}
			}()
			st := Style{Size: 14, NoWrap: true, Ellipsis: true}
			l := s.Layout("a long paragraph that must be trimmed", st, 60)
			f, err := s.format(keyOf(st))
			if err != nil {
				t.Fatal(err)
			}
			com.AddRef(f.P)
			sign := trimmingSign(t, l)
			for k, l := range s.layouts {
				s.drop(k, l)
			}
			switch action {
			case "eviction":
				for i := range 256 {
					if _, err := s.format(keyOf(Style{Size: 20 + float32(i)/16, Ellipsis: true})); err != nil {
						t.Fatal(err)
					}
				}
			case "font reset":
				// Force only this System through the reset path without changing global fonts.
				s.fontVersion--
				s.syncFonts()
				if len(s.formats) != 0 {
					t.Errorf("font reset retained %d formats", len(s.formats))
				}
			case "system release":
				s.Release()
				released = true
			}
			// The test owns the last references after the layout and cache let go.
			if refs := com.Call(sign, 2); refs != 0 {
				t.Errorf("sign references after %s = %d, want 0", action, refs)
			}
			if refs := com.Call(f.P, 2); refs != 0 {
				t.Errorf("format references after %s = %d, want 0", action, refs)
			}
		})
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

func TestRichTextUnicodeLayout(t *testing.T) {
	s, err := NewSystem(&render.Atlas{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Release()
	str := strings.Repeat("한글 😀 e\u0301 words \xff\xe2\x82\n", 16)
	st := Style{Size: 14, Weight: 400, LineHeight: 22, Runs: []Run{
		{Start: 257, End: 411, Weight: 700, Underline: true},
		{Start: 1, End: 277, Italic: true, Color: 1},
		{Start: -2, End: 12, Size: 18, Strike: true},
		{Start: 400, End: len(str) + 5, Family: "Segoe UI", Weight: 500},
		{Start: 10, End: 9, Size: 30},
	}}
	got := s.Layout(str, st, 170)
	f, err := s.format(keyOf(st))
	if err != nil {
		t.Fatal(err)
	}
	u := utf16.Encode([]rune(str))
	dl, err := s.factory.CreateTextLayout(u, dwrite.TextFormat{P: f.P}, 170, Unbounded)
	if err != nil {
		t.Fatal(err)
	}
	defer dl.Release()
	// The reference applies byte ranges using the original full-scan semantics.
	for _, r := range st.Runs {
		if r.End <= r.Start {
			continue
		}
		a, b := scanUTF8ToUTF16(str, r.Start), scanUTF8ToUTF16(str, r.End)
		rg := dwrite.TextRange{Start: a, Length: b - a}
		if r.Weight > 0 {
			dl.SetFontWeight(uint32(r.Weight), rg)
		}
		if r.Italic {
			dl.SetFontStyle(dwrite.FONT_STYLE_ITALIC, rg)
		}
		if r.Size > 0 {
			dl.SetFontSize(r.Size, rg)
		}
		if r.Family != "" {
			dl.SetFontFamilyName(r.Family, rg)
		}
		if r.Underline {
			dl.SetUnderline(true, rg)
		}
		if r.Strike {
			dl.SetStrikethrough(true, rg)
		}
		if r.Color > 0 {
			dl.SetDrawingEffect(dwrite.Effect(r.Color), rg)
		}
	}
	ref := &Layout{dl: dl, metrics: dl.Metrics(), width: 170}
	if g, want := capture(got), capture(ref); !reflect.DeepEqual(g, want) {
		t.Fatalf("rich text layout:\n got %+v\nwant %+v", g, want)
	}
	for _, b := range rand.New(rand.NewSource(2)).Perm(len(str) + 3) {
		b--
		unit := scanUTF8ToUTF16(str, b)
		for _, upstream := range []bool{false, true} {
			pos, trailing := unit, false
			if upstream && unit > 0 && int(unit) < len(u) {
				pos, trailing = unit-1, true
			} else if int(unit) >= len(u) {
				pos, trailing = uint32(len(u)-1), true
			}
			x, y, m := dl.HitTestTextPosition(pos, trailing)
			if gx, gy, gh := got.CaretAt(b, upstream); gx != x || gy != y || gh != m.Height {
				t.Fatalf("caret at byte %d upstream %v: got %v %v %v, want %v %v %v", b, upstream, gx, gy, gh, x, y, m.Height)
			}
		}
		var start uint32
		lines := dl.LineMetrics()
		for i, m := range lines {
			next := start + m.Length
			if unit < next || i == len(lines)-1 {
				end := next - m.NewlineLength
				upstream := false
				if i < len(lines)-1 && m.NewlineLength == 0 {
					end = next - m.TrailingWhitespaceLength
					upstream = m.TrailingWhitespaceLength == 0
				}
				ws, we := scanUTF16ToUTF8(str, start), scanUTF16ToUTF8(str, end)
				if gs, ge, gu := got.LineRange(b); gs != ws || ge != we || gu != upstream {
					t.Fatalf("line at byte %d: got %d %d %v, want %d %d %v", b, gs, ge, gu, ws, we, upstream)
				}
				break
			}
			start = next
		}
		end := min(b+19, len(str)+2)
		var want []Rect
		for _, m := range dl.HitTestTextRange(unit, scanUTF8ToUTF16(str, end)-unit) {
			want = append(want, Rect{m.Left, m.Top, m.Width, m.Height})
		}
		if rects := got.RangeRects(b, end); len(rects) != len(want) || len(rects) > 0 && !reflect.DeepEqual(rects, want) {
			t.Fatalf("selection at byte %d: got %v, want %v", b, rects, want)
		}
	}
	for y := float32(-10); y <= got.metrics.Height+10; y += 17 {
		for x := float32(-10); x <= 190; x += 13 {
			m, trailing, _ := dl.HitTestPoint(x, y)
			pos := m.TextPosition
			if trailing {
				pos += m.Length
			}
			if g, want := got.HitTest(x, y), scanUTF16ToUTF8(str, pos); g != want {
				t.Fatalf("hit at %v,%v: got byte %d, want %d", x, y, g, want)
			}
		}
	}
}
