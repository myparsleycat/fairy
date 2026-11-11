package fairy

import (
	"image"
	"sync/atomic"
	"time"

	"github.com/myparsleycat/fairy/internal/imaging"
)

type Fit uint8

const (
	FitCover Fit = iota
	FitContain
	FitFill
)

var imageSeq atomic.Uint64

// Image holds decoded pixels and the state used to display them.
type Image struct {
	data    imaging.Bitmap
	id      uint64
	gpu     *gpuImage
	start   time.Time
	blurs   imageBlurs
	lazy    *lazyImage
	retired atomic.Bool
}

func wrapImage(data *imaging.Bitmap) *Image {
	if data == nil {
		return nil
	}
	return &Image{data: *data, id: imageSeq.Add(1)}
}

// NewImage converts any image.Image.
func NewImage(src image.Image) *Image { return wrapImage(imaging.New(src)) }

// DecodeImage decodes PNG, JPEG, GIF or WebP bytes. Still images are rotated
// as their EXIF orientation says, and colors with an embedded ICC profile
// are converted to sRGB. Animated GIF and WebP images keep every frame and
// play when drawn; when their frames would take more than 64 MiB, the
// frames are decoded as they play. Images larger than 64 megapixels are
// rejected. Register more formats (AVIF, for one) with image.RegisterFormat.
func DecodeImage(data []byte) (*Image, error) {
	decoded, err := imaging.DecodeImage(data)
	return wrapImage(decoded), err
}

// Size returns the image size in pixels.
func (i *Image) Size() (int, int) { return i.data.W, i.data.H }

// ImageEl draws img. Without an explicit size it takes the image's pixel size.
func ImageEl(img *Image) *El {
	e := newEl(kindImage)
	e.image = img
	return e
}

// Fit sets how an image element fills its box.
func (e *El) Fit(f Fit) *El { e.fit = f; return e }
