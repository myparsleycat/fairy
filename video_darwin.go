package fairy

// Placeholder until the macOS backend has video: it keeps the public API of
// the Windows backend and does nothing.

import (
	"time"
)

// Video is a playing video file or stream, drawn by VideoView.
type Video struct{}

// OpenVideo opens a video file or URL.
func OpenVideo(source string, opts VideoOptions) (*Video, error) { return nil, errNoBackend }

func (v *Video) Close()                  {}
func (v *Video) State() VideoState       { return VideoFailed }
func (v *Video) Err() error              { return errNoBackend }
func (v *Video) Play()                   {}
func (v *Video) Pause()                  {}
func (v *Video) Playing() bool           { return false }
func (v *Video) Seek(d time.Duration)    {}
func (v *Video) Position() time.Duration { return 0 }
func (v *Video) Duration() time.Duration { return 0 }
func (v *Video) Size() (int, int)        { return 0, 0 }
func (v *Video) SetMuted(m bool)         {}
func (v *Video) Muted() bool             { return false }
func (v *Video) SetVolume(vol float64)   {}
func (v *Video) Volume() float64         { return 0 }
func (v *Video) SetLoop(loop bool)       {}

func (w *Window) paintVideo(e *El, box rectF, radii, clip [4]float32, opacity float32) {}
