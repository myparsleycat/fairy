package fairy

import "github.com/myparsleycat/fairy/keyboard"

// defaultFont is the font family of Options.Font when it is empty.
const defaultFont = ".AppleSystemUIFont"

// windowPlat is the AppKit part of a Window.
type windowPlat struct{}

// NewWindow creates a window. Call it on the UI thread: inside Main's start
// function, a handler, or Do.
func NewWindow(opts Options) (*Window, error) {
	if _, err := newWindowState(opts); err != nil {
		return nil, err
	}
	return nil, errNoBackend
}

// Show shows and activates the window.
func (w *Window) Show() {}

// Hide hides the window without closing it.
func (w *Window) Hide() {}

// Minimize minimizes the window.
func (w *Window) Minimize() {}

// ToggleMaximize maximizes or restores the window.
func (w *Window) ToggleMaximize() {}

// IsMaximized reports whether the window is maximized.
func (w *Window) IsMaximized() bool { return false }

// Maximize maximizes the window.
func (w *Window) Maximize() {}

// Restore restores a minimized or maximized window to its normal size.
func (w *Window) Restore() {}

// IsMinimized reports whether the window is minimized.
func (w *Window) IsMinimized() bool { return false }

// IsVisible reports whether the window is shown (it may be minimized).
func (w *Window) IsVisible() bool { return false }

// Bounds returns the window's normal (restored) rectangle.
func (w *Window) Bounds() Bounds { return Bounds{} }

// SetBounds moves and sizes the window's normal rectangle.
func (w *Window) SetBounds(b Bounds) {}

// Close asks the window to close, running Options.OnClose.
func (w *Window) Close() {}

// SetTitle changes the window title.
func (w *Window) SetTitle(s string) {}

// setDarkFrame switches the system title bar and frame to dark or light.
func (w *Window) setDarkFrame(dark bool) {}

// hasNativeWindow reports whether the window has a native window, which
// offscreen windows and closed ones do not.
func (w *Window) hasNativeWindow() bool { return false }

// wake asks the UI loop to run the posted calls. It reports false when the
// window is gone.
func (w *Window) wake() bool { return false }

// requestPaint asks for a frame.
func (w *Window) requestPaint() {}

// redrawAsync repaints the window without rebuilding it. It is safe from
// any goroutine.
func (w *Window) redrawAsync() {}

// viewHandle is the native handle the renderer draws into.
func (w *Window) viewHandle() uintptr { return 0 }

// systemClipboardText returns the clipboard's text, and false when it holds
// none or could not be read.
func (w *Window) systemClipboardText() (string, bool) { return "", false }

func (w *Window) systemClipboardHasText() bool { return false }

func (w *Window) setSystemClipboardText(s string) bool { return false }

// keyMods returns the modifier keys held down now.
func (w *Window) keyMods() keyboard.Mods { return 0 }

func (w *Window) setTimer(id uintptr, ms uint32) {}
func (w *Window) killTimer(id uintptr)           {}

// capturePointer keeps sending pointer events to the window while a button
// is held, also outside it.
func (w *Window) capturePointer() {}

// releasePointer ends capturePointer.
func (w *Window) releasePointer() {}

// trackMouseLeave arms the event that reports the pointer leaving the window.
func (w *Window) trackMouseLeave() {}

// rearmMouseLeave makes the next move arm trackMouseLeave again.
func (w *Window) rearmMouseLeave() {}

// pointerPos returns where the pointer is now in window coordinates, also
// outside the window.
func (w *Window) pointerPos() (x, y float32) { return w.mouseX, w.mouseY }

// setCursor shows c as the pointer over the window.
func (w *Window) setCursor(c Cursor) {}

// setPanCursor shows the autoscroll cursor for the pointer's place.
func (w *Window) setPanCursor() { w.autoScroll.cursor = w.panCursor() }

// popupMenu shows items as a native menu at window point (x, y) and waits
// until it closes.
func (w *Window) popupMenu(x, y float32, items []MenuItem) {}

// enableIME turns the input method on for the window or off.
func (w *Window) enableIME(on bool) {}

// commitComposition ends the input method's composition, keeping its text.
func (w *Window) commitComposition() {}

// updateIMEPosition moves the input method's candidate window to the caret.
func (w *Window) updateIMEPosition() {}
