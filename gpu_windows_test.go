package fairy

import (
	"image/color"
	"os"
	"os/exec"
	"testing"

	"github.com/myparsleycat/fairy/d3d"
	"github.com/myparsleycat/fairy/internal/render"
)

func TestSurfaceNeedsDraw(t *testing.T) {
	v := func(n uint64) *uint64 { return &n }
	drawn := surfaceEntry{drawn: true, version: 3, scale: 1.5, gen: 2}
	cases := []struct {
		name        string
		se          surfaceEntry
		retain      *uint64
		scale       float32
		gen         uint64
		invalidated bool
		want        bool
	}{
		{"not retained", drawn, nil, 1.5, 2, false, true},
		{"unchanged", drawn, v(3), 1.5, 2, false, false},
		{"new version", drawn, v(4), 1.5, 2, false, true},
		{"never drawn", surfaceEntry{version: 3, scale: 1.5, gen: 2}, v(3), 1.5, 2, false, true},
		{"scale changed", drawn, v(3), 2, 2, false, true},
		{"device recreated", drawn, v(3), 1.5, 3, false, true},
		{"invalidated", drawn, v(3), 1.5, 2, true, true},
		{"asked again", surfaceEntry{drawn: true, again: true, version: 3, scale: 1.5, gen: 2}, v(3), 1.5, 2, false, true},
	}
	for _, c := range cases {
		if got := surfaceNeedsDraw(&c.se, c.retain, c.scale, c.gen, c.invalidated); got != c.want {
			t.Errorf("%s: surfaceNeedsDraw = %v, want %v", c.name, got, c.want)
		}
	}
}

func clearFrame(f SurfaceFrame, c [4]float32) {
	_, rtv := f.RenderTarget()
	f.GPU().Context.ClearRenderTargetView(rtv, c)
}

// TestSurfaceImageReadback renders offscreen without a window and reads the
// premultiplied target back as straight alpha.
func TestSurfaceImageReadback(t *testing.T) {
	img, err := SurfaceImage(8, 4, 1, func(f SurfaceFrame) {
		if f.Width != 8 || f.Height != 4 || f.Generation == 0 {
			t.Errorf("frame %dx%d gen %d", f.Width, f.Height, f.Generation)
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
	got := px.NRGBAAt(3, 2)
	if got.R < 253 || got.G != 0 || got.B != 0 || got.A < 127 || got.A > 128 {
		t.Fatalf("pixel %v, want straight red at half alpha", got)
	}
	if img.Lost() {
		t.Fatal("fresh image reports lost")
	}
	img.Release()
	if !img.Lost() {
		t.Fatal("released image does not report lost")
	}
}

// TestTextureImageCopy checks that a copied texture keeps its contents when
// the app draws into its own texture again, while a wrapped one follows it.
func TestTextureImageCopy(t *testing.T) {
	var tex d3d.Texture2D
	var rtv d3d.RenderTargetView
	err := UseDevice(func(g GPU) error {
		var err error
		tex, err = g.Device.CreateTexture2D(&d3d.Texture2DDesc{
			Width: 2, Height: 2, MipLevels: 1, ArraySize: 1,
			Format: d3d.FORMAT_R8G8B8A8_UNORM, SampleDesc: d3d.SampleDesc{Count: 1},
			BindFlags: d3d.BIND_SHADER_RESOURCE | d3d.BIND_RENDER_TARGET,
		}, nil)
		if err != nil {
			return err
		}
		if rtv, err = g.Device.CreateRenderTargetView(tex, nil); err != nil {
			return err
		}
		g.Context.ClearRenderTargetView(rtv, [4]float32{0, 1, 0, 1})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer tex.Release()
	defer rtv.Release()
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
	if err := UseDevice(func(g GPU) error {
		g.Context.ClearRenderTargetView(rtv, [4]float32{0, 0, 1, 1})
		return nil
	}); err != nil {
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
		if got := px.NRGBAAt(1, 1); got != c.want {
			t.Errorf("pixel %v, want %v", got, c.want)
		}
	}
}

func TestTrimDevice(t *testing.T) {
	// A fresh process isolates the shared device from other GPU tests.
	if os.Getenv("FAIRY_TEST_TRIM_DEVICE_CHILD") != "1" {
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(exe, "-test.run=^TestTrimDevice$")
		cmd.Env = append(os.Environ(), "FAIRY_TEST_TRIM_DEVICE_CHILD=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("TrimDevice subprocess: %v\n%s", err, out)
		}
		return
	}
	if err := TrimDevice(); err != nil {
		t.Fatalf("TrimDevice without a device: %v", err)
	}
	if gen := render.CurrentGen(); gen != 0 {
		t.Fatalf("TrimDevice made a device, generation %d", gen)
	}
	dev, err := render.AcquireDevice()
	if err != nil {
		t.Fatal(err)
	}
	err = TrimDevice()
	dev.Release()
	if err != nil {
		t.Fatalf("TrimDevice: %v", err)
	}
	if gen := render.CurrentGen(); gen != 0 {
		t.Fatalf("TrimDevice retained the released device, generation %d", gen)
	}
	if err := UseDevice(func(GPU) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := TrimDevice(); err != nil {
		t.Fatalf("TrimDevice: %v", err)
	}
	err = UseDevice(func(GPU) error { return TrimDevice() })
	if err == nil {
		t.Fatal("TrimDevice inside UseDevice returned nil")
	}
}
