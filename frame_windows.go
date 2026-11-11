package fairy

import (
	"log"
	"time"

	"github.com/myparsleycat/fairy/internal/render"
	"github.com/myparsleycat/fairy/internal/win"
)

const stateTTL = 3 // frames an element's state survives without being built

func (w *Window) renderFrame() {
	w.needPaint = false
	if w.r == nil || w.width <= 0 || w.height <= 0 {
		return
	}
	// A minimized window draws nothing. Restoring it sends WM_PAINT, which
	// resumes any animation.
	if win.IsIconic(w.hwnd) {
		return
	}
	if !w.r.HasTarget() && !w.resizeRenderer() {
		return
	}
	w.wakeAt = time.Time{}
	start := time.Now()
	ticks := win.Ticks()
	dt := w.frameDT(start)
	w.lastFrame = start
	w.ft = frameTimes{}
	w.animating = false
	w.flushPointerMove()
	w.stepAutoScroll(dt)
	for i := 0; i < 3; i++ {
		if w.dirty {
			w.ft.causes |= w.dirtyBy
			w.ft.builds++
			w.ts.BeginBuild()
			w.build()
			w.ts.EndBuild()
		}
		// Tracks advance once per frame, however many times the tree is placed.
		w.dt = 0
		if i == 0 {
			w.dt = dt
		}
		w.mark()
		w.placeAll()
		w.lap(&w.ft.place)
		if w.mouseIn {
			w.updateHover(w.mouseX, w.mouseY)
		}
		w.retarget()
		if !w.dirty {
			break
		}
	}
	w.dt = dt
	w.uploaded = 0
	w.imageWorkEpoch++
	for {
		w.mark()
		ok := w.paintScene()
		w.lap(&w.ft.paint)
		w.dt = 0
		if ok || w.r.Atlas.Grow() != nil {
			break
		}
	}
	w.finishImageWork()
	w.dt = 0
	err := w.r.Draw(&w.scene, w.clearColor())
	occluded := err == nil && w.r.Occluded()
	if err == render.ErrDeviceLost {
		w.recreateRenderer()
	}
	// WM_IME_COMPOSITION arrives before the composed text is laid out, so the
	// candidate window is moved again once the caret is where it is drawn.
	if w.imeOn {
		if _, st := w.imeInput(); st != nil && st.comp != "" {
			w.updateIMEPosition()
		}
	}
	w.ts.EndFrame()
	w.a11yAfterFrame()
	if debugFrames {
		st := w.r.Stats()
		w.ft.record, w.ft.present = st.Record, st.Present
		w.logFrame(win.Ticks()-ticks, st.Batches)
	}
	w.frame++
	switch {
	case !win.IsWindowVisible(w.hwnd):
		// Hidden windows wait for the WM_PAINT that showing them sends.
	case w.animating && occluded:
		win.SetTimer(w.hwnd, timerAnim, hiddenFrameMs)
	case w.animating && w.sizeMove:
		win.SetTimer(w.hwnd, timerAnim, sizeMoveFrameMs)
	case w.animating:
		w.invalidate()
	case !w.wakeAt.IsZero():
		win.SetTimer(w.hwnd, timerAnim, uint32(max(time.Until(w.wakeAt).Milliseconds(), 1)))
	}
	if !w.animating {
		w.lastFrame = time.Time{}
	}
}

// resizeRenderer matches the swap chain to the client size and reports whether
// the renderer can draw afterwards.
func (w *Window) resizeRenderer() bool {
	err := w.r.Resize(w.width, w.height)
	switch {
	case err == render.ErrDeviceLost:
		w.recreateRenderer()
		return false
	case err != nil:
		if !w.resizeErr {
			log.Printf("fairy: resize swap chain: %v", err)
		}
		w.resizeErr = true
		win.SetTimer(w.hwnd, timerRecover, resizeRetryMs)
		return false
	}
	w.resizeErr = false
	return true
}

// frameAt asks for a frame at t without animating continuously until then.
func (w *Window) frameAt(t time.Time) {
	if w.wakeAt.IsZero() || t.Before(w.wakeAt) {
		w.wakeAt = t
	}
}

func (w *Window) newRenderer() (r *render.Renderer, err error) {
	defer func() {
		if err == nil && debugGPU {
			r.SetGPUTiming(true)
		}
	}()
	composition := w.opts.DynamicBackdrop || w.backdrop != BackdropNone
	r, err = render.New(uintptr(w.hwnd), w.width, w.height, composition)
	if err == nil || !composition {
		return r, err
	}

	log.Printf("fairy: window backdrop off: %v", err)
	w.opts.DynamicBackdrop = false
	if w.backdrop != BackdropNone {
		w.applyBackdrop(BackdropNone)
		w.backdrop = BackdropNone
	}
	return render.New(uintptr(w.hwnd), w.width, w.height, false)
}

// recreateRenderer replaces the renderer after the GPU device was lost. When a
// new one cannot be created yet, for example while a driver is updating, it
// retries with backoff and the window draws nothing until it succeeds.
func (w *Window) recreateRenderer() {
	if w.r != nil {
		w.releaseTextures()
		for _, se := range w.surfaces {
			se.target.Release()
		}
		clear(w.surfaces)
		w.r.Release()
		w.r = nil
	}
	r, err := w.newRenderer()
	if err != nil {
		w.recoverMs = min(max(w.recoverMs*2, 100), 2000)
		log.Printf("fairy: cannot recreate renderer, retrying in %dms: %v", w.recoverMs, err)
		win.SetTimer(w.hwnd, timerRecover, uint32(w.recoverMs))
		return
	}
	w.recoverMs = 0
	w.r = r
	w.ts.ResetAtlas(r.Atlas)
	clear(w.iconTiles)
	w.markDirty(dirtyDevice)
	w.invalidate()
}

func (w *Window) markDirty(c dirtyCause) {
	w.dirty = true
	w.dirtyBy |= c
}

// Virtual clock for scripts (FAIRY_CLOCK=fixed, or "clock DURATION"): frames
// advance animations by a fixed step, and only frames run by "step" advance
// them at all, so snapshots of animations are the same on every run.
type scriptClock struct {
	dt       time.Duration
	now      time.Time
	stepping bool
}

// now is the time animations and paint-time clocks read.
func (w *Window) now() time.Time {
	if c := w.clock; c != nil {
		return c.now
	}
	return time.Now()
}

// frameDT returns how far this frame advances animations.
func (w *Window) frameDT(start time.Time) float32 {
	if c := w.clock; c != nil {
		if !c.stepping {
			return 0
		}
		c.now = c.now.Add(c.dt)
		return float32(c.dt.Seconds())
	}
	if w.lastFrame.IsZero() {
		return 0
	}
	return float32(min(start.Sub(w.lastFrame).Seconds(), 0.05))
}
