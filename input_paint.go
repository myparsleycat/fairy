//go:build windows

package fairy

import (
	"math"

	"github.com/myparsleycat/fairy/internal/edit"
	"github.com/myparsleycat/fairy/internal/geom"
	"github.com/myparsleycat/fairy/internal/render"
	"github.com/myparsleycat/fairy/internal/spell"
	"github.com/myparsleycat/fairy/internal/text"
)

// inputGeom records how an input was last drawn, for pointer hit testing and IME placement.
type inputGeom struct {
	text             string
	style            text.Style
	width            float32
	originX, originY float32   // device pixels
	caret            geom.Rect // logical
	password         bool
	multiline        bool
	value            string // the unmasked text being shown
	bounds           []int  // grapheme cluster boundaries of value, for a password
}

// shownGeom returns the geometry of the field as it holds its value now. Keys
// handled before the next paint can change the value after it was drawn; the
// text is then laid out again, while the origin stays where it was drawn.
func (s *editState) shownGeom() *inputGeom {
	g := s.geom
	if g == nil || g.value == s.Value {
		return g
	}
	c := *g
	c.value, c.text = s.Value, s.Value
	if c.password {
		c.bounds = s.clusters(s.Value)
		c.text = edit.Mask(c.bounds)
	}
	if c.multiline {
		c.text += "​"
	}
	return &c
}

func (s *editState) clusters(v string) []int {
	if s.clusterBounds == nil || s.clusterText != v {
		s.clusterText, s.clusterBounds = v, text.Clusters(v)
	}
	return s.clusterBounds
}

func (s *editState) withZWSP(v string) string {
	if len(s.zwspText) != len(v)+len("​") || s.zwspText[:len(v)] != v {
		s.zwspText = v + "​"
	}
	return s.zwspText
}

func (g *inputGeom) toDisplay(b int) int {
	if g.password {
		return edit.MaskedOffset(g.bounds, edit.ClampOffset(g.value, b))
	}
	return b
}

func (g *inputGeom) fromDisplay(d int) int {
	if g.password {
		d = edit.UnmaskedOffset(g.bounds, d)
	}
	return edit.ClampOffset(g.value, d)
}

var defaultSelection = HexA(0x3b82f659)

func (w *Window) paintInput(e *El, content geom.Rect, clip geom.Rect, textColor Color, opacity float32) bool {
	st := w.editOf(e)
	st.Sync(e.input.value)
	props := e.input
	s := w.scale
	focused := w.focused == e.id
	if focused && w.imeOn != w.takesIME(e) {
		// The input turned into a password, or a modal overlay opened or
		// closed above it, while it kept focus.
		w.updateIME()
	}

	disp := st.Value
	caret := st.Caret
	selA, selB := st.Selection()
	compStart, compLen := 0, 0
	if focused && st.comp != "" {
		disp = disp[:selA] + st.comp + disp[selB:]
		compStart, compLen = selA, len(st.comp)
		caret = selA + st.compCursor
		selA, selB = 0, 0
	}
	g := &inputGeom{password: props.password, multiline: props.multiline, value: disp}
	shown := disp
	if props.password {
		g.bounds = st.clusters(disp)
		shown = edit.Mask(g.bounds)
		caret = edit.MaskedOffset(g.bounds, caret)
		selA, selB = edit.MaskedOffset(g.bounds, selA), edit.MaskedOffset(g.bounds, selB)
		if compLen > 0 {
			compLen = edit.MaskedOffset(g.bounds, compStart+compLen) - edit.MaskedOffset(g.bounds, compStart)
			compStart = edit.MaskedOffset(g.bounds, compStart)
		}
	}

	style := w.textStyle(e.tp)
	style.Ellipsis = false
	align := style.Align
	layoutW := text.Unbounded
	if props.multiline {
		style.NoWrap = false
		layoutW = content.W * s
	} else {
		style.NoWrap = true
		style.Align = text.AlignStart
	}
	measureText := shown
	if props.multiline {
		measureText = st.withZWSP(shown)
	}
	l := w.ts.Layout(measureText, style, layoutW)
	tw, th := l.Size()

	viewW, viewH := content.W*s, content.H*s
	cx, cy, ch := l.CaretAt(min(caret, len(shown)), st.upstream && compLen == 0)
	if ch == 0 {
		ch = th
	}
	if props.multiline {
		if cy-st.scrollY < 0 {
			st.scrollY = cy
		} else if cy+ch-st.scrollY > viewH {
			st.scrollY = cy + ch - viewH
		}
		st.scrollY = min(max(st.scrollY, 0), max(th-viewH, 0))
		st.scrollX = 0
	} else {
		const margin = 2
		if cx-st.scrollX > viewW-margin {
			st.scrollX = cx - viewW + margin
		} else if cx-st.scrollX < 0 {
			st.scrollX = cx
		}
		st.scrollX = min(max(st.scrollX, 0), max(tw-viewW+margin, 0))
		st.scrollY = 0
	}
	slack := float32(0)
	if !props.multiline {
		slack = text.AlignSlack(align, viewW, tw)
	}
	ox := float32(math.Round(float64(content.X*s - st.scrollX + slack)))
	oy := content.Y*s - st.scrollY
	if !props.multiline {
		oy += (viewH - th) / 2
	}
	oy = float32(math.Round(float64(oy)))

	g.text, g.style, g.width, g.originX, g.originY = measureText, style, layoutW, ox, oy
	g.caret = geom.Rect{X: (ox + cx) / s, Y: (oy + cy) / s, W: 1, H: ch / s}
	st.geom = g

	inner := clip.Intersect(geom.Rect{X: content.X - 1, Y: content.Y, W: content.W + 2, H: content.H})
	devClip := w.snap(inner)
	ok := true

	if focused && selA != selB {
		selColor := props.selColor
		if !selColor.isSet() {
			selColor = defaultSelection
		}
		for _, r := range l.RangeRects(selA, selB) {
			w.scene.Add(render.Prim{
				Bounds: [4]float32{ox + r.X, oy + r.Y, max(r.W, 2), r.H},
				Clip:   devClip,
				Color:  selColor.arr(opacity),
				Kind:   render.KindRect,
			})
		}
	}

	if shown == "" && props.placeholder != "" {
		ph := props.phColor
		if !ph.isSet() {
			ph = textColor.Alpha(textColor.A * 0.45)
		}
		pl := w.ts.Layout(props.placeholder, style, layoutW)
		px := ox
		if !props.multiline {
			pw, _ := pl.Size()
			px = float32(math.Round(float64(content.X*s + text.AlignSlack(align, viewW, pw))))
		}
		ok = pl.Draw(&w.scene, px, oy, ph.arr(opacity), devClip) && ok
	} else {
		ok = l.Draw(&w.scene, ox, oy, textColor.arr(opacity), devClip) && ok
	}

	errs := w.spellRequest(e, st)
	if compLen == 0 && len(errs) > 0 {
		// The word being typed is not marked until the caret leaves it.
		typing := focused && st.LastKind == edit.Typing
		lw := max(1, float32(math.Round(float64(s))))
		h := float32(math.Round(float64(4 * s)))
		for _, r := range errs {
			if !spell.Valid(st.Value, r) || typing && st.Caret >= r.Start && st.Caret <= r.End {
				continue
			}
			for _, rr := range l.RangeRects(r.Start, r.End) {
				w.scene.Add(render.Prim{
					Bounds: [4]float32{ox + rr.X, oy + rr.Y + rr.H - h, rr.W, h},
					Clip:   devClip,
					Color:  spellColor.arr(opacity),
					Params: [4]float32{6 * s, lw},
					Kind:   render.KindWave,
				})
			}
		}
	}

	if compLen > 0 {
		for _, r := range l.RangeRects(compStart, compStart+compLen) {
			w.scene.Add(render.Prim{
				Bounds: [4]float32{ox + r.X, oy + r.Y + r.H - max(1, float32(math.Round(float64(s)))), r.W, max(1, float32(math.Round(float64(s))))},
				Clip:   devClip,
				Color:  textColor.arr(opacity),
				Kind:   render.KindRect,
			})
		}
	}

	if focused && w.caretOn && w.osFocus {
		cc := props.caretColor
		if !cc.isSet() {
			cc = textColor
		}
		cw := float32(math.Max(1, math.Round(float64(s))))
		w.scene.Add(render.Prim{
			Bounds: [4]float32{float32(math.Round(float64(ox + cx))), oy + cy, cw, ch},
			Clip:   devClip,
			Color:  cc.arr(opacity),
			Kind:   render.KindRect,
		})
	}
	return ok
}
