package imaging

import (
	"errors"
	"image"
	"sync"
	"time"
)

// animStream plays an animation whose decoded frames would not fit the
// memory budget by decoding them as they are shown, as browsers play large
// animations. It keeps the compressed bytes; while it plays, a goroutine
// keeps the canvas at the source size and two frames at the output size,
// composites the frames in order and hands out the one asked for. It stops,
// dropping all of that, when no frame was asked for in streamIdle.
type animStream struct {
	in     Input
	dw, dh int
	Delays []time.Duration
	first  []byte          // the first frame, shown if decoding fails
	color  *colorTransform // set before the stream starts
	Bytes  int64           // what playing holds besides the first frame

	mu      sync.Mutex
	want    int // the frame asked for last
	have    int // the frame in out, or -1
	out     []byte
	running bool
	failed  bool
	kick    chan struct{}
}

var (
	// streamIdle is how long a stream decoder waits for a frame to be
	// asked for before it stops.
	streamIdle = time.Second

	errStreamRestart = errors.New("fairy: restart animation stream")
	errStreamStop    = errors.New("fairy: stop animation stream")
)

// streamBytes is what playing a stream holds besides its first frame: the
// canvas, a saved area for DisposalPrevious, a decoded frame, and two output
// frames.
func streamBytes(a animationInfo, dw, dh int) int64 {
	return int64(a.W)*int64(a.H)*4 + a.maxSaved*4 + a.maxFrame*8 + 2*int64(dw)*int64(dh)*4 + ResampleWork(a.W, a.H, dw, dh)
}

// walkFrames composites the frames of an animated GIF or WebP into s.
func (in Input) walkFrames(s *frameSink) error {
	if in.Format == "gif" {
		return decodeGIFFrames(in.data, in.Animation, s)
	}
	return decodeWebPFrames(in.data, s, false)
}

// firstFrame returns the first frame of an animation, composited on its
// canvas and scaled within bound.
func (in Input) firstFrame(bound Bound) (*Bitmap, error) {
	var first *image.NRGBA
	s := frameSink{keepAll: true, each: func(canvas *image.NRGBA) error {
		first = canvas
		return errStreamStop
	}}
	if err := in.walkFrames(&s); err != errStreamStop {
		if err == nil {
			err = errors.New("fairy: animation without frames")
		}
		return nil, err
	}
	return Resize(first, bound), nil
}

// streamImage returns an animation that decodes its frames as it plays, or
// its first frame when even that does not fit in maxBytes.
func (in Input) streamImage(bound Bound, maxBytes int64) (*Bitmap, error) {
	img, err := in.firstFrame(bound)
	if err != nil || in.Animation.Count < 2 {
		return img, err
	}
	a := in.Animation
	bytes := int64(len(in.data)) + streamBytes(a, img.W, img.H)
	if int64(len(img.Pixels))+bytes > maxBytes {
		return img, nil
	}
	st := &animStream{in: in, dw: img.W, dh: img.H, first: img.Pixels, Bytes: bytes, have: -1, kick: make(chan struct{}, 1)}
	for _, d := range a.delays {
		st.Delays = append(st.Delays, frameDelay(d))
		img.Total += st.Delays[len(st.Delays)-1]
	}
	img.Stream, img.Plays = st, a.plays
	return img, nil
}

// WithFrame calls f with frame n and reports true when the frame is ready.
// Otherwise it has the decoder go to frame n, and the caller asks again
// later. f runs with the stream locked.
func (s *animStream) WithFrame(n int, f func([]byte)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.failed:
		f(s.first)
		return true
	case s.have == n:
		f(s.out)
		return true
	}
	s.want = n
	if !s.running {
		s.running = true
		go s.run()
	} else {
		select {
		case s.kick <- struct{}{}:
		default:
		}
	}
	return false
}

// wait waits, with s.mu held, until a frame is asked for or streamIdle
// passes, and reports false for the latter.
func (s *animStream) wait() bool {
	s.mu.Unlock()
	defer s.mu.Lock()
	t := time.NewTimer(streamIdle)
	defer t.Stop()
	select {
	case <-s.kick:
		return true
	case <-t.C:
		return false
	}
}

func (s *animStream) run() {
	var work []byte
	var r *resampler
	sink := frameSink{keepAll: true}
	for {
		i := 0
		sink.each = func(canvas *image.NRGBA) error {
			err := s.step(i, canvas, &work, &r)
			i++
			return err
		}
		err := s.in.walkFrames(&sink)
		s.mu.Lock()
		switch err {
		case nil: // past the last frame: wait for the animation to repeat
			for s.want == s.have {
				if !s.wait() {
					err = errStreamStop
					break
				}
			}
		case errStreamRestart:
		case errStreamStop:
		default:
			s.failed = true
		}
		if err != nil && err != errStreamRestart {
			s.running, s.out, s.have = false, nil, -1
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
	}
}

// step handles frame i of a pass from the first frame. Frames before the
// one asked for are only composited; the next one is decoded ahead and
// handed out when it is asked for.
func (s *animStream) step(i int, canvas *image.NRGBA, work *[]byte, r **resampler) error {
	s.mu.Lock()
	want := s.want
	s.mu.Unlock()
	switch {
	case i < want:
		return nil
	case i > want+1:
		return errStreamRestart
	}
	pix := s.render(canvas, *work, r)
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		switch {
		case s.want == i:
			*work = s.out
			s.out, s.have = pix, i
			return nil
		case s.want > i:
			*work = pix
			return nil
		case s.want < i-1:
			*work = pix
			return errStreamRestart
		}
		if !s.wait() {
			return errStreamStop
		}
	}
}

// render scales canvas to the output size into buf when it is large enough.
func (s *animStream) render(canvas *image.NRGBA, buf []byte, r **resampler) []byte {
	n := s.dw * s.dh * 4
	if cap(buf) < n {
		buf = make([]byte, n)
	}
	buf = buf[:n]
	if cw, ch := canvas.Rect.Dx(), canvas.Rect.Dy(); cw == s.dw && ch == s.dh {
		premultiplyRGBA(buf, canvas.Pix)
	} else {
		if *r == nil {
			*r = newResampler(cw, ch, s.dw, s.dh)
		}
		(*r).scaleInto(canvas, &image.RGBA{Pix: buf, Stride: s.dw * 4, Rect: image.Rect(0, 0, s.dw, s.dh)})
	}
	if s.color != nil {
		s.color.apply(buf)
	}
	return buf
}
