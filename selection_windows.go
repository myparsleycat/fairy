package fairy

import (
	"math"
	"strconv"
	"strings"

	"github.com/myparsleycat/fairy/internal/render"
	"github.com/myparsleycat/fairy/internal/text"
	"github.com/myparsleycat/fairy/keyboard"
)

// Selectable lets the user select the text of Text and RichText elements in
// this element's subtree by dragging, double-clicking a word or
// triple-clicking a paragraph, and copy it with Ctrl+C or Ctrl+Insert. Ctrl+A
// then selects all of it. A selection spans the texts of one Selectable
// element in tree order; texts in separate copies join with a line break.
// Links in a RichText still open on a click without a drag. Rows of a
// virtual list that are not built are not part of the selection.
func (e *El) Selectable() *El {
	e.selectable = true
	return e
}

// selPos is a position in a selectable text: a byte offset into the text of
// the element with id el.
type selPos struct {
	el  uint64
	off int
}

// textSelection is the window's selection of read-only text.
type textSelection struct {
	scope         uint64
	anchor, focus selPos
	order         map[uint64]int // text element id to its index in tree order, for the current paint
	orderTreeVer  int
}

// selectableScope returns the nearest Selectable element containing e.
func selectableScope(e *El) *El {
	for ; e != nil; e = e.parent {
		if e.selectable {
			return e
		}
	}
	return nil
}

// selTexts lists the text elements of a selectable scope in tree order.
func selTexts(scope *El) []*El {
	var out []*El
	var walk func(*El)
	walk = func(e *El) {
		if e.kind == kindText && e.text != "" {
			out = append(out, e)
		}
		for _, c := range e.children {
			walk(c)
		}
	}
	walk(scope)
	return out
}

// textLayout returns a text element's layout and the device-pixel origin it
// is painted at.
func (w *Window) textLayout(e *El) (*text.Layout, float32, float32) {
	s := w.scale
	pad := e.padding()
	cx, cy := e.absX+pad.Left, e.absY+pad.Top
	cw, ch := max(e.w-pad.Left-pad.Right, 0), max(e.h-pad.Top-pad.Bottom, 0)
	l := w.ts.Layout(e.text, w.elTextStyle(e), cw*s)
	ty := cy * s
	if j := e.style.Justify; j == JustifyCenter || j == JustifyEnd {
		_, th := l.Size()
		free := ch*s - th
		if j == JustifyCenter {
			free /= 2
		}
		ty += max(free, 0)
	}
	return l, cx * s, ty
}

// selPosAt returns the text position nearest to window point (x, y) among
// texts: in the text whose box is nearest, first vertically, then
// horizontally. Above or below that text it is the text's start or end.
func (w *Window) selPosAt(texts []*El, x, y float32) (selPos, bool) {
	var best *El
	bestDY, bestDX := float32(math.MaxFloat32), float32(math.MaxFloat32)
	dist := func(v, lo, hi float32) float32 {
		switch {
		case v < lo:
			return lo - v
		case v >= hi:
			return v - hi
		}
		return 0
	}
	for _, t := range texts {
		if t.culled {
			continue
		}
		dy, dx := dist(y, t.absY, t.absY+t.h), dist(x, t.absX, t.absX+t.w)
		if dy < bestDY || dy == bestDY && dx < bestDX {
			best, bestDY, bestDX = t, dy, dx
		}
	}
	if best == nil {
		return selPos{}, false
	}
	switch {
	case y < best.absY:
		return selPos{best.id, 0}, true
	case y >= best.absY+best.h:
		return selPos{best.id, len(best.text)}, true
	}
	l, ox, oy := w.textLayout(best)
	return selPos{best.id, l.HitTest(x*w.scale-ox, y*w.scale-oy)}, true
}

// pressSelection starts or extends a selection for a left press at (x, y)
// on top, and reports whether the press is in a selectable scope.
func (w *Window) pressSelection(top *El, x, y float32, clicks int) bool {
	scope := selectableScope(top)
	if scope == nil {
		if w.sel != nil {
			w.sel = nil
			w.invalidate()
		}
		return false
	}
	texts := selTexts(scope)
	pos, ok := w.selPosAt(texts, x, y)
	if !ok {
		w.sel = nil
		return true
	}
	if clicks == 1 && w.mods()&keyboard.ModShift != 0 && w.sel != nil && w.sel.scope == scope.id {
		w.sel.focus = pos
		w.invalidate()
		return true
	}
	sel := &textSelection{scope: scope.id, anchor: pos, focus: pos}
	if e := w.byID[pos.el]; e != nil {
		switch {
		case clicks == 2:
			sel.anchor.off = text.PrevWord(e.text, text.NextBoundary(e.text, pos.off))
			sel.focus.off = text.NextWord(e.text, sel.anchor.off)
		case clicks >= 3:
			sel.anchor.off, sel.focus.off = 0, len(e.text)
		}
	}
	w.sel = sel
	w.invalidate()
	return true
}

// dragSelection moves the selection's focus to the pointer.
func (w *Window) dragSelection(x, y float32) {
	if w.sel == nil {
		return
	}
	scope := w.byID[w.sel.scope]
	if scope == nil {
		return
	}
	if pos, ok := w.selPosAt(selTexts(scope), x, y); ok && pos != w.sel.focus {
		w.sel.focus = pos
		w.invalidate()
	}
}

// selOrder returns the tree-order index of each text in the selection's
// scope, recomputed when the tree may have changed shape.
func (w *Window) selOrder() map[uint64]int {
	s := w.sel
	if s.order != nil && s.orderTreeVer == w.treeVer {
		return s.order
	}
	s.order = map[uint64]int{}
	s.orderTreeVer = w.treeVer
	if scope := w.byID[s.scope]; scope != nil {
		for i, t := range selTexts(scope) {
			s.order[t.id] = i
		}
	}
	return s.order
}

// selRange returns the byte range of text element e inside the selection.
func (w *Window) selRange(e *El) (int, int, bool) {
	s := w.sel
	if s == nil || s.anchor == s.focus {
		return 0, 0, false
	}
	order := w.selOrder()
	i, ok := order[e.id]
	ia, okA := order[s.anchor.el]
	ib, okB := order[s.focus.el]
	if !ok || !okA || !okB {
		return 0, 0, false
	}
	start, end := s.anchor, s.focus
	if ib < ia || ib == ia && end.off < start.off {
		start, end = end, start
		ia, ib = ib, ia
	}
	if i < ia || i > ib {
		return 0, 0, false
	}
	a, b := 0, len(e.text)
	if i == ia {
		a = min(start.off, len(e.text))
	}
	if i == ib {
		b = min(end.off, len(e.text))
	}
	return a, b, a < b
}

// paintSelection highlights the selected part of a text element laid out
// at (ox, oy) device pixels.
func (w *Window) paintSelection(e *El, l *text.Layout, ox, oy float32, clip [4]float32, opacity float32) {
	a, b, ok := w.selRange(e)
	if !ok {
		return
	}
	for _, r := range l.RangeRects(a, b) {
		w.scene.Add(render.Prim{
			Bounds: [4]float32{ox + r.X, oy + r.Y, max(r.W, 2), r.H},
			Clip:   clip,
			Color:  defaultSelection.arr(opacity),
			Kind:   render.KindRect,
		})
	}
}

// SelectedText returns the text selected in Selectable elements, or "".
func (w *Window) SelectedText() string {
	if w.sel == nil {
		return ""
	}
	scope := w.byID[w.sel.scope]
	if scope == nil {
		return ""
	}
	var parts []string
	for _, t := range selTexts(scope) {
		if a, b, ok := w.selRange(t); ok {
			parts = append(parts, t.text[a:b])
		}
	}
	return strings.Join(parts, "\n")
}

// selectedHTML returns the selection as HTML, one paragraph or heading per
// text element, or "" when none of the selected text is styled.
func (w *Window) selectedHTML() string {
	if w.sel == nil {
		return ""
	}
	scope := w.byID[w.sel.scope]
	if scope == nil {
		return ""
	}
	var sb strings.Builder
	styled := false
	for _, t := range selTexts(scope) {
		a, b, ok := w.selRange(t)
		if !ok {
			continue
		}
		styled = styled || t.spans != nil
		tag := "p"
		if t.a11y != nil && t.a11y.heading > 0 {
			tag = "h" + strconv.Itoa(int(min(t.a11y.heading, 6)))
		}
		sb.WriteString("<" + tag + ">")
		spansHTML(&sb, t.text, t.spans, a, b)
		sb.WriteString("</" + tag + ">")
	}
	if !styled {
		return ""
	}
	return sb.String()
}

// ClearSelection removes the selection of read-only text.
func (w *Window) ClearSelection() {
	if w.sel != nil {
		w.sel = nil
		w.invalidate()
	}
}

// selectionKey handles Ctrl+C, Ctrl+Insert and Ctrl+A for the selection.
func (w *Window) selectionKey(ev keyboard.KeyEvent) bool {
	if w.sel == nil || ev.Mods != keyboard.ModCtrl {
		return false
	}
	switch ev.Key {
	case keyboard.KeyC, keyboard.KeyInsert:
		if s := w.SelectedText(); s != "" {
			if h := w.selectedHTML(); h != "" {
				w.SetClipboardRich(s, h)
			} else {
				w.SetClipboardText(s)
			}
			return true
		}
	case keyboard.KeyA:
		scope := w.byID[w.sel.scope]
		if scope == nil {
			return false
		}
		texts := selTexts(scope)
		if len(texts) == 0 {
			return false
		}
		last := texts[len(texts)-1]
		w.sel.anchor, w.sel.focus = selPos{texts[0].id, 0}, selPos{last.id, len(last.text)}
		w.invalidate()
		return true
	}
	return false
}

// syncSelection drops a selection whose scope or ends left the tree.
func (w *Window) syncSelection() {
	s := w.sel
	if s == nil {
		return
	}
	if w.byID[s.scope] == nil || w.byID[s.anchor.el] == nil || w.byID[s.focus.el] == nil {
		w.sel = nil
	}
}
