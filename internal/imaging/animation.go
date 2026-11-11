package imaging

import (
	"errors"
	"image"
	"image/draw"
	"math"
	"time"
)

// animFrame is one composited frame of an animated image.
type animFrame struct {
	Pixels []byte // premultiplied RGBA, the full canvas
	Delay  time.Duration
}

// minFrameDelay is the delay browsers give frames that ask for 10 ms or
// less, which many GIFs do by accident.
const minFrameDelay = 100 * time.Millisecond

func frameDelay(d time.Duration) time.Duration {
	if d <= 10*time.Millisecond {
		return minFrameDelay
	}
	return d
}

// NewFrames makes an animated Bitmap from frames shown in turn, each for
// its delay. The frames are drawn over a transparent canvas the size of the
// first one, at their bounds' positions. plays is how many times the
// animation runs before it stops on the last frame; 0 repeats forever.
func NewFrames(frames []image.Image, delays []time.Duration, plays int) (*Bitmap, error) {
	if len(frames) == 0 || len(delays) != len(frames) {
		return nil, errors.New("fairy: NewImageFrames needs one delay per frame")
	}
	b := frames[0].Bounds()
	out := make([]image.Image, len(frames))
	for i, f := range frames {
		c := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(c, f.Bounds().Sub(b.Min), f, f.Bounds().Min, draw.Src)
		out[i] = c
	}
	return newAnimImage(out, delays, plays), nil
}

func newAnimImage(frames []image.Image, delays []time.Duration, plays int) *Bitmap {
	if len(frames) < 2 {
		return New(frames[0])
	}
	img := &Bitmap{FrameData: make([]animFrame, len(frames)), Plays: plays}
	for i, f := range frames {
		w, h, pix := premulPixels(f)
		if i == 0 {
			img.W, img.H, img.Pixels = w, h, pix
		}
		img.FrameData[i] = animFrame{Pixels: pix, Delay: frameDelay(delays[i])}
		img.Total += img.FrameData[i].Delay
	}
	return img
}

func (i *Bitmap) FrameCount() int {
	if i.Stream != nil {
		return len(i.Stream.Delays)
	}
	return len(i.FrameData)
}

func (i *Bitmap) frameDelay(n int) time.Duration {
	if i.Stream != nil {
		return i.Stream.Delays[n]
	}
	return i.FrameData[n].Delay
}

// ByteSize is the memory the decoded pixels take, and for a streamed
// animation what playing it takes.
func (i *Bitmap) ByteSize() int64 {
	switch {
	case i.Stream != nil:
		return int64(len(i.Pixels)) + i.Stream.Bytes
	case len(i.FrameData) == 0:
		return int64(len(i.Pixels))
	}
	return int64(len(i.FrameData)) * int64(len(i.Pixels))
}

// FrameAt returns the frame to show elapsed after the animation started and
// how long until the frame changes, or a negative wait once the animation
// has finished its plays.
func (i *Bitmap) FrameAt(elapsed time.Duration) (int, time.Duration) {
	count := i.FrameCount()
	if count < 2 || i.Total <= 0 {
		return 0, -1
	}
	if elapsed < 0 {
		elapsed = 0
	}
	if i.Plays > 0 && elapsed >= time.Duration(i.Plays)*i.Total {
		return count - 1, -1
	}
	t := elapsed % i.Total
	for n := range count {
		if d := i.frameDelay(n); t < d {
			return n, d - t
		} else {
			t -= d
		}
	}
	return count - 1, -1
}

// frameSink keeps the composited canvas of each frame, scaled down to fit
// within bound by Bound.Size.
type frameSink struct {
	bound     Bound
	dw, dh    int
	keepAll   bool
	frames    []image.Image // premultiplied *image.RGBA
	delays    []time.Duration
	resampler *resampler
	// each, when set, is given each composited canvas instead; an error
	// stops decoding.
	each func(canvas *image.NRGBA) error
}

func (s *frameSink) add(canvas *image.NRGBA, delay time.Duration) error {
	if s.each != nil {
		return s.each(canvas)
	}
	w, h := canvas.Rect.Dx(), canvas.Rect.Dy()
	var dst *image.RGBA
	if s.dw == w && s.dh == h {
		dst = image.NewRGBA(image.Rect(0, 0, w, h))
		premultiplyRGBA(dst.Pix, canvas.Pix)
	} else {
		if s.resampler == nil {
			s.resampler = newResampler(w, h, s.dw, s.dh)
		}
		dst = s.resampler.scale(canvas)
	}
	s.frames = append(s.frames, dst)
	s.delays = append(s.delays, delay)
	return nil
}

// Bound is how far a decode scales an image down, keeping its aspect: to
// fit inside w x h, or with cover to the smallest size that covers w x h. A
// zero w or h leaves that axis unlimited, and images are never scaled up.
type Bound struct {
	W, H  int
	Cover bool
}

// maxCoverStretch limits a covering decode to this many times its box on
// either axis, so that a panorama covering a small square does not decode
// at nearly full size.
const maxCoverStretch = 4

func (b Bound) Size(w, h int) (int, int) {
	if w <= 0 || h <= 0 {
		return w, h
	}
	if b.Cover && b.W > 0 && b.H > 0 {
		dw, dh := scaleDown(w, h, max(float64(b.W)/float64(w), float64(b.H)/float64(h)))
		cw := min(b.W, math.MaxInt/maxCoverStretch) * maxCoverStretch
		ch := min(b.H, math.MaxInt/maxCoverStretch) * maxCoverStretch
		if dw <= cw && dh <= ch {
			return dw, dh
		}
		b = Bound{W: cw, H: ch}
	}
	scale := 1.0
	if b.W > 0 && w > b.W {
		scale = float64(b.W) / float64(w)
	}
	if b.H > 0 && float64(h)*scale > float64(b.H) {
		scale = float64(b.H) / float64(h)
	}
	return scaleDown(w, h, scale)
}

func scaleDown(w, h int, scale float64) (int, int) {
	if scale >= 1 {
		return w, h
	}
	return max(1, int(float64(w)*scale+0.5)), max(1, int(float64(h)*scale+0.5))
}
