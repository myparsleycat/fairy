//go:build windows

package fairy

import (
	"time"

	"github.com/myparsleycat/fairy/internal/geom"
	"github.com/myparsleycat/fairy/internal/render"
	"github.com/myparsleycat/fairy/internal/text"
	"github.com/myparsleycat/fairy/internal/tooltip"
	"github.com/myparsleycat/fairy/motion"
)

// paintStyledTooltip draws the tooltip next to the element it belongs to,
// as SetTooltipStyle describes.
func (w *Window) paintStyledTooltip(ts *TooltipStyle) bool {
	now := time.Now()
	tm := tipTiming()
	p := w.tip.Progress(now, tm)
	switch {
	case !w.tip.Shown && (p <= 0 || now.Sub(w.tip.Left) >= tm.Duration()):
		w.tip.Left = time.Time{}
		return true
	case !w.tip.Shown || p < 1:
		w.animating = true
	}
	s := w.scale
	size := ts.FontSize
	if size <= 0 {
		size = 12
	}
	st := w.textStyle(textProps{size: size, weight: 400, family: w.opts.Font, italic: -1})
	l := w.ts.Layout(w.tip.Text, st, ts.MaxW*s)
	tw, th := l.Size()
	bw, bh := tw/s+2*ts.PadX, th/s+2*ts.PadY
	lw, lh := float32(w.width)/s, float32(w.height)/s
	a := w.tip.Anchor
	side := SideTop
	if w.tip.SideSet {
		side = Side(w.tip.Side)
	}
	x, y, used := geom.Tip(a, geom.Side(side), bw, bh, ts.Gap, lw, lh, w.chromeTop)
	side = Side(used)
	cx, cy := a.X+a.W/2, a.Y+a.H/2
	r := geom.Rect{X: x, Y: y, W: bw, H: bh}
	full := w.devRect(geom.Rect{X: 0, Y: 0, W: lw, H: lh})
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
		var ax, ay float32
		switch side {
		case SideTop, SideBottom:
			ax = min(max(cx, x+ts.Radius+halfSide), x+bw-ts.Radius-halfSide)
			ay = y + bh - 2
			if side == SideBottom {
				ay = y + 2
			}
		default:
			ay = min(max(cy, y+ts.Radius+halfSide), y+bh-ts.Radius-halfSide)
			ax = x + 2
			if side == SideLeft {
				ax = x + bw - 2
			}
		}
		bg.Kind = render.KindCallout
		bg.Params = [4]float32{ax * s, ay * s, halfSide * s, 2 * s}
	}
	w.scene.Add(bg)
	ok := l.Draw(&w.scene, (x+ts.PadX)*s, (y+ts.PadY)*s, ts.Text.arr(1), full)

	// Grow from 0.95 about the edge that faces the element, slide in from
	// it, and fade.
	ox, oy := min(max(cx, x), x+bw), y+bh
	dx, dy := float32(0), 8*(1-p)
	switch side {
	case SideBottom:
		oy, dy = y, -dy
	case SideRight:
		ox, oy, dx, dy = x, min(max(cy, y), y+bh), -dy, 0
	case SideLeft:
		ox, oy, dx, dy = x+bw, min(max(cy, y), y+bh), dy, 0
	}
	if !w.tip.Shown {
		dx, dy = 0, 0
	}
	m := turnAbout(ox*s, oy*s, 0.95+0.05*p, 0)
	m.tx += dx * s
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
	if !w.tip.Shown {
		return true
	}
	s := w.scale
	st := w.textStyle(textProps{size: 12, weight: 400, family: w.opts.Font, italic: -1})
	l := w.ts.Layout(w.tip.Text, st, 320*s)
	tw, th := l.Size()
	bw, bh := tw/s+16, th/s+10
	lw, lh := float32(w.width)/s, float32(w.height)/s
	x := min(w.mouseX+12, lw-bw-4)
	y := w.mouseY + 22
	if w.tip.SideSet {
		x, y, _ = geom.Tip(w.tip.Anchor, w.tip.Side, bw, bh, 6, lw, lh, w.chromeTop)
	} else if y+bh > lh-4 && w.mouseY-bh-8 >= w.chromeTop+4 {
		y = w.mouseY - bh - 8
	}
	full := w.devRect(geom.Rect{X: 0, Y: 0, W: lw, H: lh})
	bg, fg := Hex(0x2b2b2b), Hex(0xf2f2f2)
	if w.opts.Dark {
		bg, fg = Hex(0x3a3a3a), Hex(0xf2f2f2)
	}
	box := w.snap(geom.Rect{X: x, Y: y, W: bw, H: bh})
	radii := [4]float32{6 * s, 6 * s, 6 * s, 6 * s}
	shadow := Shadow{Y: 2, Blur: 8, Color: RGBA(0, 0, 0, 0.25)}
	w.scene.Add(w.shadowPrim(box, radii, full, shadow, 1))
	w.scene.Add(render.Prim{Bounds: box, Clip: full, Color: bg.arr(1), Radii: radii, Kind: render.KindRect})
	return l.Draw(&w.scene, (x+8)*s, (y+5)*s, fg.arr(1), full)
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

// tipTiming is how styled tooltips move.
func tipTiming() tooltip.Timing {
	var m *motion.Spec
	if ts := tooltipStyle.Load(); ts != nil {
		m = &ts.Motion
	}
	return tooltip.NewTiming(m, reducedMotion.Load())
}

func (w *Window) showTooltip() {
	for _, e := range w.hoverPath {
		if tip := w.tipOf(e); tip != "" {
			anchor := geom.Rect{X: e.absX, Y: e.absY, W: e.w, H: e.h}
			w.tip.Show(tip, anchor, geom.Side(e.tipSide), e.tipSideSet, time.Now(), tipTiming())
			w.invalidate()
			return
		}
	}
}

func (w *Window) hideTooltip() {
	w.killTimer(timerTip)
	ts := tooltipStyle.Load()
	fades := ts != nil && !ts.Motion.IsInstant() && ts.Motion.Duration() > 0
	if w.tip.Hide(time.Now(), tipTiming(), fades) {
		w.invalidate()
	}
}
