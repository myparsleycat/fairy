package fairy

import (
	"image"
	"time"

	"github.com/myparsleycat/fairy/internal/imaging"
)

// NewImageFrames makes an animated Image from frames shown in turn, each for
// its delay. The frames are drawn over a transparent canvas the size of the
// first one, at their bounds' positions. plays is how many times the
// animation runs before it stops on the last frame; 0 repeats forever.
func NewImageFrames(frames []image.Image, delays []time.Duration, plays int) (*Image, error) {
	decoded, err := imaging.NewFrames(frames, delays, plays)
	return wrapImage(decoded), err
}

// Frames returns the number of frames: 1 for a still image.
func (i *Image) Frames() int {
	if v := i.drawnDecode(); v != nil {
		return v.Frames()
	}
	return max(i.data.FrameCount(), 1)
}

// Duration returns the length of one run of an animated image, 0 for a
// still one.
func (i *Image) Duration() time.Duration {
	if v := i.drawnDecode(); v != nil {
		return v.Duration()
	}
	return i.data.Total
}

func (i *Image) drawnDecode() *Image {
	if i.lazy == nil {
		return nil
	}
	return i.lazy.last()
}

func (i *Image) animated() bool { return i.data.FrameCount() >= 2 }

const streamRetry = 8 * time.Millisecond
