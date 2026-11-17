package fairy

import (
	"html"
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
	// texts are the scope's text elements in tree order and order maps their
	// ids to their index, both for the tree of treeVer.
	texts   []*El
	order   map[uint64]int
	treeVer int
	cached  bool
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

// appendTexts appends the text elements of e's subtree in tree order.
func appendTexts(out []*El, e *El) []*El {
	if e.kind == kindText && e.text != "" {
		out = append(out, e)
	}
	for _, c := range e.children {
		out = appendTexts(out, c)
	}
	return out
}

// selTexts returns the text elements of the selection's scope in tree order,
// or nil when the scope left the tree.
func (w *Window) selTexts() []*El {
	s := w.sel
	if s.cached && s.treeVer == w.treeVer {
		return s.texts
	}
	clear(s.texts)
	s.texts = s.texts[:0]
	if s.order == nil {
		s.order = map[uint64]int{}
	}
	clear(s.order)
	if scope := w.byID[s.scope]; scope != nil {
		s.texts = appendTexts(s.texts, scope)
		for i, t := range s.texts {
			s.order[t.id] = i
		}
	}
	s.cached, s.treeVer = true, w.treeVer
	return s.texts
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
	extend := clicks == 1 && w.mods()&keyboard.ModShift != 0 && w.sel != nil && w.sel.scope == scope.id
	if !extend {
		w.sel = &textSelection{scope: scope.id}
	}
	pos, ok := w.selPosAt(w.selTexts(), x, y)
	if !ok {
		w.sel = nil
		return true
	}
	if extend {
		w.sel.focus = pos
		w.invalidate()
		return true
	}
	sel := w.sel
	sel.anchor, sel.focus = pos, pos
	if e := w.byID[pos.el]; e != nil {
		switch {
		case clicks == 2:
			sel.anchor.off = text.PrevWord(e.text, text.NextBoundary(e.text, pos.off))
			sel.focus.off = text.NextWord(e.text, sel.anchor.off)
		case clicks >= 3:
			sel.anchor.off, sel.focus.off = 0, len(e.text)
		}
	}
	w.invalidate()
	return true
}

// dragSelection moves the selection's focus to the pointer.
func (w *Window) dragSelection(x, y float32) {
	if w.sel == nil {
		return
	}
	if pos, ok := w.selPosAt(w.selTexts(), x, y); ok && pos != w.sel.focus {
		w.sel.focus = pos
		w.invalidate()
	}
}

// selOrder returns the tree-order index of each text in the selection's
// scope.
func (w *Window) selOrder() map[uint64]int {
	w.selTexts()
	return w.sel.order
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
	var parts []string
	for _, t := range w.selTexts() {
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
	var sb strings.Builder
	styled := false
	for _, t := range w.selTexts() {
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
				w.setClipboardRich(s, h)
			} else {
				w.setClipboardText(s)
			}
			return true
		}
	case keyboard.KeyA:
		texts := w.selTexts()
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

// spansHTML writes text[a:b] of a paragraph made of spans (or of plain text
// when spans is nil) as HTML.
func spansHTML(sb *strings.Builder, text string, spans []Span, a, b int) {
	if spans == nil {
		sb.WriteString(htmlText(text[a:b]))
		return
	}
	off := 0
	for _, s := range spans {
		end := off + len(s.Text)
		lo, hi := max(a, off), min(b, end)
		if lo < hi {
			t := htmlText(s.Text[lo-off : hi-off])
			code := s.Font != "" && s.Background.isSet()
			if code {
				t = "<code>" + t + "</code>"
			}
			if s.Strike {
				t = "<s>" + t + "</s>"
			}
			if s.Italic {
				t = "<i>" + t + "</i>"
			}
			if s.Weight >= 600 {
				t = "<b>" + t + "</b>"
			}
			if s.URL != "" {
				t = `<a href="` + html.EscapeString(s.URL) + `">` + t + "</a>"
			}
			sb.WriteString(t)
		}
		off = end
	}
}

func htmlText(s string) string {
	return strings.ReplaceAll(html.EscapeString(s), "\n", "<br>")
}
