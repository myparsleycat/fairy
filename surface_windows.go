package fairy

import (
	"runtime"
	"time"

	"github.com/myparsleycat/fairy/internal/render"
)

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
