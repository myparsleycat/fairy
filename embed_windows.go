package fairy

import (
	"github.com/myparsleycat/fairy/internal/embed"
	"github.com/myparsleycat/fairy/internal/geom"
)

func init() {
	embed.NewEl = func(x embed.Element) any {
		e := newEl(kindEmbed)
		e.embed = x
		return e
	}
}

// embedsMedia reports whether e shows embedded content drawn into the scene,
// as opposed to a child window.
func (e *El) embedsMedia() bool {
	_, native := e.embed.(embed.Native)
	return e.kind == kindEmbed && e.embed != nil && !native
}

func (w *Window) paintEmbed(e *El, box geom.Rect, radii, clip [4]float32, opacity float32) {
	if e.embed == nil {
		return
	}
	if n, ok := e.embed.(embed.Native); ok {
		if w.hwnd == 0 {
			return
		}
		w.natives[n] = true
	}
	c := &w.embedCtx
	if c.Window == nil {
		c.Window = w
		c.Snap = w.snap
		c.Animate = func() { w.animating = true }
		c.WakeAt = w.frameAt
	}
	c.HWND, c.Scale, c.Now, c.Scene = w.hwnd, w.scale, w.now(), &w.scene
	c.Box, c.Radii, c.Clip, c.Opacity, c.Fit = box, radii, clip, opacity, geom.Fit(e.fit)
	e.embed.Paint(c)
}

// sweepNatives tells the child windows this frame did not place.
func (w *Window) sweepNatives() {
	for n, placed := range w.natives {
		if !placed {
			n.Unplaced(w)
			delete(w.natives, n)
			continue
		}
		w.natives[n] = false
	}
}

func (w *Window) notifyNativesMoved() {
	for n := range w.natives {
		n.Moved(w)
	}
}

func (w *Window) closeNatives() {
	for n := range w.natives {
		n.HostClosed(w)
	}
}
