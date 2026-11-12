package fairy

import (
	"github.com/myparsleycat/fairy/internal/win"
)

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

// enableIME turns the input method on for the window or off.
func (w *Window) enableIME(on bool) { win.ImmEnable(w.hwnd, on) }
