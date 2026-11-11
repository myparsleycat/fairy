package fairy

import (
	"errors"
	"image"
	"runtime"
	"sync"

	"github.com/myparsleycat/fairy/d3d"
	"github.com/myparsleycat/fairy/internal/imaging"
	"github.com/myparsleycat/fairy/internal/render"
)

// GPU is the Direct3D 11 device that every window, video and surface shares,
// with its immediate context.
type GPU struct {
	Device  d3d.Device
	Context d3d.Context
	// Generation numbers the device in creation order. When the device is
	// lost (driver update, GPU reset), Fairy makes a new one with a higher
	// generation; resources made on the old one no longer work. Keep the
	// generation next to cached resources and make them again when it differs.
	Generation uint64
}

// GPU returns the frame's device and context as d3d handles. They are
// borrowed: do not release them.
func (f SurfaceFrame) GPU() GPU {
	return GPU{Device: d3d.Device{P: f.Device}, Context: d3d.Context{P: f.Context}, Generation: f.Generation}
}

// RenderTarget returns the frame's texture and render target view as d3d
// handles, borrowed for this frame.
func (f SurfaceFrame) RenderTarget() (d3d.Texture2D, d3d.RenderTargetView) {
	return d3d.Texture2D{P: f.Texture}, d3d.RenderTargetView{P: f.Target}
}

var (
	gpuMu  sync.Mutex
	gpuDev *render.Device // kept so that work without windows does not make a device per call
)

// acquireGPU returns the shared device with a reference held for the
// caller, making a new one when there is none or it was lost.
func acquireGPU() (*render.Device, error) {
	gpuMu.Lock()
	defer gpuMu.Unlock()
	d, err := render.AcquireDevice()
	if err != nil {
		return nil, err
	}
	if d != gpuDev {
		if gpuDev != nil {
			gpuDev.Release()
		}
		gpuDev = d
		d.Retain()
	}
	return d, nil
}

// withDevice runs f holding the shared device's lock; see UseDevice.
func withDevice(f func(dev *render.Device) error) error {
	dev, err := acquireGPU()
	if err != nil {
		return err
	}
	defer dev.Release()
	err = lockedDevice(dev, f)
	if d3d.DeviceLost(err) || dev.Dev.RemovedReason() != nil {
		dev.MarkLost()
		wakeWindows()
	}
	return err
}

func lockedDevice(dev *render.Device, f func(dev *render.Device) error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	dev.Lock()
	defer dev.Unlock()
	defer func() {
		if !dev.Nested() {
			dev.Ctx.ClearState()
		}
	}()
	return f(dev)
}

// UseDevice runs f with the shared device while holding the device's lock,
// so the immediate context can be used outside a Surface draw function, for
// example to render thumbnails offscreen or to read a texture back. It may
// be called from any goroutine, including the UI thread and inside a Surface
// draw function or another UseDevice.
//
// The lock is the device's ID3D10Multithread critical section, which window
// rendering and video decoding also take, so keep f short and do not wait in
// it for the UI thread (Do) or for a goroutine that calls UseDevice. Pipeline
// state f sets is cleared when it returns, unless the call is nested in a
// Surface draw function or another UseDevice; a nested f leaves the state
// it set, so the enclosing code sets what it needs again.
//
// When f's error, or the device, reports a lost device (d3d.DeviceLost), the
// device is marked lost: windows then make a new one and the next UseDevice
// call receives it with a higher Generation.
func UseDevice(f func(GPU) error) error {
	return withDevice(func(dev *render.Device) error {
		return f(GPU{Device: dev.Dev, Context: dev.Ctx, Generation: dev.Gen})
	})
}

// TrimDevice asks the graphics driver to free the memory it still keeps for
// resources the app has released. Drivers often hold the system memory of
// released vertex, index and staging buffers for reuse; call TrimDevice after
// releasing many or large resources, such as when closing a 3D view, to
// return it. Releasing every handle first is the caller's job: memory of
// resources still referenced stays allocated.
//
// It may be called from any goroutine, but not inside a Surface draw function
// or UseDevice, where it returns an error: trimming clears the pipeline state
// the enclosing code is using. It takes the device lock (see UseDevice) and
// clears the pipeline state and flushes the context before trimming. It
// returns nil and does nothing when no device exists or the device does not
// support trimming.
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
		dev.Ctx.ClearState()
		dev.Ctx.Flush()
		dev.Trim()
		return nil
	})
	if dev.Lost() {
		wakeWindows()
	}
	return err
}

// DeviceGeneration returns the Generation of the shared device, or 0 when no
// device exists or it was lost and not yet made again.
func DeviceGeneration() uint64 { return render.CurrentGen() }

// gpuImage is an Image whose pixels live in a texture.
type gpuImage struct {
	mu      sync.Mutex
	tex     *render.Texture
	gen     uint64
	premul  bool
	cleanup runtime.Cleanup
}

// TextureImageOptions configures NewTextureImage.
type TextureImageOptions struct {
	// Premultiplied says the texture holds premultiplied alpha, as Surface
	// targets and most 3D renders do. Otherwise color is straight alpha.
	Premultiplied bool
	// Copy copies the texture's current contents into a texture the Image
	// owns, so the app can keep drawing into its own. Without Copy the Image
	// shows the app's texture live: it holds a reference to it, and later
	// draws into the texture appear on the next paint. Only copied textures
	// get mipmaps when drawn at half their size or less.
	Copy bool
}

// NewTextureImage makes an Image from an ID3D11Texture2D the app rendered
// on the shared device (from UseDevice or a SurfaceFrame), so ImageEl can
// show it without reading it back to memory. The texture must be
// single-sampled, made with BIND_SHADER_RESOURCE, and in a format a pixel
// shader samples as color (UNORM, UNORM_SRGB or FLOAT, such as
// FORMAT_B8G8R8A8_UNORM or FORMAT_R8G8B8A8_UNORM).
//
// The Image keeps its texture until Release, or until it is garbage
// collected. When the device is lost the Image draws nothing and Lost
// reports true; render it again on the new device.
func NewTextureImage(tex d3d.Texture2D, opts TextureImageOptions) (*Image, error) {
	if tex.P == nil {
		return nil, errors.New("fairy: NewTextureImage with no texture")
	}
	var t *render.Texture
	var gen uint64
	err := withDevice(func(dev *render.Device) error {
		gen = dev.Gen
		var err error
		if opts.Copy {
			t, err = render.CopyTexture(dev, tex)
		} else {
			t, err = render.WrapTexture(dev, tex)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return newGPUImage(t, gen, opts.Premultiplied), nil
}

func newGPUImage(t *render.Texture, gen uint64, premul bool) *Image {
	g := &gpuImage{tex: t, gen: gen, premul: premul}
	img := &Image{id: imageSeq.Add(1), data: imaging.Bitmap{W: t.Width, H: t.Height}, gpu: g}
	g.cleanup = runtime.AddCleanup(img, func(g *gpuImage) { g.release() }, g)
	return img
}

// release frees the texture on the UI thread when one runs, so a frame
// being recorded there never sees it half released.
func (g *gpuImage) release() {
	free := func() {
		g.mu.Lock()
		t := g.tex
		g.tex = nil
		g.mu.Unlock()
		t.Release()
	}
	if !onUIThread() && enqueueAppCall(appCall{run: free, cancel: free}, msgAppCall) {
		return
	}
	free()
}

// texture returns the texture to draw for the device generation gen, or nil
// when the Image was released or belongs to another device.
func (g *gpuImage) texture(gen uint64) *render.Texture {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.gen != gen {
		return nil
	}
	return g.tex
}

// SurfaceImage renders an offscreen Image of width x height device pixels
// with draw, the way a Surface draws: the target is a cleared
// FORMAT_B8G8R8A8_UNORM texture, draw leaves premultiplied color in it, and
// pipeline state is cleared afterwards. f.Scale is scale and f.Time is 0.
// Use it for thumbnails of 3D content; it may run on any goroutine and holds
// the device lock while draw runs (see UseDevice).
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
		defer t.Release()
		dev.Ctx.ClearRenderTargetView(t.View(), [4]float32{})
		draw(SurfaceFrame{
			Device:     dev.Dev.P,
			Context:    dev.Ctx.P,
			Texture:    t.Resource().P,
			Target:     t.View().P,
			Width:      width,
			Height:     height,
			Scale:      scale,
			Generation: dev.Gen,
		})
		dev.Ctx.ClearState()
		wrapped, err := render.WrapTexture(dev, t.Resource())
		if err != nil {
			return err
		}
		img = newGPUImage(wrapped, dev.Gen, true)
		return nil
	})
	return img, err
}

// Release frees the texture of an Image made by NewTextureImage or
// SurfaceImage at once instead of when it is garbage collected. Elements
// still showing it draw nothing. It does nothing for other images.
func (i *Image) Release() {
	if i == nil || i.gpu == nil {
		return
	}
	i.gpu.cleanup.Stop()
	i.gpu.release()
}

// Lost reports whether the Image's texture was made on a device that has
// since been lost, or was released, so it no longer draws.
func (i *Image) Lost() bool {
	if i == nil || i.gpu == nil {
		return false
	}
	return i.gpu.texture(render.CurrentGen()) == nil
}

// NRGBA returns a copy of the image's pixels with straight alpha. For an
// Image backed by a texture it reads the texture back, which needs a
// B8G8R8A8 or R8G8B8A8 format; an animated image returns its first frame.
// An image an ImageCache loaded without a size limit is decoded again at
// its natural size.
func (i *Image) NRGBA() (*image.NRGBA, error) {
	if i.gpu == nil {
		if i.lazy != nil {
			full, err := i.lazy.decodeFull()
			if err != nil {
				return nil, err
			}
			i = full
		}
		img := image.NewNRGBA(image.Rect(0, 0, i.data.W, i.data.H))
		imaging.UnpremultiplyRGBA(img.Pix, i.data.Pixels)
		return img, nil
	}
	var out *image.NRGBA
	err := UseDevice(func(g GPU) error {
		t := i.gpu.texture(g.Generation)
		if t == nil {
			return errors.New("fairy: image texture was released or its device lost")
		}
		var err error
		out, err = g.Context.ReadImage(t.Resource(), i.gpu.premul)
		return err
	})
	return out, err
}
