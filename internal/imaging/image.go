package imaging

import (
	"bytes"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"time"
)

// Bitmap is a decoded image in premultiplied RGBA. An animated Bitmap holds
// its frames in FrameData, or in Stream when they are decoded as it plays.
type Bitmap struct {
	W, H      int
	Pixels    []byte
	FrameData []animFrame
	Total     time.Duration
	Plays     int
	Stream    *animStream
}

// New converts any image.Image.
func New(src image.Image) *Bitmap {
	w, h, pix := premulPixels(src)
	return &Bitmap{W: w, H: h, Pixels: pix}
}

// premulPixels returns src as tightly packed premultiplied RGBA at the
// origin, reusing the pixels of such an *image.RGBA.
func premulPixels(src image.Image) (int, int, []byte) {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	switch s := src.(type) {
	case *image.RGBA:
		if s.Stride == w*4 && b.Min == (image.Point{}) {
			return w, h, s.Pix
		}
	case *image.NRGBA:
		pix := make([]byte, w*h*4)
		for y := range h {
			premultiplyRGBA(pix[y*w*4:][:w*4], s.Pix[s.PixOffset(b.Min.X, b.Min.Y+y):][:w*4])
		}
		return w, h, pix
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Rect, src, b.Min, draw.Src)
	return w, h, dst.Pix
}

// premultiplyRGBA copies straight-alpha RGBA src into dst premultiplied.
func premultiplyRGBA(dst, src []byte) {
	for i := 0; i+3 < len(src); i += 4 {
		switch a := uint32(src[i+3]); a {
		case 255:
			copy(dst[i:i+4], src[i:i+4])
		case 0:
			dst[i], dst[i+1], dst[i+2], dst[i+3] = 0, 0, 0, 0
		default:
			dst[i] = byte((uint32(src[i])*a + 127) / 255)
			dst[i+1] = byte((uint32(src[i+1])*a + 127) / 255)
			dst[i+2] = byte((uint32(src[i+2])*a + 127) / 255)
			dst[i+3] = byte(a)
		}
	}
}

// UnpremultiplyRGBA copies premultiplied RGBA src into dst with straight alpha.
func UnpremultiplyRGBA(dst, src []byte) {
	for i := 0; i+3 < len(src); i += 4 {
		switch a := uint32(src[i+3]); a {
		case 255:
			copy(dst[i:i+4], src[i:i+4])
		case 0:
			dst[i], dst[i+1], dst[i+2], dst[i+3] = 0, 0, 0, 0
		default:
			dst[i] = byte(min(255, (uint32(src[i])*255+a/2)/a))
			dst[i+1] = byte(min(255, (uint32(src[i+1])*255+a/2)/a))
			dst[i+2] = byte(min(255, (uint32(src[i+2])*255+a/2)/a))
			dst[i+3] = byte(a)
		}
	}
}

// toNRGBA returns src as a tightly packed NRGBA image at the origin.
func toNRGBA(src image.Image) *image.NRGBA {
	b := src.Bounds()
	nrgba, ok := src.(*image.NRGBA)
	if !ok || nrgba.Stride != b.Dx()*4 || b.Min != (image.Point{}) {
		var dst draw.Image
		dst, nrgba = newNRGBATarget(src, b.Dx(), b.Dy())
		draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	}
	return nrgba
}

// newNRGBATarget returns an image to draw src into and the NRGBA view of its
// pixels. For an opaque src it is an RGBA image: opaque pixels have the same
// bytes premultiplied and straight, and image/draw converts YCbCr, Gray and
// CMYK quickly only into RGBA, writing NRGBA one pixel at a time.
func newNRGBATarget(src image.Image, w, h int) (draw.Image, *image.NRGBA) {
	r := image.Rect(0, 0, w, h)
	if o, ok := src.(interface{ Opaque() bool }); ok && o.Opaque() {
		rgba := image.NewRGBA(r)
		return rgba, &image.NRGBA{Pix: rgba.Pix, Stride: rgba.Stride, Rect: r}
	}
	nrgba := image.NewNRGBA(r)
	return nrgba, nrgba
}

// maxAnimBytes bounds the decoded frames DecodeImage keeps for one
// animation; larger animations decode their frames as they play, or show
// their first frame when even that does not fit.
const maxAnimBytes = 64 << 20

// MaxImagePixels bounds the declared size of an image, or of an animation's
// canvas, which is checked before decoding: decoders allocate the whole image
// up front, so a small file can ask for gigabytes, and running out of memory
// ends the process.
const MaxImagePixels = 64 << 20

// DecodeImage decodes PNG, JPEG, GIF or WebP bytes. Still images are rotated
// as their EXIF orientation says, and colors with an embedded ICC profile
// are converted to sRGB. Animated GIF and WebP images keep every frame and
// play when drawn; when their frames would take more than 64 MiB, the
// frames are decoded as they play. Images larger than 64 megapixels are
// rejected. Register more formats (AVIF, for one) with image.RegisterFormat.
func DecodeImage(data []byte) (*Bitmap, error) {
	return Decode(data, Bound{}, maxAnimBytes)
}

// Decode decodes data scaled down within bound. Animations whose decoded
// frames would take more than animBytes decode their frames as they play.
func Decode(data []byte, bound Bound, animBytes int64) (*Bitmap, error) {
	input, err := Inspect(data)
	if err != nil {
		return nil, err
	}
	img, err := input.decode(bound, animBytes)
	if err != nil {
		return nil, err
	}
	return input.finish(img), nil
}

func (in Input) decode(bound Bound, animBytes int64) (*Bitmap, error) {
	animErr := in.AnimErr
	if animErr == nil && (in.Animation.Count >= 2 || in.AnimatedWebP) {
		s := in.Animation.sink(bound, animBytes)
		if s.keepAll {
			animErr = in.walkFrames(&s)
			if animErr == nil && len(s.frames) != 0 {
				return newAnimImage(s.frames, s.delays, in.Animation.plays), nil
			}
		} else {
			img, err := in.streamImage(bound, animBytes)
			if err == nil {
				return img, nil
			}
			animErr = err
		}
	}
	src, _, err := image.Decode(bytes.NewReader(in.data))
	if err != nil {
		if animErr != nil {
			return nil, animErr
		}
		return nil, err
	}
	return Resize(src, in.srcBound(bound)), nil
}

func Resize(src image.Image, bound Bound) *Bitmap {
	b := src.Bounds()
	w, h := bound.Size(b.Dx(), b.Dy())
	return ResizeTo(src, w, h)
}

func ResizeTo(src image.Image, dw, dh int) *Bitmap {
	b := src.Bounds()
	if dw != b.Dx() || dh != b.Dy() {
		src = newResampler(b.Dx(), b.Dy(), dw, dh).scale(src)
	}
	return New(src)
}

// Size returns the image size in pixels.
func (i *Bitmap) Size() (int, int) { return i.W, i.H }
