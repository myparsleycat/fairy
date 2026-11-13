package fairy

import (
	"errors"
	"image"
	"image/color"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/ebitengine/purego/objc"

	"github.com/myparsleycat/fairy/internal/darwin"
)

var (
	cvPixelBufferCreate            func(alloc uintptr, w, h uint64, format uint32, attrs uintptr, out *uintptr) int32
	cvPixelBufferLockBaseAddress   func(buf uintptr, flags uint64) int32
	cvPixelBufferUnlockBaseAddress func(buf uintptr, flags uint64) int32
	cvPixelBufferGetBaseAddress    func(buf uintptr) unsafe.Pointer
	cvPixelBufferGetBytesPerRow    func(buf uintptr) uint64
	loadPixelBufferFuncs           sync.Once
)

// movieSpec describes a test movie: w x h pixels at 10 frames a second,
// where pixel (x, y) of frame i has the color paint(i, x, y), shown turned
// by transform when it is set.
type movieSpec struct {
	w, h, frames int
	paint        func(i, x, y int) color.NRGBA
	transform    *cgAffineTransform
}

// writeTestMovie writes an H.264 movie of size x size pixels at 10 frames a
// second whose frame i has the color colors(i), with AVAssetWriter.
func writeTestMovie(t *testing.T, path string, size, frames int, colors func(i int) color.NRGBA) {
	t.Helper()
	writeTestMovieSpec(t, path, movieSpec{w: size, h: size, frames: frames, paint: func(i, _, _ int) color.NRGBA { return colors(i) }})
}

func writeTestMovieSpec(t *testing.T, path string, m movieSpec) {
	t.Helper()
	if err := loadAV(); err != nil {
		t.Fatal(err)
	}
	loadPixelBufferFuncs.Do(func() {
		darwin.Func(&cvPixelBufferCreate, coreVideo, "CVPixelBufferCreate")
		darwin.Func(&cvPixelBufferLockBaseAddress, coreVideo, "CVPixelBufferLockBaseAddress")
		darwin.Func(&cvPixelBufferUnlockBaseAddress, coreVideo, "CVPixelBufferUnlockBaseAddress")
		darwin.Func(&cvPixelBufferGetBaseAddress, coreVideo, "CVPixelBufferGetBaseAddress")
		darwin.Func(&cvPixelBufferGetBytesPerRow, coreVideo, "CVPixelBufferGetBytesPerRow")
	})
	var err error
	darwin.WithPool(func() { err = writeMovie(path, m) })
	if err != nil {
		t.Fatal(err)
	}
}

func writeMovie(path string, m movieSpec) error {
	sym := func(name string) darwin.ID { return darwin.SymbolID(avFoundation, name) }
	num := func(n int) darwin.ID { return darwin.SendClass("NSNumber", "numberWithInt:", int32(n)) }
	url := darwin.SendClass("NSURL", "fileURLWithPath:", darwin.String(path))
	var nsErr darwin.ID
	writer := darwin.SendClass("AVAssetWriter", "assetWriterWithURL:fileType:error:", url, sym("AVFileTypeQuickTimeMovie"), unsafe.Pointer(&nsErr))
	if writer == 0 {
		return errors.New("AVAssetWriter: " + darwin.GoString(darwin.Send(nsErr, "localizedDescription")))
	}
	settings := darwin.SendClass("NSMutableDictionary", "dictionary")
	set := func(d darwin.ID, val, key darwin.ID) { d.Send(darwin.Sel("setObject:forKey:"), val, key) }
	set(settings, sym("AVVideoCodecTypeH264"), sym("AVVideoCodecKey"))
	set(settings, num(m.w), sym("AVVideoWidthKey"))
	set(settings, num(m.h), sym("AVVideoHeightKey"))
	input := darwin.SendClass("AVAssetWriterInput", "assetWriterInputWithMediaType:outputSettings:", sym("AVMediaTypeVideo"), settings)
	if m.transform != nil {
		input.Send(darwin.Sel("setTransform:"), *m.transform)
	}
	input.Send(darwin.Sel("setExpectsMediaDataInRealTime:"), false)
	adaptor := darwin.SendClass("AVAssetWriterInputPixelBufferAdaptor",
		"assetWriterInputPixelBufferAdaptorWithAssetWriterInput:sourcePixelBufferAttributes:", input, darwin.ID(0))
	writer.Send(darwin.Sel("addInput:"), input)
	if !darwin.Get[bool](writer, "startWriting") {
		return errors.New("startWriting: " + darwin.GoString(darwin.Send(darwin.Send(writer, "error"), "localizedDescription")))
	}
	writer.Send(darwin.Sel("startSessionAtSourceTime:"), cmTimeZero)
	for i := range m.frames {
		for !darwin.Get[bool](input, "isReadyForMoreMediaData") {
			time.Sleep(time.Millisecond)
		}
		var buf uintptr
		if cvPixelBufferCreate(0, uint64(m.w), uint64(m.h), pixelFormat32BGRA, 0, &buf) != 0 {
			return errors.New("CVPixelBufferCreate failed")
		}
		cvPixelBufferLockBaseAddress(buf, 0)
		base, stride := cvPixelBufferGetBaseAddress(buf), int(cvPixelBufferGetBytesPerRow(buf))
		pix := unsafe.Slice((*byte)(base), stride*m.h)
		for y := range m.h {
			for x := range m.w {
				c := m.paint(i, x, y)
				copy(pix[y*stride+x*4:], []byte{c.B, c.G, c.R, 255})
			}
		}
		cvPixelBufferUnlockBaseAddress(buf, 0)
		ok := darwin.Get[bool](adaptor, "appendPixelBuffer:withPresentationTime:", buf, cmTime{Value: int64(i), Timescale: 10, Flags: cmTimeValid})
		darwin.CFRelease(buf)
		if !ok {
			return errors.New("appendPixelBuffer failed")
		}
	}
	input.Send(darwin.Sel("markAsFinished"))
	writer.Send(darwin.Sel("endSessionAtSourceTime:"), cmTime{Value: int64(m.frames), Timescale: 10, Flags: cmTimeValid})
	done := make(chan struct{})
	block := objc.NewBlock(func(objc.Block) { close(done) })
	defer block.Release()
	writer.Send(darwin.Sel("finishWritingWithCompletionHandler:"), block)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		return errors.New("finishWriting timed out")
	}
	if s := darwin.Get[int64](writer, "status"); s != 2 { // AVAssetWriterStatusCompleted
		return errors.New("writer failed: " + darwin.GoString(darwin.Send(darwin.Send(writer, "error"), "localizedDescription")))
	}
	return nil
}

var (
	movieRed  = color.NRGBA{220, 30, 30, 255}
	movieBlue = color.NRGBA{30, 30, 220, 255}
)

// testMovie writes a 2 s 64x64 movie, red for the first second and blue for
// the second.
func testMovie(t *testing.T) string {
	path := filepath.Join(t.TempDir(), "test.mov")
	writeTestMovie(t, path, 64, 20, func(i int) color.NRGBA {
		if i < 10 {
			return movieRed
		}
		return movieBlue
	})
	return path
}

// waitVideo polls cond until it holds.
func waitVideo(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// looksLike reports whether got is the movie color want after encoding,
// which shifts colors through YUV: its strongest channel is want's and the
// others are well below it.
func looksLike(got, want color.NRGBA) bool {
	if got.A != 255 {
		return false
	}
	if want.R > want.B {
		return got.R > 150 && got.G < 80 && got.B < 80
	}
	return got.B > 150 && got.R < 80 && got.G < 80
}

// onUI returns f's result on the UI thread.
func onUI(f func() bool) bool {
	var ok bool
	Do(func() { ok = f() })
	return ok
}

// videoPixel renders v and returns its center pixel.
func videoPixel(t *testing.T, v *Video) color.NRGBA {
	t.Helper()
	var img *image.NRGBA
	var err error
	Do(func() { img, err = RenderImage(VideoView(v).Size(32, 32), 32, 32, 1) })
	if err != nil {
		t.Fatal(err)
	}
	return img.NRGBAAt(16, 16)
}

func TestVideoPlayback(t *testing.T) {
	path := testMovie(t)
	ended := 0
	var v *Video
	var err error
	Do(func() {
		v, err = OpenVideo(path, VideoOptions{Muted: true, OnEnded: func() { ended++ }, OnError: func(e error) { t.Errorf("OnError: %v", e) }})
	})
	if err != nil {
		t.Fatal(err)
	}
	defer Do(v.Close)
	var state VideoState
	Do(func() { state = v.State() })
	if state != VideoLoading {
		t.Fatalf("state %v after open, want loading", state)
	}
	waitVideo(t, "ready", func() bool { return onUI(func() bool { return v.State() == VideoReady }) })
	Do(func() {
		if w, h := v.Size(); w != 64 || h != 64 {
			t.Errorf("size %dx%d", w, h)
		}
		if d := v.Duration(); d < 1900*time.Millisecond || d > 2100*time.Millisecond {
			t.Errorf("duration %v", d)
		}
		if v.Playing() || v.Position() != 0 {
			t.Errorf("playing %v at %v before Play", v.Playing(), v.Position())
		}
	})

	// The first frame shows while paused.
	waitVideo(t, "first frame", func() bool { return looksLike(videoPixel(t, v), movieRed) })

	// A seek while paused brings the frame at the new position.
	Do(func() { v.Seek(1500 * time.Millisecond) })
	waitVideo(t, "frame after seek", func() bool { return looksLike(videoPixel(t, v), movieBlue) })
	Do(func() {
		if p := v.Position(); p < 1400*time.Millisecond || p > 1600*time.Millisecond {
			t.Errorf("position after seek %v", p)
		}
	})

	// Playing from 1.5 s reaches the end.
	Do(v.Play)
	Do(func() {
		if !v.Playing() || v.State() != VideoPlaying {
			t.Errorf("after Play: playing %v state %v", v.Playing(), v.State())
		}
	})
	waitVideo(t, "end", func() bool { return onUI(func() bool { return v.State() == VideoEnded }) })
	if ended != 1 {
		t.Errorf("OnEnded ran %d times", ended)
	}
	Do(func() {
		if v.Playing() {
			t.Error("playing after the end")
		}
	})

	// With Loop, playback starts over at the end instead of ending.
	Do(func() {
		v.SetLoop(true)
		v.Play() // from the start, as the video ended
		v.Seek(1700 * time.Millisecond)
	})
	waitVideo(t, "loop", func() bool { return onUI(func() bool { p := v.Position(); return p > 0 && p < time.Second }) })
	Do(func() {
		if v.State() != VideoPlaying || !v.Playing() {
			t.Errorf("after looping: state %v playing %v", v.State(), v.Playing())
		}
		v.Pause()
		if v.Playing() || v.State() != VideoReady {
			t.Errorf("after Pause: state %v playing %v", v.State(), v.Playing())
		}
	})
	if ended != 1 {
		t.Errorf("OnEnded ran %d times with Loop", ended)
	}

	Do(func() {
		if !v.Muted() {
			t.Error("not muted")
		}
		v.SetMuted(false)
		if v.Muted() {
			t.Error("muted after SetMuted(false)")
		}
		v.SetMuted(true)
		v.SetVolume(0.25)
		if vol := v.Volume(); vol < 0.24 || vol > 0.26 {
			t.Errorf("volume %v", vol)
		}
		v.SetVolume(3)
		if vol := v.Volume(); vol != 1 {
			t.Errorf("volume %v after setting 3", vol)
		}
		v.Close()
		if v.Playing() || v.Volume() != 0 || v.Position() != 0 {
			t.Error("closed video still reports playback")
		}
		if f := v.currentFrame(); f != nil {
			t.Error("closed video has a frame")
		}
		if len(v.inflight) != 0 || v.cache != 0 || v.last != 0 || v.dev != nil || len(v.blocks) != 0 {
			t.Errorf("closed video holds %d copies, cache %v, buffer %v, device %v, %d blocks",
				len(v.inflight), v.cache, v.last, v.dev, len(v.blocks))
		}
		v.Close()
	})
}

// A playing video in a window keeps the window painting, and AutoPlay
// starts it once it loads.
func TestVideoAutoPlayWindow(t *testing.T) {
	path := testMovie(t)
	var v *Video
	var err error
	Do(func() { v, err = OpenVideo(path, VideoOptions{AutoPlay: true, Muted: true, Loop: true}) })
	if err != nil {
		t.Fatal(err)
	}
	defer Do(v.Close)
	w := newTestWindow(t, func() *El { return VideoView(v).Size(64, 64) })
	waitVideo(t, "autoplay", func() bool { return onUI(func() bool { return v.State() == VideoPlaying }) })
	waitVideo(t, "a frame in the window", func() bool {
		return onUI(func() bool {
			w.renderFrame()
			return v.frame != nil && w.animating
		})
	})
	// Frames keep coming while it plays, and their buffers are released
	// once the GPU copied them.
	start := time.Now()
	frames := 0
	var last uintptr
	for time.Since(start) < 500*time.Millisecond {
		Do(func() {
			w.renderFrame()
			if v.last != last {
				frames++
				last = v.last
			}
			if n := len(v.inflight); n > 4 {
				t.Errorf("%d frame copies in flight", n)
			}
		})
		time.Sleep(5 * time.Millisecond)
	}
	if frames < 3 {
		t.Errorf("%d new frames in 500ms of playback", frames)
	}
}

func TestVideoMissingFile(t *testing.T) {
	failed := make(chan error, 1)
	var v *Video
	var err error
	Do(func() {
		v, err = OpenVideo(filepath.Join(t.TempDir(), "missing.mp4"), VideoOptions{OnError: func(e error) { failed <- e }})
	})
	if err != nil {
		t.Fatal(err)
	}
	defer Do(v.Close)
	select {
	case e := <-failed:
		var state VideoState
		var verr error
		Do(func() { state, verr = v.State(), v.Err() })
		if state != VideoFailed || verr != e || e == nil {
			t.Errorf("state %v err %v, OnError %v", state, verr, e)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no error for a missing file")
	}
	Do(func() {
		if w, h := v.Size(); w != 0 || h != 0 {
			t.Errorf("failed video has size %dx%d", w, h)
		}
		v.Play()
		if v.Playing() {
			t.Error("failed video plays")
		}
	})
}

// A movie recorded on a phone held upright is stored on its side with a
// transform that turns it; it plays turned, as on Windows.
func TestVideoRotated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rotated.mov")
	// Stored 64x32, red on the left and blue on the right; turned a quarter
	// clockwise it shows 32x64, red on top and blue below.
	writeTestMovieSpec(t, path, movieSpec{w: 64, h: 32, frames: 5,
		paint: func(_, x, _ int) color.NRGBA {
			if x < 32 {
				return movieRed
			}
			return movieBlue
		},
		transform: &cgAffineTransform{A: 0, B: 1, C: -1, D: 0, TX: 32},
	})
	var v *Video
	var err error
	Do(func() { v, err = OpenVideo(path, VideoOptions{Muted: true}) })
	if err != nil {
		t.Fatal(err)
	}
	defer Do(v.Close)
	waitVideo(t, "ready", func() bool { return onUI(func() bool { return v.State() == VideoReady }) })
	pixels := func() (top, bottom color.NRGBA) {
		var img *image.NRGBA
		Do(func() { img, err = RenderImage(VideoView(v).Size(32, 64), 32, 64, 1) })
		if err != nil {
			t.Fatal(err)
		}
		return img.NRGBAAt(16, 12), img.NRGBAAt(16, 52)
	}
	waitVideo(t, "a turned frame", func() bool {
		top, bottom := pixels()
		return looksLike(top, movieRed) && looksLike(bottom, movieBlue)
	})
	Do(func() {
		if w, h := v.Size(); w != 32 || h != 64 {
			t.Errorf("size %dx%d, want 32x64", w, h)
		}
	})
}

// A window showing a paused video keeps painting while a seek runs, however
// long it takes, and paints the frame it brings.
func TestVideoSeekKeepsPainting(t *testing.T) {
	path := testMovie(t)
	var v *Video
	var err error
	Do(func() { v, err = OpenVideo(path, VideoOptions{Muted: true}) })
	if err != nil {
		t.Fatal(err)
	}
	defer Do(v.Close)
	w := newTestWindow(t, func() *El { return VideoView(v).Size(64, 64) })
	waitVideo(t, "the first frame", func() bool { return onUI(func() bool { w.renderFrame(); return v.last != 0 }) })
	animating := false
	Do(func() {
		v.Seek(1500 * time.Millisecond)
		v.awaitUntil = time.Time{} // as if the seek took longer than videoFrameWait
		w.renderFrame()
		animating = w.animating
	})
	if !animating {
		t.Error("the window stopped painting during a seek")
	}
	waitVideo(t, "the seek to finish", func() bool { return onUI(func() bool { return v.seeking == 0 }) })
	waitVideo(t, "the frame after the seek", func() bool { return looksLike(videoPixel(t, v), movieBlue) })
}

// OnEnded runs outside a main dispatch queue block, so that a modal dialog
// it opens does not hold up the main queue's blocks and timers.
func TestVideoEndedOffMainQueue(t *testing.T) {
	path := testMovie(t)
	served := make(chan bool, 1)
	var v *Video
	var err error
	Do(func() {
		v, err = OpenVideo(path, VideoOptions{Muted: true, OnEnded: func() {
			// A nested run loop, as a modal dialog runs.
			ran := false
			darwin.Async(func() { ran = true })
			mode := darwin.SymbolID(darwin.Foundation, "NSDefaultRunLoopMode")
			loop := darwin.SendClass("NSRunLoop", "currentRunLoop")
			for deadline := time.Now().Add(2 * time.Second); !ran && time.Now().Before(deadline); {
				darwin.WithPool(func() {
					darwin.Send(loop, "runMode:beforeDate:", mode, darwin.SendClass("NSDate", "dateWithTimeIntervalSinceNow:", 0.05))
				})
			}
			served <- ran
		}})
	})
	if err != nil {
		t.Fatal(err)
	}
	defer Do(v.Close)
	waitVideo(t, "ready", func() bool { return onUI(func() bool { return v.State() == VideoReady }) })
	Do(func() {
		v.Seek(1800 * time.Millisecond)
		v.Play()
	})
	select {
	case ran := <-served:
		if !ran {
			t.Error("the main queue stalled while OnEnded ran a nested run loop")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("OnEnded did not run")
	}
}
