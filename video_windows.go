package fairy

import (
	"fmt"
	"time"

	"github.com/myparsleycat/fairy/internal/mf"
	"github.com/myparsleycat/fairy/internal/render"
)

// Video plays a file or URL with Media Foundation, decoding into a texture on
// the GPU. Formats are those Windows supports, such as MP4 (H.264, HEVC with
// the extension), WebM (VP9, AV1 with the extension) and MP3. Use it from the
// UI thread.
type Video struct {
	source string
	opts   VideoOptions
	dev    *render.Device
	eng    *mf.Engine
	frame  *render.Target
	state  VideoState
	err    error
	resume time.Duration // position to restore after the device is recreated
	closed bool
}

// OpenVideo starts loading source, a file path or URL. Errors while loading
// are reported later through VideoOptions.OnError and Err.
func OpenVideo(source string, opts VideoOptions) (*Video, error) {
	v := &Video{source: source, opts: opts}
	if err := v.start(); err != nil {
		return nil, err
	}
	return v, nil
}

func (v *Video) start() error {
	dev, err := render.AcquireDevice()
	if err != nil {
		return err
	}
	eng, err := mf.NewEngine(dev.Dev.P, func(event uint32, _ uintptr, _ uint32) {
		switch event {
		case mf.EventLoadedMetadata, mf.EventCanPlay, mf.EventPlay, mf.EventPlaying, mf.EventPause,
			mf.EventEnded, mf.EventError, mf.EventSeeked, mf.EventDurationChange:
			Go(func() { v.onEvent(event) })
		}
	})
	if err != nil {
		dev.Release()
		return err
	}
	v.dev, v.eng, v.state, v.err = dev, eng, VideoLoading, nil
	eng.SetLoop(v.opts.Loop)
	eng.SetMuted(v.opts.Muted)
	eng.SetAutoPlay(v.opts.AutoPlay)
	if err := eng.SetSource(v.source); err != nil {
		v.release()
		return err
	}
	return nil
}

func (v *Video) onEvent(event uint32) {
	if v.eng == nil {
		return
	}
	switch event {
	case mf.EventLoadedMetadata:
		if v.state == VideoLoading {
			v.state = VideoReady
		}
		if v.resume > 0 {
			v.eng.Seek(v.resume.Seconds())
			v.resume = 0
		}
	case mf.EventPlay, mf.EventPlaying:
		v.state = VideoPlaying
	case mf.EventPause:
		if v.state == VideoPlaying {
			v.state = VideoReady
		}
	case mf.EventEnded:
		if !v.opts.Loop {
			v.state = VideoEnded
			if v.opts.OnEnded != nil {
				v.opts.OnEnded()
			}
		}
	case mf.EventError:
		code, hr := v.eng.Error()
		v.state = VideoFailed
		v.err = fmt.Errorf("fairy: video %s: media engine error %d (%v)", v.source, code, hr)
		if v.opts.OnError != nil {
			v.opts.OnError(v.err)
		}
	}
}

func (v *Video) release() {
	if v.eng != nil {
		v.eng.Release()
		v.eng = nil
	}
	v.frame.Release()
	v.frame = nil
	if v.dev != nil {
		v.dev.Release()
		v.dev = nil
	}
}

// Close stops playback and frees the decoder.
func (v *Video) Close() {
	v.closed = true
	v.release()
}

// State returns the playback state.
func (v *Video) State() VideoState { return v.state }

// Err returns the error that stopped playback, if any.
func (v *Video) Err() error { return v.err }

// Play starts or resumes playback; at the end it starts over.
func (v *Video) Play() {
	if v.eng == nil {
		return
	}
	if v.state == VideoEnded {
		v.eng.Seek(0)
	}
	v.eng.Play()
}

// Pause pauses playback.
func (v *Video) Pause() {
	if v.eng != nil {
		v.eng.Pause()
	}
}

// Playing reports whether the video is playing.
func (v *Video) Playing() bool { return v.eng != nil && !v.eng.Paused() && v.state == VideoPlaying }

// Seek moves to position d.
func (v *Video) Seek(d time.Duration) {
	if v.eng != nil {
		v.eng.Seek(d.Seconds())
	}
}

// Position returns the current playback position.
func (v *Video) Position() time.Duration {
	if v.eng == nil {
		return 0
	}
	return secondsToDuration(v.eng.CurrentTime())
}

// Duration returns the length of the media, or 0 while unknown.
func (v *Video) Duration() time.Duration {
	if v.eng == nil {
		return 0
	}
	return secondsToDuration(v.eng.Duration())
}

// Size returns the frame size in pixels, or 0, 0 before metadata loads.
func (v *Video) Size() (int, int) {
	if v.eng == nil || v.state == VideoLoading || v.state == VideoFailed {
		return 0, 0
	}
	return v.eng.VideoSize()
}

// SetMuted mutes or unmutes the audio.
func (v *Video) SetMuted(m bool) {
	v.opts.Muted = m
	if v.eng != nil {
		v.eng.SetMuted(m)
	}
}

// Muted reports whether the audio is muted.
func (v *Video) Muted() bool { return v.opts.Muted }

// SetVolume sets the volume from 0 to 1.
func (v *Video) SetVolume(vol float64) {
	if v.eng != nil {
		v.eng.SetVolume(min(max(vol, 0), 1))
	}
}

// Volume returns the volume from 0 to 1.
func (v *Video) Volume() float64 {
	if v.eng == nil {
		return 0
	}
	return v.eng.Volume()
}

// SetLoop makes playback start over at the end.
func (v *Video) SetLoop(loop bool) {
	v.opts.Loop = loop
	if v.eng != nil {
		v.eng.SetLoop(loop)
	}
}

// currentFrame transfers a new frame if one is ready and returns the texture
// holding the latest frame.
func (v *Video) currentFrame() *render.Target {
	if v.closed {
		return nil
	}
	if v.dev != nil && v.dev.Lost() {
		v.resume = v.Position()
		playing := v.Playing()
		v.release()
		v.opts.AutoPlay = playing
		if v.start() != nil {
			return nil
		}
	}
	if v.eng == nil || v.state == VideoLoading || v.state == VideoFailed {
		return nil
	}
	w, h := v.eng.VideoSize()
	if w <= 0 || h <= 0 {
		return nil
	}
	fresh := false
	if v.frame == nil || v.frame.Width != w || v.frame.Height != h {
		v.frame.Release()
		t, err := render.NewTarget(v.dev, w, h)
		if err != nil {
			v.frame = nil
			return nil
		}
		v.frame, fresh = t, true
	}
	if v.eng.NewFrame() || fresh {
		v.eng.Transfer(v.frame.Resource().P, w, h)
	}
	return v.frame
}

func (w *Window) paintVideo(e *El, box rectF, radii, clip [4]float32, opacity float32) {
	v := e.video
	if v == nil || box.empty() {
		return
	}
	frame := v.currentFrame()
	if v.Playing() || v.state == VideoLoading {
		w.animating = true
	}
	if frame == nil {
		return
	}
	dst, uv := fitImage(box, float32(frame.Width), float32(frame.Height), e.fit)
	w.scene.AddImage(render.Prim{
		Bounds: w.snap(dst),
		Clip:   clip,
		Color:  [4]float32{1, 1, 1, opacity},
		Radii:  radii,
		UV:     uv,
	}, &frame.Texture)
}
