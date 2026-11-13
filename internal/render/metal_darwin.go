//go:build darwin

package render

import (
	"errors"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego/objc"

	"github.com/myparsleycat/fairy/internal/darwin"
)

// id is an Objective-C object: a Metal or Core Animation object here.
type id = objc.ID

// Metal enumeration values used here.
const (
	pixelR8Unorm    = 10
	pixelRGBA8Unorm = 70
	pixelBGRA8Unorm = 80

	usageShaderRead   = 1
	usageRenderTarget = 4

	storagePrivate = 2

	resourceStorageShared = 0 // MTLResourceStorageModeShared in MTLResourceOptions

	loadActionLoad   = 1
	loadActionClear  = 2
	storeActionStore = 1

	primitiveTriangleStrip = 4

	blendZero               = 0
	blendOne                = 1
	blendOneMinusSourceAlpa = 5
	blendOpAdd              = 0

	cbStatusCompleted = 4
	cbStatusError     = 5

	cbErrorDeviceRemoved = 11 // MTLCommandBufferErrorDeviceRemoved

	// maxTextureSize is the largest texture side every Mac GPU supports.
	maxTextureSize = 16384
)

type mtlOrigin struct{ X, Y, Z uint64 }
type mtlSize struct{ W, H, D uint64 }
type mtlClearColor struct{ R, G, B, A float64 }
type mtlViewport struct{ X, Y, W, H, ZNear, ZFar float64 }

var (
	mtlCreateSystemDefaultDevice func() id
	mtlCopyAllDevices            func() id
	cgColorSpaceCreateWithName   func(name uintptr) uintptr
	cgColorSpaceRelease          func(cs uintptr)
	pthreadSelf                  func() uintptr
)

func init() {
	darwin.Func(&mtlCreateSystemDefaultDevice, darwin.Metal, "MTLCreateSystemDefaultDevice")
	darwin.Func(&mtlCopyAllDevices, darwin.Metal, "MTLCopyAllDevices")
	darwin.Func(&cgColorSpaceCreateWithName, darwin.CoreGraphics, "CGColorSpaceCreateWithName")
	darwin.Func(&cgColorSpaceRelease, darwin.CoreGraphics, "CGColorSpaceRelease")
	darwin.Func(&pthreadSelf, darwin.System, "pthread_self")
}

// nsError returns the description of an NSError, or a fallback.
func nsError(what string, err id) error {
	if err == 0 {
		return errors.New("render: " + what + " failed")
	}
	return errors.New("render: " + what + ": " + darwin.GoString(darwin.Send(err, "localizedDescription")))
}

// newTexture2D makes a 2D texture, or returns an error when the device cannot.
func newTexture2D(dev id, format, width, height int, mipmapped bool, usage, storage uint64) (id, error) {
	if width < 1 || height < 1 || width > maxTextureSize || height > maxTextureSize {
		return 0, errors.New("render: texture size out of range")
	}
	var tex id
	darwin.WithPool(func() {
		desc := darwin.SendClass("MTLTextureDescriptor", "texture2DDescriptorWithPixelFormat:width:height:mipmapped:",
			uint64(format), uint64(width), uint64(height), mipmapped)
		desc.Send(darwin.Sel("setUsage:"), usage)
		desc.Send(darwin.Sel("setStorageMode:"), storage)
		tex = darwin.Send(dev, "newTextureWithDescriptor:", desc)
	})
	if tex == 0 {
		return 0, errors.New("render: could not create a texture")
	}
	return tex, nil
}

// newBuffer makes a shared buffer of n bytes.
func newBuffer(dev id, n int) (id, error) {
	b := darwin.Send(dev, "newBufferWithLength:options:", uint64(max(n, 16)), uint64(resourceStorageShared))
	if b == 0 {
		return 0, errors.New("render: could not create a buffer")
	}
	return b, nil
}

// bufferBytes returns the contents of a shared buffer of n bytes.
func bufferBytes(buf id, n int) []byte {
	return unsafe.Slice((*byte)(darwin.Get[unsafe.Pointer](buf, "contents")), n)
}

// blitUpload encodes a copy of rows of data, rowBytes apart from offset in
// buf, to the w x h region at (x, y) of tex.
func blitUpload(blit, buf id, offset, rowBytes int, tex id, x, y, w, h int) {
	blit.Send(darwin.Sel("copyFromBuffer:sourceOffset:sourceBytesPerRow:sourceBytesPerImage:sourceSize:toTexture:destinationSlice:destinationLevel:destinationOrigin:"),
		buf, uint64(offset), uint64(rowBytes), uint64(rowBytes*h), mtlSize{uint64(w), uint64(h), 1},
		tex, uint64(0), uint64(0), mtlOrigin{uint64(x), uint64(y), 0})
}

// uploadTexture copies the w x h pixels of data, rowBytes apart, into tex at
// (x, y) on the device's queue, after the work already committed and before
// any committed later, and regenerates its mips when it has them.
func uploadTexture(d *Device, tex id, x, y, w, h int, data []byte, rowBytes int, mips bool) error {
	var err error
	darwin.WithPool(func() {
		buf := darwin.Send(d.dev, "newBufferWithBytes:length:options:", unsafe.Pointer(&data[0]), uint64(rowBytes*h), uint64(resourceStorageShared))
		runtime.KeepAlive(data)
		if buf == 0 {
			err = errors.New("render: could not create an upload buffer")
			return
		}
		// The command buffer keeps buf until the copy ran.
		defer darwin.Release(buf)
		cb := darwin.Send(d.queue, "commandBuffer")
		if cb == 0 {
			err = errors.New("render: could not create a command buffer")
			return
		}
		blit := darwin.Send(cb, "blitCommandEncoder")
		blitUpload(blit, buf, 0, rowBytes, tex, x, y, w, h)
		if mips {
			blit.Send(darwin.Sel("generateMipmapsForTexture:"), tex)
		}
		blit.Send(darwin.Sel("endEncoding"))
		cb.Send(darwin.Sel("commit"))
	})
	return err
}

// readTexture copies the w x h BGRA or RGBA texels of tex to memory after the
// work committed before, and waits for them.
func readTexture(d *Device, tex id, w, h int) ([]byte, error) {
	var out []byte
	var err error
	darwin.WithPool(func() {
		buf, e := newBuffer(d.dev, w*h*4)
		if e != nil {
			err = e
			return
		}
		defer darwin.Release(buf)
		cb := darwin.Send(d.queue, "commandBuffer")
		if cb == 0 {
			err = errors.New("render: could not create a command buffer")
			return
		}
		blit := darwin.Send(cb, "blitCommandEncoder")
		blitDownload(blit, tex, w, h, buf)
		blit.Send(darwin.Sel("endEncoding"))
		cb.Send(darwin.Sel("commit"))
		cb.Send(darwin.Sel("waitUntilCompleted"))
		if darwin.Get[uint64](cb, "status") != cbStatusCompleted {
			err = commandBufferError(d, cb)
			return
		}
		out = append([]byte(nil), bufferBytes(buf, w*h*4)...)
	})
	return out, err
}

// blitDownload encodes a copy of the w x h texels of tex into buf.
func blitDownload(blit, tex id, w, h int, buf id) {
	blit.Send(darwin.Sel("copyFromTexture:sourceSlice:sourceLevel:sourceOrigin:sourceSize:toBuffer:destinationOffset:destinationBytesPerRow:destinationBytesPerImage:"),
		tex, uint64(0), uint64(0), mtlOrigin{}, mtlSize{uint64(w), uint64(h), 1},
		buf, uint64(0), uint64(w*4), uint64(w*h*4))
}

// commandBufferError reports why cb failed, marking the device lost when it
// was removed.
func commandBufferError(d *Device, cb id) error {
	e := darwin.Send(cb, "error")
	if e != 0 && darwin.Get[int64](e, "code") == cbErrorDeviceRemoved {
		d.MarkLost()
		return ErrDeviceLost
	}
	return nsError("command buffer", e)
}

// toNRGBA converts BGRA texels in src to RGBA in dst, unpremultiplying
// when premultiplied is set.
func toNRGBA(dst, src []byte, premultiplied bool) {
	for i := 0; i+3 < len(src) && i+3 < len(dst); i += 4 {
		b, g, r, a := src[i], src[i+1], src[i+2], src[i+3]
		if premultiplied && a != 0 && a != 255 {
			r, g, b = unpremul(r, a), unpremul(g, a), unpremul(b, a)
		} else if premultiplied && a == 0 {
			r, g, b = 0, 0, 0
		}
		dst[i], dst[i+1], dst[i+2], dst[i+3] = r, g, b, a
	}
}

func unpremul(c, a byte) byte {
	return byte(min((uint32(c)*255+uint32(a)/2)/uint32(a), 255))
}
