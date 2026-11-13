package fairy

import (
	"image/color"
	"testing"
	"time"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/darwin"
)

// clearMetal clears the id<MTLTexture> tex to c, premultiplied, with a
// render pass committed on queue, as an app's Metal code would.
func clearMetal(queue, tex unsafe.Pointer, c [4]float32) {
	darwin.WithPool(func() {
		cb := darwin.Send(darwin.ID(uintptr(queue)), "commandBuffer")
		pass := darwin.SendClass("MTLRenderPassDescriptor", "renderPassDescriptor")
		ca := darwin.Send(darwin.Send(pass, "colorAttachments"), "objectAtIndexedSubscript:", uint64(0))
		ca.Send(darwin.Sel("setTexture:"), darwin.ID(uintptr(tex)))
		ca.Send(darwin.Sel("setLoadAction:"), uint64(2))  // MTLLoadActionClear
		ca.Send(darwin.Sel("setStoreAction:"), uint64(1)) // MTLStoreActionStore
		ca.Send(darwin.Sel("setClearColor:"), struct{ R, G, B, A float64 }{float64(c[0]), float64(c[1]), float64(c[2]), float64(c[3])})
		darwin.Send(cb, "renderCommandEncoderWithDescriptor:", pass).Send(darwin.Sel("endEncoding"))
		cb.Send(darwin.Sel("commit"))
	})
}

func clearFrame(f SurfaceFrame, c [4]float32) { clearMetal(f.Context, f.Texture, c) }

func wantNRGBA(t *testing.T, got color.NRGBA, want color.NRGBA, tol int) {
	t.Helper()
	d := func(a, b uint8) bool { x := int(a) - int(b); return x >= -tol && x <= tol }
	if !d(got.R, want.R) || !d(got.G, want.G) || !d(got.B, want.B) || !d(got.A, want.A) {
		t.Errorf("pixel %v, want %v ±%d", got, want, tol)
	}
}

// TestSurfaceImageReadback renders offscreen without a window and reads the
// premultiplied target back as straight alpha.
func TestSurfaceImageReadback(t *testing.T) {
	img, err := SurfaceImage(8, 4, 1, func(f SurfaceFrame) {
		if f.Width != 8 || f.Height != 4 || f.Generation == 0 || f.Device == nil || f.Context == nil || f.Texture == nil || f.Target != nil {
			t.Errorf("frame %+v", f)
		}
		if g := f.GPU(); g.Device != f.Device || g.Queue != f.Context || g.Generation != f.Generation {
			t.Errorf("GPU %+v", g)
		}
		clearFrame(f, [4]float32{0.5, 0, 0, 0.5}) // premultiplied half-transparent red
	})
	if err != nil {
		t.Fatal(err)
	}
	defer img.Release()
	if w, h := img.Size(); w != 8 || h != 4 {
		t.Fatalf("size %dx%d", w, h)
	}
	px, err := img.NRGBA()
	if err != nil {
		t.Fatal(err)
	}
	wantNRGBA(t, px.NRGBAAt(3, 2), color.NRGBA{255, 0, 0, 128}, 2)
	if img.Lost() {
		t.Fatal("fresh image reports lost")
	}
	img.Release()
	Do(func() {}) // off the UI thread the texture is freed there
	if !img.Lost() {
		t.Fatal("released image does not report lost")
	}
}

// TestTextureImageCopy checks that a copied texture keeps its contents when
// the app draws into its own texture again, while a wrapped one follows it.
func TestTextureImageCopy(t *testing.T) {
	var tex, queue unsafe.Pointer
	err := UseDevice(func(g GPU) error {
		if DeviceGeneration() != g.Generation {
			t.Errorf("DeviceGeneration %d, GPU %d", DeviceGeneration(), g.Generation)
		}
		queue = g.Queue
		desc := darwin.SendClass("MTLTextureDescriptor", "texture2DDescriptorWithPixelFormat:width:height:mipmapped:",
			uint64(70), uint64(2), uint64(2), false) // MTLPixelFormatRGBA8Unorm
		desc.Send(darwin.Sel("setUsage:"), uint64(1|4)) // shader read, render target
		desc.Send(darwin.Sel("setStorageMode:"), uint64(2))
		tex = unsafe.Add(nil, uintptr(darwin.Send(darwin.ID(uintptr(g.Device)), "newTextureWithDescriptor:", desc)))
		clearMetal(g.Queue, tex, [4]float32{0, 1, 0, 1})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer darwin.Release(darwin.ID(uintptr(tex)))
	copied, err := NewTextureImage(tex, TextureImageOptions{Copy: true})
	if err != nil {
		t.Fatal(err)
	}
	defer copied.Release()
	live, err := NewTextureImage(tex, TextureImageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer live.Release()
	if err := UseDevice(func(g GPU) error { clearMetal(queue, tex, [4]float32{0, 0, 1, 1}); return nil }); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		img  *Image
		want color.NRGBA
	}{{copied, color.NRGBA{0, 255, 0, 255}}, {live, color.NRGBA{0, 0, 255, 255}}} {
		px, err := c.img.NRGBA()
		if err != nil {
			t.Fatal(err)
		}
		wantNRGBA(t, px.NRGBAAt(1, 1), c.want, 0)
	}
	// RenderImage draws the GPU image like any other.
	out, err := RenderImage(ImageEl(live).Size(4, 4), 4, 4, 1)
	if err != nil {
		t.Fatal(err)
	}
	wantNRGBA(t, out.NRGBAAt(2, 2), color.NRGBA{0, 0, 255, 255}, 1)
	if _, err := NewTextureImage(nil, TextureImageOptions{}); err == nil {
		t.Error("NewTextureImage without a texture succeeded")
	}
	if err := TrimDevice(); err != nil {
		t.Error(err)
	}
	if err := UseDevice(func(GPU) error { return TrimDevice() }); err == nil {
		t.Error("TrimDevice inside UseDevice succeeded")
	}
}

// A Surface's draw clears its target with Metal and the frame shows it.
func TestSurfaceRenderImage(t *testing.T) {
	draws := 0
	el := Surface(func(f SurfaceFrame) bool {
		draws++
		if f.Width != 20 || f.Height != 10 || f.Scale != 2 {
			t.Errorf("frame %dx%d scale %v", f.Width, f.Height, f.Scale)
		}
		clearFrame(f, [4]float32{0, 0, 1, 1})
		return false
	}).Size(10, 5)
	img, err := RenderImage(Box(el).Size(20, 10).Fill(Hex(0xff0000)), 20, 10, 2)
	if err != nil {
		t.Fatal(err)
	}
	if draws != 1 {
		t.Errorf("%d draws", draws)
	}
	wantNRGBA(t, img.NRGBAAt(5, 5), color.NRGBA{0, 0, 255, 255}, 1)
	wantNRGBA(t, img.NRGBAAt(35, 15), color.NRGBA{255, 0, 0, 255}, 1)
}

// A retained Surface draws again only for a new version, InvalidateSurface,
// or after asking to; a surface that left the tree loses its target.
func TestSurfaceRetain(t *testing.T) {
	version := uint64(1)
	show := true
	again := false
	draws := 0
	w := newTestWindow(t, func() *El {
		if !show {
			return Box()
		}
		return Surface(func(f SurfaceFrame) bool {
			draws++
			clearFrame(f, [4]float32{1, 1, 0, 1})
			return again
		}).Retain(version).Key("s").Size(40, 30)
	})
	frame := func() {
		Do(func() {
			w.markDirty(dirtyUpdate)
			w.renderFrame()
		})
	}
	frame()
	frame()
	if draws != 1 {
		t.Fatalf("after two frames: %d draws, want 1", draws)
	}
	version = 2
	frame()
	if draws != 2 {
		t.Fatalf("new version: %d draws, want 2", draws)
	}
	Do(func() { w.InvalidateSurface("s") })
	frame()
	frame()
	if draws != 3 {
		t.Fatalf("invalidated: %d draws, want 3", draws)
	}
	again = true
	Do(func() { w.InvalidateSurface("s") })
	frame()
	var animating bool
	Do(func() { animating = w.animating })
	if !animating {
		t.Error("a draw that asked for another frame did not keep the window animating")
	}
	again = false
	frame()
	frame()
	if draws != 5 {
		t.Fatalf("asked again: %d draws, want 5", draws)
	}
	var n int
	Do(func() { n = len(w.surfaces) })
	if n != 1 {
		t.Fatalf("%d surfaces", n)
	}
	show = false
	start := time.Now()
	frame()
	Do(func() { n = len(w.surfaces) })
	if n != 0 {
		t.Fatalf("%d surfaces after removing the element", n)
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Logf("hidden window frame took %v", d)
	}
}
