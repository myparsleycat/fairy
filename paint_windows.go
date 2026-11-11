package fairy

import (
	"math"
	"time"

	"github.com/myparsleycat/fairy/internal/render"
	"github.com/myparsleycat/fairy/motion"
)

var pulseEpoch = time.Now()

// paintScene converts the laid-out tree into primitives. It returns false when
// the glyph atlas overflowed and the frame must be painted again.
func (w *Window) paintScene() bool {
	w.scene.Reset()
	ok := true
	if w.root != nil {
		w.stepGhosts()
		ok = w.paint(w.root, 1, w.opts.TextColor)
		for _, l := range w.layers {
			if !l.hidden {
				ok = w.paint(l.root, 1, w.opts.TextColor) && ok
			}
		}
		ok = w.paintGhostsOf(nil, 1, w.opts.TextColor) && ok
	}
	ok = w.paintFlights() && ok
	if w.tipText != "" && (w.tipShown || !w.tipLeft.IsZero()) {
		ok = w.paintTooltip() && ok
	}
	w.sweepSurfaces()
	w.sweepWebViews()
	w.sweepTextures(w.frame, maxIdleTextureBytes)
	return ok
}

func (w *Window) paint(e *El, opacity float32, inheritedColor Color) bool {
	if e.culled {
		return true
	}
	if e.flying && !w.paintingFlight {
		w.flights = append(w.flights, flight{e, opacity, inheritedColor, w.scene.Clip})
		return true
	}
	if e.live != nil {
		w.applyLive(e)
	}
	if e.xfOwn {
		start, clips := len(w.scene.Prims), len(w.scene.Clips)
		e.xfOwn = false
		ok := w.paint(e, opacity, inheritedColor)
		e.xfOwn = true
		w.transformPrims(e, start)
		// Clips set inside the subtree turn with it.
		for i := clips; i < len(w.scene.Clips); i++ {
			w.scene.Clips[i].Space = 1
		}
		return ok
	}
	r := rectF{e.absX, e.absY, e.w, e.h}
	clip := e.clipR
	ok := true
	s := w.scale
	devClip := w.snap(clip)
	hov := w.hovered(e)

	textColor := inheritedColor
	if e.tp.color.isSet() {
		textColor = e.tp.color
	}
	if hov && e.hoverText.isSet() {
		textColor = e.hoverText
	}
	var focused, focusVisible bool
	if e.focusBorder.isSet() || e.focusVisBorder.isSet() || e.focusFill.isSet() || e.focusRing.width > 0 {
		focused, focusVisible = w.focusOf(e)
	}
	fill := e.fill
	if hov && e.hoverFill.isSet() {
		fill = e.hoverFill
	}
	if focused && e.focusFill.isSet() {
		fill = e.focusFill
	}
	if w.highlighted(e) {
		fill = e.focus.highlight
	}
	if w.pressed(e) && e.pressFill.isSet() {
		fill = e.pressFill
	}
	borderColor := e.borderColor
	if hov && e.hoverBorder.isSet() {
		borderColor = e.hoverBorder
	}
	if focused && e.focusBorder.isSet() {
		borderColor = e.focusBorder
	}
	if focusVisible && e.focusVisBorder.isSet() {
		borderColor = e.focusVisBorder
	}
	rg := e.ring
	if e.hoverRing.width > 0 {
		if hov {
			rg = e.hoverRing
		} else if rg.width == 0 {
			rg.width = e.hoverRing.width
		}
	}
	if e.focusRing.width > 0 {
		if focusVisible {
			rg = e.focusRing
		} else if rg.width == 0 {
			rg.width = e.focusRing.width
		}
	}
	if w.drag != nil && w.drag.target == e.id && w.drag.rejected {
		if e.dropReject.isSet() {
			borderColor = e.dropReject
		}
	} else if w.drag != nil && w.drag.target == e.id {
		if e.dropFill.isSet() {
			fill = e.dropFill
		}
		if e.dropBorder.isSet() {
			borderColor = e.dropBorder
		}
	}
	alpha := e.opacity
	if m := e.motion; m != nil && e.state != nil {
		st := e.state
		fill = vecColor(w.animate(st, animFill, 4, 0.002, colorVec(fill), *m))
		borderColor = vecColor(w.animate(st, animBorder, 4, 0.002, colorVec(borderColor), *m))
		if e.tp.color.isSet() || e.hoverText.isSet() {
			textColor = vecColor(w.animate(st, animText, 4, 0.002, colorVec(textColor), *m))
		}
		// Springs and retargeted eases can overshoot the target.
		alpha = min(max(w.animate(st, animOpacity, 1, 0.002, [4]float32{e.opacity}, *m)[0], 0), 1)
		if rg.width > 0 {
			rg.color = vecColor(w.animate(st, animRing, 4, 0.002, colorVec(rg.color), *m))
		}
	}
	if e.pulse != nil && !reducedMotion.Load() {
		alpha *= e.pulse.at(w.now().Sub(pulseEpoch).Seconds())
		w.animating = true
	}
	opacity *= alpha * e.fade
	if opacity <= 0.001 {
		return true
	}
	// An isolated element draws at full opacity into a layer, which then
	// goes onto the window at the element's.
	layered := e.isolate || e.blur > 0
	layerMark, groupOpacity := 0, opacity
	if layered {
		layerMark, opacity = w.scene.BeginLayer(), 1
	}

	// The shader expects each radius to fit in half the box; larger ones, as
	// from Radius(999) for a pill, would leave the shape empty.
	maxR := min(r.w, r.h) * s / 2
	radii := [4]float32{min(e.radii[0]*s, maxR), min(e.radii[1]*s, maxR), min(e.radii[2]*s, maxR), min(e.radii[3]*s, maxR)}
	box := w.snap(r)

	for i, sh := range e.shadows {
		if m := e.motion; m != nil && e.state != nil && i < 2 {
			sh.Color = vecColor(w.animate(e.state, animShadow0+animProp(i), 4, 0.002, colorVec(sh.Color), *m))
		}
		if sh.Inset || !sh.Color.isSet() {
			continue
		}
		w.scene.Add(w.shadowPrim(box, radii, devClip, sh, opacity))
	}

	if e.backdropBlur > 0 {
		w.scene.AddBackdrop(render.Prim{Bounds: box, Clip: devClip, Radii: radii, Color: [4]float32{1, 1, 1, opacity}}, e.backdropBlur*s)
	}

	if rg.width > 0 && rg.color.isSet() {
		// Use the same pixel-aligned box as the fill so it cannot cover the ring.
		d := snapWidth(rg.width, s)
		w.scene.Add(render.Prim{
			Bounds:      [4]float32{box[0] - d, box[1] - d, box[2] + 2*d, box[3] + 2*d},
			Clip:        devClip,
			BorderColor: rg.color.arr(opacity),
			Border:      [4]float32{d, d, d, d},
			Radii:       [4]float32{radii[0] + d, radii[1] + d, radii[2] + d, radii[3] + d},
			Kind:        render.KindRect,
		})
	}

	border := render.Prim{
		Bounds:      box,
		Clip:        devClip,
		BorderColor: borderColor.arr(opacity),
		Radii:       radii,
		Kind:        render.KindRect,
	}
	hasBorder := (borderColor.isSet() || e.fillPadding) && (e.border.Top > 0 || e.border.Right > 0 || e.border.Bottom > 0 || e.border.Left > 0)
	if hasBorder {
		border.Border = [4]float32{
			snapWidth(e.border.Top, s), snapWidth(e.border.Right, s),
			snapWidth(e.border.Bottom, s), snapWidth(e.border.Left, s),
		}
		if e.borderDashed && !(focused && e.focusBorder.isSet()) {
			border.Flags |= render.FlagDashed
		}
		if e.fillPadding {
			border.Flags |= render.FlagPaddingBox
		}
	}
	// Media can fill the whole box, so its border must be drawn on top.
	mediaBorder := hasBorder && (e.kind == kindImage || e.kind == kindSurface || e.kind == kindVideo)
	if fill.isSet() || e.grad != nil || (hasBorder && !mediaBorder) {
		p := border
		p.Color = fill.arr(opacity)
		if mediaBorder {
			p.Border = [4]float32{}
		}
		if e.grad != nil {
			p.Flags |= render.FlagGradient
			p.Color = e.grad.from.arr(opacity)
			p.Color2 = e.grad.to.arr(opacity)
			p.Params[0] = e.grad.angle
		}
		w.scene.Add(p)
	}

	for _, sh := range e.shadows {
		if !sh.Inset || !sh.Color.isSet() {
			continue
		}
		w.scene.Add(w.shadowPrim(box, radii, devClip, sh, opacity))
	}

	pad := e.padding()
	content := rectF{r.x + pad.Left, r.y + pad.Top, max(r.w-pad.Left-pad.Right, 0), max(r.h-pad.Top-pad.Bottom, 0)}

	outerClip := w.scene.Clip
	var inner, innerRadii [4]float32
	rounded := false
	if e.clip {
		inner = w.innerBox(e, box)
		t, l := inner[1]-box[1], inner[0]-box[0]
		b, rt := box[3]-inner[3]-t, box[2]-inner[2]-l
		innerRadii = [4]float32{
			max(radii[0]-max(l, t), 0),
			max(radii[1]-max(t, rt), 0),
			max(radii[2]-max(rt, b), 0),
			max(radii[3]-max(b, l), 0),
		}
		rounded = innerRadii != [4]float32{}
		feather := w.edgeFeather(e)
		if rounded || feather != [4]float32{} {
			w.scene.PushClip(render.ClipRec{Rect: inner, Radii: innerRadii, Feather: feather})
		}
	}

	switch e.kind {
	case kindText:
		if e.text != "" {
			l, tx, ty := w.textLayout(e)
			if w.sel != nil {
				w.paintSelection(e, l, tx, ty, devClip, opacity)
			}
			if len(e.spans) > 0 {
				w.paintRichBackgrounds(e, l, tx, ty, devClip, opacity)
				ok = l.DrawRuns(&w.scene, tx, ty, textColor.arr(opacity), spanColors(e, opacity), devClip) && ok
			} else {
				start := len(w.scene.Prims)
				ok = l.Draw(&w.scene, tx, ty, textColor.arr(opacity), devClip) && ok
				if e.shimmer != nil && !reducedMotion.Load() {
					shimmerGlyphs(w.scene.Prims[start:], e.shimmer, w.now().Sub(pulseEpoch).Seconds(),
						e.shimmer.highlight.arr(opacity))
					w.animating = true
				}
			}
		}
	case kindImage:
		w.paintImage(e, content, radii, devClip, opacity)
	case kindInput:
		ok = w.paintInput(e, content, clip, textColor, opacity) && ok
	case kindIcon:
		ok = w.paintIcon(e, content, textColor, devClip, opacity) && ok
	case kindSpinner:
		w.paintSpinner(content, textColor, devClip, opacity)
	case kindArc:
		w.paintArc(e, content, textColor, devClip, opacity)
	case kindSurface:
		w.paintSurface(e, content, radii, devClip, opacity)
	case kindVideo:
		w.paintVideo(e, content, radii, devClip, opacity)
	case kindWebView:
		w.placeWebView(e, content)
	}
	if mediaBorder {
		own := w.scene.Clip
		w.scene.Clip = outerClip
		w.scene.Add(border)
		w.scene.Clip = own
	}

	if len(w.ghosts) > 0 {
		ok = w.paintGhostsOf(e, opacity, textColor) && ok
	}
	for _, c := range e.children {
		ok = w.paint(c, opacity, textColor) && ok
	}
	// Scroll bars are not faded, but stay inside rounded corners.
	if w.scene.Clip != outerClip && e.edgeFade > 0 {
		w.scene.Clip = outerClip
		if rounded {
			w.scene.PushClip(render.ClipRec{Rect: inner, Radii: innerRadii})
		}
	}
	w.paintScrollBars(e, textColor, opacity)
	w.scene.Clip = outerClip
	if layered {
		w.scene.EndLayer(layerMark, render.Prim{Clip: devClip, Color: [4]float32{1, 1, 1, groupOpacity}}, e.blur*s)
	}
	return ok
}

// innerBox is box, in device pixels, less e's border: the area Clip keeps.
func (w *Window) innerBox(e *El, box [4]float32) [4]float32 {
	s := w.scale
	t, r, b, l := snapWidth(e.border.Top, s), snapWidth(e.border.Right, s), snapWidth(e.border.Bottom, s), snapWidth(e.border.Left, s)
	return [4]float32{box[0] + l, box[1] + t, max(box[2]-l-r, 0), max(box[3]-t-b, 0)}
}

// edgeFeather is how far ScrollEdgeFade fades each edge (top, right,
// bottom, left) in device pixels: the full width once 15 px are scrolled
// past that edge, less before.
func (w *Window) edgeFeather(e *El) [4]float32 {
	st := e.state
	if e.edgeFade <= 0 || st == nil {
		return [4]float32{}
	}
	n := &e.node
	f := func(hidden float32) float32 { return e.edgeFade * w.scale * min(max(hidden/15, 0), 1) }
	var out [4]float32
	if e.style.ScrollY {
		out[0], out[2] = f(st.scrollY), f(n.ContentH-n.H-st.scrollY)
	}
	if e.style.ScrollX {
		out[3], out[1] = f(st.scrollX), f(n.ContentW-n.W-st.scrollX)
	}
	return out
}

func (w *Window) shadowPrim(box, radii, clip [4]float32, sh Shadow, opacity float32) render.Prim {
	s := w.scale
	x, y, d := sh.X*s, sh.Y*s, sh.Spread*s
	if sh.Blur <= 0 {
		// Align hard shadow edges without quantizing blurred shadows.
		round := func(v float32) float32 { return float32(math.Round(float64(v))) }
		x, y, d = round(x), round(y), round(d)
	}
	p := render.Prim{
		Clip:   clip,
		Color:  sh.Color.arr(opacity),
		Params: [4]float32{sh.Blur * 0.5 * s},
		Kind:   render.KindShadow,
	}
	if sh.Inset {
		d = -d
		p.Flags = render.FlagInset
		p.UV, p.Border = box, radii
	}
	p.Bounds = [4]float32{box[0] + x - d, box[1] + y - d, box[2] + 2*d, box[3] + 2*d}
	maxR := max(0, min(p.Bounds[2], p.Bounds[3])/2)
	for i, r := range radii {
		p.Radii[i] = min(max(r+d, 0), maxR)
	}
	return p
}

func snapWidth(v, s float32) float32 {
	if v <= 0 {
		return 0
	}
	return float32(math.Max(1, math.Round(float64(v*s))))
}

// shimmerGlyphs recolors the glyph prims of one text by their place in it.
func shimmerGlyphs(prims []render.Prim, p *shimmerProps, elapsed float64, highlight [4]float32) {
	left, right := float32(math.Inf(1)), float32(math.Inf(-1))
	for _, g := range prims {
		if g.Kind == render.KindGlyph {
			left, right = min(left, g.Bounds[0]), max(right, g.Bounds[0]+g.Bounds[2])
		}
	}
	if right <= left {
		return
	}
	for i := range prims {
		g := &prims[i]
		if g.Kind != render.KindGlyph {
			continue
		}
		t := p.at(elapsed, (g.Bounds[0]+g.Bounds[2]/2-left)/(right-left))
		for c := range 4 {
			g.Color[c] += (highlight[c] - g.Color[c]) * t
		}
	}
}

var pulseCurve = motion.Bezier(1, 0.4, 0, 0.6, 1)

// pulseAt returns the opacity of a pulse at elapsed seconds.
func (p *pulseProps) at(elapsed float64) float32 {
	phase := float32(math.Mod(elapsed, float64(p.period))) / p.period
	if phase < 0.5 {
		y, _ := pulseCurve.BezierAt(phase * 2)
		return 1 - (1-p.low)*y
	}
	y, _ := pulseCurve.BezierAt(phase*2 - 1)
	return p.low + (1-p.low)*y
}

func abs32(f float32) float32 {
	if f < 0 {
		return -f
	}
	return f
}

func colorVec(c Color) [4]float32 {
	// Interpolate in premultiplied space so fades to transparent keep their hue.
	return [4]float32{c.R * c.A, c.G * c.A, c.B * c.A, c.A}
}

func vecColor(v [4]float32) Color {
	if v[3] <= 0.0001 {
		return Transparent
	}
	clamp := func(f float32) float32 { return min(max(f, 0), 1) }
	a := clamp(v[3])
	return Color{clamp(v[0] / v[3]), clamp(v[1] / v[3]), clamp(v[2] / v[3]), a}
}
