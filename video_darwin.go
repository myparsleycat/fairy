package fairy

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/myparsleycat/fairy/internal/darwin"
	"github.com/myparsleycat/fairy/internal/render"
)

// Video plays a file or URL with AVFoundation. AVPlayer decodes it and
// AVPlayerItemVideoOutput hands its frames over as Metal-compatible BGRA
// pixel buffers, which are copied on the GPU into a texture the window
// draws. Formats are those macOS plays, such as MP4 and QuickTime movies
// (H.264, HEVC), HLS streams and MP3. Use it from the UI thread.
type Video struct {
	source string
	opts   VideoOptions
	state  VideoState
	err    error
	closed bool

	player, item, output darwin.ID
	observers            []darwin.ID // NSNotificationCenter tokens
	blocks               []objc.Block
	polling              bool      // a status check is scheduled
	awaitUntil           time.Time // keep painting until a frame arrives or then

	dev      *render.Device
	cache    uintptr        // CVMetalTextureCacheRef on dev
	frame    *render.Target // the latest frame
	last     uintptr        // CVPixelBufferRef of the latest frame
	inflight []videoCopy
}

// videoCopy is a frame copy the GPU may still be running, with the buffers
// it reads, released once it finished.
type videoCopy struct {
	fence    *render.Fence
	tex, buf uintptr // CVMetalTextureRef, CVPixelBufferRef
}

// cmTime is CoreMedia's CMTime.
type cmTime struct {
	Value     int64
	Timescale int32
	Flags     uint32
	Epoch     int64
}

const (
	cmTimeValid      = 1
	cmTimeInfinite   = 4 | 8 // positive or negative infinity
	cmTimeIndefinite = 16

	avStatusReady  = 1
	avStatusFailed = 2

	avEndAdvanceNone = 2 // AVPlayerActionAtItemEndNone
	avEndPause       = 1 // AVPlayerActionAtItemEndPause

	avTimeControlPaused = 0

	pixelFormat32BGRA = 0x42475241 // kCVPixelFormatType_32BGRA, 'BGRA'
	mtlPixelBGRA8     = 80         // MTLPixelFormatBGRA8Unorm

	// videoFrameWait is how long a paused video keeps painting for the frame
	// a seek or the first load brings.
	videoFrameWait = 2 * time.Second
)

var cmTimeZero = cmTime{Timescale: 1, Flags: cmTimeValid}

// seconds returns t in seconds, or NaN when it is invalid or not finite.
func (t cmTime) seconds() float64 {
	if t.Flags&cmTimeValid == 0 || t.Flags&(cmTimeInfinite|cmTimeIndefinite) != 0 || t.Timescale <= 0 {
		return math.NaN()
	}
	return float64(t.Value) / float64(t.Timescale)
}

func durationCMTime(d time.Duration) cmTime {
	return cmTime{Value: int64(max(d, 0)), Timescale: int32(time.Second), Flags: cmTimeValid}
}

var (
	avFoundation, coreVideo uintptr

	cvMetalTextureCacheCreate                 func(alloc, cacheAttrs, device, texAttrs uintptr, out *uintptr) int32
	cvMetalTextureCacheCreateTextureFromImage func(alloc, cache, image, attrs uintptr, format, w, h, plane uint64, out *uintptr) int32
	cvMetalTextureGetTexture                  func(tex uintptr) uintptr
	cvMetalTextureCacheFlush                  func(cache uintptr, options uint64)
	cvPixelBufferGetWidth                     func(buf uintptr) uint64
	cvPixelBufferGetHeight                    func(buf uintptr) uint64
	caCurrentMediaTime                        func() float64
)

// loadAV loads AVFoundation and CoreVideo the first time a video opens.
var loadAV = sync.OnceValue(func() error {
	const fw = "/System/Library/Frameworks/"
	var err error
	if avFoundation, err = purego.Dlopen(fw+"AVFoundation.framework/AVFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
		return fmt.Errorf("fairy: load AVFoundation: %w", err)
	}
	if coreVideo, err = purego.Dlopen(fw+"CoreVideo.framework/CoreVideo", purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
		return fmt.Errorf("fairy: load CoreVideo: %w", err)
	}
	darwin.Func(&cvMetalTextureCacheCreate, coreVideo, "CVMetalTextureCacheCreate")
	darwin.Func(&cvMetalTextureCacheCreateTextureFromImage, coreVideo, "CVMetalTextureCacheCreateTextureFromImage")
	darwin.Func(&cvMetalTextureGetTexture, coreVideo, "CVMetalTextureGetTexture")
	darwin.Func(&cvMetalTextureCacheFlush, coreVideo, "CVMetalTextureCacheFlush")
	darwin.Func(&cvPixelBufferGetWidth, coreVideo, "CVPixelBufferGetWidth")
	darwin.Func(&cvPixelBufferGetHeight, coreVideo, "CVPixelBufferGetHeight")
	darwin.Func(&caCurrentMediaTime, darwin.QuartzCore, "CACurrentMediaTime")
	return nil
})

// OpenVideo starts loading source, a file path or URL. Errors while loading
// are reported later through VideoOptions.OnError and Err.
func OpenVideo(source string, opts VideoOptions) (*Video, error) {
	if err := loadAV(); err != nil {
		return nil, err
	}
	v := &Video{source: source, opts: opts, state: VideoLoading}
	var err error
	darwin.WithPool(func() { err = v.start() })
	if err != nil {
		v.Close()
		return nil, err
	}
	return v, nil
}

// videoURL returns an NSURL for a URL with a scheme, or for a file path.
func videoURL(source string) darwin.ID {
	if i := strings.Index(source, "://"); i > 0 && !strings.ContainsAny(source[:i], `/\`) {
		return darwin.SendClass("NSURL", "URLWithString:", darwin.String(source))
	}
	return darwin.SendClass("NSURL", "fileURLWithPath:", darwin.String(source))
}

func (v *Video) start() error {
	url := videoURL(v.source)
	if url == 0 {
		return fmt.Errorf("fairy: video %s: not a valid URL", v.source)
	}
	v.item = darwin.Retain(darwin.SendClass("AVPlayerItem", "playerItemWithURL:", url))
	if v.item == 0 {
		return fmt.Errorf("fairy: video %s: could not create a player item", v.source)
	}
	attrs := darwin.SendClass("NSMutableDictionary", "dictionary")
	attrs.Send(darwin.Sel("setObject:forKey:"), darwin.SendClass("NSNumber", "numberWithUnsignedInt:", uint32(pixelFormat32BGRA)),
		darwin.SymbolID(coreVideo, "kCVPixelBufferPixelFormatTypeKey"))
	attrs.Send(darwin.Sel("setObject:forKey:"), darwin.SendClass("NSNumber", "numberWithBool:", true),
		darwin.SymbolID(coreVideo, "kCVPixelBufferMetalCompatibilityKey"))
	v.output = darwin.Send(darwin.SendClass("AVPlayerItemVideoOutput", "alloc"), "initWithPixelBufferAttributes:", attrs)
	if v.output == 0 {
		return errors.New("fairy: could not create a video output")
	}
	v.item.Send(darwin.Sel("addOutput:"), v.output)
	v.player = darwin.Retain(darwin.SendClass("AVPlayer", "playerWithPlayerItem:", v.item))
	if v.player == 0 {
		return errors.New("fairy: could not create a player")
	}
	v.player.Send(darwin.Sel("setActionAtItemEnd:"), v.endAction())
	v.player.Send(darwin.Sel("setMuted:"), v.opts.Muted)
	v.observe("AVPlayerItemDidPlayToEndTimeNotification", func(darwin.ID) { v.onEnded() })
	v.observe("AVPlayerItemFailedToPlayToEndTimeNotification", func(note darwin.ID) {
		info := darwin.Send(note, "userInfo")
		var nsErr darwin.ID
		if info != 0 {
			nsErr = darwin.Send(info, "objectForKey:", darwin.SymbolID(avFoundation, "AVPlayerItemFailedToPlayToEndTimeErrorKey"))
		}
		v.fail(nsErr)
	})
	if v.opts.AutoPlay {
		v.player.Send(darwin.Sel("play"))
	}
	v.schedulePoll()
	return nil
}

func (v *Video) endAction() int64 {
	if v.opts.Loop {
		return avEndAdvanceNone
	}
	return avEndPause
}

// observe calls f on the main thread when the player item posts the
// notification name.
func (v *Video) observe(name string, f func(note darwin.ID)) {
	block := objc.NewBlock(func(_ objc.Block, note objc.ID) {
		if !v.closed {
			f(note)
		}
	})
	center := darwin.SendClass("NSNotificationCenter", "defaultCenter")
	token := darwin.Send(center, "addObserverForName:object:queue:usingBlock:",
		darwin.SymbolID(avFoundation, name), v.item, darwin.SendClass("NSOperationQueue", "mainQueue"), block)
	v.observers = append(v.observers, darwin.Retain(token))
	v.blocks = append(v.blocks, block)
}

// schedulePoll checks the item's status soon on the UI thread. AVPlayerItem
// reports readiness only through key-value observing, so it is polled while
// the video loads.
func (v *Video) schedulePoll() {
	if v.polling || v.closed {
		return
	}
	v.polling = true
	time.AfterFunc(15*time.Millisecond, func() { darwin.Perform(v.poll) })
}

func (v *Video) poll() {
	v.polling = false
	if v.closed || v.state != VideoLoading {
		return
	}
	switch darwin.Get[int64](v.item, "status") {
	case avStatusReady:
		v.state = VideoReady
		if darwin.Get[float32](v.player, "rate") != 0 {
			v.state = VideoPlaying
		}
		v.awaitFrame()
		updateWindows()
	case avStatusFailed:
		v.fail(darwin.Send(v.item, "error"))
	default:
		v.schedulePoll()
	}
}

func (v *Video) fail(nsErr darwin.ID) {
	if v.closed || v.state == VideoFailed {
		return
	}
	msg := "playback failed"
	if nsErr != 0 {
		msg = darwin.GoString(darwin.Send(nsErr, "localizedDescription"))
		if reason := darwin.GoString(darwin.Send(nsErr, "localizedFailureReason")); reason != "" && reason != msg {
			msg += ": " + reason
		}
	}
	v.state = VideoFailed
	v.err = fmt.Errorf("fairy: video %s: %s", v.source, msg)
	if v.opts.OnError != nil {
		v.opts.OnError(v.err)
	}
	updateWindows()
}

func (v *Video) onEnded() {
	if v.opts.Loop {
		v.seek(cmTimeZero)
		return
	}
	v.state = VideoEnded
	if v.opts.OnEnded != nil {
		v.opts.OnEnded()
	}
	updateWindows()
}

// Close stops playback and frees the player and the frame textures.
func (v *Video) Close() {
	if v.closed {
		return
	}
	v.closed = true
	darwin.WithPool(func() {
		center := darwin.SendClass("NSNotificationCenter", "defaultCenter")
		for _, o := range v.observers {
			center.Send(darwin.Sel("removeObserver:"), o)
			darwin.Release(o)
		}
		for _, b := range v.blocks {
			b.Release()
		}
		v.observers, v.blocks = nil, nil
		if v.player != 0 {
			v.player.Send(darwin.Sel("pause"))
			v.player.Send(darwin.Sel("replaceCurrentItemWithPlayerItem:"), darwin.ID(0))
		}
		if v.item != 0 && v.output != 0 {
			v.item.Send(darwin.Sel("removeOutput:"), v.output)
		}
		darwin.Release(v.output)
		darwin.Release(v.item)
		darwin.Release(v.player)
		v.player, v.item, v.output = 0, 0, 0
	})
	v.releaseGPU()
	if v.last != 0 {
		darwin.CFRelease(v.last)
		v.last = 0
	}
}

// State returns the playback state.
func (v *Video) State() VideoState { return v.state }

// Err returns the error that stopped playback, if any.
func (v *Video) Err() error { return v.err }

// Play starts or resumes playback; at the end it starts over.
func (v *Video) Play() {
	if v.player == 0 || v.state == VideoFailed {
		return
	}
	if v.state == VideoEnded {
		v.seek(cmTimeZero)
		v.state = VideoReady
	}
	v.player.Send(darwin.Sel("play"))
	if v.state == VideoReady {
		v.state = VideoPlaying
	}
}

// Pause pauses playback.
func (v *Video) Pause() {
	if v.player == 0 {
		return
	}
	v.player.Send(darwin.Sel("pause"))
	if v.state == VideoPlaying {
		v.state = VideoReady
	}
}

// Playing reports whether the video is playing.
func (v *Video) Playing() bool {
	return v.player != 0 && v.state == VideoPlaying &&
		darwin.Get[int64](v.player, "timeControlStatus") != avTimeControlPaused
}

// Seek moves to position d.
func (v *Video) Seek(d time.Duration) {
	if v.player == 0 {
		return
	}
	v.seek(durationCMTime(d))
	if v.state == VideoEnded {
		v.state = VideoReady
	}
}

func (v *Video) seek(t cmTime) {
	v.player.Send(darwin.Sel("seekToTime:toleranceBefore:toleranceAfter:"), t, cmTimeZero, cmTimeZero)
	v.awaitFrame()
}

// awaitFrame keeps the windows showing v painting until the next frame
// arrives, as one does after a seek even while paused.
func (v *Video) awaitFrame() { v.awaitUntil = time.Now().Add(videoFrameWait) }

// Position returns the current playback position.
func (v *Video) Position() time.Duration {
	if v.player == 0 {
		return 0
	}
	return secondsToDuration(darwin.Get[cmTime](v.player, "currentTime").seconds())
}

// Duration returns the length of the media, or 0 while unknown.
func (v *Video) Duration() time.Duration {
	if v.item == 0 {
		return 0
	}
	return secondsToDuration(darwin.Get[cmTime](v.item, "duration").seconds())
}

// Size returns the frame size in pixels, or 0, 0 before metadata loads.
func (v *Video) Size() (int, int) {
	if v.item == 0 || v.state == VideoLoading || v.state == VideoFailed {
		return 0, 0
	}
	s := darwin.Get[darwin.CGSize](v.item, "presentationSize")
	if s.W <= 0 || s.H <= 0 {
		if v.last != 0 {
			return int(cvPixelBufferGetWidth(v.last)), int(cvPixelBufferGetHeight(v.last))
		}
		return 0, 0
	}
	return int(math.Round(s.W)), int(math.Round(s.H))
}

// SetMuted mutes or unmutes the audio.
func (v *Video) SetMuted(m bool) {
	v.opts.Muted = m
	if v.player != 0 {
		v.player.Send(darwin.Sel("setMuted:"), m)
	}
}

// Muted reports whether the audio is muted.
func (v *Video) Muted() bool { return v.opts.Muted }

// SetVolume sets the volume from 0 to 1.
func (v *Video) SetVolume(vol float64) {
	if v.player != 0 {
		v.player.Send(darwin.Sel("setVolume:"), float32(min(max(vol, 0), 1)))
	}
}

// Volume returns the volume from 0 to 1.
func (v *Video) Volume() float64 {
	if v.player == 0 {
		return 0
	}
	return float64(darwin.Get[float32](v.player, "volume"))
}

// SetLoop makes playback start over at the end.
func (v *Video) SetLoop(loop bool) {
	v.opts.Loop = loop
	if v.player != 0 {
		v.player.Send(darwin.Sel("setActionAtItemEnd:"), v.endAction())
	}
}

// acquireGPU makes the frame texture cache on the shared device, replacing
// what was made on a device that was lost.
func (v *Video) acquireGPU() bool {
	if v.dev != nil && !v.dev.Lost() {
		return true
	}
	v.releaseGPU()
	dev, err := render.AcquireDevice()
	if err != nil {
		return false
	}
	var cache uintptr
	if cvMetalTextureCacheCreate(0, 0, dev.MTLDevice(), 0, &cache) != 0 || cache == 0 {
		dev.Release()
		return false
	}
	v.dev, v.cache = dev, cache
	return true
}

// releaseGPU frees the frame texture and the cache once the GPU finished
// the copies that read from them.
func (v *Video) releaseGPU() {
	v.reap(true)
	v.frame.Release()
	v.frame = nil
	if v.cache != 0 {
		cvMetalTextureCacheFlush(v.cache, 0)
		darwin.CFRelease(v.cache)
		v.cache = 0
	}
	if v.dev != nil {
		v.dev.Release()
		v.dev = nil
	}
}

// reap releases the buffers of the copies the GPU finished, or of all of
// them after waiting when wait is set.
func (v *Video) reap(wait bool) {
	if len(v.inflight) == 0 {
		return
	}
	keep := v.inflight[:0]
	for _, c := range v.inflight {
		if wait {
			c.fence.Wait()
		}
		if !c.fence.Done() {
			keep = append(keep, c)
			continue
		}
		c.fence.Release()
		darwin.CFRelease(c.tex)
		darwin.CFRelease(c.buf)
	}
	clear(v.inflight[len(keep):])
	v.inflight = keep
	if v.cache != 0 {
		cvMetalTextureCacheFlush(v.cache, 0)
	}
}

// currentFrame takes the output's frame for this display time, if a new one
// is ready, and returns the texture holding the latest frame.
func (v *Video) currentFrame() *render.Target {
	if v.closed || v.player == 0 {
		return nil
	}
	v.reap(false)
	if v.state == VideoLoading || v.state == VideoFailed {
		return nil
	}
	if !v.acquireGPU() {
		return nil
	}
	fresh := false
	darwin.WithPool(func() {
		t := darwin.Get[cmTime](v.output, "itemTimeForHostTime:", caCurrentMediaTime())
		if !darwin.Get[bool](v.output, "hasNewPixelBufferForItemTime:", t) {
			return
		}
		buf := uintptr(darwin.Send(v.output, "copyPixelBufferForItemTime:itemTimeForDisplay:", t, uintptr(0)))
		if buf == 0 {
			return
		}
		if v.last != 0 {
			darwin.CFRelease(v.last)
		}
		v.last, fresh = buf, true
		v.awaitUntil = time.Time{}
	})
	if v.last == 0 {
		return nil
	}
	w, h := int(cvPixelBufferGetWidth(v.last)), int(cvPixelBufferGetHeight(v.last))
	if w <= 0 || h <= 0 {
		return nil
	}
	if v.frame == nil || v.frame.Width != w || v.frame.Height != h {
		v.frame.Release()
		t, err := render.NewTarget(v.dev, w, h)
		if err != nil {
			v.frame = nil
			return nil
		}
		v.frame, fresh = t, true
	}
	if fresh {
		v.copyFrame(w, h)
	}
	return v.frame
}

// copyFrame copies the latest pixel buffer into the frame texture on the GPU.
func (v *Video) copyFrame(w, h int) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	v.dev.Lock()
	defer v.dev.Unlock()
	var tex uintptr
	if cvMetalTextureCacheCreateTextureFromImage(0, v.cache, v.last, 0, mtlPixelBGRA8, uint64(w), uint64(h), 0, &tex) != 0 || tex == 0 {
		return
	}
	fence, err := render.CopyToTexture(v.dev, cvMetalTextureGetTexture(tex), &v.frame.Texture)
	if err != nil {
		darwin.CFRelease(tex)
		return
	}
	v.inflight = append(v.inflight, videoCopy{fence: fence, tex: tex, buf: darwin.CFRetain(v.last)})
}

func (w *Window) paintVideo(e *El, box rectF, radii, clip [4]float32, opacity float32) {
	v := e.video
	if v == nil || box.empty() {
		return
	}
	frame := v.currentFrame()
	if v.Playing() || !v.awaitUntil.IsZero() && time.Now().Before(v.awaitUntil) {
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
