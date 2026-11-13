//go:build darwin

package render

import (
	"image"
	"image/color"
	"math"
	"runtime"
	"testing"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/darwin"
)

// offscreen returns an offscreen renderer released when the test ends.
func offscreen(t *testing.T, w, h int) *Renderer {
	t.Helper()
	r, err := NewOffscreen(w, h)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Release)
	return r
}

func backdropRenderer(t *testing.T, w, h int) *Renderer { return offscreen(t, w, h) }

// render draws the scene build makes into a new w x h offscreen renderer.
func render(t *testing.T, w, h int, clear [4]float32, build func(s *Scene, r *Renderer)) *image.NRGBA {
	t.Helper()
	r := offscreen(t, w, h)
	var s Scene
	build(&s, r)
	return drawReadClear(t, r, &s, clear)
}

func drawReadClear(t *testing.T, r *Renderer, s *Scene, clear [4]float32) *image.NRGBA {
	t.Helper()
	if err := r.Draw(s, clear); err != nil {
		t.Fatal(err)
	}
	img, err := r.ReadTarget()
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func near(a, b uint8, tol int) bool { d := int(a) - int(b); return d >= -tol && d <= tol }

// wantPixel checks the pixel at (x, y) against c within tol per channel.
func wantPixel(t *testing.T, img *image.NRGBA, x, y int, c color.NRGBA, tol int) {
	t.Helper()
	got := img.NRGBAAt(x, y)
	if !near(got.R, c.R, tol) || !near(got.G, c.G, tol) || !near(got.B, c.B, tol) || !near(got.A, c.A, tol) {
		t.Errorf("pixel (%d, %d) = %v, want %v ±%d", x, y, got, c, tol)
	}
}

// wantAlpha checks only the alpha of the pixel at (x, y) is in [lo, hi].
func wantAlpha(t *testing.T, img *image.NRGBA, x, y int, lo, hi uint8) {
	t.Helper()
	if a := img.NRGBAAt(x, y).A; a < lo || a > hi {
		t.Errorf("pixel (%d, %d) alpha = %d, want %d to %d", x, y, a, lo, hi)
	}
}

var (
	red        = [4]float32{1, 0, 0, 1}
	blue       = [4]float32{0, 0, 1, 1}
	white      = [4]float32{1, 1, 1, 1}
	solidRed   = color.NRGBA{255, 0, 0, 255}
	solidGreen = color.NRGBA{0, 255, 0, 255}
	solidBlue  = color.NRGBA{0, 0, 255, 255}
	none       = color.NRGBA{}
)

func rect(b [4]float32, c [4]float32) Prim {
	return Prim{Bounds: b, Clip: NoClip, Color: c, Kind: KindRect}
}

func TestLayoutMatchesShader(t *testing.T) {
	if s := unsafe.Sizeof(Prim{}); s != 192 {
		t.Errorf("Prim is %d bytes, shaders.metal expects 192", s)
	}
	if s := unsafe.Sizeof(ClipRec{}); s != 64 {
		t.Errorf("ClipRec is %d bytes, shaders.metal expects 64", s)
	}
	if s := unsafe.Sizeof(globals{}); s != 48 {
		t.Errorf("globals is %d bytes, shaders.metal expects 48", s)
	}
	if s := unsafe.Sizeof(batchParams{}); s != 16 {
		t.Errorf("batchParams is %d bytes, shaders.metal expects 16", s)
	}
	if o := unsafe.Offsetof(Prim{}.Kind); o != 144 {
		t.Errorf("Prim.Kind at %d, want 144", o)
	}
	if o := unsafe.Offsetof(Prim{}.Xform); o != 160 {
		t.Errorf("Prim.Xform at %d, want 160", o)
	}
}

func TestClearAndSolidRect(t *testing.T) {
	img := render(t, 32, 32, [4]float32{0, 0, 1, 1}, func(s *Scene, _ *Renderer) {
		s.Add(rect([4]float32{8, 8, 16, 16}, red))
	})
	wantPixel(t, img, 2, 2, solidBlue, 0)
	wantPixel(t, img, 8, 8, solidRed, 0)
	wantPixel(t, img, 16, 16, solidRed, 0)
	wantPixel(t, img, 23, 23, solidRed, 0)
	wantPixel(t, img, 24, 16, solidBlue, 0)
}

func TestTranslucentClearIsStraightAlpha(t *testing.T) {
	r := offscreen(t, 4, 4)
	img := drawReadClear(t, r, &Scene{}, [4]float32{1, 0, 0, 0.5})
	wantPixel(t, img, 1, 1, color.NRGBA{255, 0, 0, 128}, 1)
}

func TestRoundedRectWithBorder(t *testing.T) {
	img := render(t, 32, 32, [4]float32{}, func(s *Scene, _ *Renderer) {
		p := rect([4]float32{4, 4, 24, 24}, red)
		p.Radii = [4]float32{8, 8, 8, 8}
		p.Border = [4]float32{2, 2, 2, 2}
		p.BorderColor = blue
		s.Add(p)
	})
	wantPixel(t, img, 16, 16, solidRed, 0)
	wantPixel(t, img, 16, 4, solidBlue, 0)
	wantPixel(t, img, 16, 5, solidBlue, 0)
	wantPixel(t, img, 4, 16, solidBlue, 0)
	wantPixel(t, img, 16, 7, solidRed, 0)
	wantPixel(t, img, 4, 4, none, 0)  // outside the rounded corner
	wantAlpha(t, img, 6, 6, 1, 254)   // the corner's antialiased edge
	wantPixel(t, img, 1, 16, none, 0) // outside the box
}

func TestDashedBorder(t *testing.T) {
	img := render(t, 64, 8, [4]float32{}, func(s *Scene, _ *Renderer) {
		p := rect([4]float32{0, 0, 64, 8}, [4]float32{})
		p.Border = [4]float32{2, 2, 2, 2}
		p.BorderColor = blue
		p.Flags = FlagDashed
		s.Add(p)
	})
	// Dashes are 3 border widths long and 2 apart: 6 on, 4 off.
	wantPixel(t, img, 12, 0, solidBlue, 0)
	wantPixel(t, img, 17, 0, none, 0)
	wantPixel(t, img, 22, 1, solidBlue, 0)
}

func TestGradient(t *testing.T) {
	img := render(t, 64, 8, [4]float32{}, func(s *Scene, _ *Renderer) {
		p := rect([4]float32{0, 0, 64, 8}, red)
		p.Color2 = blue
		p.Flags = FlagGradient
		p.Params[0] = math.Pi / 2 // left to right
		s.Add(p)
	})
	wantPixel(t, img, 0, 4, color.NRGBA{253, 0, 2, 255}, 2)
	wantPixel(t, img, 32, 4, color.NRGBA{125, 0, 130, 255}, 2)
	wantPixel(t, img, 63, 4, color.NRGBA{2, 0, 253, 255}, 2)
}

func TestShadow(t *testing.T) {
	img := render(t, 64, 64, [4]float32{}, func(s *Scene, _ *Renderer) {
		s.Add(Prim{Bounds: [4]float32{16, 16, 32, 32}, Clip: NoClip, Color: [4]float32{0, 0, 0, 1}, Params: [4]float32{4}, Kind: KindShadow})
	})
	wantPixel(t, img, 32, 32, color.NRGBA{0, 0, 0, 255}, 1)
	wantAlpha(t, img, 16, 32, 100, 180) // half the gaussian at the edge
	wantAlpha(t, img, 10, 32, 1, 60)    // the blur past the edge
	wantPixel(t, img, 1, 1, none, 0)
}

func TestInsetShadow(t *testing.T) {
	img := render(t, 64, 64, [4]float32{}, func(s *Scene, _ *Renderer) {
		s.Add(Prim{
			Bounds: [4]float32{16, 16, 32, 32}, UV: [4]float32{8, 8, 48, 48},
			Clip: NoClip, Color: [4]float32{0, 0, 0, 1}, Params: [4]float32{2}, Kind: KindShadow, Flags: FlagInset,
		})
	})
	wantPixel(t, img, 32, 32, none, 0)                      // inside the hole
	wantPixel(t, img, 10, 32, color.NRGBA{0, 0, 0, 255}, 1) // shaded element outside the hole
	wantPixel(t, img, 2, 2, none, 0)                        // outside the element
}

func TestArcAndCallout(t *testing.T) {
	img := render(t, 64, 32, [4]float32{}, func(s *Scene, _ *Renderer) {
		// The right half of a ring, from the top clockwise.
		s.Add(Prim{Bounds: [4]float32{0, 0, 32, 32}, Clip: NoClip, Color: red, Params: [4]float32{0, math.Pi, 4, 0}, Kind: KindArc})
		s.Add(Prim{Bounds: [4]float32{40, 12, 16, 16}, Clip: NoClip, Color: blue, Params: [4]float32{48, 6, 4, 0}, Kind: KindCallout})
	})
	wantPixel(t, img, 29, 16, solidRed, 0)
	wantPixel(t, img, 2, 16, none, 0)
	wantPixel(t, img, 16, 16, none, 0)
	wantPixel(t, img, 48, 20, solidBlue, 0) // body
	wantPixel(t, img, 48, 5, solidBlue, 0)  // the diamond above it
	wantPixel(t, img, 41, 2, none, 0)
}

func TestWave(t *testing.T) {
	img := render(t, 40, 10, [4]float32{}, func(s *Scene, _ *Renderer) {
		s.Add(Prim{Bounds: [4]float32{0, 0, 40, 6}, Clip: NoClip, Color: red, Params: [4]float32{8, 1.5}, Kind: KindWave})
	})
	for x := range 40 {
		sum := 0
		for y := range 6 {
			sum += int(img.NRGBAAt(x, y).A)
		}
		if sum < 200 {
			t.Errorf("column %d covers %d, want the line", x, sum)
		}
		wantPixel(t, img, x, 8, none, 0)
	}
}

func TestPrimClipRect(t *testing.T) {
	img := render(t, 32, 32, [4]float32{}, func(s *Scene, _ *Renderer) {
		p := rect([4]float32{0, 0, 32, 32}, red)
		p.Clip = [4]float32{0, 0, 10, 32}
		s.Add(p)
	})
	wantPixel(t, img, 5, 5, solidRed, 0)
	wantPixel(t, img, 9, 5, solidRed, 0)
	wantPixel(t, img, 10, 5, none, 0)
}

func TestRoundedAndNestedClips(t *testing.T) {
	img := render(t, 64, 32, [4]float32{}, func(s *Scene, _ *Renderer) {
		prev := s.PushClip(ClipRec{Rect: [4]float32{0, 0, 32, 32}, Radii: [4]float32{16, 16, 16, 16}})
		s.Add(rect([4]float32{0, 0, 32, 32}, red))
		s.Clip = prev
		prev = s.PushClip(ClipRec{Rect: [4]float32{32, 0, 32, 32}, Radii: [4]float32{16, 16, 16, 16}})
		s.PushClip(ClipRec{Rect: [4]float32{48, 0, 16, 32}})
		s.Add(rect([4]float32{32, 0, 32, 32}, blue))
		s.Clip = prev
	})
	wantPixel(t, img, 1, 1, none, 0)
	wantPixel(t, img, 16, 16, solidRed, 0)
	wantPixel(t, img, 16, 1, solidRed, 0)
	wantPixel(t, img, 40, 16, none, 0) // left of the inner clip
	wantPixel(t, img, 56, 16, solidBlue, 0)
	wantPixel(t, img, 62, 2, none, 0) // outside the outer circle
}

func TestFeatheredClip(t *testing.T) {
	img := render(t, 32, 4, [4]float32{}, func(s *Scene, _ *Renderer) {
		s.PushClip(ClipRec{Rect: [4]float32{0, 0, 32, 4}, Feather: [4]float32{0, 0, 0, 16}})
		s.Add(rect([4]float32{0, 0, 32, 4}, red))
	})
	wantAlpha(t, img, 0, 2, 0, 5)
	wantAlpha(t, img, 8, 2, 120, 160) // smoothstep(8.5 / 16)
	wantPixel(t, img, 20, 2, solidRed, 0)
}

func TestXformRotation(t *testing.T) {
	img := render(t, 40, 40, [4]float32{}, func(s *Scene, _ *Renderer) {
		p := rect([4]float32{0, 0, 20, 10}, red)
		// A quarter turn clockwise on screen: (x, y) -> (30 - y, x + 5).
		p.Flags = FlagXform
		p.Xform = [4]float32{0, 1, -1, 0}
		p.Xlate = [2]float32{30, 5}
		p.Clip2 = NoClip
		s.Add(p)
	})
	wantPixel(t, img, 25, 15, solidRed, 0)
	wantPixel(t, img, 21, 6, solidRed, 0)
	wantPixel(t, img, 28, 23, solidRed, 0)
	wantPixel(t, img, 5, 5, none, 0)   // where the rect is before the turn
	wantPixel(t, img, 25, 27, none, 0) // past its far end
}

func TestXformClip2(t *testing.T) {
	img := render(t, 40, 40, [4]float32{}, func(s *Scene, _ *Renderer) {
		p := rect([4]float32{0, 0, 20, 20}, red)
		p.Flags = FlagXform
		p.Xform = [4]float32{2, 0, 0, 2}
		p.Clip2 = [4]float32{0, 0, 10, 20} // the left half, before scaling
		s.Add(p)
	})
	wantPixel(t, img, 10, 10, solidRed, 0)
	wantPixel(t, img, 19, 30, solidRed, 0)
	wantPixel(t, img, 21, 10, none, 0)
}

func TestImageStraightAndPremultiplied(t *testing.T) {
	// Half-transparent red, premultiplied.
	pix := make([]byte, 4*4*4)
	for i := 0; i < len(pix); i += 4 {
		copy(pix[i:], []byte{128, 0, 0, 128})
	}
	img := render(t, 16, 8, [4]float32{}, func(s *Scene, r *Renderer) {
		tex, err := r.NewTexture(4, 4, pix, false)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(tex.Release)
		s.AddImage(Prim{Bounds: [4]float32{0, 0, 8, 8}, Clip: NoClip, UV: [4]float32{0, 0, 1, 1}, Color: white, Flags: FlagPremultiplied}, tex)
		s.AddImage(Prim{Bounds: [4]float32{8, 0, 8, 8}, Clip: NoClip, UV: [4]float32{0, 0, 1, 1}, Color: white}, tex)
	})
	wantPixel(t, img, 4, 4, color.NRGBA{255, 0, 0, 128}, 2)
	// The same texels taken as straight alpha are half as red.
	wantPixel(t, img, 12, 4, color.NRGBA{128, 0, 0, 128}, 2)
}

func TestImagesBatchByTexture(t *testing.T) {
	solid := func(c []byte) []byte {
		pix := make([]byte, 2*2*4)
		for i := 0; i < len(pix); i += 4 {
			copy(pix[i:], c)
		}
		return pix
	}
	r := offscreen(t, 40, 8)
	texR, err := r.NewTexture(2, 2, solid([]byte{255, 0, 0, 255}), false)
	if err != nil {
		t.Fatal(err)
	}
	defer texR.Release()
	texG, err := r.NewTexture(2, 2, solid([]byte{0, 255, 0, 255}), false)
	if err != nil {
		t.Fatal(err)
	}
	defer texG.Release()
	var s Scene
	for i, tex := range []*Texture{texR, texR, texG, texR, texG} {
		s.AddImage(Prim{Bounds: [4]float32{float32(i * 8), 0, 8, 8}, Clip: NoClip, UV: [4]float32{0, 0, 1, 1}, Color: white, Flags: FlagPremultiplied}, tex)
	}
	s.Add(rect([4]float32{36, 0, 4, 8}, blue))
	img := drawReadClear(t, r, &s, [4]float32{})
	for i, c := range []color.NRGBA{solidRed, solidRed, solidGreen, solidRed, solidGreen} {
		wantPixel(t, img, i*8+2, 4, c, 0)
	}
	wantPixel(t, img, 38, 4, solidBlue, 0)
	if b := r.Stats().Batches; b != 4 {
		t.Errorf("Batches = %d, want 4: one per change of texture, the rect drawn with the last", b)
	}
}

func TestImageRoundedAndOpacity(t *testing.T) {
	pix := make([]byte, 2*2*4)
	for i := 0; i < len(pix); i += 4 {
		copy(pix[i:], []byte{0, 0, 255, 255})
	}
	img := render(t, 16, 16, [4]float32{}, func(s *Scene, r *Renderer) {
		tex, err := r.NewTexture(2, 2, pix, false)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(tex.Release)
		s.AddImage(Prim{Bounds: [4]float32{0, 0, 16, 16}, Radii: [4]float32{8, 8, 8, 8}, Clip: NoClip, UV: [4]float32{0, 0, 1, 1}, Color: [4]float32{1, 1, 1, 0.5}}, tex)
	})
	wantPixel(t, img, 8, 8, color.NRGBA{0, 0, 255, 128}, 1)
	wantPixel(t, img, 0, 0, none, 0)
}

func TestTextureUpdate(t *testing.T) {
	r := offscreen(t, 4, 4)
	pix := make([]byte, 4*4*4)
	for i := 0; i < len(pix); i += 4 {
		copy(pix[i:], []byte{255, 0, 0, 255})
	}
	tex, err := r.NewTexture(4, 4, pix, true)
	if err != nil {
		t.Fatal(err)
	}
	defer tex.Release()
	var s Scene
	s.AddImage(Prim{Bounds: [4]float32{0, 0, 4, 4}, Clip: NoClip, UV: [4]float32{0, 0, 1, 1}, Color: white, Flags: FlagPremultiplied}, tex)
	wantPixel(t, drawReadClear(t, r, &s, [4]float32{}), 2, 2, solidRed, 0)
	for i := 0; i < len(pix); i += 4 {
		copy(pix[i:], []byte{0, 255, 0, 255})
	}
	runtime.LockOSThread()
	r.Device().Lock()
	tex.Update(r.Device(), pix)
	r.Device().Unlock()
	runtime.UnlockOSThread()
	wantPixel(t, drawReadClear(t, r, &s, [4]float32{}), 2, 2, solidGreen, 0)
}

// glyphAlpha is glyph_alpha of shaders.metal.
func glyphAlpha(sample float64, c [3]float64) float64 {
	g := gammaRatios(1.8)
	brightness := c[0]*0.30 + c[1]*0.59 + c[2]*0.11
	k := 1.0 * min(max(4*(0.75-brightness), 0), 1)
	contrasted := sample * (k + 1) / (sample*k + 1)
	adjust := float64(g[0])*brightness + float64(g[1])
	correction := adjust*contrasted + (float64(g[2])*brightness + float64(g[3]))
	return min(max(contrasted+contrasted*(1-contrasted)*correction, 0), 1)
}

func TestGlyphCoverage(t *testing.T) {
	r := offscreen(t, 32, 8)
	mask := make([]byte, 8*8)
	for i := range mask {
		mask[i] = 255
		if i >= 32 {
			mask[i] = 128
		}
	}
	tile, ok := r.Atlas.Insert(8, 8, mask)
	if !ok {
		t.Fatal("atlas insert failed")
	}
	as := float32(r.Atlas.Size())
	uv := [4]float32{float32(tile.X) / as, float32(tile.Y) / as, 8 / as, 8 / as}
	var s Scene
	s.Add(Prim{Bounds: [4]float32{0, 0, 8, 8}, Clip: NoClip, UV: uv, Color: white, Kind: KindGlyph})
	s.Add(Prim{Bounds: [4]float32{8, 0, 8, 8}, Clip: NoClip, UV: uv, Color: [4]float32{0, 0, 0, 1}, Kind: KindGlyph})
	s.Add(Prim{Bounds: [4]float32{16, 0, 8, 8}, Clip: NoClip, UV: uv, Color: white, Kind: KindGlyph, Flags: FlagRawAlpha})
	img := drawReadClear(t, r, &s, [4]float32{})
	wantPixel(t, img, 4, 1, color.NRGBA{255, 255, 255, 255}, 0)
	half := 128.0 / 255
	wantPixel(t, img, 4, 6, color.NRGBA{255, 255, 255, uint8(math.Round(255 * glyphAlpha(half, [3]float64{1, 1, 1})))}, 2)
	wantPixel(t, img, 12, 6, color.NRGBA{0, 0, 0, uint8(math.Round(255 * glyphAlpha(half, [3]float64{})))}, 2)
	wantPixel(t, img, 20, 6, color.NRGBA{255, 255, 255, 128}, 1)
}

func TestAtlasGrowClearsTexture(t *testing.T) {
	r := offscreen(t, 16, 8)
	full := func(w, h int) []byte {
		b := make([]byte, w*h)
		for i := range b {
			b[i] = 255
		}
		return b
	}
	for range 2 {
		if err := r.Atlas.Grow(); err != nil {
			t.Fatal(err)
		}
		if r.Atlas.Size() > 4096 {
			break
		}
	}
	tile, ok := r.Atlas.Insert(4, 4, full(4, 4))
	if !ok {
		t.Fatal("atlas insert failed")
	}
	as := float32(r.Atlas.Size())
	var s Scene
	// One texel wider and taller than the tile, over the padding.
	s.Add(Prim{Bounds: [4]float32{0, 0, 5, 5}, Clip: NoClip, UV: [4]float32{float32(tile.X) / as, float32(tile.Y) / as, 5 / as, 5 / as}, Color: white, Kind: KindGlyph, Flags: FlagRawAlpha})
	img := drawReadClear(t, r, &s, [4]float32{})
	wantPixel(t, img, 2, 2, color.NRGBA{255, 255, 255, 255}, 0)
	wantPixel(t, img, 4, 2, none, 0)
	wantPixel(t, img, 2, 4, none, 0)
}

func TestLayerOpacity(t *testing.T) {
	img := render(t, 40, 20, [4]float32{}, func(s *Scene, _ *Renderer) {
		mark := s.BeginLayer()
		s.Add(rect([4]float32{0, 0, 20, 20}, red))
		s.Add(rect([4]float32{10, 0, 20, 20}, blue))
		s.EndLayer(mark, Prim{Clip: NoClip, Color: [4]float32{1, 1, 1, 0.5}}, 0)
	})
	// The group fades as one: the overlap shows blue alone, not red through it.
	wantPixel(t, img, 15, 10, color.NRGBA{0, 0, 255, 128}, 1)
	wantPixel(t, img, 5, 10, color.NRGBA{255, 0, 0, 128}, 1)
	wantPixel(t, img, 35, 10, none, 0)
}

func TestNestedLayersAndLayerBlur(t *testing.T) {
	img := render(t, 64, 32, [4]float32{}, func(s *Scene, _ *Renderer) {
		outer := s.BeginLayer()
		inner := s.BeginLayer()
		s.Add(rect([4]float32{4, 4, 16, 16}, red))
		s.EndLayer(inner, Prim{Clip: NoClip, Color: [4]float32{1, 1, 1, 0.5}}, 0)
		s.Add(rect([4]float32{36, 8, 16, 16}, blue))
		s.EndLayer(outer, Prim{Clip: NoClip, Color: [4]float32{1, 1, 1, 1}}, 0)
		blur := s.BeginLayer()
		s.Add(rect([4]float32{36, 8, 16, 16}, red))
		s.EndLayer(blur, Prim{Clip: NoClip, Color: white}, 3)
	})
	wantPixel(t, img, 12, 12, color.NRGBA{255, 0, 0, 128}, 1)
	// The blurred red over the blue: solid in the middle, fading at its edges.
	if c := img.NRGBAAt(44, 16); c.R < 200 || c.B > 40 || c.A < 250 {
		t.Errorf("blurred layer center = %v, want red over the blue", c)
	}
	if c := img.NRGBAAt(36, 16); c.R < 60 || c.B < 60 {
		t.Errorf("blurred layer edge = %v, want red and blue mixed", c)
	}
	wantAlpha(t, img, 33, 16, 1, 128) // the blur reaching past the box
}

func TestSnapshotIsOpaque(t *testing.T) {
	r := offscreen(t, 8, 8)
	var got *image.NRGBA
	r.RequestSnapshot(func(img *image.NRGBA) { got = img })
	var s Scene
	s.Add(rect([4]float32{0, 0, 4, 8}, [4]float32{0, 0, 1, 0.5}))
	if err := r.Draw(&s, [4]float32{}); err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("snapshot not taken")
	}
	wantPixel(t, got, 1, 1, color.NRGBA{0, 0, 128, 255}, 1)
	wantPixel(t, got, 6, 1, color.NRGBA{0, 0, 0, 255}, 0)
}

func TestManyFramesAndPrims(t *testing.T) {
	r := offscreen(t, 100, 100)
	for frame := range 8 {
		var s Scene
		n := 100 * (frame + 1) * (frame + 1)
		for i := range n {
			x, y := float32(i%100), float32(i/100%100)
			c := red
			if frame%2 == 1 {
				c = blue
			}
			s.Add(rect([4]float32{x, y, 1, 1}, c))
		}
		img := drawReadClear(t, r, &s, [4]float32{})
		want := solidRed
		if frame%2 == 1 {
			want = solidBlue
		}
		wantPixel(t, img, 0, 0, want, 0)
		last := min(n, 10000) - 1
		wantPixel(t, img, last%100, last/100, want, 0)
	}
}

// TestLayerRenderer draws through a CAMetalLayer's drawables, as a window
// does, reading each frame back with a snapshot.
func TestLayerRenderer(t *testing.T) {
	var layer id
	withPool(func() { layer = darwin.New("CAMetalLayer") })
	defer darwin.Release(layer)
	obj := darwin.New("NSObject")
	defer darwin.Release(obj)
	if _, err := New(uintptr(obj), 8, 8, false); err == nil {
		t.Error("New accepted an object that is not a CAMetalLayer")
	}
	r, err := New(uintptr(layer), 32, 16, false)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	if r.Transparent() || !darwin.Get[bool](layer, "isOpaque") || darwin.Get[bool](layer, "framebufferOnly") {
		t.Error("layer not set up opaque with a readable drawable")
	}
	if sz := darwin.Get[darwin.CGSize](layer, "drawableSize"); sz.W != 32 || sz.H != 16 {
		t.Errorf("drawableSize = %v, want 32 x 16", sz)
	}
	if err := r.SetTransparent(true); err != nil || !r.Transparent() || darwin.Get[bool](layer, "isOpaque") {
		t.Errorf("SetTransparent(true) = %v, layer still opaque", err)
	}
	if err := r.Resize(40, 20); err != nil {
		t.Fatal(err)
	}
	if sz := darwin.Get[darwin.CGSize](layer, "drawableSize"); sz.W != 40 || sz.H != 20 {
		t.Errorf("drawableSize = %v after Resize(40, 20)", sz)
	}
	for frame := range 5 {
		var got *image.NRGBA
		r.RequestSnapshot(func(img *image.NRGBA) { got = img })
		var s Scene
		s.Add(rect([4]float32{0, 0, 20, 20}, red))
		// A backdrop copies from the drawable.
		s.AddBackdrop(Prim{Bounds: [4]float32{24, 4, 8, 8}, Clip: NoClip, Color: white}, 2)
		if err := r.Draw(&s, [4]float32{0, 0, 1, 1}); err != nil {
			t.Fatal(err)
		}
		if r.Occluded() || got == nil {
			t.Fatalf("frame %d: no drawable", frame)
		}
		if got.Bounds().Dx() != 40 || got.Bounds().Dy() != 20 {
			t.Fatalf("snapshot is %v, want 40 x 20", got.Bounds())
		}
		wantPixel(t, got, 5, 5, solidRed, 0)
		wantPixel(t, got, 30, 15, solidBlue, 0)
		wantPixel(t, got, 28, 8, solidBlue, 0)
	}
}

func TestDeviceLockIsRecursive(t *testing.T) {
	r := offscreen(t, 1, 1)
	d := r.Device()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if DeviceLockedByCurrentThread() {
		t.Fatal("locked before Lock")
	}
	d.Lock()
	d.Lock()
	if !d.Nested() || !DeviceLockedByCurrentThread() {
		t.Error("nested lock not held")
	}
	done := make(chan bool)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		done <- DeviceLockedByCurrentThread()
	}()
	if <-done {
		t.Error("another thread holds the lock")
	}
	d.Unlock()
	if d.Nested() || !DeviceLockedByCurrentThread() {
		t.Error("outer lock lost")
	}
	d.Unlock()
	if DeviceLockedByCurrentThread() {
		t.Error("lock held after Unlock")
	}
}

func TestSharedDevice(t *testing.T) {
	a, err := AcquireDevice()
	if err != nil {
		t.Fatal(err)
	}
	defer a.Release()
	b := AcquireCurrentDevice()
	if b != a {
		t.Fatal("AcquireCurrentDevice returned another device")
	}
	b.Release()
	if CurrentGen() != a.Gen || a.Gen == 0 {
		t.Errorf("CurrentGen = %d, want %d", CurrentGen(), a.Gen)
	}
	if a.Info.Software || a.Info.Name == "" {
		t.Errorf("Info = %+v", a.Info)
	}
}

func TestGPUTiming(t *testing.T) {
	r := offscreen(t, 8, 8)
	r.SetGPUTiming(true)
	var s Scene
	s.Add(rect([4]float32{0, 0, 8, 8}, red))
	var last uint64
	got := false
	for range 6 {
		if err := r.Draw(&s, [4]float32{}); err != nil {
			t.Fatal(err)
		}
		if st := r.Stats(); st.GPUValid {
			got = true
			if st.GPUFrame <= last || st.GPUFrame >= r.submission || st.GPU <= 0 {
				t.Fatalf("sample %+v after frame %d at submission %d", st, last, r.submission)
			}
			last = st.GPUFrame
		}
		// Waits for the frame, so the next Draw reports it.
		if _, err := r.ReadTarget(); err != nil {
			t.Fatal(err)
		}
	}
	if !got {
		t.Fatal("no GPU time reported")
	}
	r.SetGPUTiming(false)
	if r.Stats().GPUValid {
		t.Fatal("disabled timer retained a sample")
	}
	if err := r.Draw(&s, [4]float32{}); err != nil {
		t.Fatal(err)
	}
	if r.Stats().GPUValid {
		t.Fatal("disabled timer reported a sample")
	}
}

func BenchmarkDraw(b *testing.B) {
	r, err := NewOffscreen(800, 600)
	if err != nil {
		b.Fatal(err)
	}
	defer r.Release()
	var s Scene
	for i := range 2000 {
		s.Add(rect([4]float32{float32(i % 780), float32(i / 4 % 580), 20, 20}, red))
	}
	s.AddBackdrop(Prim{Bounds: [4]float32{100, 100, 200, 100}, Clip: NoClip, Color: white}, 8)
	b.ReportAllocs()
	for b.Loop() {
		if err := r.Draw(&s, [4]float32{1, 1, 1, 1}); err != nil {
			b.Fatal(err)
		}
	}
}
