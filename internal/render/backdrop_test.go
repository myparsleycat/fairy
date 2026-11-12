//go:build windows

package render

import (
	"image"
	"testing"
)

func backdropRenderer(t *testing.T, w, h int) *Renderer {
	t.Helper()
	d := warpDevice(t)
	r := &Renderer{ds: d, dev: d.Dev, ctx: d.Ctx, width: w, height: h}
	t.Cleanup(func() { r.ds = nil; r.Release() })
	var err error
	if r.target, err = NewTarget(d, w, h); err != nil {
		t.Fatal(err)
	}
	r.rtv = r.target.rtv
	if err := r.init(); err != nil {
		t.Fatal(err)
	}
	return r
}

// stripes fills the scene with 4-pixel red and blue columns, which a wide
// blur turns into an even purple.
func stripes(s *Scene, w, h int) {
	for x := 0; x < w; x += 4 {
		c := [4]float32{1, 0, 0, 1}
		if x/4%2 == 1 {
			c = [4]float32{0, 0, 1, 1}
		}
		s.Add(Prim{Bounds: [4]float32{float32(x), 0, 4, float32(h)}, Clip: NoClip, Color: c, Kind: KindRect})
	}
}

func drawRead(t *testing.T, r *Renderer, s *Scene) *image.NRGBA {
	t.Helper()
	if err := r.Draw(s, [4]float32{}); err != nil {
		t.Fatal(err)
	}
	img, err := r.ReadTarget()
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func isPurple(img *image.NRGBA, x, y int) bool {
	c := img.NRGBAAt(x, y)
	d := int(c.R) - int(c.B)
	return c.A > 250 && d > -24 && d < 24 && c.R > 90 && c.B > 90
}

func TestBackdropBlurEdges(t *testing.T) {
	r := backdropRenderer(t, 128, 64)
	var s Scene
	stripes(&s, 128, 64)
	s.AddBackdrop(Prim{Bounds: [4]float32{32, 16, 64, 32}, Clip: NoClip, Color: [4]float32{1, 1, 1, 1}}, 6)
	img := drawRead(t, r, &s)
	for _, p := range [][2]int{{64, 32}, {33, 32}, {94, 32}, {64, 17}, {64, 46}} {
		if !isPurple(img, p[0], p[1]) {
			t.Errorf("pixel %v = %v, want the blurred purple up to the box edges", p, img.NRGBAAt(p[0], p[1]))
		}
	}
}

// Over a translucent frame, as under a system backdrop, the blur replaces
// what is under the box instead of letting the sharp pixels show through.
func TestBackdropBlurReplacesTranslucentPixels(t *testing.T) {
	r := backdropRenderer(t, 128, 64)
	var s Scene
	stripes(&s, 128, 64)
	for i := range s.Prims {
		s.Prims[i].Color[3] = 0.3
	}
	s.AddBackdrop(Prim{Bounds: [4]float32{32, 16, 64, 32}, Clip: NoClip, Color: [4]float32{1, 1, 1, 1}}, 6)
	img := drawRead(t, r, &s)
	c := img.NRGBAAt(64, 32)
	if d := int(c.R) - int(c.B); c.A < 64 || c.A > 90 || d < -24 || d > 24 {
		t.Fatalf("pixel %v, want the purple blur at the stripes' 0.3 alpha", c)
	}
}

// Prims between BeginBackdrop and EndBackdrop, as the element's own shadow,
// draw once after the copy and stay out of the blur.
func TestBackdropBlurLeavesOutPrimsAfterMark(t *testing.T) {
	r := backdropRenderer(t, 128, 64)
	var s Scene
	stripes(&s, 128, 64)
	mark := s.BeginBackdrop()
	s.Add(Prim{Bounds: [4]float32{24, 8, 80, 48}, Clip: NoClip, Color: [4]float32{0, 1, 0, 1}, Kind: KindRect})
	s.EndBackdrop(mark, Prim{Bounds: [4]float32{32, 16, 64, 32}, Clip: NoClip, Color: [4]float32{1, 1, 1, 1}}, 6)
	img := drawRead(t, r, &s)
	if c := img.NRGBAAt(64, 32); !isPurple(img, 64, 32) || c.G > 8 {
		t.Errorf("inside the box = %v, want the stripes' purple without the green", c)
	}
	if c := img.NRGBAAt(28, 32); c.G < 250 || c.R > 4 || c.B > 4 {
		t.Errorf("outside the box = %v, want the green drawn after the copy", c)
	}
}

func TestBackdropBlurFollowsTransform(t *testing.T) {
	r := backdropRenderer(t, 64, 32)
	var s Scene
	s.Add(Prim{Bounds: [4]float32{0, 0, 32, 32}, Clip: NoClip, Color: [4]float32{1, 0, 0, 1}, Kind: KindRect})
	s.Add(Prim{Bounds: [4]float32{32, 0, 32, 32}, Clip: NoClip, Color: [4]float32{0, 0, 1, 1}, Kind: KindRect})
	s.AddBackdrop(Prim{Bounds: [4]float32{4, 4, 24, 24}, Clip: NoClip, Color: [4]float32{1, 1, 1, 1}}, 1)
	// As transformPrims does for an element scaling in: drawn at (42, 10, 12, 12).
	p := &s.Prims[len(s.Prims)-1]
	p.Flags |= FlagXform
	p.Xform, p.Xlate, p.Clip2 = [4]float32{0.5, 0, 0, 0.5}, [2]float32{40, 8}, NoClip
	img := drawRead(t, r, &s)
	if c := img.NRGBAAt(48, 16); c.B < 200 || c.R > 40 {
		t.Fatalf("pixel inside the scaled box = %v, want the blue under it", c)
	}
}

func TestBackdropBlurManyPerFrame(t *testing.T) {
	r := backdropRenderer(t, 256, 64)
	var s Scene
	stripes(&s, 256, 64)
	for i := range 8 {
		s.AddBackdrop(Prim{Bounds: [4]float32{float32(8 + i*30), 16, 24, 32}, Clip: NoClip, Color: [4]float32{1, 1, 1, 1}}, 6)
	}
	img := drawRead(t, r, &s)
	for i := range 8 {
		if x := 20 + i*30; !isPurple(img, x, 32) {
			t.Errorf("backdrop %d at x=%d = %v, want it drawn", i, x, img.NRGBAAt(x, 32))
		}
	}
}
