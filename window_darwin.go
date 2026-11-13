package fairy

import (
	"fmt"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ebitengine/purego/objc"

	"github.com/myparsleycat/fairy/internal/darwin"
	"github.com/myparsleycat/fairy/internal/mackey"
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
	// frameTimer paces frames in place of the display link before macOS 14,
	// whose NSView has no displayLinkWithTarget:selector:.
	frameTimer *darwin.Timer
	timers     map[uintptr]*darwin.Timer
	closed     bool
	ime        imeState
	// event is the input event being handled, whose modifiers keyMods
	// reports.
	event darwin.ID
	// deadKeys composes dead keys typed while no input method sees keys.
	deadKeys mackey.DeadKeys
	// ctrlClick is set while a Control-click, the secondary click on a
	// Mac, is pressed: its mouseUp: is a right button up too.
	ctrlClick bool
	// placedInset is ControlsInset after placeControls last moved the
	// window controls.
	placedInset float32
}

// byView and byDelegate find the Window an AppKit callback is for.
var (
	byView     = map[darwin.ID]*Window{}
	byDelegate = map[darwin.ID]*Window{}
)

// viewWindow returns the open Window whose view is self, or nil once it is
// closed.
func viewWindow(self darwin.ID) *Window {
	if w := byView[self]; w != nil && !w.closed {
		return w
	}
	return nil
}

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
	if opts.Frameless {
		style |= nsFullSizeContentView
	}
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
	if opts.Frameless {
		w.setupFrameless()
	}
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

	w.registerDrop()

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
	if darwin.Get[bool](view, "respondsToSelector:", darwin.Sel("displayLinkWithTarget:selector:")) {
		w.link = darwin.Send(view, "displayLinkWithTarget:selector:", view, darwin.Sel("fairyFrame:"))
		darwin.Send(w.link, "setPaused:", true)
		darwin.Send(w.link, "addToRunLoop:forMode:", darwin.SendClass("NSRunLoop", "mainRunLoop"),
			darwin.SymbolID(darwin.Foundation, "NSRunLoopCommonModes"))
	}

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
	// Closing a web view hands the keyboard back to the window's view,
	// which secures a focused password input again: close them first.
	w.closeWebViews()
	w.securePassword(nil)
	w.releaseBackdrop()
	w.clearDockProgress()
	w.a11yShutdown()
	w.closed = true
	for id, t := range w.timers {
		t.Stop()
		delete(w.timers, id)
	}
	if w.link != 0 {
		darwin.Send(w.link, "invalidate")
		w.link = 0
	}
	w.frameTimer.Stop()
	w.frameTimer = nil
	w.release()
	delete(openWindows, w.nsWindow)
	delete(byView, w.view)
	delete(byDelegate, w.delegate)
	darwin.Send(w.nsWindow, "setDelegate:", darwin.ID(0))
	darwin.Release(w.delegate)
	darwin.Release(w.view)
	// The window may be closing inside its own close: release it afterwards.
	// The handles are cleared so that a call on the closed Window messages
	// nil, which does nothing, rather than a freed object.
	nw := w.nsWindow
	darwin.Async(func() { darwin.Release(nw) })
	w.nsWindow, w.view, w.layer, w.delegate = 0, 0, 0, 0
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
// from the top of the primary screen.
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

// mainScreenHeight is the height of the primary screen, the one with the
// menu bar, whose bottom left corner is the origin of screen coordinates.
// ([NSScreen mainScreen] is the screen with the key window.)
func mainScreenHeight() float64 {
	screens := darwin.SendClass("NSScreen", "screens")
	if screens == 0 || darwin.Get[uint64](screens, "count") == 0 {
		return 0
	}
	return darwin.Get[darwin.CGRect](darwin.Send(screens, "objectAtIndex:", uint64(0)), "frame").Size.H
}

// Close asks the window to close, running Options.OnClose.
func (w *Window) Close() {
	// Event handlers can run inside renderFrame. Keep its resources alive
	// until it returns, as posting WM_CLOSE does on Windows. performClose:
	// is not used: it beeps when OnClose keeps the window open.
	darwin.Perform(func() {
		if !w.closed && w.mayClose() {
			w.closeNow()
		}
	})
}

// mayClose asks Options.OnClose whether the window may close.
func (w *Window) mayClose() bool { return w.opts.OnClose == nil || w.opts.OnClose() }

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
	if !w.hasNativeWindow() {
		return
	}
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

// wake asks the UI loop to run the posted calls. The UI thread discards
// them when no native window remains.
func (w *Window) wake() bool {
	// Only inspect native state on the UI thread; Post may run elsewhere.
	darwin.Perform(func() {
		if w.hasNativeWindow() {
			w.runPosted()
		} else {
			w.mu.Lock()
			w.posted = nil
			w.mu.Unlock()
		}
	})
	return true
}

// requestPaint starts the display link, which draws a frame on the next
// refresh of the display, or the frame timer where there is no link.
func (w *Window) requestPaint() {
	switch {
	case w.closed:
	case w.link != 0 && !w.linkOn:
		w.linkOn = true
		darwin.Send(w.link, "setPaused:", false)
	case w.link == 0 && w.frameTimer == nil:
		// The timer calls the view's fairyFrame:, as the display link does.
		view := w.view
		w.frameTimer = darwin.NewTimer(time.Second/60, func() { darwin.Send(view, "fairyFrame:", darwin.ID(0)) })
	}
}

// nsWindowOcclusionStateVisible is the NSWindowOcclusionState bit of a
// window some part of which is on screen.
const nsWindowOcclusionStateVisible = 1 << 1

// onDisplayLink draws the frame the window asked for, and stops the display
// link once none is wanted. A window entirely out of sight draws nothing,
// since no drawable comes for it; it draws again once it shows.
func (w *Window) onDisplayLink() {
	occluded := darwin.Get[uint64](w.nsWindow, "occlusionState")&nsWindowOcclusionStateVisible == 0
	if w.needPaint && !occluded {
		w.renderFrame()
		w.placeControls()
	}
	if !w.needPaint || occluded {
		w.stopPaint()
	}
}

// stopPaint pauses the display link or stops the frame timer.
func (w *Window) stopPaint() {
	if w.linkOn && w.link != 0 {
		w.linkOn = false
		darwin.Send(w.link, "setPaused:", true)
	}
	w.frameTimer.Stop()
	w.frameTimer = nil
}

// redrawAsync repaints the window without rebuilding it. It is safe from
// any goroutine.
func (w *Window) redrawAsync() {
	darwin.Perform(func() {
		if !w.closed {
			w.invalidate()
		}
	})
}

// NSWindow returns the window's NSWindow, or 0 once it is closed.
func (w *Window) NSWindow() uintptr {
	if w.closed {
		return 0
	}
	return uintptr(w.nsWindow)
}

// viewHandle is the CAMetalLayer the renderer draws into.
func (w *Window) viewHandle() uintptr { return uintptr(w.layer) }

// pasteboard is the clipboard. Tests swap in a private pasteboard.
var pasteboard = func() darwin.ID { return darwin.SendClass("NSPasteboard", "generalPasteboard") }

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
	return setPasteboard(map[string]any{"public.utf8-plain-text": s})
}

// keyMods returns the modifier keys held with the event being handled, or
// held down now outside of events.
func (w *Window) keyMods() keyboard.Mods {
	if w.event != 0 {
		return macMods(darwin.Get[uint64](w.event, "modifierFlags"))
	}
	return macMods(darwin.Get[uint64](darwin.ID(darwin.Cls("NSEvent")), "modifierFlags"))
}

// handling makes ev the event keyMods reads until the returned function runs.
func (w *Window) handling(ev darwin.ID) func() {
	prev := w.event
	w.event = ev
	return func() { w.event = prev }
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

// Events from AppKit.

func (w *Window) eventPoint(ev darwin.ID) (float32, float32) {
	return w.viewPoint(darwin.Get[darwin.CGPoint](ev, "locationInWindow"))
}

func (w *Window) mouseDown(ev darwin.ID, button int) {
	defer w.handling(ev)()
	w.focusView()
	w.commitComposition()
	x, y := w.eventPoint(ev)
	if button == 0 && macMods(darwin.Get[uint64](ev, "modifierFlags"))&(keyboard.ModCtrl|keyboard.ModSuper) == keyboard.ModCtrl {
		// Control-click is the secondary click on a Mac.
		w.ctrlClick = true
		w.onButtonDown(x, y, 1)
		return
	}
	if button == 0 && !w.scripted && w.inDragRegion(x, y) {
		if darwin.Get[int64](ev, "clickCount") == 2 {
			w.titleBarDoubleClick()
		} else {
			darwin.Send(w.nsWindow, "performWindowDragWithEvent:", ev)
		}
		return
	}
	w.onButtonDown(x, y, button)
}

func (w *Window) mouseUp(ev darwin.ID, button int) {
	defer w.handling(ev)()
	x, y := w.eventPoint(ev)
	if button == 0 && w.ctrlClick {
		w.ctrlClick = false
		button = 1
	}
	w.onButtonUp(x, y, button)
}

func (w *Window) mouseMoved(ev darwin.ID) {
	defer w.handling(ev)()
	if w.scripted {
		return
	}
	x, y := w.eventPoint(ev)
	if w.press == nil && w.overWebView(x, y) {
		// The web view under the pointer has it, as a child window does.
		w.onMouseLeave()
		return
	}
	w.onMouseMove(x, y)
	// As WM_SETCURSOR does on Windows, each move shows the cursor of what
	// is under the pointer: the view sets no cursor rects.
	if !w.closed && w.mouseIn {
		w.applyCursor()
	}
}

func (w *Window) scrollWheel(ev darwin.ID) {
	if w.scripted {
		return
	}
	defer w.handling(ev)()
	x, y := w.eventPoint(ev)
	if w.overWebView(x, y) {
		return // a web view passed on a scroll it did not take
	}
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
	if w.focusedWebView() != nil {
		w.webViewKey(ev)
		return
	}
	defer w.handling(ev)()
	code := darwin.Get[uint16](ev, "keyCode")
	repeat := darwin.Get[bool](ev, "isARepeat")
	chars := darwin.GoString(darwin.Send(ev, "characters"))
	typed := typedText(chars)
	flags := darwin.Get[uint64](ev, "modifierFlags")
	if !w.imeOn && macMods(flags)&nonTypingMods == 0 {
		// Without the text input system, as in password inputs, the event's
		// characters carry no dead-key state: compose them here.
		if s, ok := w.deadKeys.Type(code, flags); ok {
			typed = typedText(s)
		}
	}
	k := mackey.Key(code)
	// While the input method composes, it sees keys first.
	composing := w.composition() != ""
	if !composing {
		w.wantChar = false
		if k != 0 && w.onKeyDown(k, repeat, typed) && !w.wantChar {
			return
		}
		if w.keyMods()&nonTypingMods != 0 {
			return
		}
	}
	if w.imeOn {
		if w.imeKey(ev, composing) && k != 0 {
			w.wantChar = false
			w.onKeyDown(k, repeat, typed)
		}
		return
	}
	for _, r := range typed {
		w.onTypedRune(r)
	}
}

// flagsChanged reports a modifier key going down to the key handlers, as
// Windows does with WM_KEYDOWN for Shift, Control, Alt and the Windows key.
func (w *Window) flagsChanged(ev darwin.ID) {
	if w.focusedWebView() != nil || w.composition() != "" {
		return
	}
	defer w.handling(ev)()
	code := darwin.Get[uint16](ev, "keyCode")
	var mask uint64
	switch code {
	case 0x38, 0x3C:
		mask = nsShift
	case 0x3B, 0x3E:
		mask = nsControl
	case 0x3A, 0x3D:
		mask = nsOption
	case 0x37, 0x36:
		mask = nsCommand
	default:
		return
	}
	if darwin.Get[uint64](ev, "modifierFlags")&mask == 0 {
		return // released
	}
	if k := mackey.Key(code); k != 0 {
		w.onKeyDown(k, false, "")
	}
}

// forwardEdit passes an edit action the view received from a web view
// inside it on to the window, which owns their undo manager.
func forwardEdit(view darwin.ID, action string, sender darwin.ID) {
	win := darwin.Send(view, "window")
	if win != 0 && darwin.Send(win, "firstResponder") != view {
		darwin.Send(win, action, sender)
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
			if w := viewWindow(self); w != nil {
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
				if w.press == nil && w.autoScroll == nil {
					darwin.Send(darwin.SendClass("NSCursor", "arrowCursor"), "set")
				}
			}
		})},
		{Cmd: darwin.Sel("scrollWheel:"), Fn: on((*Window).scrollWheel)},
		{Cmd: darwin.Sel("keyDown:"), Fn: on((*Window).keyDown)},
		{Cmd: darwin.Sel("keyUp:"), Fn: func(id, sel, id) {}},
		{Cmd: darwin.Sel("flagsChanged:"), Fn: on((*Window).flagsChanged)},
		// NSWindow answers undo: and redo: with its undo manager, which web
		// views share. The view answers them itself so that, while it has
		// the keyboard, the Edit menu's items stay disabled and ⌘Z reaches
		// keyDown:, and otherwise passes them on to the window.
		{Cmd: darwin.Sel("undo:"), Fn: func(self id, _ sel, sender id) { forwardEdit(self, "undo:", sender) }},
		{Cmd: darwin.Sel("redo:"), Fn: func(self id, _ sel, sender id) { forwardEdit(self, "redo:", sender) }},
		{Cmd: darwin.Sel("validateMenuItem:"), Fn: func(self id, _ sel, item id) bool {
			switch darwin.Get[darwin.SEL](item, "action") {
			case darwin.Sel("undo:"), darwin.Sel("redo:"):
				win := darwin.Send(self, "window")
				if win == 0 || darwin.Send(win, "firstResponder") == self {
					return false
				}
				return darwin.Get[bool](win, "validateMenuItem:", item)
			}
			return true
		}},
	})
	if err != nil {
		panic(err)
	}
	addTextInputClient(c)
	addFocusTracking(c)
	addDropTarget(c)
	addAccessibility(c)
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
			return w == nil || w.mayClose()
		}},
		{Cmd: darwin.Sel("windowWillClose:"), Fn: on(func(w *Window) {
			w.windowClosed()
			quitIfIdle()
		})},
		{Cmd: darwin.Sel("windowDidResize:"), Fn: on(func(w *Window) {
			w.onResize(w.pixelSize())
			w.placeControls()
		})},
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
		// While a web view has the keyboard, the window's elements have
		// lost focus already and do not get it back with the window.
		{Cmd: darwin.Sel("windowDidBecomeKey:"), Fn: on(func(w *Window) {
			if w.focusedWebView() == nil {
				w.onFocusChange(true)
				w.securePassword(w.focusedEl())
			}
		})},
		{Cmd: darwin.Sel("windowDidResignKey:"), Fn: on(func(w *Window) {
			w.securePassword(nil) // give other apps the user's input source back
			if w.osFocus {
				w.onFocusChange(false)
			}
		})},
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
		// Draw the frames held back while the window was out of sight.
		{Cmd: darwin.Sel("windowDidChangeOcclusionState:"), Fn: on(func(w *Window) {
			if w.needPaint {
				w.requestPaint()
			}
		})},
	})
	if err != nil {
		panic(err)
	}
	return c
})
