package fairy

import (
	"math"
	"slices"
	"strings"

	"github.com/myparsleycat/fairy/internal/edit"
	"github.com/myparsleycat/fairy/internal/render"
	"github.com/myparsleycat/fairy/internal/text"
)

// inputGeom records how an input was last drawn, for pointer hit testing and IME placement.
type inputGeom struct {
	text             string
	style            text.Style
	width            float32
	originX, originY float32 // device pixels
	caret            rectF   // logical
	password         bool
	multiline        bool
	value            string // the unmasked text being shown
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
		c.text = maskText(s.Value)
	}
	if c.multiline {
		c.text += "​"
	}
	return &c
}

func (g *inputGeom) toDisplay(b int) int {
	if g.password {
		return maskedOffset(g.value, edit.ClampOffset(g.value, b))
	}
	return b
}

func (g *inputGeom) fromDisplay(d int) int {
	if g.password {
		d = unmaskedOffset(g.value, d)
	}
	return edit.ClampOffset(g.value, d)
}

var defaultSelection = HexA(0x3b82f659)

func (w *Window) paintInput(e *El, content rectF, clip rectF, textColor Color, opacity float32) bool {
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
		shown = maskText(disp)
		caret = maskedOffset(disp, caret)
		selA, selB = maskedOffset(disp, selA), maskedOffset(disp, selB)
		if compLen > 0 {
			compLen = maskedOffset(disp, compStart+compLen) - maskedOffset(disp, compStart)
			compStart = maskedOffset(disp, compStart)
		}
	}

	style := w.textStyle(e.tp)
	style.Ellipsis = false
	align := style.Align
	layoutW := text.Unbounded
	if props.multiline {
		style.NoWrap = false
		layoutW = content.w * s
	} else {
		style.NoWrap = true
		style.Align = text.AlignStart
	}
	measureText := shown
	if props.multiline {
		measureText = shown + "​"
	}
	l := w.ts.Layout(measureText, style, layoutW)
	tw, th := l.Size()

	viewW, viewH := content.w*s, content.h*s
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
		slack = alignSlack(align, viewW, tw)
	}
	ox := float32(math.Round(float64(content.x*s - st.scrollX + slack)))
	oy := content.y*s - st.scrollY
	if !props.multiline {
		oy += (viewH - th) / 2
	}
	oy = float32(math.Round(float64(oy)))

	g.text, g.style, g.width, g.originX, g.originY = measureText, style, layoutW, ox, oy
	g.caret = rectF{(ox + cx) / s, (oy + cy) / s, 1, ch / s}
	st.geom = g

	inner := clip.intersect(rectF{content.x - 1, content.y, content.w + 2, content.h})
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
			px = float32(math.Round(float64(content.x*s + alignSlack(align, viewW, pw))))
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
			if !validSpellRange(st.Value, r) || typing && st.Caret >= r.start && st.Caret <= r.end {
				continue
			}
			for _, rr := range l.RangeRects(r.start, r.end) {
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

// alignSlack is how far a single-line value of width tw moves right inside a field of width viewW for its text
// alignment. A value wider than the field scrolls from the start, as a browser input does.
func alignSlack(align text.Align, viewW, tw float32) float32 {
	if tw >= viewW {
		return 0
	}
	switch align {
	case text.AlignCenter:
		return (viewW - tw) / 2
	case text.AlignEnd:
		return viewW - tw
	}
	return 0
}

// A password shows one dot per grapheme cluster, so the caret, which moves
// by clusters, steps over one dot at a time.
func maskText(s string) string {
	return strings.Repeat("•", len(text.Clusters(s))-1)
}

// maskedOffset maps a byte offset in s to the start of its cluster's dot in
// maskText(s).
func maskedOffset(s string, b int) int {
	bounds := text.Clusters(s)
	i, found := slices.BinarySearch(bounds, b)
	if !found {
		i--
	}
	return i * len("•")
}

// unmaskedOffset maps a byte offset in the masked text back to s.
func unmaskedOffset(s string, mb int) int {
	bounds := text.Clusters(s)
	return bounds[min(max(mb/len("•"), 0), len(bounds)-1)]
}
