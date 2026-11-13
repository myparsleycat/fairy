package fairy

import (
	"runtime"
	"time"

	"github.com/myparsleycat/fairy/internal/darwin"
	"github.com/myparsleycat/fairy/internal/render"
)

func (w *Window) drawSurface(e *El, se *surfaceEntry, dev *render.Device, width, height int) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	dev.Lock()
	defer dev.Unlock()
	tex := se.target.MTLTexture()
	render.ClearTexture(dev, tex, [4]float32{})
	f := metalFrame(dev, tex, width, height, w.scale)
	f.Time = time.Since(se.start)
	darwin.WithPool(func() { se.again = e.surface(f) })
	se.drawn = true
	se.scale = w.scale
	if e.retain != nil {
		se.version = *e.retain
	}
}
