package fairy

import (
	"strings"

	"github.com/myparsleycat/fairy/internal/edit"
	"github.com/myparsleycat/fairy/internal/text"
	"github.com/myparsleycat/fairy/internal/win"
	"github.com/myparsleycat/fairy/keyboard"
)

func (w *Window) editOf(e *El) *editState {
	if e.state == nil {
		e.state = w.state(e.id)
	}
	if e.state.edit == nil {
		e.state.edit = &editState{Value: e.input.value, Reported: e.input.value, Caret: len(e.input.value), Anchor: len(e.input.value)}
	}
	st := e.state.edit
	st.ClampCaret()
	return st
}

func (w *Window) commitEdit(e *El, st *editState, value string) {
	if e.input.onChange == nil {
		return
	}
	st.Edited()
	st.upstream = false
	st.Reported = value
	e.input.onChange(value)
	w.resetBlink()
	w.Update()
}

// insertText replaces the selection with s as an undo step of its own.
func (w *Window) insertText(e *El, s string) { w.insertKind(e, s, edit.Other) }

// typeText inserts typed text, which joins the undo step of the typing
// before it.
func (w *Window) typeText(e *El, s string) { w.insertKind(e, s, edit.Typing) }

func (w *Window) insertKind(e *El, s string, kind edit.Kind) {
	st := w.editOf(e)
	s = edit.Sanitize(s, e.input.multiline)
	if s == "" && !st.HasSelection() {
		return
	}
	if st.HasSelection() || strings.Contains(s, "\n") {
		kind = edit.Other
	}
	st.PushUndo(kind)
	st.ReplaceSelection(s)
	w.commitEdit(e, st, st.Value)
}

func (w *Window) inputKey(e *El, ev keyboard.KeyEvent) bool {
	st := w.editOf(e)
	props := e.input
	if props.onKey != nil && inputHookSees(ev, st.comp != "") {
		if props.onKey(ev) {
			st.HandlerRan = true
			st.BreakUndo()
			w.Update()
			return true
		}
	}
	if st.comp != "" {
		w.wantChar = true
		return true // the IME owns keys while composing
	}
	editable := props.onChange != nil
	shift := ev.Mods&keyboard.ModShift != 0
	ctrl := ev.Mods&keyboard.ModCtrl != 0
	alt := ev.Mods&keyboard.ModAlt != 0
	s := st.Value
	move := func(to int) {
		st.Caret = to
		if !shift {
			st.Anchor = to
		}
		st.HasPreferredX = false
		st.upstream = false
		w.resetBlink()
		w.invalidate()
	}
	del := func(a, b int, kind edit.Kind) {
		if !editable || a == b {
			return
		}
		st.PushUndo(kind)
		st.Value = s[:a] + s[b:]
		st.Caret, st.Anchor = a, a
		w.commitEdit(e, st, st.Value)
	}
	switch ev.Key {
	case keyboard.KeyLeft:
		switch {
		case !shift && st.HasSelection():
			a, _ := st.Selection()
			move(a)
		case ctrl:
			move(text.PrevWord(s, st.Caret))
		default:
			move(text.PrevBoundary(s, st.Caret))
		}
		return true
	case keyboard.KeyRight:
		switch {
		case !shift && st.HasSelection():
			_, b := st.Selection()
			move(b)
		case ctrl:
			move(text.NextWord(s, st.Caret))
		default:
			move(text.NextBoundary(s, st.Caret))
		}
		return true
	case keyboard.KeyHome:
		if props.multiline && !ctrl {
			a, _, _ := w.visualLine(st)
			move(a)
		} else {
			move(0)
		}
		return true
	case keyboard.KeyEnd:
		if props.multiline && !ctrl {
			_, b, up := w.visualLine(st)
			move(b)
			st.upstream = up
		} else {
			move(len(s))
		}
		return true
	case keyboard.KeyUp, keyboard.KeyDown:
		if !props.multiline {
			return false
		}
		w.moveVertical(e, st, ev.Key == keyboard.KeyDown, shift)
		return true
	case keyboard.KeyBackspace:
		if st.HasSelection() {
			a, b := st.Selection()
			del(a, b, edit.Other)
		} else if ctrl {
			del(text.PrevWord(s, st.Caret), st.Caret, edit.Other)
		} else {
			del(text.PrevDeleteBoundary(s, st.Caret), st.Caret, edit.Deleting)
		}
		return true
	case keyboard.KeyDelete:
		if shift && !ctrl && !alt {
			w.runEdit(e, st, EditCut)
			return true
		}
		if st.HasSelection() {
			a, b := st.Selection()
			del(a, b, edit.Other)
		} else if ctrl {
			del(st.Caret, text.NextWord(s, st.Caret), edit.Other)
		} else {
			del(st.Caret, text.NextBoundary(s, st.Caret), edit.Deleting)
		}
		return true
	case keyboard.KeyEnter:
		if props.multiline && editable && !ctrl {
			w.typeText(e, "\n")
			return true
		}
		if props.onSubmit != nil {
			props.onSubmit(st.Value)
			st.HandlerRan = true
			w.Update()
			return true
		}
		return false
	}
	if ctrl && !alt {
		switch ev.Key {
		case keyboard.KeyA:
			w.runEdit(e, st, EditSelectAll)
			return true
		case keyboard.KeyC, keyboard.KeyInsert:
			w.runEdit(e, st, EditCopy)
			return true
		case keyboard.KeyX:
			w.runEdit(e, st, EditCut)
			return true
		case keyboard.KeyV:
			w.runEdit(e, st, EditPaste)
			return true
		case keyboard.KeyZ:
			if shift {
				w.runEdit(e, st, EditRedo)
			} else {
				w.runEdit(e, st, EditUndo)
			}
			return true
		case keyboard.KeyY:
			w.runEdit(e, st, EditRedo)
			return true
		}
	}
	if ev.Is(keyboard.KeyInsert, keyboard.ModShift) && editable {
		w.runEdit(e, st, EditPaste)
		return true
	}
	if !ctrl && !alt && isTypingKey(ev.Key) {
		w.wantChar = true
		return true // the character arrives through WM_CHAR
	}
	return false
}

// paste offers the paste to the Input's OnPaste handler, then inserts the
// clipboard text unless the handler took it. It reports whether either
// happened: a clipboard without text, such as one holding an image, leaves
// the selection in place.
func (w *Window) paste(e *El, st *editState) bool {
	if e.input.onChange == nil {
		return false
	}
	if e.input.onPaste != nil {
		if e.input.onPaste() {
			st.HandlerRan = true
			st.BreakUndo()
			w.Update()
			return true
		}
	}
	s, ok := w.clipboardText()
	if !ok || edit.Sanitize(s, e.input.multiline) == "" {
		return false
	}
	w.insertText(e, s)
	return true
}

func isTypingKey(k keyboard.Key) bool {
	return k == keyboard.KeySpace || (k >= '0' && k <= '9') || (k >= 'A' && k <= 'Z') ||
		(k >= 0x60 && k <= 0x6F) || (k >= 0xBA && k <= 0xE2) || k == win.VK_PROCESSKEY
}

func (w *Window) undo(e *El, st *editState) {
	if e.input.onChange == nil || !st.UndoStep() {
		return
	}
	w.commitEdit(e, st, st.Value)
}

func (w *Window) redo(e *El, st *editState) {
	if e.input.onChange == nil || !st.RedoStep() {
		return
	}
	w.commitEdit(e, st, st.Value)
}

func lineStart(s string, i int) int {
	for i > 0 && s[i-1] != '\n' {
		i--
	}
	return i
}

func lineEnd(s string, i int) int {
	for i < len(s) && s[i] != '\n' {
		i++
	}
	return i
}

// visualLine returns the byte range of the displayed line holding the caret,
// and whether its end is drawn upstream (see text.Layout.LineRange). Without a
// drawn layout it falls back to the newline-delimited line.
func (w *Window) visualLine(st *editState) (start, end int, upstream bool) {
	g := st.shownGeom()
	if g == nil {
		return lineStart(st.Value, st.Caret), lineEnd(st.Value, st.Caret), false
	}
	l := w.ts.Layout(g.text, g.style, g.width)
	at := g.toDisplay(st.Caret)
	if st.upstream && at > 0 {
		at = text.PrevBoundary(g.text, at)
	}
	a, b, up := l.LineRange(at)
	b = min(b, len(g.text)-len("​"))
	return g.fromDisplay(a), g.fromDisplay(b), up
}

func (w *Window) moveVertical(e *El, st *editState, down, shift bool) {
	g := st.shownGeom()
	if g == nil {
		return
	}
	l := w.ts.Layout(g.text, g.style, g.width)
	cx, cy, ch := l.CaretAt(g.toDisplay(st.Caret), st.upstream)
	st.upstream = false
	if !st.HasPreferredX {
		st.PreferredX, st.HasPreferredX = cx, true
	}
	ty := cy - ch/2
	if down {
		ty = cy + ch*1.5
	}
	off := g.fromDisplay(l.HitTest(st.PreferredX, ty))
	st.Caret = off
	if !shift {
		st.Anchor = off
	}
	w.resetBlink()
	w.invalidate()
}

// pressInput places the caret for a press inside an input.
func (w *Window) pressInput(e *El, x, y float32, clicks int) {
	st := w.editOf(e)
	if st.comp != "" {
		return
	}
	shift := w.mods()&keyboard.ModShift != 0
	w.moveCaretTo(e, x, y, shift)
	switch {
	case clicks == 2:
		st.Anchor = text.PrevWord(st.Value, text.NextBoundary(st.Value, st.Caret))
		st.Caret = text.NextWord(st.Value, st.Anchor)
	case clicks >= 3:
		st.Anchor, st.Caret = 0, len(st.Value)
	}
	w.invalidate()
}

func (w *Window) moveCaretTo(e *El, x, y float32, extend bool) {
	st := w.editOf(e)
	g := st.shownGeom()
	if g == nil {
		return
	}
	l := w.ts.Layout(g.text, g.style, g.width)
	off := g.fromDisplay(l.HitTest(x*w.scale-g.originX, y*w.scale-g.originY))
	st.Caret = off
	if !extend {
		st.Anchor = off
	}
	st.HasPreferredX = false
	st.upstream = false
	w.resetBlink()
	w.invalidate()
}

// IME.
