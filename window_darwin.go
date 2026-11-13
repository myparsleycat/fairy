package fairy

import (
	"fmt"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ebitengine/purego/objc"

	"github.com/myparsleycat/fairy/internal/darwin"
	"github.com/myparsleycat/fairy/internal/text"
	"github.com/myparsleycat/fairy/keyboard"
)

// defaultFont is the font family of Options.Font when it is empty: the
// system UI font.
const defaultFont = ".AppleSystemUIFont"

// windowPlat is the AppKit part of a Window.
type windowPlat struct {
	nsWindow darwin.ID // NSWindow
	view     darwin.ID // FairyView, the content view
	layer    darwin.ID // the view's CAMetalLayer
	delegate darwin.ID // FairyWindowDelegate
	link     darwin.ID // CADisplayLink that paces frames while one is wanted
	linkOn   bool
	timers   map[uintptr]*darwin.Timer
	closed   bool
}

// byView and byDelegate find the Window an AppKit callback is for.
var (
	byView     = map[darwin.ID]*Window{}
	byDelegate = map[darwin.ID]*Window{}
)

// NSWindowStyleMask bits.
const (
	nsTitled         = 1 << 0
	nsClosable       = 1 << 1
	nsMiniaturizable = 1 << 2
	nsResizable      = 1 << 3
)

// NewWindow creates a window. Call it on the UI thread: inside Main's start
// function, a handler, or Do.
func NewWindow(opts Options) (*Window, error) {
	w, err := newWindowState(opts)
	if err != nil {
		return nil, err
	}
	opts = w.opts
	launchApp()
	viewClass()
	delegateClass()

	style := uint64(nsTitled | nsClosable | nsMiniaturizable | nsResizable)
	nw := darwin.Send(darwin.SendClass("NSWindow", "alloc"), "initWithContentRect:styleMask:backing:defer:",
		darwin.Rect(0, 0, float64(opts.Width), float64(opts.Height)), style, uint64(2), false)
	if nw == 0 {
		return nil, fmt.Errorf("fairy: create window failed")
	}
	darwin.Send(nw, "setReleasedWhenClosed:", false)
	darwin.Send(nw, "setTitle:", darwin.String(opts.Title))
	if opts.MinWidth > 0 || opts.MinHeight > 0 {
		darwin.Send(nw, "setContentMinSize:", darwin.CGSize{W: float64(opts.MinWidth), H: float64(opts.MinHeight)})
	}
	darwin.Send(nw, "setAcceptsMouseMovedEvents:", true)
	darwin.Send(nw, "center")
	w.nsWindow = nw
	w.setDarkFrame(opts.Dark)
	w.setWindowBackground()

	view := darwin.Send(darwin.SendClass("FairyView", "alloc"), "initWithFrame:",
		darwin.Rect(0, 0, float64(opts.Width), float64(opts.Height)))
	w.view = view
	byView[view] = w
	darwin.Send(view, "setWantsLayer:", true)
	w.layer = darwin.Send(view, "layer")
	darwin.Send(nw, "setContentView:", view)
	darwin.Send(nw, "makeFirstResponder:", view)
	tracking := darwin.Send(darwin.SendClass("NSTrackingArea", "alloc"), "initWithRect:options:owner:userInfo:",
		darwin.CGRect{}, uint64(0x01|0x02|0x80|0x200), view, darwin.ID(0)) // entered/exited, moved, always active, in visible rect
	darwin.Send(view, "addTrackingArea:", tracking)
	darwin.Release(tracking)

	w.delegate = darwin.New("FairyWindowDelegate")
	byDelegate[w.delegate] = w
	darwin.Send(nw, "setDelegate:", w.delegate)
	openWindows[nw] = w

	w.scale = float32(darwin.Get[float64](nw, "backingScaleFactor"))
	darwin.Send(w.layer, "setContentsScale:", float64(w.scale))
	w.width, w.height = w.pixelSize()
	if w.r, err = w.newRenderer(); err != nil {
		w.closeNow()
		return nil, fmt.Errorf("fairy: renderer: %w", err)
	}
	if opts.Backdrop != BackdropNone && BackdropSupported(opts.Backdrop) {
		_ = w.SetBackdrop(opts.Backdrop)
	}
	if w.ts, err = text.NewSystem(w.r.Atlas); err != nil {
		w.closeNow()
		return nil, fmt.Errorf("fairy: text: %w", err)
	}
	w.ts.SetColorGlyphTextures(w.newGlyphTexture)
	w.link = darwin.Send(view, "displayLinkWithTarget:selector:", view, darwin.Sel("fairyFrame:"))
	darwin.Send(w.link, "setPaused:", true)
	darwin.Send(w.link, "addToRunLoop:forMode:", darwin.SendClass("NSRunLoop", "mainRunLoop"),
		darwin.SymbolID(darwin.Foundation, "NSRunLoopCommonModes"))

	w.renderFrame()
	if !opts.StartHidden {
		w.Show()
	}
	w.snapshotFromEnv()
	w.runScriptFromEnv()
	liveMu.Lock()
	live[w] = struct{}{}
	liveMu.Unlock()
	watchMemoryPressure()
	return w, nil
}

// pixelSize is the content view's size in device pixels.
func (w *Window) pixelSize() (int, int) {
	b := darwin.Get[darwin.CGRect](w.view, "bounds")
	return int(b.Size.W*float64(w.scale) + 0.5), int(b.Size.H*float64(w.scale) + 0.5)
}

func (w *Window) setWindowBackground() {
	c := w.opts.Background
	if !c.isSet() {
		return
	}
	a := c.arr(1)
	bg := darwin.SendClass("NSColor", "colorWithSRGBRed:green:blue:alpha:", float64(a[0]), float64(a[1]), float64(a[2]), float64(1))
	darwin.Send(w.nsWindow, "setBackgroundColor:", bg)
}

// closeNow closes the window without asking OnClose.
func (w *Window) closeNow() {
	if w.nsWindow == 0 || w.closed {
		return
	}
	darwin.Send(w.nsWindow, "close")
	w.windowClosed()
}

// windowClosed releases what the closed window held.
func (w *Window) windowClosed() {
	if w.closed {
		return
	}
	w.closed = true
	for id, t := range w.timers {
		t.Stop()
		delete(w.timers, id)
	}
	if w.link != 0 {
		darwin.Send(w.link, "invalidate")
		w.link = 0
	}
	w.release()
	delete(openWindows, w.nsWindow)
	delete(byView, w.view)
	delete(byDelegate, w.delegate)
	darwin.Send(w.nsWindow, "setDelegate:", darwin.ID(0))
	darwin.Release(w.delegate)
	darwin.Release(w.view)
	w.layer = 0
	// The window may be closing inside its own close: release it afterwards.
	nw := w.nsWindow
	darwin.Async(func() { darwin.Release(nw) })
}

// Show shows and activates the window.
func (w *Window) Show() {
	if w.closed {
		return
	}
	if w.IsMinimized() {
		darwin.Send(w.nsWindow, "deminiaturize:", darwin.ID(0))
	}
	darwin.Send(w.nsWindow, "makeKeyAndOrderFront:", darwin.ID(0))
	darwin.Send(nsApp, "activateIgnoringOtherApps:", true)
	w.invalidate()
	w.shown()
}

// Hide hides the window without closing it.
func (w *Window) Hide() {
	if !w.closed {
		darwin.Send(w.nsWindow, "orderOut:", darwin.ID(0))
	}
}

// Minimize minimizes the window.
func (w *Window) Minimize() {
	if !w.closed {
		darwin.Send(w.nsWindow, "miniaturize:", darwin.ID(0))
	}
}

// ToggleMaximize maximizes or restores the window.
func (w *Window) ToggleMaximize() {
	if !w.closed {
		darwin.Send(w.nsWindow, "zoom:", darwin.ID(0))
	}
}

// IsMaximized reports whether the window is maximized (zoomed).
func (w *Window) IsMaximized() bool { return !w.closed && darwin.Get[bool](w.nsWindow, "isZoomed") }

// Maximize maximizes the window.
func (w *Window) Maximize() {
	if !w.IsMaximized() {
		w.ToggleMaximize()
	}
}

// Restore restores a minimized or maximized window to its normal size.
func (w *Window) Restore() {
	switch {
	case w.IsMinimized():
		darwin.Send(w.nsWindow, "deminiaturize:", darwin.ID(0))
	case w.IsMaximized():
		w.ToggleMaximize()
	}
}

// IsMinimized reports whether the window is minimized.
func (w *Window) IsMinimized() bool {
	return w.nsWindow != 0 && !w.closed && darwin.Get[bool](w.nsWindow, "isMiniaturized")
}

// IsVisible reports whether the window is shown (it may be minimized).
func (w *Window) IsVisible() bool {
	return w.nsWindow != 0 && !w.closed && (darwin.Get[bool](w.nsWindow, "isVisible") || w.IsMinimized())
}

// Bounds returns the window's frame in screen points, with y measured down
// from the top of the main screen.
func (w *Window) Bounds() Bounds {
	if w.closed {
		return Bounds{}
	}
	f := darwin.Get[darwin.CGRect](w.nsWindow, "frame")
	top := mainScreenHeight() - (f.Origin.Y + f.Size.H)
	return Bounds{int(f.Origin.X), int(top), int(f.Size.W), int(f.Size.H)}
}

// SetBounds moves and sizes the window's frame, in the coordinates Bounds uses.
func (w *Window) SetBounds(b Bounds) {
	if w.closed {
		return
	}
	y := mainScreenHeight() - float64(b.Y+b.H)
	darwin.Send(w.nsWindow, "setFrame:display:", darwin.Rect(float64(b.X), y, float64(b.W), float64(b.H)), true)
}

func mainScreenHeight() float64 {
	s := darwin.SendClass("NSScreen", "mainScreen")
	if s == 0 {
		return 0
	}
	return darwin.Get[darwin.CGRect](s, "frame").Size.H
}

// Close asks the window to close, running Options.OnClose.
func (w *Window) Close() {
	if !w.closed {
		darwin.Send(w.nsWindow, "performClose:", darwin.ID(0))
	}
}

// resizeClient sizes the window for a content area of width x height
// logical pixels, keeping its top left corner in place.
func (w *Window) resizeClient(width, height float32) {
	if w.closed {
		return
	}
	f := darwin.Get[darwin.CGRect](w.nsWindow, "frame")
	top := f.Origin.Y + f.Size.H
	darwin.Send(w.nsWindow, "setContentSize:", darwin.CGSize{W: float64(width), H: float64(height)})
	f = darwin.Get[darwin.CGRect](w.nsWindow, "frame")
	darwin.Send(w.nsWindow, "setFrameTopLeftPoint:", darwin.CGPoint{X: f.Origin.X, Y: top})
}

// SetTitle changes the window title.
func (w *Window) SetTitle(s string) {
	if !w.closed {
		darwin.Send(w.nsWindow, "setTitle:", darwin.String(s))
	}
}

// setDarkFrame switches the title bar and frame to dark or light.
func (w *Window) setDarkFrame(dark bool) {
	name := "NSAppearanceNameAqua"
	if dark {
		name = "NSAppearanceNameDarkAqua"
	}
	ap := darwin.SendClass("NSAppearance", "appearanceNamed:", darwin.SymbolID(darwin.AppKit, name))
	darwin.Send(w.nsWindow, "setAppearance:", ap)
}

// hasNativeWindow reports whether the window has a native window, which
// offscreen windows and closed ones do not.
func (w *Window) hasNativeWindow() bool { return w.nsWindow != 0 && !w.closed }

// wake asks the UI loop to run the posted calls. It reports false when the
// window is gone.
func (w *Window) wake() bool {
	if w.nsWindow == 0 || w.closed {
		return false
	}
	darwin.Async(func() {
		if !w.closed {
			w.runPosted()
		}
	})
	return true
}

// requestPaint starts the display link, which draws a frame on the next
// refresh of the display.
func (w *Window) requestPaint() {
	if w.link != 0 && !w.linkOn {
		w.linkOn = true
		darwin.Send(w.link, "setPaused:", false)
	}
}

// onDisplayLink draws the frame the window asked for, and stops the display
// link once none is wanted.
func (w *Window) onDisplayLink() {
	if w.needPaint {
		w.renderFrame()
	}
	if !w.needPaint && w.linkOn && w.link != 0 {
		w.linkOn = false
		darwin.Send(w.link, "setPaused:", true)
	}
}

// redrawAsync repaints the window without rebuilding it. It is safe from
// any goroutine.
func (w *Window) redrawAsync() {
	darwin.Async(func() {
		if !w.closed {
			w.invalidate()
		}
	})
}

// viewHandle is the CAMetalLayer the renderer draws into.
func (w *Window) viewHandle() uintptr { return uintptr(w.layer) }

func pasteboard() darwin.ID { return darwin.SendClass("NSPasteboard", "generalPasteboard") }

var pasteboardString = sync.OnceValue(func() darwin.ID {
	return darwin.SymbolID(darwin.AppKit, "NSPasteboardTypeString")
})

// systemClipboardText returns the clipboard's text, and false when it holds
// none or could not be read.
func (w *Window) systemClipboardText() (string, bool) {
	var s string
	var ok bool
	darwin.WithPool(func() {
		str := darwin.Send(pasteboard(), "stringForType:", pasteboardString())
		s, ok = darwin.GoString(str), str != 0
	})
	return s, ok
}

func (w *Window) systemClipboardHasText() bool {
	_, ok := w.systemClipboardText()
	return ok
}

func (w *Window) setSystemClipboardText(s string) bool {
	ok := false
	darwin.WithPool(func() {
		pb := pasteboard()
		darwin.Send(pb, "clearContents")
		ok = darwin.Get[bool](pb, "setString:forType:", darwin.String(s), pasteboardString())
	})
	return ok
}

// keyMods returns the modifier keys held down now.
func (w *Window) keyMods() keyboard.Mods {
	return macMods(darwin.Get[uint64](darwin.ID(darwin.Cls("NSEvent")), "modifierFlags"))
}

// setTimer calls onTimer(id) every ms milliseconds until killTimer, replacing
// a timer with the same id, as Windows timers do.
func (w *Window) setTimer(id uintptr, ms uint32) {
	if w.closed {
		return
	}
	w.killTimer(id)
	if w.timers == nil {
		w.timers = map[uintptr]*darwin.Timer{}
	}
	w.timers[id] = darwin.NewTimer(time.Duration(ms)*time.Millisecond, func() {
		if !w.closed {
			w.onTimer(id)
		}
	})
}

func (w *Window) killTimer(id uintptr) {
	if t, ok := w.timers[id]; ok {
		t.Stop()
		delete(w.timers, id)
	}
}

// AppKit sends the drags and the release of a press to the view it began
// in, also outside the window, so no capture is needed.
func (w *Window) capturePointer() {}
func (w *Window) releasePointer() {}

// The view's tracking area reports the pointer leaving at all times.
func (w *Window) trackMouseLeave()  {}
func (w *Window) rearmMouseLeave()  {}
func (w *Window) ignoreMouseLeave() {}

// pointerPos returns where the pointer is now in window coordinates, also
// outside the window.
func (w *Window) pointerPos() (x, y float32) {
	if w.closed {
		return w.mouseX, w.mouseY
	}
	p := darwin.Get[darwin.CGPoint](w.nsWindow, "mouseLocationOutsideOfEventStream")
	return w.viewPoint(p)
}

// viewPoint converts a point in window coordinates to logical view
// coordinates, which are points with the origin at the top left.
func (w *Window) viewPoint(p darwin.CGPoint) (float32, float32) {
	v := darwin.Get[darwin.CGPoint](w.view, "convertPoint:fromView:", p, darwin.ID(0))
	return float32(v.X), float32(v.Y)
}

// setCursor shows c as the pointer over the window.
func (w *Window) setCursor(c Cursor) {
	name := "arrowCursor"
	switch c {
	case CursorPointer:
		name = "pointingHandCursor"
	case CursorText:
		name = "IBeamCursor"
	case CursorMove:
		name = "openHandCursor"
	case CursorNotAllowed:
		name = "operationNotAllowedCursor"
	case CursorResizeEW:
		name = "resizeLeftRightCursor"
	case CursorResizeNS:
		name = "resizeUpDownCursor"
	case CursorCrosshair:
		name = "crosshairCursor"
	}
	darwin.Send(darwin.SendClass("NSCursor", name), "set")
}

// setPanCursor shows the autoscroll cursor for the pointer's place.
func (w *Window) setPanCursor() {
	w.autoScroll.cursor = w.panCursor()
	darwin.Send(darwin.SendClass("NSCursor", "closedHandCursor"), "set")
}

// popupMenu shows items as a native menu at window point (x, y) and waits
// until it closes.
func (w *Window) popupMenu(x, y float32, items []MenuItem) {}

// enableIME turns the input method on for the window or off.
func (w *Window) enableIME(on bool) {}

// commitComposition ends the input method's composition, keeping its text.
func (w *Window) commitComposition() {}

// updateIMEPosition moves the input method's candidate window to the caret.
func (w *Window) updateIMEPosition() {}

// Events from AppKit.

func (w *Window) eventPoint(ev darwin.ID) (float32, float32) {
	return w.viewPoint(darwin.Get[darwin.CGPoint](ev, "locationInWindow"))
}

func (w *Window) mouseDown(ev darwin.ID, button int) {
	x, y := w.eventPoint(ev)
	w.onButtonDown(x, y, button)
}

func (w *Window) mouseUp(ev darwin.ID, button int) {
	x, y := w.eventPoint(ev)
	w.onButtonUp(x, y, button)
}

func (w *Window) mouseMoved(ev darwin.ID) {
	if !w.scripted {
		w.onMouseMove(w.eventPoint(ev))
	}
}

func (w *Window) scrollWheel(ev darwin.ID) {
	if w.scripted {
		return
	}
	x, y := w.eventPoint(ev)
	precise := darwin.Get[bool](ev, "hasPreciseScrollingDeltas")
	dx := darwin.Get[float64](ev, "scrollingDeltaX")
	dy := darwin.Get[float64](ev, "scrollingDeltaY")
	if precise {
		// Trackpads report points; a notch scrolls 100.
		dx, dy = dx/100, dy/100
	}
	if dy != 0 {
		w.onWheel(x, y, float32(dy), false, precise)
	}
	if dx != 0 {
		w.onWheel(x, y, float32(dx), true, precise)
	}
}

func (w *Window) keyDown(ev darwin.ID) {
	code := darwin.Get[uint16](ev, "keyCode")
	repeat := darwin.Get[bool](ev, "isARepeat")
	chars := darwin.GoString(darwin.Send(ev, "characters"))
	typed := typedText(chars)
	w.wantChar = false
	if k := macKey(code); k != 0 && w.onKeyDown(k, repeat, typed) && !w.wantChar {
		return
	}
	if w.keyMods()&(keyboard.ModCtrl|keyboard.ModSuper) != 0 {
		return
	}
	for _, r := range typed {
		w.onTypedRune(r)
	}
}

// typedText is the text a key typed, without the control characters and
// the private-use characters AppKit reports for function keys.
func typedText(s string) string {
	for i, r := range s {
		if r < 0x20 || r == 0x7f || r >= 0xF700 && r <= 0xF8FF || r == utf8.RuneError {
			return s[:i]
		}
	}
	return s
}

// AppKit classes.

var viewClass = sync.OnceValue(func() darwin.Class {
	type id = darwin.ID
	type sel = darwin.SEL
	on := func(f func(w *Window, ev id)) func(self id, _ sel, ev id) {
		return func(self id, _ sel, ev id) {
			if w := byView[self]; w != nil && !w.closed {
				f(w, ev)
			}
		}
	}
	c, err := objc.RegisterClass("FairyView", darwin.Cls("NSView"), nil, nil, []objc.MethodDef{
		{Cmd: darwin.Sel("isFlipped"), Fn: func(id, sel) bool { return true }},
		{Cmd: darwin.Sel("acceptsFirstResponder"), Fn: func(id, sel) bool { return true }},
		{Cmd: darwin.Sel("acceptsFirstMouse:"), Fn: func(id, sel, id) bool { return true }},
		{Cmd: darwin.Sel("wantsUpdateLayer"), Fn: func(id, sel) bool { return true }},
		{Cmd: darwin.Sel("makeBackingLayer"), Fn: func(self id, _ sel) id {
			return darwin.SendClass("CAMetalLayer", "layer")
		}},
		{Cmd: darwin.Sel("fairyFrame:"), Fn: on(func(w *Window, _ id) { w.onDisplayLink() })},
		{Cmd: darwin.Sel("mouseDown:"), Fn: on(func(w *Window, ev id) { w.mouseDown(ev, 0) })},
		{Cmd: darwin.Sel("mouseUp:"), Fn: on(func(w *Window, ev id) { w.mouseUp(ev, 0) })},
		{Cmd: darwin.Sel("rightMouseDown:"), Fn: on(func(w *Window, ev id) { w.mouseDown(ev, 1) })},
		{Cmd: darwin.Sel("rightMouseUp:"), Fn: on(func(w *Window, ev id) { w.mouseUp(ev, 1) })},
		{Cmd: darwin.Sel("otherMouseDown:"), Fn: on(func(w *Window, ev id) {
			if darwin.Get[int64](ev, "buttonNumber") == 2 {
				w.mouseDown(ev, 2)
			}
		})},
		{Cmd: darwin.Sel("otherMouseUp:"), Fn: on(func(w *Window, ev id) {
			switch darwin.Get[int64](ev, "buttonNumber") {
			case 2:
				w.mouseUp(ev, 2)
			case 3:
				w.onKeyDown(keyboard.KeyBrowserBack, false, "")
			case 4:
				w.onKeyDown(keyboard.KeyBrowserForward, false, "")
			}
		})},
		{Cmd: darwin.Sel("mouseMoved:"), Fn: on((*Window).mouseMoved)},
		{Cmd: darwin.Sel("mouseDragged:"), Fn: on((*Window).mouseMoved)},
		{Cmd: darwin.Sel("rightMouseDragged:"), Fn: on((*Window).mouseMoved)},
		{Cmd: darwin.Sel("otherMouseDragged:"), Fn: on((*Window).mouseMoved)},
		{Cmd: darwin.Sel("mouseEntered:"), Fn: on((*Window).mouseMoved)},
		{Cmd: darwin.Sel("mouseExited:"), Fn: on(func(w *Window, _ id) {
			if !w.scripted {
				w.onMouseLeave()
			}
		})},
		{Cmd: darwin.Sel("scrollWheel:"), Fn: on((*Window).scrollWheel)},
		{Cmd: darwin.Sel("keyDown:"), Fn: on((*Window).keyDown)},
		{Cmd: darwin.Sel("keyUp:"), Fn: func(id, sel, id) {}},
		{Cmd: darwin.Sel("flagsChanged:"), Fn: func(id, sel, id) {}},
	})
	if err != nil {
		panic(err)
	}
	return c
})

var delegateClass = sync.OnceValue(func() darwin.Class {
	type id = darwin.ID
	type sel = darwin.SEL
	on := func(f func(w *Window)) func(self id, _ sel, n id) {
		return func(self id, _ sel, n id) {
			if w := byDelegate[self]; w != nil && !w.closed {
				f(w)
			}
		}
	}
	c, err := objc.RegisterClass("FairyWindowDelegate", darwin.Cls("NSObject"), nil, nil, []objc.MethodDef{
		{Cmd: darwin.Sel("windowShouldClose:"), Fn: func(self id, _ sel, sender id) bool {
			w := byDelegate[self]
			return w == nil || w.opts.OnClose == nil || w.opts.OnClose()
		}},
		{Cmd: darwin.Sel("windowWillClose:"), Fn: on(func(w *Window) {
			w.windowClosed()
			quitIfIdle()
		})},
		{Cmd: darwin.Sel("windowDidResize:"), Fn: on(func(w *Window) { w.onResize(w.pixelSize()) })},
		{Cmd: darwin.Sel("windowDidChangeBackingProperties:"), Fn: on(func(w *Window) {
			s := float32(darwin.Get[float64](w.nsWindow, "backingScaleFactor"))
			if s == w.scale {
				return
			}
			w.scale = s
			darwin.Send(w.layer, "setContentsScale:", float64(s))
			InvalidateMemos()
			w.markDirty(dirtyResize)
			w.onResize(w.pixelSize())
			w.invalidate()
		})},
		{Cmd: darwin.Sel("windowDidBecomeKey:"), Fn: on(func(w *Window) { w.onFocusChange(true) })},
		{Cmd: darwin.Sel("windowDidResignKey:"), Fn: on(func(w *Window) { w.onFocusChange(false) })},
		{Cmd: darwin.Sel("windowWillStartLiveResize:"), Fn: on(func(w *Window) { w.sizeMove = true })},
		{Cmd: darwin.Sel("windowDidEndLiveResize:"), Fn: on(func(w *Window) {
			w.sizeMove = false
			w.killTimer(timerAnim)
			if w.animating || !w.wakeAt.IsZero() {
				w.invalidate()
			}
		})},
		{Cmd: darwin.Sel("windowDidMiniaturize:"), Fn: on(func(w *Window) {
			// Pixels stay in memory, so the textures upload again on restore.
			w.releaseTextures()
		})},
		{Cmd: darwin.Sel("windowDidDeminiaturize:"), Fn: on(func(w *Window) { w.invalidate() })},
	})
	if err != nil {
		panic(err)
	}
	return c
})
