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
	w, h, pix := premulPixels(src, false)
	return &Bitmap{W: w, H: h, Pixels: pix}
}

// newOwned is New for an image that nothing else uses afterwards: a tightly
// packed *image.NRGBA is premultiplied in place instead of copied.
func newOwned(src image.Image) *Bitmap {
	w, h, pix := premulPixels(src, true)
	return &Bitmap{W: w, H: h, Pixels: pix}
}

// premulPixels returns src as tightly packed premultiplied RGBA at the
// origin, reusing the pixels of such an *image.RGBA, and with owned those of
// such an *image.NRGBA, which it premultiplies in place.
func premulPixels(src image.Image, owned bool) (int, int, []byte) {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	switch s := src.(type) {
	case *image.RGBA:
		// A SubImage of the top rows passes the rest of its parent's pixels.
		if s.Stride == w*4 && b.Min == (image.Point{}) && len(s.Pix) == w*h*4 {
			return w, h, s.Pix
		}
	case *image.NRGBA:
		if owned && s.Stride == w*4 && b.Min == (image.Point{}) && len(s.Pix) == w*h*4 {
			premultiplyRGBA(s.Pix, s.Pix)
			return w, h, s.Pix
		}
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
// dst may be src: each pixel is read before it is written.
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
// dst may be src: each pixel is read before it is written.
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
	img, err := input.decode(bound, animBytes, animBytes)
	if err != nil {
		return nil, err
	}
	return input.finish(img), nil
}

// decode keeps every frame of an animation whose frames fit keepBytes and
// otherwise decodes its frames as it plays, or keeps only its first frame
// when even playing would take more than animBytes.
func (in Input) decode(bound Bound, keepBytes, animBytes int64) (*Bitmap, error) {
	animErr := in.AnimErr
	if animErr == nil && (in.Animation.Count >= 2 || in.AnimatedWebP) {
		s := in.Animation.sink(bound, keepBytes)
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
	src, format, err := image.Decode(bytes.NewReader(in.data))
	if err != nil {
		if animErr != nil {
			return nil, animErr
		}
		return nil, err
	}
	if format == "webp" {
		expandVP8Range(src)
	}
	return resize(src, in.srcBound(bound), in.meta.orient), nil
}

// resize scales src, which nothing else uses afterwards, within bound and
// turns it as EXIF orientation o says. A scaled image is turned row by row
// as it is resampled.
func resize(src image.Image, bound Bound, o uint8) *Bitmap {
	b := src.Bounds()
	w, h := bound.Size(b.Dx(), b.Dy())
	switch {
	case w == b.Dx() && h == b.Dy():
		img := newOwned(src)
		img.Pixels, img.W, img.H = orientPixels(img.Pixels, img.W, img.H, o)
		return img
	case !orientTurns(o):
		return ResizeTo(src, w, h)
	}
	t, ow, oh := newOrientWriter(make([]byte, w*h*4), w, h, o)
	newResampler(b.Dx(), b.Dy(), w, h).scaleTo(src, nil, t)
	return &Bitmap{W: ow, H: oh, Pixels: t.dst}
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
