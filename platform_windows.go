package fairy

import (
	"time"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/win"
)

// monoTicks returns a monotonic time for frame statistics.
func monoTicks() time.Duration { return win.Ticks() }

// doubleClickMs is the longest gap between the clicks of a double click.
func doubleClickMs() uint32 { return win.GetDoubleClickTime() }

// wheelScrollLines is how many lines a wheel notch scrolls; wheelPageScroll
// means a page.
func wheelScrollLines() uint32 {
	lines := uint32(3)
	win.SystemParametersInfo(win.SPI_GETWHEELSCROLLLINES, 0, unsafe.Pointer(&lines))
	return lines
}

// currentThreadID identifies the OS thread the caller runs on.
func currentThreadID() uint32 { return win.GetCurrentThreadId() }

// viewHandle is the native handle the renderer draws into.
func (w *Window) viewHandle() uintptr { return uintptr(w.hwnd) }

// capturePointer keeps sending pointer events to the window while a button
// is held, also outside it.
func (w *Window) capturePointer() { win.SetCapture(w.hwnd) }

// releasePointer ends capturePointer.
func (w *Window) releasePointer() { win.ReleaseCapture() }

// trackMouseLeave arms the event that reports the pointer leaving the window.
func (w *Window) trackMouseLeave() {
	if w.tracking || w.ncHover {
		return
	}
	t := win.TRACKMOUSEEVENT{Size: uint32(unsafe.Sizeof(win.TRACKMOUSEEVENT{})), Flags: win.TME_LEAVE, Hwnd: w.hwnd}
	win.TrackMouseEvent(&t)
	w.tracking = true
}

// redrawAsync repaints the window without rebuilding it. It is safe from
// any goroutine.
func (w *Window) redrawAsync() {
	if h := w.hwnd; h != 0 {
		win.InvalidateRect(h, false)
	}
}
