package fairy

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/render"
	"github.com/myparsleycat/fairy/internal/text"
	"github.com/myparsleycat/fairy/internal/uia"
	"github.com/myparsleycat/fairy/internal/win"
	"github.com/myparsleycat/fairy/keyboard"
)

const msgWake = win.WM_APP + 1

// windowPlat is the Win32 part of a Window.
type windowPlat struct {
	hwnd       win.HWND
	tracking   bool   // TrackMouseEvent is armed for WM_MOUSELEAVE
	charHigh   uint16 // high surrogate of a WM_CHAR pair awaiting its low half
	ncTracking bool   // TrackMouseEvent is armed for the non-client maximize button
	ncHover    bool   // the pointer is over a MaximizeButton, which Windows treats as non-client
}

// hasNativeWindow reports whether the window has a native window, which
// offscreen windows and closed ones do not.
func (w *Window) hasNativeWindow() bool { return w.hwnd != 0 }

// wake asks the message loop to run the posted calls. It reports false when
// the window is gone.
func (w *Window) wake() bool { return win.PostMessage(w.hwnd, msgWake, 0, 0) }

// requestPaint asks for a WM_PAINT.
func (w *Window) requestPaint() {
	// InvalidateRect with no window repaints every window on the desktop.
	if w.hwnd != 0 {
		win.InvalidateRect(w.hwnd, false)
	}
}

// systemClipboardText returns the clipboard's text, and false when it holds
// none or could not be opened.
func (w *Window) systemClipboardText() (string, bool) { return win.ClipboardText(w.hwnd) }

func (w *Window) systemClipboardHasText() bool { return win.ClipboardHasText() }

// popupMenu shows items as a native menu at window point (x, y) and waits
// until it closes. It reports false when there is no window to show it in.
func (w *Window) popupMenu(x, y float32, items []MenuItem) bool {
	if w.hwnd == 0 {
		return false
	}
	p := win.POINT{X: int32(x * w.scale), Y: int32(y * w.scale)}
	win.ClientToScreen(w.hwnd, &p)
	showMenu(w.hwnd, p.X, p.Y, items)
	return true
}

func (w *Window) setSystemClipboardText(s string) bool { return win.SetClipboardText(w.hwnd, s) }

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

func (w *Window) setTimer(id uintptr, ms uint32) { win.SetTimer(w.hwnd, id, ms) }
func (w *Window) killTimer(id uintptr)           { win.KillTimer(w.hwnd, id) }

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
	opts.DynamicBackdrop = opts.DynamicBackdrop && BackdropSupported(BackdropMica)
	if opts.View == nil {
		return nil, errors.New("fairy: Options.View is required")
	}
	if !onUIThread() {
		return nil, errors.New("fairy: NewWindow must run on the UI thread (inside Main, a handler or Do)")
	}
	if err := registerClass(); err != nil {
		return nil, fmt.Errorf("fairy: register class: %w", err)
	}
	if opts.Width <= 0 {
		opts.Width = 960
	}
	if opts.Height <= 0 {
		opts.Height = 640
	}
	if opts.FontSize <= 0 {
		opts.FontSize = 14
	}
	if opts.Font == "" {
		opts.Font = "Segoe UI"
	}
	if !opts.Background.isSet() {
		if opts.Dark {
			opts.Background = Hex(0x1e1e1e)
		} else {
			opts.Background = Hex(0xffffff)
		}
	}
	if !opts.TextColor.isSet() {
		if opts.Dark {
			opts.TextColor = Hex(0xe6e6e6)
		} else {
			opts.TextColor = Hex(0x1a1a1a)
		}
	}

	w := &Window{
		opts:      opts,
		states:    map[uint64]*elState{},
		hoverIDs:  map[uint64]bool{},
		textures:  map[uint64]*texEntry{},
		surfaces:  map[uint64]*surfaceEntry{},
		webviews:  map[*WebView]bool{},
		iconTiles: map[iconKey]render.Tile{},
		byID:      map[uint64]*El{},
		prevByID:  map[uint64]*El{},
		locals:    map[string]*localEntry{},
		dirty:     true,
	}
	dpi := win.GetDpiForSystem()
	w.scale = float32(dpi) / 96
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
	if win.IsIconic(w.hwnd) {
		win.ShowWindow(w.hwnd, win.SW_RESTORE)
	} else {
		win.ShowWindow(w.hwnd, win.SW_SHOW)
	}
	win.SetForegroundWindow(w.hwnd)
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

// SetColors switches the window between light and dark as Options.Dark,
// Background and TextColor do at creation: the system frame and title bar,
// tooltips, the color behind the view and the default text color. A zero
// color keeps the current one. Call it on the UI thread.
func (w *Window) SetColors(dark bool, background, text Color) {
	if dark != w.opts.Dark {
		w.opts.Dark = dark
		v := int32(0)
		if dark {
			v = 1
		}
		win.DwmSetWindowAttribute(w.hwnd, win.DWMWA_USE_IMMERSIVE_DARK_MODE, unsafe.Pointer(&v), 4)
	}
	if background.isSet() {
		w.opts.Background = background
	}
	if text.isSet() {
		w.opts.TextColor = text
	}
	w.Update()
}

// HWND returns the native window handle.
func (w *Window) HWND() uintptr { return uintptr(w.hwnd) }

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
	if debugSlow > 0 && msg != win.WM_PAINT {
		defer slow(fmt.Sprintf("message 0x%04x", msg), time.Now())
	}
	return w.handle(msg, wp, lp)
}

func (w *Window) handle(msg uint32, wp, lp uintptr) uintptr {
	// Touch arrives as WM_POINTER*; drop any mouse message Windows still
	// synthesizes from it so a tap is not handled twice. While a touch drags,
	// the mouse is ignored too: the drag shares the mouse press, and the
	// cursor's position would make it jump.
	if msg >= win.WM_MOUSEMOVE && msg <= win.WM_MBUTTONUP &&
		(w.touch != nil && w.touch.phase == touchPressed || win.IsMouseFromTouch()) {
		return 0
	}
	switch msg {
	case win.WM_PAINT:
		// Validate first: a frame that keeps animating invalidates again.
		win.ValidateRect(w.hwnd)
		w.renderFrame()
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
		w.notifyWebViewsMoved()
	case win.WM_SIZE:
		if wp == win.SIZE_MINIMIZED {
			// Pixels stay in memory, so the textures upload again on restore.
			w.releaseTextures()
			return 0
		}
		nw, nh := int(win.LOWORD(lp)), int(win.HIWORD(lp))
		if nw != w.width || nh != w.height {
			w.width, w.height = nw, nh
			if w.r != nil {
				w.resizeRenderer()
			}
			w.markDirty(dirtyResize)
			// Draw now so the modal resize loop shows the new size immediately.
			w.renderFrame()
		}
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
		w.markDirty(dirtyResize)
		w.invalidate()
		return 0
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
		delta := float32(raw) / win.WHEEL_DELTA
		hwheel := msg == win.WM_MOUSEHWHEEL
		if hwheel {
			delta = -delta
		}
		// Precision touchpads and high-resolution wheels send fractions of a
		// notch. A whole notch in the middle of such a stream is treated alike.
		now := time.Now()
		if raw%win.WHEEL_DELTA != 0 {
			w.preciseWheelAt = now
		}
		precise := now.Sub(w.preciseWheelAt) < 100*time.Millisecond
		w.onWheel(x, y, delta, hwheel, precise)
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
		if w.opts.OnFocus != nil {
			w.opts.OnFocus(true)
			w.Update()
		}
		w.osFocus = true
		w.resetBlink()
		w.invalidate()
		return 0
	case win.WM_KILLFOCUS:
		if w.opts.OnFocus != nil {
			w.opts.OnFocus(false)
			w.Update()
		}
		w.osFocus = false
		if w.acc != nil {
			w.acc.focus = 0
		}
		w.commitComposition()
		w.cancelPress()
		w.stopAutoScroll()
		w.invalidate()
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
		switch wp {
		case timerCaret:
			w.caretOn = !w.caretOn || w.clock != nil
			w.invalidate()
		case timerTip:
			w.killTimer(timerTip)
			w.showTooltip()
		case timerAnim:
			w.killTimer(timerAnim)
			w.invalidate()
		case timerTouch:
			w.killTimer(timerTouch)
			w.onTouchLongPress()
		case timerRecover:
			w.killTimer(timerRecover)
			if w.r == nil {
				w.recreateRenderer()
			} else {
				w.invalidate() // renderFrame retries a failed resize
			}
		}
		return 0
	case msgWake:
		w.runPosted()
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
	w.closeWebViews()
	liveMu.Lock()
	delete(live, w)
	liveMu.Unlock()
	w.releaseTextures()
	w.textures = nil
	for _, se := range w.surfaces {
		se.target.Release()
	}
	w.surfaces = nil
	if w.ts != nil {
		w.ts.Release()
		w.ts = nil
	}
	if w.r != nil {
		w.r.Release()
		w.r = nil
	}
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

// setCursor shows c as the pointer over the window.
func (w *Window) setCursor(c Cursor) {
	id := uintptr(win.IDC_ARROW)
	switch c {
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

// onChar handles a WM_CHAR, which carries one UTF-16 code unit.
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
