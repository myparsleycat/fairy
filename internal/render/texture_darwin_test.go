//go:build darwin

package render

import (
	"sync"
	"testing"
)

func testDevice(t *testing.T) *Device {
	t.Helper()
	d, err := AcquireDevice()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Release)
	return d
}

func TestTextureConcurrentMips(t *testing.T) {
	d := testDevice(t)
	for range 16 {
		tex, err := NewTexture(d, 128, 128, make([]byte, 128*128*4), false)
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for range 2 {
			wg.Go(func() { <-start; errs <- tex.EnsureMips(d) })
		}
		close(start)
		wg.Wait()
		for range 2 {
			if err := <-errs; err != nil {
				t.Error(err)
			}
		}
		if tex.CanMip() {
			t.Error("texture has no mip chain after promotion")
		}
		tex.Release()
	}
}

func TestTextureFailedMipsKeepsPixels(t *testing.T) {
	d := testDevice(t)
	pixels := make([]byte, 4*4*4)
	for p := range 16 {
		copy(pixels[p*4:], []byte{60, 120, 240, 255})
	}
	tex, err := NewTexture(d, 4, 4, pixels, false)
	if err != nil {
		t.Fatal(err)
	}
	defer tex.Release()
	original := tex.resource()
	tex.Width = 1 << 30
	if err := tex.EnsureMips(d); err == nil {
		t.Fatal("invalid mip texture dimensions succeeded")
	}
	tex.Width = 4
	if tex.resource() != original || !tex.CanMip() {
		t.Fatal("failed promotion changed the texture")
	}
	got, err := readTexture(d, tex.resource(), 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(pixels) {
		t.Fatal("failed promotion changed the pixels")
	}
}

func TestTextureDrawDuringPromotion(t *testing.T) {
	r := offscreen(t, 8, 8)
	d := r.Device()
	for range 8 {
		pixels := make([]byte, 128*128*4)
		for p := range 128 * 128 {
			copy(pixels[p*4:], []byte{128, 64, 0, 255})
		}
		tex, err := NewTexture(d, 128, 128, pixels, false)
		if err != nil {
			t.Fatal(err)
		}
		var scene Scene
		scene.AddImage(Prim{Bounds: [4]float32{0, 0, 8, 8}, Clip: NoClip, UV: [4]float32{0, 0, 1, 1}, Color: [4]float32{1, 1, 1, 1}, Flags: FlagPremultiplied}, tex)
		start := make(chan struct{})
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		wg.Go(func() { <-start; errs <- tex.EnsureMips(d) })
		wg.Go(func() {
			<-start
			var drawErr error
			for range 8 {
				if drawErr = r.Draw(&scene, [4]float32{}); drawErr != nil {
					break
				}
			}
			errs <- drawErr
		})
		close(start)
		wg.Wait()
		for range 2 {
			if err := <-errs; err != nil {
				t.Error(err)
			}
		}
		got, err := r.ReadTarget()
		if err != nil {
			t.Fatal(err)
		}
		px := got.NRGBAAt(4, 4)
		if px.R != 128 || px.G != 64 || px.A != 255 {
			t.Fatalf("draw during promotion changed pixels: %v", px)
		}
		if tex.ByteSize() != 128*128*4+64*64*4+32*32*4+16*16*4+8*8*4+4*4*4+2*2*4+4 {
			t.Fatal("mip texel storage was miscounted")
		}
		tex.Release()
	}
}

// A texture drawn at a quarter of its size samples its mips once promoted,
// so a fine checkerboard averages out instead of aliasing.
func TestTextureMipsFilterReduction(t *testing.T) {
	r := offscreen(t, 8, 8)
	const n = 64
	pixels := make([]byte, n*n*4)
	for y := range n {
		for x := range n {
			if (x+y)%2 == 0 {
				copy(pixels[(y*n+x)*4:], []byte{255, 255, 255, 255})
			} else {
				copy(pixels[(y*n+x)*4:], []byte{0, 0, 0, 255})
			}
		}
	}
	tex, err := r.NewTexture(n, n, pixels, false)
	if err != nil {
		t.Fatal(err)
	}
	defer tex.Release()
	if err := tex.EnsureMips(r.Device()); err != nil {
		t.Fatal(err)
	}
	var s Scene
	s.AddImage(Prim{Bounds: [4]float32{0, 0, 8, 8}, Clip: NoClip, UV: [4]float32{0, 0, 1, 1}, Color: [4]float32{1, 1, 1, 1}, Flags: FlagPremultiplied}, tex)
	img := drawReadClear(t, r, &s, [4]float32{})
	for _, p := range [][2]int{{1, 1}, {4, 4}, {6, 2}} {
		if c := img.NRGBAAt(p[0], p[1]); c.R < 112 || c.R > 144 {
			t.Errorf("pixel %v = %v, want the checkerboard's gray", p, c)
		}
	}
}
