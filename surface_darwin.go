package fairy

// Placeholder until the macOS backend has surfaces and GPU images: it keeps the public API of
// the Windows backend and does nothing.

import (
	"image"

	"github.com/myparsleycat/fairy/internal/render"
)

type surfaceEntry struct {
	target *render.Target
}

func (w *Window) paintSurface(e *El, box rectF, radii, clip [4]float32, opacity float32) {}
func (w *Window) sweepSurfaces()                                                         {}

// InvalidateSurface makes the Surface with key draw again on the next frame.
func (w *Window) InvalidateSurface(key string) {}

// gpuImage is an Image whose pixels live in a texture.
type gpuImage struct{ premul bool }

func (g *gpuImage) texture(gen uint64) *render.Texture { return nil }

func (g *gpuImage) readNRGBA() (*image.NRGBA, error) { return nil, errNoBackend }

// Release frees an Image's GPU texture. Other images need no release.
func (i *Image) Release() {}

// Lost reports whether the Image's texture was lost with its device.
func (i *Image) Lost() bool { return false }
