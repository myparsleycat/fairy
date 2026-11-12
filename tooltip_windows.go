package fairy

import (
	"time"

	"github.com/myparsleycat/fairy/internal/render"
	"github.com/myparsleycat/fairy/internal/text"
	"github.com/myparsleycat/fairy/internal/win"
)

func (w *Window) tipDuration() time.Duration {
	ts := tooltipStyle.Load()
	if ts == nil || ts.Motion.IsInstant() || ts.Motion.Duration() <= 0 || reducedMotion.Load() {
		return 0
	}
	return time.Duration(float64(ts.Motion.Duration()) * float64(time.Second))
}

// tipCurve returns how far a styled tooltip's motion has come after elapsed.
func (w *Window) tipCurve(elapsed time.Duration) float32 {
	d := w.tipDuration()
	if d <= 0 || elapsed >= d {
		return 1
	}
	x := float32(max(elapsed, 0)) / float32(d)
	if m := tooltipStyle.Load().Motion; m.IsBezier() {
		y, _ := m.BezierAt(x)
		return y
	}
	u := 1 - x
	return 1 - u*u*u
}

// tipElapsedAt returns how long the tooltip's motion takes to reach
// progress p, the inverse of tipCurve.
func (w *Window) tipElapsedAt(p float32) time.Duration {
	d := w.tipDuration()
	lo, hi := time.Duration(0), d
	for range 24 {
		mid := (lo + hi) / 2
		if w.tipCurve(mid) < p {
			lo = mid
		} else {
			hi = mid
		}
	}
	return hi
}

// tipProgress returns how much of a styled tooltip shows at now, from 0 to 1.
func (w *Window) tipProgress(now time.Time) float32 {
	if !w.tipShown {
		if w.tipLeft.IsZero() {
			return 0
		}
		return w.tipFrom * (1 - w.tipCurve(now.Sub(w.tipLeft)))
	}
	return w.tipCurve(now.Sub(w.tipAt))
}

// paintStyledTooltip draws the tooltip next to the element it belongs to,
// as SetTooltipStyle describes.
func (w *Window) paintStyledTooltip(ts *TooltipStyle) bool {
	now := time.Now()
	p := w.tipProgress(now)
	switch {
	case !w.tipShown && (p <= 0 || now.Sub(w.tipLeft) >= w.tipDuration()):
		w.tipLeft = time.Time{}
		return true
	case !w.tipShown || p < 1:
		w.animating = true
	}
	s := w.scale
	size := ts.FontSize
	if size <= 0 {
		size = 12
	}
	st := w.textStyle(textProps{size: size, weight: 400, family: w.opts.Font, italic: -1})
	l := w.ts.Layout(w.tipText, st, ts.MaxW*s)
	tw, th := l.Size()
	bw, bh := tw/s+2*ts.PadX, th/s+2*ts.PadY
	lw, lh := float32(w.width)/s, float32(w.height)/s
	a := w.tipAnchor
	top := true
	y := a.y - ts.Gap - bh
	if y < w.chromeTop+4 && a.y+a.h+ts.Gap+bh <= lh-4 {
		y, top = a.y+a.h+ts.Gap, false
	}
	cx := a.x + a.w/2
	x := min(max(cx-bw/2, 4), max(lw-bw-4, 4))
	r := rectF{x, y, bw, bh}
	full := w.devRect(rectF{0, 0, lw, lh})
	rad := ts.Radius * s
	start := len(w.scene.Prims)

	bg := render.Prim{
		Bounds: w.snap(r),
		Clip:   full,
		Color:  ts.Fill.arr(1),
		Radii:  [4]float32{rad, rad, rad, rad},
		Kind:   render.KindRect,
	}
	if ts.Arrow {
		const halfSide = 5
		ax := min(max(cx, x+ts.Radius+halfSide), x+bw-ts.Radius-halfSide)
		ay := y + bh - 2
		if !top {
			ay = y + 2
		}
		bg.Kind = render.KindCallout
		bg.Params = [4]float32{ax * s, ay * s, halfSide * s, 2 * s}
	}
	w.scene.Add(bg)
	ok := l.Draw(&w.scene, (x+ts.PadX)*s, (y+ts.PadY)*s, ts.Text.arr(1), full)

	// Grow from 0.95 about the edge that faces the element, slide in from
	// it, and fade.
	oy, dy := y+bh, 8*(1-p)
	if !top {
		oy, dy = y, -dy
	}
	if !w.tipShown {
		dy = 0
	}
	m := turnAbout(min(max(cx, x), x+bw)*s, oy*s, 0.95+0.05*p, 0)
	m.ty += dy * s
	prims := w.scene.Prims[start:]
	for i := range prims {
		pr := &prims[i]
		pr.Color[3] *= p
		if pr.Flags&render.FlagXform == 0 {
			pr.Flags |= render.FlagXform
			pr.Xform, pr.Xlate = [4]float32{1, 0, 0, 1}, [2]float32{}
			pr.Clip2 = render.NoClip
		}
		n := m.then(xform{pr.Xform[0], pr.Xform[1], pr.Xform[2], pr.Xform[3], pr.Xlate[0], pr.Xlate[1]})
		pr.Xform, pr.Xlate = [4]float32{n.a, n.b, n.c, n.d}, [2]float32{n.tx, n.ty}
	}
	return ok
}

func (w *Window) paintTooltip() bool {
	if ts := tooltipStyle.Load(); ts != nil {
		return w.paintStyledTooltip(ts)
	}
	if !w.tipShown {
		return true
	}
	s := w.scale
	st := w.textStyle(textProps{size: 12, weight: 400, family: w.opts.Font, italic: -1})
	l := w.ts.Layout(w.tipText, st, 320*s)
	tw, th := l.Size()
	bw, bh := tw/s+16, th/s+10
	lw, lh := float32(w.width)/s, float32(w.height)/s
	x := min(w.mouseX+12, lw-bw-4)
	y := w.mouseY + 22
	if y+bh > lh-4 && w.mouseY-bh-8 >= w.chromeTop+4 {
		y = w.mouseY - bh - 8
	}
	full := w.devRect(rectF{0, 0, lw, lh})
	bg, fg := Hex(0x2b2b2b), Hex(0xf2f2f2)
	if w.opts.Dark {
		bg, fg = Hex(0x3a3a3a), Hex(0xf2f2f2)
	}
	box := w.snap(rectF{x, y, bw, bh})
	radii := [4]float32{6 * s, 6 * s, 6 * s, 6 * s}
	shadow := Shadow{Y: 2, Blur: 8, Color: RGBA(0, 0, 0, 0.25)}
	w.scene.Add(w.shadowPrim(box, radii, full, shadow, 1))
	w.scene.Add(render.Prim{Bounds: box, Clip: full, Color: bg.arr(1), Radii: radii, Kind: render.KindRect})
	return l.Draw(&w.scene, (x+8)*s, (y+5)*s, fg.arr(1), full)
}

func (w *Window) showTooltip() {
	for _, e := range w.hoverPath {
		if tip := w.tipOf(e); tip != "" {
			now := time.Now()
			if w.tipLeft.IsZero() || w.tipText != tip || w.tipAnchor != (rectF{e.absX, e.absY, e.w, e.h}) {
				w.tipAt = now
			} else {
				// The same tooltip came back while leaving: it turns around
				// from where it has faded to.
				w.tipAt = now.Add(-w.tipElapsedAt(w.tipProgress(now)))
			}
			w.tipText = tip
			w.tipShown = true
			w.tipLeft = time.Time{}
			w.tipAnchor = rectF{e.absX, e.absY, e.w, e.h}
			w.invalidate()
			return
		}
	}
}

// tipOf returns the tooltip the element shows now, or "".
func (w *Window) tipOf(e *El) string {
	if !e.tipTruncated {
		return e.tooltip
	}
	if !w.truncated(e) {
		return ""
	}
	if e.tooltip != "" {
		return e.tooltip
	}
	return e.text
}

// truncated reports whether Ellipsis trims the text of e at its drawn width.
func (w *Window) truncated(e *El) bool {
	if e.kind != kindText || !e.tp.ellipsis || e.text == "" {
		return false
	}
	st := w.elTextStyle(e)
	st.Ellipsis = false
	nw, _ := w.ts.Layout(e.text, st, text.Unbounded).Size()
	pad := e.padding()
	return nw > max(e.w-pad.Left-pad.Right, 0)*w.scale+0.5
}

func (w *Window) hideTooltip() {
	win.KillTimer(w.hwnd, timerTip)
	if w.tipShown {
		if ts := tooltipStyle.Load(); ts != nil && !ts.Motion.IsInstant() && ts.Motion.Duration() > 0 {
			now := time.Now()
			w.tipFrom = w.tipProgress(now) // read while it still counts as shown
			w.tipLeft = now
		}
		w.tipShown = false
		w.invalidate()
	}
}
