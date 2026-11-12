package fairy

import (
	"sync"
	"time"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/pancursor"
	"github.com/myparsleycat/fairy/internal/win"
)

// defaultFont is the font family of Options.Font when it is empty.
const defaultFont = "Segoe UI"

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

// rearmMouseLeave makes the next move arm trackMouseLeave again.
func (w *Window) rearmMouseLeave() { w.tracking = false }

// redrawAsync repaints the window without rebuilding it. It is safe from
// any goroutine.
func (w *Window) redrawAsync() {
	if h := w.hwnd; h != 0 {
		win.InvalidateRect(h, false)
	}
}

// pointerPos returns where the pointer is now in window coordinates, also
// outside the window.
func (w *Window) pointerPos() (x, y float32) {
	p := win.GetCursorPos()
	win.ScreenToClient(w.hwnd, &p)
	return float32(p.X) / w.scale, float32(p.Y) / w.scale
}

func (w *Window) setPanCursor() {
	a := w.autoScroll
	a.cursor = w.panCursor()
	size := int(win.GetSystemMetricsForDpi(win.SM_CXCURSOR, uint32(w.scale*96+0.5)))
	if size <= 0 {
		size = 32
	}
	win.SetCursor(panCursorHandle(a.cursor, size))
}

var panCursors struct {
	sync.Mutex
	m map[[2]int]win.HANDLE
}

// panCursorHandle returns the cursor c at size pixels, made on first use and
// kept for the life of the process.
func panCursorHandle(c pancursor.Kind, size int) win.HANDLE {
	panCursors.Lock()
	defer panCursors.Unlock()
	key := [2]int{int(c), size}
	if h, ok := panCursors.m[key]; ok {
		return h
	}
	h := win.CreateCursorFromBGRA(pancursor.Pixels(c, size), size, size, size/2, size/2)
	if h == 0 {
		h = win.LoadCursor(win.IDC_SIZEALL)
	}
	if panCursors.m == nil {
		panCursors.m = map[[2]int]win.HANDLE{}
	}
	panCursors.m[key] = h
	return h
}

// systemLocale is the user's locale as a BCP 47 tag, such as "en-US".
func systemLocale() string { return win.UserLocale() }
