package fairy

import (
	"errors"
	"image"
	"runtime"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/darwin"
	"github.com/myparsleycat/fairy/internal/render"
)

// GPU is the Metal device that every window, video and surface shares, with
// the command queue the windows render with.
type GPU struct {
	// Device is the id<MTLDevice> and Queue its id<MTLCommandQueue>. Work
	// committed to Queue runs in commit order with the windows' frames, so a
	// frame committed later sees what it drew. They are borrowed: do not
	// release them.
	Device, Queue unsafe.Pointer
	// Generation numbers the device in creation order. When the device is
	// lost (an external GPU unplugged), Fairy makes a new one with a higher
	// generation; resources made on the old one no longer work. Keep the
	// generation next to cached resources and make them again when it differs.
	Generation uint64
}

// GPU returns the frame's device and queue.
func (f SurfaceFrame) GPU() GPU {
	return GPU{Device: f.Device, Queue: f.Context, Generation: f.Generation}
}

// objcPointer turns an Objective-C object into the pointer the public API
// carries. Objective-C objects are not Go memory.
func objcPointer(p uintptr) unsafe.Pointer { return unsafe.Add(nil, p) }

// metalFrame describes tex, a width x height target on dev, to a Surface.
func metalFrame(dev *render.Device, tex uintptr, width, height int, scale float32) SurfaceFrame {
	return SurfaceFrame{
		Device:     objcPointer(dev.MTLDevice()),
		Context:    objcPointer(dev.MTLCommandQueue()),
		Texture:    objcPointer(tex),
		Width:      width,
		Height:     height,
		Scale:      scale,
		Generation: dev.Gen,
	}
}

// withDevice runs f holding the shared device's lock; see UseDevice.
func withDevice(f func(dev *render.Device) error) error {
	dev, err := acquireGPU()
	if err != nil {
		return err
	}
	defer dev.Release()
	err = lockedDevice(dev, f)
	if errors.Is(err, render.ErrDeviceLost) || dev.Lost() {
		dev.MarkLost()
		wakeWindows()
	}
	return err
}

func lockedDevice(dev *render.Device, f func(dev *render.Device) error) (err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	dev.Lock()
	defer dev.Unlock()
	darwin.WithPool(func() { err = f(dev) })
	return err
}

// UseDevice runs f with the shared device while holding the device's lock,
// so the app can render with Metal outside a Surface draw function, for
// example to render thumbnails offscreen or to read a texture back. It may
// be called from any goroutine, including the UI thread and inside a Surface
// draw function or another UseDevice. f runs inside an autorelease pool.
//
// The lock is the one window rendering and video frame copies also take, so
// keep f short and do not wait in it for the UI thread (Do) or for a
// goroutine that calls UseDevice. Commit the command buffers f encodes on
// GPU.Queue before it returns, so that the frames drawn later see their
// result.
//
// When f returns an error wrapping a lost device, or the device reports it
// was removed, the device is marked lost: windows then make a new one and
// the next UseDevice call receives it with a higher Generation.
func UseDevice(f func(GPU) error) error {
	return withDevice(func(dev *render.Device) error {
		return f(GPU{Device: objcPointer(dev.MTLDevice()), Queue: objcPointer(dev.MTLCommandQueue()), Generation: dev.Gen})
	})
}

// TrimDevice exists for portability: Metal returns the memory of released
// resources itself, so it only checks that it is not called inside a
// Surface draw function or UseDevice, where it returns an error as on
// Windows. It returns nil when no device exists.
func TrimDevice() error {
	dev := render.AcquireCurrentDevice()
	if dev == nil {
		return nil
	}
	defer dev.Release()
	err := lockedDevice(dev, func(dev *render.Device) error {
		if dev.Nested() {
			return errors.New("fairy: TrimDevice inside a Surface draw function or UseDevice")
		}
		dev.Trim()
		return nil
	})
	if dev.Lost() {
		wakeWindows()
	}
	return err
}

// NewTextureImage makes an Image from an id<MTLTexture> the app rendered on
// the shared device (from UseDevice or a SurfaceFrame), so ImageEl can show
// it without reading it back to memory. The texture must be a single-sampled
// 2D texture with MTLTextureUsageShaderRead in a color format, such as
// MTLPixelFormatBGRA8Unorm or MTLPixelFormatRGBA8Unorm.
//
// The Image keeps its texture until Release, or until it is garbage
// collected. When the device is lost the Image draws nothing and Lost
// reports true; render it again on the new device.
func NewTextureImage(tex unsafe.Pointer, opts TextureImageOptions) (*Image, error) {
	if tex == nil {
		return nil, errors.New("fairy: NewTextureImage with no texture")
	}
	var t *render.Texture
	var gen uint64
	err := withDevice(func(dev *render.Device) error {
		gen = dev.Gen
		var err error
		if opts.Copy {
			t, err = render.CopyTexture(dev, uintptr(tex))
		} else {
			t, err = render.WrapTexture(dev, uintptr(tex))
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return newGPUImage(t, gen, opts.Premultiplied), nil
}

// SurfaceImage renders an offscreen Image of width x height device pixels
// with draw, the way a Surface draws: the target is a cleared
// MTLPixelFormatBGRA8Unorm texture, and draw leaves premultiplied color in
// it with command buffers it commits on the frame's queue. f.Scale is scale
// and f.Time is 0. Use it for thumbnails of 3D content; it may run on any
// goroutine and holds the device lock while draw runs (see UseDevice).
func SurfaceImage(width, height int, scale float32, draw func(SurfaceFrame)) (*Image, error) {
	if width <= 0 || height <= 0 {
		return nil, errors.New("fairy: SurfaceImage needs a positive size")
	}
	var img *Image
	err := withDevice(func(dev *render.Device) error {
		t, err := render.NewTarget(dev, width, height)
		if err != nil {
			return err
		}
		if err := render.ClearTexture(dev, t.MTLTexture(), [4]float32{}); err != nil {
			t.Release()
			return err
		}
		draw(metalFrame(dev, t.MTLTexture(), width, height, scale))
		img = newGPUImage(&t.Texture, dev.Gen, true)
		return nil
	})
	return img, err
}

// readNRGBA reads the texture back with straight alpha.
func (g *gpuImage) readNRGBA() (*image.NRGBA, error) {
	var out *image.NRGBA
	err := withDevice(func(dev *render.Device) error {
		t := g.texture(dev.Gen)
		if t == nil {
			return errors.New("fairy: image texture was released or its device lost")
		}
		var err error
		out, err = render.ReadTexture(dev, t, g.premul)
		return err
	})
	return out, err
}
