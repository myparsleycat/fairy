// Package imagecache decodes images on background goroutines and keeps the
// decoded pixels in memory up to a byte budget, evicting the least recently
// used. It also holds the pixels behind every fairy.Image.
package imagecache

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/myparsleycat/fairy/internal/imaging"
)

// Wake makes every window paint again; a cache calls it when a load finishes
// or images it held go away, so that windows request what they show again.
var Wake = func() {}

var imageSeq atomic.Uint64

// NewID returns an identity for an image whose pixels live elsewhere, such
// as in a texture.
func NewID() uint64 { return imageSeq.Add(1) }

// Image is decoded pixels, or for a Lazy image the size its decodes are
// made from.
type Image struct {
	Data    imaging.Bitmap
	ID      uint64
	Lazy    *Lazy
	Retired atomic.Bool // the cache that made it has evicted it
	blurs   imageBlurs
	owner   atomic.Value
}

// NewImage wraps decoded pixels, or returns nil for nil.
func NewImage(data *imaging.Bitmap) *Image {
	if data == nil {
		return nil
	}
	return &Image{Data: *data, ID: imageSeq.Add(1)}
}

// Owner returns the value set by SetOwner, or nil.
func (i *Image) Owner() any { return i.owner.Load() }

// SetOwner records o, the value that presents i to apps, unless one was
// recorded already. It reports whether o was recorded.
func (i *Image) SetOwner(o any) bool { return i.owner.CompareAndSwap(nil, o) }

// Size returns the image size in pixels.
func (i *Image) Size() (int, int) { return i.Data.W, i.Data.H }

// Frames returns the number of frames: 1 for a still image.
func (i *Image) Frames() int {
	if v := i.drawnDecode(); v != nil {
		return v.Frames()
	}
	return max(i.Data.FrameCount(), 1)
}

// Duration returns the length of one run of an animated image, 0 for a
// still one.
func (i *Image) Duration() time.Duration {
	if v := i.drawnDecode(); v != nil {
		return v.Duration()
	}
	return i.Data.Total
}

func (i *Image) drawnDecode() *Image {
	if i.Lazy == nil {
		return nil
	}
	return i.Lazy.Last()
}

// Animated reports whether the image has two frames or more.
func (i *Image) Animated() bool { return i.Data.FrameCount() >= 2 }

func finite(v float32) bool { return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0) }
