package fairy

import (
	"runtime"
	"sync"

	"github.com/myparsleycat/fairy/internal/imaging"
	"github.com/myparsleycat/fairy/internal/render"
)

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
	if !onUIThread() && enqueueAppCall(appCall{run: free, cancel: free}, false) {
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
