package fairy

import "time"

// VideoOptions configures OpenVideo.
type VideoOptions struct {
	AutoPlay bool
	Loop     bool
	Muted    bool
	// OnEnded runs on the UI thread when playback reaches the end without Loop.
	OnEnded func()
	// OnError runs on the UI thread when the source cannot be played.
	OnError func(error)
}

// VideoState is the playback state of a Video.
type VideoState uint8

const (
	VideoLoading VideoState = iota
	VideoReady              // metadata is known; paused
	VideoPlaying
	VideoEnded
	VideoFailed
)

// VideoView draws the current frame of v. Size it like a box. The frame is
// letterboxed by default; call Fit to change that. The window keeps painting
// while v plays.
func VideoView(v *Video) *El {
	e := newEl(kindVideo)
	e.video = v
	e.fit = FitContain
	return e
}

func secondsToDuration(s float64) time.Duration {
	if s != s || s < 0 || s > 1e9 { // NaN, unknown or infinite (live streams)
		return 0
	}
	return time.Duration(s * float64(time.Second))
}
