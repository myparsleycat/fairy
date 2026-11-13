//go:build darwin

package render

import (
	"time"

	"github.com/myparsleycat/fairy/internal/darwin"
)

// gpuTimer reads how long the GPU ran each frame's command buffer, from its
// GPUStartTime and GPUEndTime once it completed. It never waits: a frame's
// time is reported by a later Draw.
type gpuTimer struct {
	pending []timedFrame
}

type timedFrame struct {
	cb    id // retained
	frame uint64
}

// maxTimedFrames bounds the frames awaiting their time, as the Windows
// timer has four query slots.
const maxTimedFrames = 4

type gpuSample struct {
	duration time.Duration
	frame    uint64
	valid    bool
}

// collect returns the time of the latest frame completed since the last
// call, and forgets every completed frame.
func (t *gpuTimer) collect() gpuSample {
	var sample gpuSample
	kept := t.pending[:0]
	for _, p := range t.pending {
		status := darwin.Get[uint64](p.cb, "status")
		if status < cbStatusCompleted {
			kept = append(kept, p)
			continue
		}
		if status == cbStatusCompleted && (!sample.valid || p.frame > sample.frame) {
			start := darwin.Get[float64](p.cb, "GPUStartTime")
			end := darwin.Get[float64](p.cb, "GPUEndTime")
			if start > 0 && end >= start {
				sample = gpuSample{duration: time.Duration((end - start) * float64(time.Second)), frame: p.frame, valid: true}
			}
		}
		darwin.Release(p.cb)
	}
	clear(t.pending[len(kept):])
	t.pending = kept
	return sample
}

// track times cb, the command buffer of renderer submission frame, unless
// too many frames already await their time.
func (t *gpuTimer) track(cb id, frame uint64) {
	if len(t.pending) >= maxTimedFrames {
		return
	}
	t.pending = append(t.pending, timedFrame{cb: darwin.Retain(cb), frame: frame})
}

func (t *gpuTimer) release() {
	for _, p := range t.pending {
		darwin.Release(p.cb)
	}
	t.pending = nil
}
