package fairy

import (
	"fmt"
	"log"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/myparsleycat/fairy/d3d"
	"github.com/myparsleycat/fairy/internal/frameprof"
	"github.com/myparsleycat/fairy/internal/gesture"
	"github.com/myparsleycat/fairy/internal/pancursor"
	"github.com/myparsleycat/fairy/internal/render"
	"github.com/myparsleycat/fairy/internal/text"
	"github.com/myparsleycat/fairy/internal/uia"
	"github.com/myparsleycat/fairy/internal/win"
	"github.com/myparsleycat/fairy/keyboard"
)

// windowPlat is the Win32 part of a Window.
type windowPlat struct {
	hwnd       win.HWND
	tracking   bool   // TrackMouseEvent is armed for WM_MOUSELEAVE
	charHigh   uint16 // high surrogate of a WM_CHAR pair awaiting its low half
	ncTracking bool   // TrackMouseEvent is armed for the non-client maximize button
	ncHover    bool   // the pointer is over a MaximizeButton, which Windows treats as non-client
}

const msgWake = win.WM_APP + 1

// defaultFont is the font family of Options.Font when it is empty.
const defaultFont = "Segoe UI"

// hasNativeWindow reports whether the window has a native window, which
// headless windows and closed ones do not.
func (w *Window) hasNativeWindow() bool { return w.hwnd != 0 }

// wake asks the message loop to run the posted calls. It reports false when
// the window is gone.
func (w *Window) wake() bool { return win.PostMessage(w.hwnd, msgWake, 0, 0) }

// paintWaiting draws the other visible windows that wait for a frame.
// GetMessage hands out WM_PAINT for one window at a time, always the same one
// first, so a window that animates without pause would keep the others from
// drawing at all.
func paintWaiting(painted *Window) {
	var waiting []*Window
	for _, o := range windows {
		if o != painted && o.needPaint && o.hl == nil && o.IsVisible() {
			waiting = append(waiting, o)
		}
	}
	for _, o := range waiting {
		// A frame drawn before may have closed the window.
		if windows[o.hwnd] != o || !o.needPaint {
			continue
		}
		win.ValidateRect(o.hwnd)
		o.renderFrame()
	}
}

// maxTimersPerPaint bounds the timer messages dispatchDueTimers takes for one
// window, so a timer shorter than a frame cannot hold the paint handler.
const maxTimersPerPaint = 8

// dispatchDueTimers runs the due timers of every Fairy window and its child
// windows. GetMessage generates WM_TIMER only while no WM_PAINT waits, so a
// window that animates without pause would keep the timers of the others,
// such as a spinner's next frame or the caret blink, from firing. A
// PeekMessage filtered to WM_TIMER returns them regardless.
func dispatchDueTimers() {
	var m win.MSG
	for hwnd := range windows {
		for range maxTimersPerPaint {
			// A timer handler may have closed the window.
			if windows[hwnd] == nil || !win.PeekMessage(&m, hwnd, win.WM_TIMER, win.WM_TIMER, win.PM_REMOVE) {
				break
			}
			if m.Hwnd == hwnd && m.WParam == timerTouch {
				// A long press runs the app's click handlers, which must not
				// run inside a paint handler, so the main loop takes it. The
				// filter would return the posted copy again.
				win.PostMessage(m.Hwnd, m.Message, m.WParam, m.LParam)
				break
			}
			win.DispatchMessage(&m)
		}
	}
}

// requestPaint asks for the next frame: a WM_PAINT, or a scheduled frame for
// a headless window.
func (w *Window) requestPaint() {
	switch {
	case w.hl != nil:
		w.scheduleFrame()
	case w.hwnd != 0:
		// InvalidateRect with no window repaints every window on the desktop.
		win.InvalidateRect(w.hwnd, false)
	}
}

// keyMods returns the modifier keys held down now.
func (w *Window) keyMods() keyboard.Mods {
	var m keyboard.Mods
	if win.GetKeyState(win.VK_SHIFT) < 0 {
		m |= keyboard.ModShift
	}
	if win.GetKeyState(win.VK_CONTROL) < 0 {
		m |= keyboard.ModCtrl
	}
	if win.GetKeyState(win.VK_MENU) < 0 {
		m |= keyboard.ModAlt
	}
	if win.GetKeyState(win.VK_LWIN) < 0 || win.GetKeyState(win.VK_RWIN) < 0 {
		m |= keyboard.ModSuper
	}
	return m
}

// setTimer starts or resets the window timer id, which calls onTimer every
// ms milliseconds until killTimer.
func (w *Window) setTimer(id uintptr, ms uint32) { win.SetTimer(w.hwnd, id, ms) }

// killTimer stops the window timer id.
func (w *Window) killTimer(id uintptr) { win.KillTimer(w.hwnd, id) }

// setDarkFrame switches the system title bar and frame between light and dark.
func (w *Window) setDarkFrame(dark bool) {
	v := int32(0)
	if dark {
		v = 1
	}
	win.DwmSetWindowAttribute(w.hwnd, win.DWMWA_USE_IMMERSIVE_DARK_MODE, unsafe.Pointer(&v), 4)
}

var (
	wndProcPtr = syscall.NewCallback(wndProc)
	windows    = map[win.HWND]*Window{}
	classOnce  sync.Once
	classErr   error
	className  = syscall.StringToUTF16Ptr("NahidaFairyWindow")
)

func registerClass() error {
	classOnce.Do(func() {
		win.SetProcessDpiAwarenessContext(win.DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2)
		wc := win.WNDCLASSEXW{
			Size:      uint32(unsafe.Sizeof(win.WNDCLASSEXW{})),
			Style:     win.CS_HREDRAW | win.CS_VREDRAW,
			WndProc:   wndProcPtr,
			Instance:  win.GetModuleHandle(),
			Cursor:    win.LoadCursor(win.IDC_ARROW),
			ClassName: className,
		}
		_, classErr = win.RegisterClassEx(&wc)
	})
	return classErr
}

func init() { loadReducedMotion() }

// loadReducedMotion reads the "Animation effects" accessibility setting.
func loadReducedMotion() {
	on := int32(1)
	if win.SystemParametersInfo(win.SPI_GETCLIENTAREAANIMATION, 0, unsafe.Pointer(&on)) {
		reducedMotion.Store(on == 0)
	}
}

// NewWindow creates a window. Call it on the UI thread: inside Main's start
// function, a handler, or Do.
func NewWindow(opts Options) (*Window, error) {
	w, err := newWindowState(opts)
	if err != nil {
		return nil, err
	}
	opts = w.opts
	if err := registerClass(); err != nil {
		return nil, fmt.Errorf("fairy: register class: %w", err)
	}
	dpi := win.GetDpiForSystem()
	w.scale = float32(dpi) / 96
	if opts.Headless {
		if err := w.initHeadless(); err != nil {
			return nil, err
		}
		return w, nil
	}
	rc := w.outerRect(int32(opts.Width*w.scale), int32(opts.Height*w.scale), dpi)
	title, _ := syscall.UTF16PtrFromString(opts.Title)
	hwnd, err := win.CreateWindowEx(
		win.WS_EX_APPWINDOW|win.WS_EX_NOREDIRECTIONBITMAP,
		className,
		title,
		windowStyle,
		win.CW_USEDEFAULT,
		win.CW_USEDEFAULT,
		rc.Right-rc.Left,
		rc.Bottom-rc.Top,
		0,
		0,
		win.GetModuleHandle(),
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("fairy: create window: %w", err)
	}
	w.hwnd = hwnd
	windows[hwnd] = w

	if d := win.GetDpiForWindow(hwnd); float32(d)/96 != w.scale {
		w.scale = float32(d) / 96
		wr := win.GetWindowRect(hwnd)
		rc := w.outerRect(int32(opts.Width*w.scale), int32(opts.Height*w.scale), d)
		win.SetWindowPos(
			hwnd,
			0,
			wr.Left,
			wr.Top,
			rc.Right-rc.Left,
			rc.Bottom-rc.Top,
			win.SWP_NOZORDER|win.SWP_NOACTIVATE,
		)
	}

	if opts.Dark {
		v := int32(1)
		win.DwmSetWindowAttribute(hwnd, win.DWMWA_USE_IMMERSIVE_DARK_MODE, unsafe.Pointer(&v), 4)
	}
	if opts.Frameless {
		m := win.MARGINS{Top: 1}
		win.DwmExtendFrameIntoClientArea(hwnd, &m)
		win.SetWindowPos(hwnd, 0, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOZORDER|win.SWP_FRAMECHANGED)
	}
	if icon := win.CreateIconFromPNG(opts.Icon, 0); icon != 0 {
		win.SendMessage(hwnd, 0x0080 /* WM_SETICON */, 1, uintptr(icon))
		win.SendMessage(hwnd, 0x0080, 0, uintptr(icon))
	}

	cr := win.GetClientRect(hwnd)
	w.width, w.height = int(cr.Right-cr.Left), int(cr.Bottom-cr.Top)
	if w.r, err = w.newRenderer(); err != nil {
		win.DestroyWindow(hwnd)
		return nil, fmt.Errorf("fairy: renderer: %w", err)
	}
	if opts.Backdrop != BackdropNone && BackdropSupported(opts.Backdrop) {
		// A failed transparent swap chain leaves the window opaque, as documented.
		_ = w.SetBackdrop(opts.Backdrop)
	}
	if w.ts, err = text.NewSystem(w.r.Atlas); err != nil {
		win.DestroyWindow(hwnd)
		return nil, fmt.Errorf("fairy: text: %w", err)
	}
	win.ImmEnable(hwnd, false)
	w.registerDrop()

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

// WS_CAPTION preserves native minimize/maximize animations; WM_NCCALCSIZE
// removes the visible caption for frameless windows.
const windowStyle = win.WS_OVERLAPPEDWINDOW | win.WS_CLIPCHILDREN

// outerRect returns the window rectangle whose client area is cw x ch device
// pixels. A frameless window has no caption but keeps the invisible left,
// right and bottom resize borders.
func (w *Window) outerRect(cw, ch int32, dpi uint32) win.RECT {
	rc := win.RECT{Right: cw, Bottom: ch}
	win.AdjustWindowRectExForDpi(&rc, windowStyle, false, win.WS_EX_APPWINDOW, dpi)
	if w.opts.Frameless {
		rc.Top = 0
	}
	return rc
}

// Show shows and activates the window.
func (w *Window) Show() {
	if w.hl == nil {
		if win.IsIconic(w.hwnd) {
			win.ShowWindow(w.hwnd, win.SW_RESTORE)
		} else {
			win.ShowWindow(w.hwnd, win.SW_SHOW)
		}
		win.SetForegroundWindow(w.hwnd)
	}
	if !w.ready {
		w.ready = true
		if w.opts.OnReady != nil {
			w.Post(func() { w.opts.OnReady(w) })
		}
	}
}

// Hide hides the window without closing it.
func (w *Window) Hide() { win.ShowWindow(w.hwnd, win.SW_HIDE) }

// Minimize minimizes the window.
func (w *Window) Minimize() { win.ShowWindow(w.hwnd, win.SW_MINIMIZE) }

// ToggleMaximize maximizes or restores the window.
func (w *Window) ToggleMaximize() {
	if win.IsZoomed(w.hwnd) {
		win.ShowWindow(w.hwnd, win.SW_RESTORE)
	} else {
		win.ShowWindow(w.hwnd, win.SW_MAXIMIZE)
	}
}

// IsMaximized reports whether the window is maximized.
func (w *Window) IsMaximized() bool { return win.IsZoomed(w.hwnd) }

// Maximize maximizes the window.
func (w *Window) Maximize() { win.ShowWindow(w.hwnd, win.SW_MAXIMIZE) }

// Restore restores a minimized or maximized window to its normal size.
func (w *Window) Restore() { win.ShowWindow(w.hwnd, win.SW_RESTORE) }

// IsMinimized reports whether the window is minimized.
func (w *Window) IsMinimized() bool { return win.IsIconic(w.hwnd) }

// IsVisible reports whether the window is shown (it may be minimized).
func (w *Window) IsVisible() bool { return win.IsWindowVisible(w.hwnd) }

// Bounds returns the window's normal (restored) rectangle in workspace
// coordinates, also while it is minimized or maximized. Save it to reopen the window in place.
// Like the rectangle of a framed window, it includes the invisible resize
// borders, which a frameless window also keeps on its left, right and bottom.
func (w *Window) Bounds() Bounds {
	r := win.GetWindowPlacement(w.hwnd).NormalPosition
	return Bounds{int(r.Left), int(r.Top), int(r.Right - r.Left), int(r.Bottom - r.Top)}
}

// SetBounds moves and sizes the window's normal rectangle in workspace coordinates.
// A maximized window keeps its state and restores to b.
func (w *Window) SetBounds(b Bounds) {
	p := win.GetWindowPlacement(w.hwnd)
	p.NormalPosition = win.RECT{Left: int32(b.X), Top: int32(b.Y), Right: int32(b.X + b.W), Bottom: int32(b.Y + b.H)}
	p.Flags = 0
	if !win.IsWindowVisible(w.hwnd) {
		p.ShowCmd = win.SW_HIDE
	}
	win.SetWindowPlacement(w.hwnd, &p)
}

// Close asks the window to close, running Options.OnClose.
func (w *Window) Close() { win.PostMessage(w.hwnd, win.WM_CLOSE, 0, 0) }

// SetTitle changes the window title.
func (w *Window) SetTitle(s string) { win.SetWindowText(w.hwnd, s) }

// HWND returns the native window handle, or 0 once the window is closed.
func (w *Window) HWND() uintptr {
	if w.closed.Load() {
		return 0
	}
	return uintptr(w.hwnd)
}

func (w *Window) clientPoint(lp uintptr) (float32, float32) {
	x, y := win.PointFromLParam(lp)
	return float32(x) / w.scale, float32(y) / w.scale
}

func (w *Window) screenToLogical(lp uintptr) (float32, float32) {
	x, y := win.PointFromLParam(lp)
	p := win.POINT{X: x, Y: y}
	win.ScreenToClient(w.hwnd, &p)
	return float32(p.X) / w.scale, float32(p.Y) / w.scale
}

func wndProc(hwnd win.HWND, msg uint32, wp, lp uintptr) uintptr {
	w := windows[hwnd]
	if w == nil {
		return win.DefWindowProc(hwnd, msg, wp, lp)
	}
	if frameprof.Slow > 0 && msg != win.WM_PAINT {
		defer frameprof.LogSlow(fmt.Sprintf("message 0x%04x", msg), time.Now())
	}
	return w.handle(msg, wp, lp)
}

func (w *Window) handle(msg uint32, wp, lp uintptr) uintptr {
	// Touch arrives as WM_POINTER*; drop any mouse message Windows still
	// synthesizes from it so a tap is not handled twice. While a touch drags,
	// the mouse is ignored too: the drag shares the mouse press, and the
	// cursor's position would make it jump.
	if msg >= win.WM_MOUSEMOVE && msg <= win.WM_MBUTTONUP &&
		(w.touch != nil && w.touch.Phase == gesture.Pressed || win.IsMouseFromTouch()) {
		return 0
	}
	switch msg {
	case win.WM_PAINT:
		// Validate first: a frame that keeps animating invalidates again.
		win.ValidateRect(w.hwnd)
		w.renderFrame()
		paintWaiting(w)
		dispatchDueTimers()
		return 0
	case win.WM_ERASEBKGND:
		return 1
	case win.WM_GETOBJECT:
		if int32(lp) == uia.RootObjectID {
			return w.a11yGetObject(wp, lp)
		}
	case win.WM_ENTERSIZEMOVE:
		w.sizeMove = true
	case win.WM_EXITSIZEMOVE:
		w.sizeMove = false
		w.killTimer(timerAnim)
		if w.animating || !w.wakeAt.IsZero() {
			w.invalidate()
		}
	case win.WM_MOVE:
		w.notifyNativesMoved()
	case win.WM_SIZE:
		if w.hl != nil {
			// Only SetHeadlessSize sizes a headless window.
			return 0
		}
		if wp == win.SIZE_MINIMIZED {
			// Pixels stay in memory, so the textures upload again on restore.
			w.releaseTextures()
			if w.r != nil {
				w.r.Trim()
			}
			return 0
		}
		w.onResize(int(win.LOWORD(lp)), int(win.HIWORD(lp)))
		return 0
	case win.WM_DPICHANGED:
		w.scale = float32(win.HIWORD(wp)) / 96
		InvalidateMemos()
		rc := (*win.RECT)(unsafe.Pointer(lp))
		win.SetWindowPos(
			w.hwnd,
			0,
			rc.Left,
			rc.Top,
			rc.Right-rc.Left,
			rc.Bottom-rc.Top,
			win.SWP_NOZORDER|win.SWP_NOACTIVATE,
		)
		w.markDirty(frameprof.CauseResize)
		w.redrawWhole()
		w.invalidate()
		return 0
	case win.WM_DISPLAYCHANGE:
		w.redrawWhole()
		w.invalidate()
	case win.WM_GETMINMAXINFO:
		mmi := (*win.MINMAXINFO)(unsafe.Pointer(lp))
		if w.opts.MinWidth > 0 || w.opts.MinHeight > 0 {
			rc := w.outerRect(int32(w.opts.MinWidth*w.scale), int32(w.opts.MinHeight*w.scale), uint32(w.scale*96))
			mmi.MinTrackSize = win.POINT{X: rc.Right - rc.Left, Y: rc.Bottom - rc.Top}
		}
		return 0
	case win.WM_NCCALCSIZE:
		if w.opts.Frameless && wp != 0 {
			// Keep the default left, right and bottom borders, which DWM draws
			// as invisible resize bands outside the visible window, and drop
			// only the caption. A maximized window hangs over the monitor edge
			// by the frame thickness, so its top is moved back on screen.
			p := (*win.NCCALCSIZE_PARAMS)(unsafe.Pointer(lp))
			top := p.Rgrc[0].Top
			r := win.DefWindowProc(w.hwnd, msg, wp, lp)
			p.Rgrc[0].Top = top
			if win.IsZoomed(w.hwnd) {
				dpi := uint32(w.scale * 96)
				p.Rgrc[0].Top += win.GetSystemMetricsForDpi(
					win.SM_CYFRAME,
					dpi,
				) + win.GetSystemMetricsForDpi(
					win.SM_CXPADDEDBORDER,
					dpi,
				)
			}
			return r
		}
	case win.WM_NCHITTEST:
		if w.opts.Frameless {
			return w.ncHitTest(lp)
		}
	case win.WM_SETCURSOR:
		if win.LOWORD(lp) == win.HTCLIENT {
			w.applyCursor()
			return 1
		}
	case win.WM_NCMOUSEMOVE:
		if wp == win.HTMAXBUTTON && w.opts.Frameless {
			if !w.ncTracking {
				t := win.TRACKMOUSEEVENT{
					Size:  uint32(unsafe.Sizeof(win.TRACKMOUSEEVENT{})),
					Flags: win.TME_LEAVE | win.TME_NONCLIENT,
					Hwnd:  w.hwnd,
				}
				win.TrackMouseEvent(&t)
				w.ncTracking = true
			}
			if !w.scripted {
				w.ncHover = true
				w.onMouseMove(w.screenToLogical(lp))
			}
			// DefWindowProc starts the hover that opens the Windows 11 snap
			// layout flyout, so the message still goes on to it.
		}
	case win.WM_NCMOUSELEAVE:
		w.ncTracking = false
		if w.ncHover && !w.scripted {
			w.ncHover = false
			p := win.GetCursorPos()
			win.ScreenToClient(w.hwnd, &p)
			if p.X < 0 || p.Y < 0 || p.X >= int32(w.width) || p.Y >= int32(w.height) {
				w.onMouseLeave()
			}
		}
	case win.WM_NCLBUTTONDOWN, win.WM_NCLBUTTONDBLCLK:
		if wp == win.HTMAXBUTTON && w.opts.Frameless {
			x, y := w.screenToLogical(lp)
			w.onButtonDown(x, y, 0)
			return 0
		}
	case win.WM_NCLBUTTONUP:
		if wp == win.HTMAXBUTTON && w.opts.Frameless {
			x, y := w.screenToLogical(lp)
			w.onButtonUp(x, y, 0)
			return 0
		}
	case win.WM_NCRBUTTONUP:
		if wp == win.HTCAPTION && w.opts.Frameless {
			w.showSystemMenu(win.PointFromLParam(lp))
			return 0
		}
	case win.WM_MOUSEMOVE:
		w.ncHover = false
		if !w.scripted {
			w.onMouseMove(w.clientPoint(lp))
		}
		return 0
	case win.WM_MOUSELEAVE:
		w.tracking = false
		if !w.scripted {
			w.onMouseLeave()
		}
		return 0
	case win.WM_LBUTTONDOWN, win.WM_LBUTTONDBLCLK:
		x, y := w.clientPoint(lp)
		w.onButtonDown(x, y, 0)
		return 0
	case win.WM_RBUTTONDOWN:
		x, y := w.clientPoint(lp)
		w.onButtonDown(x, y, 1)
		return 0
	case win.WM_MBUTTONDOWN:
		x, y := w.clientPoint(lp)
		w.onButtonDown(x, y, 2)
		return 0
	case win.WM_LBUTTONUP:
		x, y := w.clientPoint(lp)
		w.onButtonUp(x, y, 0)
		return 0
	case win.WM_RBUTTONUP:
		x, y := w.clientPoint(lp)
		w.onButtonUp(x, y, 1)
		return 0
	case win.WM_MBUTTONUP:
		x, y := w.clientPoint(lp)
		w.onButtonUp(x, y, 2)
		return 0
	case win.WM_CAPTURECHANGED:
		if win.HWND(lp) != w.hwnd {
			if w.press != nil {
				w.cancelPress()
			}
			if a := w.autoScroll; a != nil && a.held {
				a.held = false
				w.stopAutoScroll()
			}
		}
		return 0
	case win.WM_POINTERDOWN, win.WM_POINTERUPDATE, win.WM_POINTERUP:
		id := uint32(win.LOWORD(wp))
		if t, ok := win.GetPointerType(id); !ok || t != win.PT_TOUCH {
			break
		}
		if w.scripted {
			return 0
		}
		flags := win.HIWORD(wp)
		x, y := w.screenToLogical(lp)
		// The fling velocity needs when the input happened, not when the
		// message got here, which can be a frame later.
		at := time.Now()
		if age, ok := win.PointerAge(id); ok {
			at = at.Add(-age)
		}
		switch {
		case flags&win.POINTER_MESSAGE_FLAG_CANCELED != 0:
			w.onTouchCancel(id)
		case msg == win.WM_POINTERDOWN:
			if flags&win.POINTER_MESSAGE_FLAG_PRIMARY != 0 {
				w.onTouchDown(id, x, y, at)
			}
		case msg == win.WM_POINTERUPDATE:
			w.onTouchMove(id, x, y, at)
		default:
			w.onTouchUp(id, x, y, at)
		}
		return 0
	case win.WM_POINTERCAPTURECHANGED:
		w.onTouchCancel(uint32(win.LOWORD(wp)))
		return 0
	case win.WM_MOUSEWHEEL, win.WM_MOUSEHWHEEL:
		if w.scripted {
			return 0
		}
		x, y := w.screenToLogical(lp)
		raw := int16(win.HIWORD(wp))
		w.wheel(x, y, float32(raw)/win.WHEEL_DELTA, raw%win.WHEEL_DELTA != 0, msg == win.WM_MOUSEHWHEEL)
		return 0
	case win.WM_XBUTTONDOWN:
		return 1
	case win.WM_XBUTTONUP:
		switch win.HIWORD(wp) {
		case 1:
			w.onKeyDown(keyboard.KeyBrowserBack, false, "")
		case 2:
			w.onKeyDown(keyboard.KeyBrowserForward, false, "")
		}
		return 1
	case win.WM_KEYDOWN, win.WM_SYSKEYDOWN:
		w.wantChar = false
		if w.onKeyDown(keyboard.Key(wp), lp&(1<<30) != 0, w.peekTypedText()) {
			if !w.wantChar {
				w.dropTranslatedChars()
			}
			return 0
		}
		if msg == win.WM_SYSKEYDOWN {
			break
		}
		return 0
	case win.WM_CHAR:
		w.onChar(uint16(wp))
		return 0
	case win.WM_SYSCHAR:
		// Swallow Alt+key beeps when nothing uses them; keep Alt+Space for the system menu.
		if wp != ' ' {
			return 0
		}
	case win.WM_IME_SETCONTEXT:
		return win.DefWindowProc(w.hwnd, msg, wp, lp&^win.ISC_SHOWUICOMPOSITIONWINDOW)
	case win.WM_IME_STARTCOMPOSITION:
		w.updateIMEPosition()
		return 0
	case win.WM_IME_COMPOSITION:
		w.onIMEComposition(lp)
		return 0
	case win.WM_IME_ENDCOMPOSITION:
		w.onIMEEnd()
		return 0
	case win.WM_SETFOCUS:
		w.onFocusChange(true)
		return 0
	case win.WM_KILLFOCUS:
		w.onFocusChange(false)
		return 0
	case win.WM_SETTINGCHANGE:
		if wp == win.SPI_SETCLIENTAREAANIMATION {
			loadReducedMotion()
			InvalidateMemos()
			// Running layout and scroll animations finish on the next frame,
			// and views that check ReducedMotion are built again.
			w.Update()
		}
		if isColorSetChange(lp) {
			loadSystemDark()
			InvalidateMemos()
			w.Update()
		}
	case win.WM_TIMER:
		w.onTimer(wp)
		return 0
	case msgWake:
		w.runPosted()
		return 0
	case msgPaint:
		if w.needPaint {
			w.renderFrame()
		}
		return 0
	case msgInvalidate:
		w.invalidate()
		return 0
	case msgInput:
		w.runInjected()
		return 0
	case win.WM_CLOSE:
		if w.opts.OnClose != nil && !w.opts.OnClose() {
			return 0
		}
		win.DestroyWindow(w.hwnd)
		return 0
	case win.WM_DESTROY:
		w.destroy()
		quitIfIdle()
		return 0
	}
	return win.DefWindowProc(w.hwnd, msg, wp, lp)
}

// peekTypedText returns the character TranslateMessage queued for the key
// being handled, leaving it queued. A character outside the Basic
// Multilingual Plane arrives as two messages and is not reported.
func (w *Window) peekTypedText() string {
	var m win.MSG
	if !win.PeekMessage(&m, w.hwnd, win.WM_CHAR, win.WM_CHAR, win.PM_NOREMOVE) {
		return ""
	}
	r := rune(m.WParam)
	if r < 0x20 || r == 0x7f || utf16.IsSurrogate(r) {
		return ""
	}
	return string(r)
}

// dropTranslatedChars removes the characters that TranslateMessage queued for
// the key being handled. The message loop translates a key before dispatching
// it, so a key that a handler used would otherwise also arrive as text, for
// example in an input that the handler just focused.
func (w *Window) dropTranslatedChars() {
	var m win.MSG
	for win.PeekMessage(&m, w.hwnd, win.WM_CHAR, win.WM_DEADCHAR, win.PM_REMOVE) {
	}
	for win.PeekMessage(&m, w.hwnd, win.WM_SYSCHAR, win.WM_SYSDEADCHAR, win.PM_REMOVE) {
	}
}

func (w *Window) destroy() {
	w.a11yShutdown()
	w.revokeDrop()
	w.closeNatives()
	w.release()
	delete(windows, w.hwnd)
}

func (w *Window) ncHitTest(lp uintptr) uintptr {
	sx, sy := win.PointFromLParam(lp)
	p := win.POINT{X: sx, Y: sy}
	win.ScreenToClient(w.hwnd, &p)
	if !win.IsZoomed(w.hwnd) {
		// Only the top resize band lies inside the client area. The left,
		// right and bottom bands are the invisible borders outside it that
		// WM_NCCALCSIZE keeps.
		dpi := uint32(w.scale * 96)
		pad := win.GetSystemMetricsForDpi(win.SM_CXPADDEDBORDER, dpi)
		bx := win.GetSystemMetricsForDpi(win.SM_CXFRAME, dpi) + pad
		by := win.GetSystemMetricsForDpi(win.SM_CYFRAME, dpi) + pad
		cw, ch := int32(w.width), int32(w.height)
		nearLeft, nearRight := p.X < bx, p.X >= cw-bx
		nearTop, nearBottom := p.Y < by, p.Y >= ch-by
		switch {
		case p.Y >= ch && nearLeft, p.X < 0 && nearBottom:
			return win.HTBOTTOMLEFT
		case p.Y >= ch && nearRight, p.X >= cw && nearBottom:
			return win.HTBOTTOMRIGHT
		case p.Y >= ch:
			return win.HTBOTTOM
		case nearTop && nearLeft:
			return win.HTTOPLEFT
		case nearTop && nearRight:
			return win.HTTOPRIGHT
		case nearTop:
			return win.HTTOP
		case p.X < 0:
			return win.HTLEFT
		case p.X >= cw:
			return win.HTRIGHT
		}
	} else if p.X < 0 || p.Y < 0 || p.X >= int32(w.width) || p.Y >= int32(w.height) {
		// A maximized window's frame overhangs the monitor, possibly onto
		// another one; that strip belongs to nothing.
		return win.HTNOWHERE
	}
	x, y := float32(p.X)/w.scale, float32(p.Y)/w.scale
	return w.captionHitTest(x, y)
}

func (w *Window) captionHitTest(x, y float32) uintptr {
	for e := w.pointerHitTest(x, y); e != nil; e = e.parent {
		if e.maxButton {
			return win.HTMAXBUTTON
		}
		if e.interactive() {
			return win.HTCLIENT
		}
		if e.dragRegion {
			return win.HTCAPTION
		}
	}
	return win.HTCLIENT
}

// showSystemMenu opens the window menu at screen point (x, y). DefWindowProc
// only opens it over its own caption, which a frameless window has removed,
// and leaves the item states to be set here.
func (w *Window) showSystemMenu(x, y int32) {
	m := win.GetSystemMenu(w.hwnd)
	if m == 0 {
		return
	}
	maximized := win.IsZoomed(w.hwnd)
	win.EnableMenuItem(m, win.SC_RESTORE, maximized)
	win.EnableMenuItem(m, win.SC_MOVE, !maximized)
	win.EnableMenuItem(m, win.SC_SIZE, !maximized)
	win.EnableMenuItem(m, win.SC_MINIMIZE, true)
	win.EnableMenuItem(m, win.SC_MAXIMIZE, !maximized)
	win.EnableMenuItem(m, win.SC_CLOSE, true)
	if maximized {
		win.SetMenuDefaultItem(m, win.SC_RESTORE)
	} else {
		win.SetMenuDefaultItem(m, win.SC_MAXIMIZE)
	}
	if cmd := win.TrackPopupMenu(m, win.TPM_RETURNCMD|win.TPM_RIGHTBUTTON, x, y, w.hwnd); cmd != 0 {
		win.PostMessage(w.hwnd, win.WM_SYSCOMMAND, cmd, 0)
	}
}

func (w *Window) applyCursor() {
	if w.hl != nil {
		w.reportCursor()
		return
	}
	if w.autoScroll != nil {
		w.setPanCursor()
		return
	}
	id := uintptr(win.IDC_ARROW)
	switch w.wantedCursor() {
	case CursorPointer:
		id = win.IDC_HAND
	case CursorText:
		id = win.IDC_IBEAM
	case CursorMove:
		id = win.IDC_SIZEALL
	case CursorNotAllowed:
		id = win.IDC_NO
	case CursorResizeEW:
		id = win.IDC_SIZEWE
	case CursorResizeNS:
		id = win.IDC_SIZENS
	case CursorWait:
		id = win.IDC_WAIT
	case CursorCrosshair:
		id = win.IDC_CROSS
	}
	win.SetCursor(win.LoadCursor(id))
}

// wantedCursor is the pointer shape for the element under the pointer or the
// press in progress. Autoscroll, which draws its own shapes, asks for Move.
func (w *Window) wantedCursor() Cursor {
	if w.autoScroll != nil {
		return CursorMove
	}
	c := CursorDefault
	for _, e := range w.hoverPath {
		if e.links && w.linkAt(e, w.mouseX, w.mouseY) != nil {
			c = CursorPointer
			break
		}
		if e.cursor != CursorInherit {
			c = e.cursor
			break
		}
		if e.kind == kindText && e.text != "" && selectableScope(e) != nil {
			c = CursorText
			break
		}
	}
	if w.press != nil && w.press.cursor != CursorInherit {
		c = w.press.cursor
	}
	return c
}

// trackMouseLeave arms the WM_MOUSELEAVE that reports the pointer leaving
// the window.
func (w *Window) trackMouseLeave() {
	if w.tracking || w.ncHover {
		return
	}
	t := win.TRACKMOUSEEVENT{Size: uint32(unsafe.Sizeof(win.TRACKMOUSEEVENT{})), Flags: win.TME_LEAVE, Hwnd: w.hwnd}
	win.TrackMouseEvent(&t)
	w.tracking = true
}

// ignoreMouseLeave stops arming trackMouseLeave, for windows the pointer is
// never over: scripted and headless ones.
func (w *Window) ignoreMouseLeave() { w.tracking = true }

// rearmMouseLeave makes the next move arm trackMouseLeave again.
func (w *Window) rearmMouseLeave() { w.tracking = false }

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
	win.SetCursor(pancursor.Handle(a.cursor, size))
}

// redrawAsync repaints the window without rebuilding it. It is safe from
// any goroutine.
func (w *Window) redrawAsync() {
	switch h := w.hwnd; {
	case h != 0 && w.opts.Headless:
		// A message-only window gets no WM_PAINT; the window paces
		// its own frames on the UI thread.
		win.PostMessage(h, msgInvalidate, 0, 0)
	case h != 0:
		win.InvalidateRect(h, false)
	}
}

// resizeClient resizes the window so its client area is width x height
// logical pixels, keeping its position.
func (w *Window) resizeClient(width, height float32) {
	r := win.GetWindowRect(w.hwnd)
	cw, ch := int32(width*w.scale), int32(height*w.scale)
	if w.opts.Frameless {
		// The visible frameless window is its client area; the invisible
		// resize borders are not part of the requested size.
		rc := w.outerRect(cw, ch, uint32(w.scale*96))
		cw, ch = rc.Right-rc.Left, rc.Bottom-rc.Top
	}
	win.SetWindowPos(w.hwnd, 0, r.Left, r.Top, cw, ch, win.SWP_NOZORDER|win.SWP_NOACTIVATE)
}

func (w *Window) newRenderer() (r *render.Renderer, err error) {
	defer func() {
		if err != nil {
			return
		}
		if frameprof.GPU {
			r.SetGPUTiming(true)
		}
		frameprof.SetDamageTracking(r)
	}()
	if w.hl != nil {
		return render.NewOffscreenFormat(w.width, w.height, d3d.FORMAT_R8G8B8A8_UNORM)
	}
	composition := w.opts.Transparent || w.opts.DynamicBackdrop || w.backdrop != BackdropNone
	r, err = render.New(uintptr(w.hwnd), w.width, w.height, composition)
	if err == nil || !composition {
		return r, err
	}

	log.Printf("fairy: window transparency and backdrop off: %v", err)
	w.opts.DynamicBackdrop = false
	if w.backdrop != BackdropNone {
		w.applyBackdrop(BackdropNone)
		w.backdrop = BackdropNone
	}
	return render.New(uintptr(w.hwnd), w.width, w.height, false)
}

// onChar handles a WM_CHAR, joining the halves of a surrogate pair.
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
	w.onTypedRune(r)
}
