//go:build windows

package render

import (
	"runtime"
	"sync"
	"testing"
	"unsafe"

	"github.com/myparsleycat/fairy/d3d"
	"github.com/myparsleycat/fairy/internal/com"
	"golang.org/x/sys/windows"
)

// d3d11SDKVersion is D3D11_SDK_VERSION.
const d3d11SDKVersion = 7

func warpDevice(t *testing.T) *Device {
	t.Helper()
	return warpDeviceLevel(t, d3d.FEATURE_LEVEL_11_0)
}

func warpDeviceLevel(t *testing.T, level uint32) *Device {
	t.Helper()
	create := windows.NewLazySystemDLL("d3d11.dll").NewProc("D3D11CreateDevice")
	var dev, ctx unsafe.Pointer
	hr, _, _ := create.Call(0, d3d.DRIVER_TYPE_WARP, 0, d3d.CREATE_DEVICE_BGRA_SUPPORT,
		uintptr(unsafe.Pointer(&level)), 1, d3d11SDKVersion,
		uintptr(unsafe.Pointer(&dev)), 0, uintptr(unsafe.Pointer(&ctx)))
	if err := com.HRESULT(int32(hr)).Err("create WARP test device"); err != nil {
		t.Fatal(err)
	}
	mt, err := com.QueryInterface(dev, &iidID3D10Multithread)
	if err != nil {
		com.Release(ctx)
		com.Release(dev)
		t.Fatal(err)
	}
	com.Call(mt, 5, 1)
	d := &Device{Dev: d3d.Device{P: dev}, Ctx: d3d.Context{P: ctx}, Info: d3d.DeviceInfo{DriverType: d3d.DRIVER_TYPE_WARP, Software: true, FeatureLevel: level}, mt: mt}
	t.Cleanup(func() {
		com.Release(mt)
		d.Ctx.ClearState()
		d.Ctx.Release()
		d.Dev.Release()
	})
	return d
}

func TestTextureConcurrentMips(t *testing.T) {
	d := warpDevice(t)
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
	d := warpDevice(t)
	pixels := make([]byte, 4*4*4)
	for p := range 16 {
		copy(pixels[p*4:], []byte{60, 120, 240, 255})
	}
	tex, err := NewTexture(d, 4, 4, pixels, false)
	if err != nil {
		t.Fatal(err)
	}
	defer tex.Release()
	original := tex.Resource()
	tex.Width = 1 << 30
	if err := tex.EnsureMips(d); err == nil {
		t.Fatal("invalid mip texture dimensions succeeded")
	}
	tex.Width = 4
	if tex.Resource() != original || !tex.CanMip() {
		t.Fatal("failed promotion changed the texture")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	d.Lock()
	defer d.Unlock()
	got, err := d.Ctx.ReadImage(tex.Resource(), true)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Pix) != string(pixels) {
		t.Fatal("failed promotion changed the pixels")
	}
}

func TestTextureDrawDuringPromotion(t *testing.T) {
	d := warpDevice(t)
	r := &Renderer{width: 8, height: 8, ds: d, dev: d.Dev, ctx: d.Ctx}
	var err error
	if r.target, err = NewTarget(d, 8, 8); err != nil {
		t.Fatal(err)
	}
	r.rtv = r.target.rtv
	t.Cleanup(func() { r.ds = nil; r.Release() })
	if err := r.init(); err != nil {
		t.Fatal(err)
	}
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
