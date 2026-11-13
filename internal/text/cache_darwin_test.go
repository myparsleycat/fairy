package text

import (
	"slices"
	"testing"

	"github.com/myparsleycat/fairy/internal/render"
)

// CoreText makes new fallback fonts for every layout; their glyphs are
// rasterized once all the same.
func TestFallbackGlyphsCachedAcrossLayouts(t *testing.T) {
	atlas := newFakeAtlas(2048)
	s := newSystem(atlas)
	defer s.Release()
	var made int
	s.SetColorGlyphTextures(func(w, h int, rgba []byte) (*render.Texture, error) {
		made++
		return &render.Texture{Width: w, Height: h, Premultiplied: true}, nil
	})
	draw := func(str string) {
		t.Helper()
		var scene render.Scene
		if !s.Layout(str, Style{Size: 14}, Unbounded).Draw(&scene, 0, 0, [4]float32{0, 0, 0, 1}, render.NoClip) {
			t.Fatal("atlas full")
		}
	}
	draw("안녕하세요 세계 😀")
	inserts, faces, textures := atlas.inserts, len(s.faces), made
	if inserts < 6 || textures != 1 {
		t.Fatalf("first layout inserted %d masks and made %d textures", inserts, textures)
	}
	for _, str := range []string{"안녕하세요 세계 😀 ", "안녕하세요 세계 😀\n", "안녕하세요 세계 😀  "} {
		draw(str)
	}
	if atlas.inserts != inserts || len(s.faces) != faces || made != textures {
		t.Errorf("relaying out the same glyphs inserted %d masks, kept %d faces and made %d textures; want %d, %d and %d",
			atlas.inserts, len(s.faces), made, inserts, faces, textures)
	}
}

// Dropping the face identities while a layout records would retire the
// emoji textures it already refers to; they must stay until it records again.
func TestFaceLimitKeepsRecordedTextures(t *testing.T) {
	defer func(n int) { faceLimit = n }(faceLimit)
	faceLimit = 2
	s := newTestSystem(t)
	s.SetColorGlyphTextures(func(w, h int, rgba []byte) (*render.Texture, error) {
		return &render.Texture{Width: w, Height: h, Premultiplied: true}, nil
	})
	l := s.Layout("😀a한b😀", Style{Size: 14}, Unbounded)
	for frame := range 3 {
		var scene render.Scene
		l.Draw(&scene, 0, 0, [4]float32{0, 0, 0, 1}, render.NoClip)
		images := 0
		for _, tex := range l.images {
			images++
			if slices.Contains(s.color.dead, tex) || !slices.Contains(s.color.owned, tex) {
				t.Errorf("frame %d draws a retired emoji texture", frame)
			}
		}
		if images == 0 {
			t.Errorf("frame %d drew no emoji", frame)
		}
		s.EndFrame()
	}
}
