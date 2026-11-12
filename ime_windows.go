package fairy

import (
	"github.com/myparsleycat/fairy/internal/win"
)

func (w *Window) updateIME() {
	if w.hwnd == 0 {
		return
	}
	e := w.focusedEl()
	editing := e != nil && e.kind == kindInput
	on := w.takesIME(e)
	if on != w.imeOn {
		win.ImmEnable(w.hwnd, on)
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

func (w *Window) onIMEComposition(lp uintptr) {
	e, st := w.imeInput()
	if e == nil {
		return
	}
	himc := win.ImmGetContext(w.hwnd)
	if himc == 0 {
		return
	}
	defer win.ImmReleaseContext(w.hwnd, himc)
	if lp&win.GCS_RESULTSTR != 0 {
		res := win.ImmGetCompositionString(himc, win.GCS_RESULTSTR)
		st.comp = ""
		if res != "" {
			w.typeText(e, res)
		}
	}
	if lp&win.GCS_COMPSTR != 0 {
		st.comp = win.ImmGetCompositionString(himc, win.GCS_COMPSTR)
		cur := win.ImmGetCompositionCursor(himc)
		st.compCursor = utf16ToByteOffset(st.comp, cur)
	} else if lp&win.GCS_RESULTSTR == 0 {
		st.comp = ""
	}
	w.resetBlink()
	w.updateIMEPosition()
	w.invalidate()
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

func (w *Window) commitComposition() {
	_, st := w.imeInput()
	if st == nil || st.comp == "" {
		return
	}
	himc := win.ImmGetContext(w.hwnd)
	if himc == 0 {
		return
	}
	win.ImmNotifyIME(himc, win.NI_COMPOSITIONSTR, win.CPS_COMPLETE, 0)
	win.ImmReleaseContext(w.hwnd, himc)
}

func (w *Window) updateIMEPosition() {
	_, st := w.imeInput()
	if st == nil || st.geom == nil {
		return
	}
	himc := win.ImmGetContext(w.hwnd)
	if himc == 0 {
		return
	}
	defer win.ImmReleaseContext(w.hwnd, himc)
	c := st.geom.caret
	x, y, h := int32(c.x*w.scale), int32(c.y*w.scale), int32(c.h*w.scale)
	win.ImmSetCompositionWindow(himc, &win.COMPOSITIONFORM{Style: win.CFS_POINT, CurrPos: win.POINT{X: x, Y: y}})
	win.ImmSetCandidateWindow(himc, &win.CANDIDATEFORM{
		Style:   win.CFS_EXCLUDE,
		CurrPos: win.POINT{X: x, Y: y + h},
		Area:    win.RECT{Left: x, Top: y, Right: x + 1, Bottom: y + h},
	})
}
