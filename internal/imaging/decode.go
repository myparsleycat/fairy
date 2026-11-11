package imaging

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"time"
)

const WorkBudget int64 = 96 << 20

type Input struct {
	data []byte
	// config has the size of the image as shown, after the rotation its
	// metadata asks for; srcSize is the size of the coded image.
	Config       image.Config
	Format       string
	meta         imageMeta
	Animation    animationInfo
	AnimatedWebP bool
	AnimErr      error
	avif         avifInfo
}

type animationInfo struct {
	W, H               int
	first              image.Rectangle
	Count, plays       int
	maxFrame, maxSaved int64 // areas in pixels
	delays             []time.Duration
}

func imageTooLarge(w, h int) bool {
	return w > 0 && h > MaxImagePixels/w
}

func Inspect(data []byte) (Input, error) {
	in := Input{data: data}
	if c, format, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		if imageTooLarge(c.Width, c.Height) {
			return in, fmt.Errorf("fairy: image of %dx%d pixels exceeds the limit of %d megapixels",
				c.Width, c.Height, MaxImagePixels>>20)
		}
		in.Config, in.Format = c, format
	}
	if in.Format == "" || in.Format == "avif" {
		if a, ok := inspectAVIF(data); ok {
			in.avif = a
			// Without a registered decoder, the size from the boxes still
			// lets WIC decode the image.
			if in.Format == "" {
				if imageTooLarge(a.w, a.h) {
					return in, fmt.Errorf("fairy: image of %dx%d pixels exceeds the limit of %d megapixels",
						a.w, a.h, MaxImagePixels>>20)
				}
				in.Format, in.Config = "avif", image.Config{ColorModel: color.RGBAModel, Width: a.w, Height: a.h}
			}
		}
	}
	switch {
	case in.Format == "gif" && bytes.HasPrefix(data, []byte("GIF8")):
		in.Animation, in.AnimErr = walkGIF(data, nil)
	case len(data) >= 30 && string(data[:4]) == "RIFF" &&
		string(data[8:16]) == "WEBPVP8X" && data[20]&0x02 != 0:
		in.AnimatedWebP = true
		in.Animation, in.AnimErr = inspectWebP(data)
	}
	if in.Format == "avif" {
		in.meta = imageMeta{icc: in.avif.icc, primaries: in.avif.primaries, transfer: in.avif.transfer}
	} else {
		in.meta = readImageMeta(in.Format, data)
	}
	if in.Animation.Count != 0 || in.AnimatedWebP || in.AnimErr != nil {
		in.meta.orient = 0
	}
	if orientSwaps(in.meta.orient) {
		in.Config.Width, in.Config.Height = in.Config.Height, in.Config.Width
	}
	return in, nil
}

// srcSize returns the size of the coded image, before rotation.
func (in Input) srcSize() (int, int) {
	if orientSwaps(in.meta.orient) {
		return in.Config.Height, in.Config.Width
	}
	return in.Config.Width, in.Config.Height
}

// srcBound returns bound for the coded image, before rotation.
func (in Input) srcBound(bound Bound) Bound {
	if orientSwaps(in.meta.orient) {
		bound.W, bound.H = bound.H, bound.W
	}
	return bound
}

// finish turns a decode into the image as shown: rotated as its metadata
// says and converted to sRGB.
func (in Input) finish(img *Bitmap) *Bitmap {
	if img == nil {
		return nil
	}
	if t := in.meta.colorTransform(); t != nil {
		if len(img.FrameData) == 0 {
			t.apply(img.Pixels)
		}
		if img.Stream != nil {
			img.Stream.color = t
		}
		for _, f := range img.FrameData {
			t.apply(f.Pixels)
		}
	}
	if len(img.FrameData) == 0 && img.Pixels != nil {
		img.Pixels, img.W, img.H = orientPixels(img.Pixels, img.W, img.H, in.meta.orient)
	}
	return img
}

// DecodePixels decodes the image scaled within bound, as Decode does, or only
// the first frame of an animation when firstOnly is set. An animation keeps
// all its frames only when they fit keepBytes; past that it decodes them as it
// plays, within animBytes. wic, when non-nil,
// is tried first for the formats wicDecoderClass accepts; if it returns nil
// the Go decoders run instead, and usedWIC reports which produced img. run,
// when non-nil, runs each attempt under a memory reservation of the given
// bytes and reports false when it did not run the attempt, in which case
// DecodePixels returns a nil image and no error. A nil run decodes on the
// calling goroutine without reserving memory.
func (in Input) DecodePixels(bound Bound, keepBytes, animBytes int64, firstOnly bool,
	wic func(Input, Bound) *Bitmap, run func(int64, func()) bool,
) (img *Bitmap, usedWIC bool, err error) {
	if wic != nil && wicDecoderClass(in) != nil {
		f := func() { img = in.finish(wic(in, bound)) }
		if run == nil {
			f()
		} else if !run(min(WorkBudget, wicWork(in, bound)+in.finishWork(bound)), f) {
			return nil, false, nil
		}
		if img != nil {
			return img, true, nil
		}
	}
	f := func() {
		if firstOnly {
			img, err = in.decodeFirst()
		} else {
			img, err = in.decode(bound, keepBytes, animBytes)
		}
		if err == nil {
			img = in.finish(img)
		}
	}
	if run != nil {
		run(min(WorkBudget, in.work(bound, keepBytes)+in.finishWork(bound)), f)
	} else {
		f()
	}
	if err != nil {
		return nil, false, err
	}
	return img, false, nil
}

func (in Input) finishWork(bound Bound) int64 {
	var bytes int64
	if in.meta.orient >= 2 && in.meta.orient <= 8 {
		w, h := bound.Size(in.Config.Width, in.Config.Height)
		bytes += int64(w) * int64(h) * 4
	}
	if len(in.meta.icc) > 0 || in.meta.primaries != 0 || in.meta.transfer != 0 {
		bytes += 8 << 10
	}
	return bytes
}

func (in Input) decodeFirst() (*Bitmap, error) {
	a := in.Animation
	if a.Count > 0 {
		switch {
		case in.Format == "gif":
			f, err := gif.Decode(bytes.NewReader(in.data))
			if err != nil {
				return nil, err
			}
			canvas := image.NewNRGBA(image.Rect(0, 0, a.W, a.H))
			draw.Draw(canvas, f.Bounds(), f, f.Bounds().Min, draw.Src)
			return New(canvas), nil
		case in.AnimatedWebP:
			s := a.sink(Bound{}, 0)
			if err := decodeWebPFrames(in.data, &s, true); err != nil {
				return nil, err
			}
			if len(s.frames) != 0 {
				return New(s.frames[0]), nil
			}
		}
	}
	return in.decode(Bound{}, 0, 0)
}

func (a animationInfo) sink(bound Bound, maxBytes int64) frameSink {
	s := frameSink{bound: bound}
	if a.Count == 0 {
		return s
	}
	s.dw, s.dh = bound.Size(a.W, a.H)
	frameBytes := int64(s.dw) * int64(s.dh) * 4
	s.keepAll = a.Count >= 2 && frameBytes > 0 && maxBytes/frameBytes >= int64(a.Count)
	return s
}

// work caps the reservation, not the size of any individual image. An
// unknown or larger decode reserves the entire budget and runs alone.
func (in Input) work(bound Bound, keepBytes int64) int64 {
	if in.AnimErr != nil {
		return WorkBudget
	}
	a := in.Animation
	if a.Count > 0 {
		s := a.sink(bound, keepBytes)
		if !s.keepAll {
			// The first frame, composited on the canvas, which a stream
			// keeps; later frames decode while the animation plays.
			return min(WorkBudget, streamBytes(a, s.dw, s.dh))
		}
		frameBytes := int64(s.dw) * int64(s.dh) * 4
		if int64(a.Count) > WorkBudget/frameBytes {
			return WorkBudget
		}
		return min(WorkBudget,
			int64(a.Count)*frameBytes+int64(a.W)*int64(a.H)*4+
				a.maxFrame*8+a.maxSaved*4+ResampleWork(a.W, a.H, s.dw, s.dh))
	}
	if decoded, ok := in.decodedBytes(); ok {
		w, h := in.srcSize()
		return stillImageWork(w, h, in.srcBound(bound), decoded)
	}
	return WorkBudget
}

// decodedBytes estimates what the standard decoders allocate for a still
// image, from per-pixel amounts measured for each kind of file. Progressive
// JPEGs keep every coefficient until the last scan, and lossy WebP with
// alpha decodes its alpha plane with the lossless decoder.
func (in Input) decodedBytes() (int64, bool) {
	w, h := in.Config.Width, in.Config.Height
	if w <= 0 || h <= 0 || imageTooLarge(w, h) {
		return 0, false
	}
	var perPixel int64
	data := in.data
	switch {
	case in.Format == "png" && len(data) > 28 && bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		switch in.Config.ColorModel {
		case color.RGBA64Model, color.NRGBA64Model:
			perPixel = 8
		case color.Gray16Model:
			perPixel = 2
		case color.GrayModel:
			perPixel = 1
		default:
			perPixel = 4
			if _, ok := in.Config.ColorModel.(color.Palette); ok {
				perPixel = 1
			}
		}
		if data[28] == 1 { // Adam7 decodes each pass into an image of its own first
			perPixel *= 2
		}
	case in.Format == "jpeg" && bytes.HasPrefix(data, []byte("\xff\xd8")):
		perPixel = 4
		if jpegProgressive(data) {
			perPixel = 16
		}
	case in.Format == "webp" && len(data) >= 16 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		perPixel = webpDecodedPerPixel(data[12:])
	default:
		return 0, false
	}
	return perPixel * int64(w) * int64(h), true
}

// webpDecodedPerPixel looks past an extended header for the bitstream: lossy
// decodes to YCbCr, lossy with alpha also decodes an alpha plane through the
// lossless decoder, and lossless decodes to NRGBA.
func webpDecodedPerPixel(chunks []byte) int64 {
	alpha := false
	for len(chunks) > 0 {
		c, rest, err := nextRIFFChunk(chunks)
		if err != nil {
			break
		}
		switch c.id {
		case "ALPH":
			alpha = true
		case "VP8 ":
			if alpha {
				return 7
			}
			return 2
		case "VP8L":
			return 5
		}
		chunks = rest
	}
	return 7
}

// jpegProgressive reports whether the frame header before the first scan is
// progressive.
func jpegProgressive(data []byte) bool {
	for i := 2; i+4 <= len(data) && data[i] == 0xff; {
		switch m := data[i+1]; {
		case m == 0xc2 || m == 0xc6 || m == 0xca || m == 0xce:
			return true
		case m == 0xda:
			return false
		case m == 0xff:
			i++
		case m == 0x01 || m >= 0xd0 && m <= 0xd8:
			i += 2
		default:
			i += 2 + (int(data[i+2])<<8 | int(data[i+3]))
		}
	}
	return false
}

// stillImageWork adds to decoded, what the decoder allocates, the output and
// either the conversion to NRGBA or the resampler's rows.
func stillImageWork(w, h int, bound Bound, decoded int64) int64 {
	if w <= 0 || h <= 0 || imageTooLarge(w, h) {
		return WorkBudget
	}
	dw, dh := bound.Size(w, h)
	if dw == w && dh == h {
		return min(WorkBudget, decoded+int64(w)*int64(h)*4)
	}
	return min(WorkBudget, decoded+int64(dw)*int64(dh)*4+ResampleWork(w, h, dw, dh))
}
