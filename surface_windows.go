package fairy

import (
	"runtime"
	"time"

	"github.com/myparsleycat/fairy/internal/render"
)

type surfaceEntry struct {
	target *render.Target
	start  time.Time
	frame  int

	drawn   bool    // target holds a finished draw
	again   bool    // the last draw asked to be called on the next frame
	version uint64  // Retain version of the last draw
	scale   float32 // window scale at the last draw
	gen     uint64  // device generation of the target
}

// surfaceNeedsDraw decides whether a surface runs its draw function this
// paint. A surface without Retain draws every time.
func surfaceNeedsDraw(se *surfaceEntry, retain *uint64, scale float32, gen uint64, invalidated bool) bool {
	return retain == nil || !se.drawn || se.again || invalidated ||
		se.version != *retain || se.scale != scale || se.gen != gen
}

func (w *Window) paintSurface(e *El, box rectF, radii, clip [4]float32, opacity float32) {
	dst := w.snap(box)
	width, height := int(dst[2]), int(dst[3])
	if width <= 0 || height <= 0 || e.surface == nil {
		return
	}
	se := w.surfaces[e.id]
	if se == nil {
		se = &surfaceEntry{start: time.Now()}
		w.surfaces[e.id] = se
	}
	se.frame = w.frame
	dev := w.r.Device()
	if se.target == nil || se.target.Width != width || se.target.Height != height || se.gen != dev.Gen {
		se.target.Release()
		se.drawn = false
		t, err := render.NewTarget(dev, width, height)
		if err != nil {
			se.target = nil
			return
		}
		se.target = t
		se.gen = dev.Gen
	}
	invalidated := e.key != "" && w.invalidSurfaces[e.key]
	if surfaceNeedsDraw(se, e.retain, w.scale, dev.Gen, invalidated) {
		w.drawSurface(e, se, dev, width, height)
	}
	if se.again {
		w.animating = true
	}
	w.scene.AddImage(render.Prim{
		Bounds: dst,
		Clip:   clip,
		Color:  [4]float32{1, 1, 1, opacity},
		Radii:  radii,
		UV:     [4]float32{0, 0, 1, 1},
		Flags:  render.FlagPremultiplied,
	}, &se.target.Texture)
}

func (w *Window) drawSurface(e *El, se *surfaceEntry, dev *render.Device, width, height int) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	dev.Lock()
	defer dev.Unlock()
	dev.Ctx.ClearRenderTargetView(se.target.View(), [4]float32{})
	se.again = e.surface(SurfaceFrame{
		Device:     dev.Dev.P,
		Context:    dev.Ctx.P,
		Texture:    se.target.Resource().P,
		Target:     se.target.View().P,
		Width:      width,
		Height:     height,
		Scale:      w.scale,
		Time:       time.Since(se.start),
		Generation: dev.Gen,
	})
	dev.Ctx.ClearState()
	se.drawn = true
	se.scale = w.scale
	if e.retain != nil {
		se.version = *e.retain
	}
}

// sweepSurfaces frees targets of surfaces that were not painted this frame
// and forgets invalidations of surfaces that are gone.
func (w *Window) sweepSurfaces() {
	for id, se := range w.surfaces {
		if se.frame != w.frame {
			se.target.Release()
			delete(w.surfaces, id)
		}
	}
	clear(w.invalidSurfaces)
}

// InvalidateSurface makes the retained Surface with key draw again on the
// next paint, and asks for that paint. Use it when what the surface shows
// changed without a new Retain version, as when a model finishes loading.
// Call it on the UI thread; from other goroutines use Post.
func (w *Window) InvalidateSurface(key string) {
	if w.invalidSurfaces == nil {
		w.invalidSurfaces = map[string]bool{}
	}
	w.invalidSurfaces[key] = true
	w.invalidate()
}
