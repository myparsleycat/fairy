package fairy

func (w *Window) updateIME() {
	if !w.hasNativeWindow() {
		return
	}
	e := w.focusedEl()
	editing := e != nil && e.kind == kindInput
	on := w.takesIME(e)
	if on != w.imeOn {
		w.enableIME(on)
		w.imeOn = on
		if !on && editing {
			w.editOf(e).comp = ""
		}
	}
	if editing {
		w.caretOn = true
		w.setTimer(timerCaret, caretBlinkMs)
	} else {
		w.killTimer(timerCaret)
	}
}

func (w *Window) imeInput() (*El, *editState) {
	e := w.focusedEl()
	if !w.takesIME(e) {
		return nil, nil
	}
	return e, w.editOf(e)
}

func utf16ToByteOffset(s string, u int) int {
	if u < 0 {
		return len(s)
	}
	n := 0
	for i, r := range s {
		if n >= u {
			return i
		}
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return len(s)
}

func (w *Window) onIMEEnd() {
	if _, st := w.imeInput(); st != nil && st.comp != "" {
		st.comp = ""
		w.invalidate()
	}
}
