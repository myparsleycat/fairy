package text

import (
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/myparsleycat/fairy/internal/render"
)

// Carets of Korean and emoji text stop only at grapheme clusters, move right
// at each one, and hit testing returns cluster boundaries.
func TestKoreanAndEmojiCarets(t *testing.T) {
	s := newTestSystem(t)
	str := "가나다 😀👨‍👩‍👧‍👦🇰🇷👋🏽 한글é"
	l := s.Layout(str, Style{Size: 16}, Unbounded)
	if l.Lines() != 1 {
		t.Fatalf("lines = %d, want 1", l.Lines())
	}
	cl := Clusters(str)
	prev := float32(-1)
	for _, b := range cl {
		x, _, _ := l.Caret(b)
		if x <= prev {
			t.Errorf("caret at byte %d: x %v, not right of %v", b, x, prev)
		}
		prev = x
	}
	w, _ := l.Size()
	for x := float32(-5); x < w+5; x += 0.75 {
		if b := l.HitTest(x, 8); !slices.Contains(cl, b) {
			t.Fatalf("hit at x %v: byte %d is not a cluster boundary %v", x, b, cl)
		}
	}
	// Hitting just right of a caret lands on that caret.
	for _, b := range cl[:len(cl)-1] {
		x, _, _ := l.Caret(b)
		if got := l.HitTest(x+0.5, 8); got != b {
			t.Errorf("hit right of caret %d: got %d", b, got)
		}
	}
}

func TestEmptyAndNewlineLines(t *testing.T) {
	s := newTestSystem(t)
	st := Style{Size: 14}
	e := s.Layout("", st, Unbounded)
	if w, h := e.Size(); w != 0 || h < 14 || e.Lines() != 1 {
		t.Errorf("empty text: size %v x %v, %d lines; want zero width, a line of height", w, h, e.Lines())
	}
	if x, y, h := e.Caret(0); x != 0 || y != 0 || h < 14 {
		t.Errorf("empty caret %v %v %v", x, y, h)
	}
	l := s.Layout("ab\r\ncd\nef\n", st, Unbounded)
	ms := l.LineMetrics()
	want := []struct{ length, newline uint32 }{{4, 2}, {3, 1}, {3, 1}, {0, 0}}
	if len(ms) != len(want) {
		t.Fatalf("lines %+v, want %d", ms, len(want))
	}
	for i, w := range want {
		if ms[i].Length != w.length || ms[i].NewlineLength != w.newline || ms[i].TrailingWhitespaceLength != w.newline {
			t.Errorf("line %d: %+v, want length %d newline %d", i, ms[i], w.length, w.newline)
		}
	}
	// The caret after the final newline sits on the empty last line.
	_, y, _ := l.Caret(len("ab\r\ncd\nef\n"))
	if want := ms[0].Height + ms[1].Height + ms[2].Height; y != want {
		t.Errorf("caret after the last newline at y %v, want %v", y, want)
	}
	if s, e, up := l.LineRange(0); s != 0 || e != 2 || up {
		t.Errorf("first line range %d..%d %v, want 0..2", s, e, up)
	}
	// A hit right of a line ends before its newline.
	if b := l.HitTest(500, 1); b != 2 {
		t.Errorf("hit right of the first line: byte %d, want 2", b)
	}
}

func TestWrapping(t *testing.T) {
	s := newTestSystem(t)
	st := Style{Size: 14}
	long := "averyveryverylongwordwithoutanyspacesthatmustbreakinside and then words"
	l := s.Layout(long, st, 60)
	if l.Lines() < 4 {
		t.Fatalf("lines = %d, want the long word broken", l.Lines())
	}
	for i, ln := range l.lines {
		if ln.inkWidth > 60 {
			t.Errorf("line %d is %v wide, past 60", i, ln.inkWidth)
		}
	}
	if _, _, up := l.LineRange(0); !up {
		t.Error("a line broken inside a word should end upstream")
	}
	// Words wrap at spaces, which stay on the line they end.
	w := s.Layout("hello world", st, 50)
	ms := w.LineMetrics()
	if len(ms) != 2 || ms[0].Length != 6 || ms[0].TrailingWhitespaceLength != 1 {
		t.Fatalf("hello world at 50: %+v", ms)
	}
	if s, e, up := w.LineRange(0); s != 0 || e != 5 || up {
		t.Errorf("line range %d..%d %v, want 0..5", s, e, up)
	}
	_, y, _ := w.CaretAt(6, true)
	_, y2, _ := w.CaretAt(6, false)
	if y != 0 || y2 == 0 {
		t.Errorf("caret at the wrap: upstream y %v, downstream y %v", y, y2)
	}
	if n := s.Layout(long, Style{Size: 14, NoWrap: true}, 60).Lines(); n != 1 {
		t.Errorf("NoWrap lines = %d, want 1", n)
	}
}

func TestAlignment(t *testing.T) {
	s := newTestSystem(t)
	w, _ := s.Measure("Hi", Style{Size: 14}, Unbounded)
	for _, c := range []struct {
		align Align
		want  float32
	}{{AlignStart, 0}, {AlignCenter, (200 - w) / 2}, {AlignEnd, 200 - w}} {
		l := s.Layout("Hi", Style{Size: 14, Align: c.align}, 200)
		if x, _, _ := l.Caret(0); x < c.want-1 || x > c.want+1 {
			t.Errorf("align %d: caret at %v, want %v", c.align, x, c.want)
		}
	}
	// Without a width, aligned text stays at the origin.
	if x, _, _ := s.Layout("Hi", Style{Size: 14, Align: AlignCenter}, Unbounded).Caret(0); x != 0 {
		t.Errorf("unbounded centered caret at %v, want 0", x)
	}
}

func TestWeightsStylesAndFamilies(t *testing.T) {
	s := newTestSystem(t)
	regular, _ := s.Measure("Hello world", Style{Size: 14}, Unbounded)
	bold, _ := s.Measure("Hello world", Style{Size: 14, Weight: 700}, Unbounded)
	if bold <= regular {
		t.Errorf("bold width %v, regular %v", bold, regular)
	}
	if f := s.font("", 400, true, 14); ctFontGetSymbolicTraits(f)&ctFontItalicTrait == 0 {
		t.Error("italic system font has no italic trait")
	}
	if f := s.font("Helvetica Neue", 700, false, 14); ctFontGetSymbolicTraits(f)&ctFontBoldTrait == 0 {
		t.Errorf("bold Helvetica Neue is %q without the bold trait", familyOf(f))
	}
	// The Windows UI font names and the system font name are the system font.
	want := capture(s.Layout("Settings", Style{Size: 14}, Unbounded))
	for _, fam := range []string{".AppleSystemUIFont", "Segoe UI", "Segoe UI Variable", "No Such Family"} {
		if got := capture(s.Layout("Settings", Style{Family: fam, Size: 14}, Unbounded)); !reflect.DeepEqual(got.Runs, want.Runs) {
			t.Errorf("%q does not lay out as the system font:\n got %v\nwant %v", fam, got.Runs, want.Runs)
		}
	}
	// Code fonts are monospaced.
	for _, fam := range []string{"Cascadia Mono", "Consolas"} {
		a, _ := s.Measure("iiii", Style{Family: fam, Size: 14}, Unbounded)
		b, _ := s.Measure("MMMM", Style{Family: fam, Size: 14}, Unbounded)
		if a != b {
			t.Errorf("%q is not monospaced: %v and %v", fam, a, b)
		}
	}
	if got := runFamilies(s.Layout("😀", Style{Family: "Segoe UI Emoji", Size: 14}, Unbounded)); !slices.Equal(got, []string{"Apple Color Emoji"}) {
		t.Errorf("Segoe UI Emoji draws with %v", got)
	}
	// Runs change the font of their range only.
	l := s.Layout("plain bold", Style{Size: 14, Runs: []Run{{Start: 6, End: 10, Weight: 700, Family: "Menlo"}}}, Unbounded)
	if got := runFamilies(l); len(got) != 2 || got[1] != "Menlo" {
		t.Errorf("run families %v, want the second in Menlo", got)
	}
}

// Missing glyphs come from the system fallback, or first from the families
// that SetFallbackFamilies names.
func TestFallbackFamilies(t *testing.T) {
	s := newTestSystem(t)
	const korean = "Hello 한국어"
	def := runFamilies(s.Layout(korean, Style{Size: 14}, Unbounded))
	if len(def) < 2 {
		t.Fatalf("Korean text draws with %v, want a fallback run", def)
	}
	if len(s.familyFaces("AppleMyungjo")) == 0 {
		t.Skip("AppleMyungjo is not installed")
	}
	if slices.Contains(def, "AppleMyungjo") {
		t.Fatalf("the system fallback already picks AppleMyungjo: %v", def)
	}
	SetFallbackFamilies([]string{"No Such Family", "AppleMyungjo"})
	defer SetFallbackFamilies(nil)
	got := runFamilies(s.Layout(korean, Style{Size: 14}, Unbounded))
	if !slices.Contains(got, "AppleMyungjo") {
		t.Errorf("with fallback families the text draws with %v", got)
	}
	if got[0] == "AppleMyungjo" {
		t.Errorf("the Latin text left the requested font: %v", got)
	}
	// Emoji keep the system fallback after the families.
	got = runFamilies(s.Layout("한😀", Style{Size: 14}, Unbounded))
	if !slices.ContainsFunc(got, func(f string) bool { return strings.Contains(f, "Apple Color Emoji") }) {
		t.Errorf("emoji draw with %v", got)
	}
}

func TestRegisterFont(t *testing.T) {
	if _, err := RegisterFont([]byte("not a font")); err == nil {
		t.Error("RegisterFont accepted garbage")
	}
	var data []byte
	for _, p := range []string{"/System/Library/Fonts/Supplemental/Georgia.ttf", "/System/Library/Fonts/Supplemental/Arial.ttf", "/System/Library/Fonts/Helvetica.ttc"} {
		if b, err := os.ReadFile(p); err == nil {
			data = b
			break
		}
	}
	if data == nil {
		t.Skip("no known font file on this system")
	}
	names, err := RegisterFont(data)
	if err != nil || len(names) == 0 {
		t.Fatalf("RegisterFont = %v, %v", names, err)
	}
	s := newTestSystem(t)
	if kind, _ := classify(names[0]); kind != familyNamed || len(registeredFaces(names[0])) == 0 {
		t.Fatalf("%q is not registered", names[0])
	}
	if got := runFamilies(s.Layout("Registered", Style{Family: names[0], Size: 14}, Unbounded)); !slices.Equal(got, []string{names[0]}) {
		t.Errorf("registered %q draws with %v", names[0], got)
	}
}

func TestDrawGlyphs(t *testing.T) {
	atlas := newFakeAtlas(2048)
	s := newSystem(atlas)
	defer s.Release()
	l := s.Layout("Hello", Style{Size: 14}, Unbounded)
	var scene render.Scene
	if !l.Draw(&scene, 10.4, 20.6, [4]float32{0, 0, 0, 1}, render.NoClip) {
		t.Fatal("atlas full")
	}
	if len(scene.Prims) != 5 {
		t.Fatalf("drew %d prims, want 5", len(scene.Prims))
	}
	_, h := l.Size()
	for _, p := range scene.Prims {
		if p.Kind != render.KindGlyph || p.Bounds[0] < 9 || p.Bounds[1] < 20 || p.Bounds[1]+p.Bounds[3] > 21+h {
			t.Errorf("glyph prim %+v outside the text at (10, 21)", p)
		}
		if p.UV[0]+p.UV[2] > 1 || p.UV[1]+p.UV[3] > 1 {
			t.Errorf("glyph UV %v outside the atlas", p.UV)
		}
	}
	// The two l glyphs at different subpixel offsets may share a mask, the
	// rest are new; drawing again rasterizes nothing.
	inserts := atlas.inserts
	if inserts < 3 {
		t.Errorf("inserted %d masks", inserts)
	}
	l.Draw(&scene, 0, 0, [4]float32{0, 0, 0, 1}, render.NoClip)
	if atlas.inserts != inserts {
		t.Errorf("redraw inserted %d more masks", atlas.inserts-inserts)
	}
	// Clipped glyphs are skipped.
	scene.Reset()
	l.Draw(&scene, 0, 0, [4]float32{0, 0, 0, 1}, [4]float32{0, 0, 1, 1})
	if len(scene.Prims) > 1 {
		t.Errorf("clipped draw kept %d prims", len(scene.Prims))
	}

	// A full atlas reports false; after it grows the text draws again.
	small := newFakeAtlas(16)
	s2 := newSystem(small)
	defer s2.Release()
	l2 := s2.Layout("Hello world, a longer line", Style{Size: 14}, Unbounded)
	scene.Reset()
	if l2.Draw(&scene, 0, 0, [4]float32{0, 0, 0, 1}, render.NoClip) {
		t.Fatal("a 16 texel atlas held the whole line")
	}
	small.size = 2048
	small.clear()
	scene.Reset()
	if !l2.Draw(&scene, 0, 0, [4]float32{0, 0, 0, 1}, render.NoClip) || len(scene.Prims) < 20 {
		t.Fatalf("after growing: %d prims", len(scene.Prims))
	}
}

func TestRasterizeCoverage(t *testing.T) {
	s := newTestSystem(t)
	font := s.font("", 400, false, 20)
	u := []uint16{'H'}
	var g uint16
	if !ctFontGetGlyphsForCharacters(font, &u[0], &g, 1) {
		t.Fatal("no glyph for H")
	}
	m, ok := rasterize(font, g, 0, 0)
	if !ok || len(m.pix) != m.w*m.h {
		t.Fatalf("rasterize: %+v %v", m, ok)
	}
	if m.top >= 0 || m.top < -16 || m.left < -1 || m.left > 3 || m.h < 12 || m.h > 17 {
		t.Errorf("H at 20px: %dx%d at (%d, %d)", m.w, m.h, m.left, m.top)
	}
	if !slices.Contains(m.pix, 255) {
		t.Error("H has no solid stem pixel")
	}
	shifted, _ := rasterize(font, g, 0.5, 0)
	if slices.Equal(shifted.pix, m.pix) {
		t.Error("a half pixel shift drew the same mask")
	}
}

func TestColorEmojiImages(t *testing.T) {
	s := newTestSystem(t)
	l := s.Layout("a😀b", Style{Size: 20}, Unbounded)
	kinds := func() []uint32 {
		var scene render.Scene
		if !l.Draw(&scene, 0, 0, [4]float32{0, 0, 0, 0.5}, render.NoClip) {
			t.Fatal("atlas full")
		}
		var out []uint32
		for _, p := range scene.Prims {
			out = append(out, p.Kind)
			if p.Kind == render.KindImage && (p.Color != [4]float32{1, 1, 1, 0.5} || p.Flags&render.FlagPremultiplied == 0) {
				t.Errorf("emoji prim color %v flags %d", p.Color, p.Flags)
			}
		}
		return out
	}
	// Without textures the emoji is drawn as coverage.
	if got := kinds(); !slices.Equal(got, []uint32{render.KindGlyph, render.KindGlyph, render.KindGlyph}) {
		t.Errorf("without textures drew %v", got)
	}
	var made int
	colorful := false
	s.SetColorGlyphTextures(func(w, h int, rgba []byte) (*render.Texture, error) {
		made++
		if len(rgba) != w*h*4 || w < 10 || h < 10 {
			t.Errorf("texture %dx%d with %d bytes", w, h, len(rgba))
		}
		for i := 0; i+3 < len(rgba); i += 4 {
			if rgba[i] != rgba[i+1] || rgba[i+1] != rgba[i+2] {
				colorful = true
			}
		}
		return &render.Texture{Width: w, Height: h, Premultiplied: true}, nil
	})
	if got := kinds(); !slices.Equal(got, []uint32{render.KindGlyph, render.KindImage, render.KindGlyph}) {
		t.Errorf("with textures drew %v", got)
	}
	kinds()
	if made != 1 || !colorful {
		t.Errorf("made %d textures, colorful %v; want one in color", made, colorful)
	}
	s.ResetAtlas(&render.Atlas{})
	if len(s.color.dead) != 1 || len(s.color.entries) != 0 {
		t.Errorf("ResetAtlas kept %d textures, %d wait for release", len(s.color.entries), len(s.color.dead))
	}
	s.SetColorGlyphTextures(nil)
	if len(s.color.dead) != 1 {
		t.Errorf("%d textures wait for release, want 1", len(s.color.dead))
	}
	s.EndFrame()
	if len(s.color.dead) != 0 {
		t.Errorf("EndFrame left %d textures", len(s.color.dead))
	}
}
