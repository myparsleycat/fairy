package fairy

import (
	"unicode/utf16"

	"github.com/myparsleycat/fairy/internal/win"
	"github.com/myparsleycat/fairy/keyboard"
)

func (w *Window) setFocus(e *El) {
	var id uint64
	key := ""
	if e != nil {
		id, key = e.id, e.key
	}
	if id == w.focused {
		return
	}
	w.commitComposition()
	prev := w.focusedEl()
	w.focused, w.focusKey = id, key
	w.updateIME()
	w.invalidate()
	if prev != nil && prev.onBlur != nil {
		prev.onBlur()
		w.Update()
	}
}

// setFocusBy moves focus to e and records whether the keyboard moved it.
func (w *Window) setFocusBy(e *El, keyboard bool) {
	w.setFocus(e)
	w.keyFocus, w.hoverFocus = keyboard, false
}

func (w *Window) focusedEl() *El {
	if w.focused == 0 {
		return nil
	}
	return w.byID[w.focused]
}

func (w *Window) resetBlink() {
	w.caretOn = true
	if w.focusedEl() != nil {
		win.SetTimer(w.hwnd, timerCaret, caretBlinkMs)
	}
}

// topLayer returns the topmost visible layer, or nil.
// topLayer returns the topmost popover or modal overlay. Pass-through
// overlays such as toasts are skipped: they never take Escape or outside presses.
func (w *Window) topLayer() *layer {
	for i := len(w.layers) - 1; i >= 0; i-- {
		l := w.layers[i]
		if !l.hidden && (l.pop != nil || l.modal) {
			return l
		}
	}
	return nil
}

// takesIME reports whether an input method may compose into e. Password
// inputs never take it, as in Win32 password edits and browsers.
func (w *Window) takesIME(e *El) bool {
	return e != nil && e.kind == kindInput && e.input.onChange != nil && !e.input.password && !w.underModal(e)
}

// modalFloor returns the index of the topmost modal overlay, or -1. Keys
// reach only that layer and the layers above it, such as popovers opened from
// it.
func (w *Window) modalFloor() int {
	for i := len(w.layers) - 1; i >= 0; i-- {
		if l := w.layers[i]; !l.hidden && l.modal {
			return i
		}
	}
	return -1
}

// underModal reports whether a modal overlay above e keeps keys from it.
func (w *Window) underModal(e *El) bool {
	m := w.modalFloor()
	if m < 0 || e == nil {
		return false
	}
	r := rootOf(e)
	for _, l := range w.layers[m:] {
		if l.root == r {
			return false
		}
	}
	return true
}

// keyTarget returns the focused element unless a modal overlay keeps keys
// from it.
func (w *Window) keyTarget() *El {
	if e := w.focusedEl(); e != nil && !w.underModal(e) {
		return e
	}
	return nil
}

// focusScope returns the elements Tab cycles through: those of the topmost
// modal overlay and the layers above it when there is one, otherwise all of
// them.
func (w *Window) focusScope() []*El {
	if m := w.modalFloor(); m >= 0 {
		// Layers are placed after the view in order, so these run to the end.
		return w.focusables[w.layers[m].focusStart:]
	}
	return w.focusables
}

func (w *Window) focusNext(back bool) {
	scope := w.tabStops(w.focusScope())
	if len(scope) == 0 {
		return
	}
	idx := -1
	for i, e := range scope {
		if e.id == w.focused {
			idx = i
			break
		}
	}
	if back {
		idx--
		if idx < 0 {
			idx = len(scope) - 1
		}
	} else {
		idx = (idx + 1) % len(scope)
	}
	e := scope[idx]
	w.setFocusBy(e, true)
	w.scrollIntoView(e)
	if e.kind == kindInput && e.state != nil && e.state.edit != nil {
		ed := e.state.edit
		ed.Anchor, ed.Caret = 0, len(ed.Value)
	}
}

func (w *Window) onKeyDown(k keyboard.Key, repeat bool, text string) bool {
	if w.autoScroll != nil {
		// Any key ends autoscroll and does nothing else.
		w.stopAutoScroll()
		return true
	}
	ev := keyboard.KeyEvent{Key: k, Mods: w.mods(), Repeat: repeat, Text: text}
	focused := w.keyTarget()
	if focused != nil && focused.kind == kindInput && w.inputKey(focused, ev) {
		return true
	}
	shown := w.keyFocus || w.hoverFocus
	if navigates(ev) {
		w.keyFocus = true
	}
	if k == keyboard.KeyEscape && ev.Mods == 0 {
		if p := w.press; p != nil && p.dragging {
			w.cancelPress()
			win.ReleaseCapture()
			return true
		}
		if l := w.topLayer(); l != nil && l.onDismiss != nil {
			l.onDismiss()
			w.Update()
			return true
		}
	}
	if k == keyboard.KeyTab && ev.Mods&^keyboard.ModShift == 0 && len(w.focusables) > 0 {
		w.focusNext(ev.Mods&keyboard.ModShift != 0)
		return true
	}
	start := focused
	if start == nil {
		start = w.root
		if m := w.modalFloor(); m >= 0 {
			start = w.layers[m].root
		}
	}
	for e := start; e != nil; e = e.parent {
		if e.onKey != nil && e.onKey(ev) {
			w.Update()
			return true
		}
	}
	if (w.sel == nil || !w.underModal(w.byID[w.sel.scope])) && w.selectionKey(ev) {
		return true
	}
	if focused != nil && w.groupKey(ev, shown) {
		w.Update()
		return true
	}
	if focused != nil && focused.kind != kindInput && (k == keyboard.KeyEnter || k == keyboard.KeySpace) && ev.Mods == 0 && focused.onClick != nil {
		// A row whose highlight the pointer took away chooses nothing.
		if focused.focus.highlight.isSet() && !shown {
			w.keyFocus = false
			return true
		}
		if !repeat {
			focused.onClick()
			w.Update()
		}
		return true
	}
	if w.opts.OnKey != nil && w.opts.OnKey(ev) {
		w.Update()
		return true
	}
	return false
}

func (w *Window) onChar(c uint16) {
	r := rune(c)
	high := w.charHigh
	w.charHigh = 0
	switch {
	case c >= 0xD800 && c < 0xDC00:
		w.charHigh = c
		return
	case c >= 0xDC00 && c < 0xE000:
		// A low surrogate without its high half is dropped.
		if high == 0 {
			return
		}
		r = utf16.DecodeRune(rune(high), r)
	}
	if r < 0x20 && r != '\t' || r == 0x7f {
		return
	}
	e := w.keyTarget()
	if e == nil || e.kind != kindInput || e.input.onChange == nil {
		return
	}
	if r == '\t' && !e.input.multiline {
		return
	}
	w.typeText(e, string(r))
}

// Text input editing.
