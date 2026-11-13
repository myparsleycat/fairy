//go:build darwin

package render

import (
	"errors"
	"fmt"
	"image"

	"github.com/myparsleycat/fairy/internal/darwin"
)

// Metal values used for textures the app or a decoder made.
const (
	textureType2D = 2

	pixelRGBA8UnormSRGB = 71
	pixelBGRA8UnormSRGB = 81

	// Depth and stencil formats lie in this range of MTLPixelFormat.
	pixelDepthFirst = 250
	pixelDepthLast  = 262
)

// MTLDevice returns the device's id<MTLDevice>.
func (d *Device) MTLDevice() uintptr { return uintptr(d.dev) }

// MTLCommandQueue returns the id<MTLCommandQueue> every window renders
// with. Work committed to it runs in commit order with the windows' frames.
func (d *Device) MTLCommandQueue() uintptr { return uintptr(d.queue) }

// textureInfo checks that tex is a single-sampled 2D color texture on d that
// shaders can read, and returns its size and format.
func textureInfo(d *Device, tex id) (w, h, format int, err error) {
	if tex == 0 {
		return 0, 0, 0, errors.New("render: no texture")
	}
	withPool(func() {
		switch {
		case darwin.Send(tex, "device") != d.dev:
			err = errors.New("render: texture belongs to another Metal device")
		case darwin.Get[uint64](tex, "textureType") != textureType2D:
			err = errors.New("render: texture is not 2D")
		case darwin.Get[uint64](tex, "sampleCount") > 1:
			err = errors.New("render: texture is multisampled")
		case darwin.Get[uint64](tex, "usage")&usageShaderRead == 0:
			err = errors.New("render: texture lacks MTLTextureUsageShaderRead")
		}
		w = int(darwin.Get[uint64](tex, "width"))
		h = int(darwin.Get[uint64](tex, "height"))
		format = int(darwin.Get[uint64](tex, "pixelFormat"))
	})
	if err == nil && format >= pixelDepthFirst && format <= pixelDepthLast {
		err = errors.New("render: texture has a depth or stencil format")
	}
	return w, h, format, err
}

// WrapTexture makes a Texture that draws tex, an id<MTLTexture> made on d,
// as it is when each frame is drawn. It holds a reference to tex.
func WrapTexture(d *Device, tex uintptr) (*Texture, error) {
	w, h, format, err := textureInfo(d, id(tex))
	if err != nil {
		return nil, err
	}
	return &Texture{tex: darwin.Retain(id(tex)), Width: w, Height: h, Format: uint32(format)}, nil
}

// CopyTexture copies tex, an id<MTLTexture> made on d, into a new texture of
// the same size and format. The copy runs on the device's queue after the
// work committed before. Hold the device lock around it.
func CopyTexture(d *Device, tex uintptr) (*Texture, error) {
	w, h, format, err := textureInfo(d, id(tex))
	if err != nil {
		return nil, err
	}
	out, err := newTexture2D(d.dev, format, w, h, false, usageShaderRead, storagePrivate)
	if err != nil {
		return nil, err
	}
	f, err := copyTexture(d, id(tex), out, w, h)
	if err != nil {
		darwin.Release(out)
		return nil, err
	}
	f.Release()
	return &Texture{tex: out, Width: w, Height: h, Format: uint32(format), owned: true}, nil
}

// Fence tracks a command buffer committed to the device's queue.
type Fence struct{ cb id }

// Done reports whether the GPU finished the work, or it failed.
func (f *Fence) Done() bool {
	return f == nil || f.cb == 0 || darwin.Get[uint64](f.cb, "status") >= cbStatusCompleted
}

// Wait waits until the GPU finished the work.
func (f *Fence) Wait() {
	if f != nil && f.cb != 0 {
		f.cb.Send(darwin.Sel("waitUntilCompleted"))
	}
}

// Release drops the fence's reference to the command buffer.
func (f *Fence) Release() {
	if f != nil {
		darwin.Release(f.cb)
		f.cb = 0
	}
}

// CopyToTexture copies src, an id<MTLTexture> of dst's size and format,
// into dst on the device's queue, after the work committed before and before
// the frames drawn after. The fence tells when the GPU no longer reads src.
// Hold the device lock around it.
func CopyToTexture(d *Device, src uintptr, dst *Texture) (*Fence, error) {
	dst.mu.RLock()
	defer dst.mu.RUnlock()
	if src == 0 || dst.tex == 0 {
		return nil, errors.New("render: copy without a texture")
	}
	var w, h, format uint64
	withPool(func() {
		w = darwin.Get[uint64](id(src), "width")
		h = darwin.Get[uint64](id(src), "height")
		format = darwin.Get[uint64](id(src), "pixelFormat")
	})
	if int(w) != dst.Width || int(h) != dst.Height || uint32(format) != dst.Format {
		return nil, errors.New("render: copy between textures of different sizes or formats")
	}
	return copyTexture(d, id(src), dst.tex, dst.Width, dst.Height)
}

func copyTexture(d *Device, src, dst id, w, h int) (*Fence, error) {
	var f *Fence
	var err error
	withPool(func() {
		cb := darwin.Send(d.queue, "commandBuffer")
		if cb == 0 {
			err = errors.New("render: could not create a command buffer")
			return
		}
		blit := darwin.Send(cb, "blitCommandEncoder")
		blit.Send(darwin.Sel("copyFromTexture:sourceSlice:sourceLevel:sourceOrigin:sourceSize:toTexture:destinationSlice:destinationLevel:destinationOrigin:"),
			src, uint64(0), uint64(0), mtlOrigin{}, mtlSize{uint64(w), uint64(h), 1},
			dst, uint64(0), uint64(0), mtlOrigin{})
		blit.Send(darwin.Sel("endEncoding"))
		cb.Send(darwin.Sel("commit"))
		f = &Fence{cb: darwin.Retain(cb)}
	})
	return f, err
}

// ClearTexture clears tex, an id<MTLTexture> that can be rendered to, to c,
// premultiplied, on the device's queue after the work committed before.
// Hold the device lock around it.
func ClearTexture(d *Device, tex uintptr, c [4]float32) error {
	var err error
	withPool(func() {
		cb := darwin.Send(d.queue, "commandBuffer")
		if cb == 0 {
			err = errors.New("render: could not create a command buffer")
			return
		}
		darwin.Send(cb, "renderCommandEncoderWithDescriptor:", clearPass(id(tex), c)).Send(darwin.Sel("endEncoding"))
		cb.Send(darwin.Sel("commit"))
	})
	return err
}

// ReadTexture reads t back after the work committed before, as straight
// alpha when premultiplied is set. It reads 8-bit RGBA and BGRA formats.
// Hold the device lock around it.
func ReadTexture(d *Device, t *Texture, premultiplied bool) (*image.NRGBA, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.tex == 0 {
		return nil, errors.New("render: texture was released")
	}
	bgra := false
	switch t.Format {
	case pixelBGRA8Unorm, pixelBGRA8UnormSRGB:
		bgra = true
	case pixelRGBA8Unorm, pixelRGBA8UnormSRGB:
	default:
		return nil, fmt.Errorf("render: cannot read back pixel format %d", t.Format)
	}
	pix, err := readTexture(d, t.tex, t.Width, t.Height)
	if err != nil {
		return nil, err
	}
	if !bgra {
		for i := 0; i+3 < len(pix); i += 4 {
			pix[i], pix[i+2] = pix[i+2], pix[i]
		}
	}
	img := image.NewNRGBA(image.Rect(0, 0, t.Width, t.Height))
	toNRGBA(img.Pix, pix, premultiplied)
	return img, nil
}
