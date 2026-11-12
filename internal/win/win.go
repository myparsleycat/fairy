//go:build windows

// Package win holds the Win32 calls the framework needs, loaded lazily from system DLLs.
package win

import (
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	dwmapi   = windows.NewLazySystemDLL("dwmapi.dll")
	imm32    = windows.NewLazySystemDLL("imm32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	uxtheme  = windows.NewLazySystemDLL("uxtheme.dll")

	pRegisterClassExW              = user32.NewProc("RegisterClassExW")
	pCreateWindowExW               = user32.NewProc("CreateWindowExW")
	pDefWindowProcW                = user32.NewProc("DefWindowProcW")
	pDestroyWindow                 = user32.NewProc("DestroyWindow")
	pShowWindow                    = user32.NewProc("ShowWindow")
	pGetMessageW                   = user32.NewProc("GetMessageW")
	pPeekMessageW                  = user32.NewProc("PeekMessageW")
	pTranslateMessage              = user32.NewProc("TranslateMessage")
	pDispatchMessageW              = user32.NewProc("DispatchMessageW")
	pPostMessageW                  = user32.NewProc("PostMessageW")
	pPostQuitMessage               = user32.NewProc("PostQuitMessage")
	pLoadCursorW                   = user32.NewProc("LoadCursorW")
	pSetCursor                     = user32.NewProc("SetCursor")
	pGetClientRect                 = user32.NewProc("GetClientRect")
	pGetWindowRect                 = user32.NewProc("GetWindowRect")
	pSetWindowPos                  = user32.NewProc("SetWindowPos")
	pAdjustWindowRectExForDpi      = user32.NewProc("AdjustWindowRectExForDpi")
	pGetDpiForWindow               = user32.NewProc("GetDpiForWindow")
	pGetDpiForSystem               = user32.NewProc("GetDpiForSystem")
	pSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	pSetCapture                    = user32.NewProc("SetCapture")
	pReleaseCapture                = user32.NewProc("ReleaseCapture")
	pGetCapture                    = user32.NewProc("GetCapture")
	pTrackMouseEvent               = user32.NewProc("TrackMouseEvent")
	pGetPointerType                = user32.NewProc("GetPointerType")
	pGetPointerInfo                = user32.NewProc("GetPointerInfo")
	pGetMessageExtraInfo           = user32.NewProc("GetMessageExtraInfo")
	pInvalidateRect                = user32.NewProc("InvalidateRect")
	pValidateRect                  = user32.NewProc("ValidateRect")
	pScreenToClient                = user32.NewProc("ScreenToClient")
	pClientToScreen                = user32.NewProc("ClientToScreen")
	pGetKeyState                   = user32.NewProc("GetKeyState")
	pSetWindowTextW                = user32.NewProc("SetWindowTextW")
	pGetDoubleClickTime            = user32.NewProc("GetDoubleClickTime")
	pGetSystemMetricsForDpi        = user32.NewProc("GetSystemMetricsForDpi")
	pMonitorFromWindow             = user32.NewProc("MonitorFromWindow")
	pSetTimer                      = user32.NewProc("SetTimer")
	pKillTimer                     = user32.NewProc("KillTimer")
	pIsZoomed                      = user32.NewProc("IsZoomed")
	pIsIconic                      = user32.NewProc("IsIconic")
	pSetForegroundWindow           = user32.NewProc("SetForegroundWindow")
	pGetCursorPos                  = user32.NewProc("GetCursorPos")
	pOpenClipboard                 = user32.NewProc("OpenClipboard")
	pCloseClipboard                = user32.NewProc("CloseClipboard")
	pEmptyClipboard                = user32.NewProc("EmptyClipboard")
	pGetClipboardData              = user32.NewProc("GetClipboardData")
	pSetClipboardData              = user32.NewProc("SetClipboardData")
	pIsClipboardFormatAvailable    = user32.NewProc("IsClipboardFormatAvailable")
	pMsgWaitForMultipleObjectsEx   = user32.NewProc("MsgWaitForMultipleObjectsEx")
	pCreateIconFromResourceEx      = user32.NewProc("CreateIconFromResourceEx")
	pSendMessageW                  = user32.NewProc("SendMessageW")
	pSendMessageTimeoutW           = user32.NewProc("SendMessageTimeoutW")
	pSystemParametersInfoW         = user32.NewProc("SystemParametersInfoW")
	pRegisterHotKey                = user32.NewProc("RegisterHotKey")
	pUnregisterHotKey              = user32.NewProc("UnregisterHotKey")

	pGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	pGlobalAlloc      = kernel32.NewProc("GlobalAlloc")
	pGlobalFree       = kernel32.NewProc("GlobalFree")
	pGlobalLock       = kernel32.NewProc("GlobalLock")
	pGlobalUnlock     = kernel32.NewProc("GlobalUnlock")

	pDwmExtendFrameIntoClientArea = dwmapi.NewProc("DwmExtendFrameIntoClientArea")
	pDwmSetWindowAttribute        = dwmapi.NewProc("DwmSetWindowAttribute")
	pDwmFlush                     = dwmapi.NewProc("DwmFlush")

	pImmGetContext            = imm32.NewProc("ImmGetContext")
	pImmReleaseContext        = imm32.NewProc("ImmReleaseContext")
	pImmGetCompositionStringW = imm32.NewProc("ImmGetCompositionStringW")
	pImmSetCompositionWindow  = imm32.NewProc("ImmSetCompositionWindow")
	pImmSetCandidateWindow    = imm32.NewProc("ImmSetCandidateWindow")
	pImmNotifyIME             = imm32.NewProc("ImmNotifyIME")

	pGetStockObject = gdi32.NewProc("GetStockObject")
)

type HWND = windows.HWND
type HANDLE = windows.Handle

type POINT struct{ X, Y int32 }

type RECT struct{ Left, Top, Right, Bottom int32 }

type MSG struct {
	Hwnd    HWND
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
	Private uint32
}

type WNDCLASSEXW struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   HANDLE
	Icon       HANDLE
	Cursor     HANDLE
	Background HANDLE
	MenuName   *uint16
	ClassName  *uint16
	IconSm     HANDLE
}

type TRACKMOUSEEVENT struct {
	Size      uint32
	Flags     uint32
	Hwnd      HWND
	HoverTime uint32
}

type MARGINS struct{ Left, Right, Top, Bottom int32 }

type NCCALCSIZE_PARAMS struct {
	Rgrc [3]RECT
	Ppos uintptr
}

type MINMAXINFO struct {
	Reserved     POINT
	MaxSize      POINT
	MaxPosition  POINT
	MinTrackSize POINT
	MaxTrackSize POINT
}

type COMPOSITIONFORM struct {
	Style   uint32
	CurrPos POINT
	Area    RECT
}

type CANDIDATEFORM struct {
	Index   uint32
	Style   uint32
	CurrPos POINT
	Area    RECT
}

func RegisterClassEx(wc *WNDCLASSEXW) (uint16, error) {
	r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(wc)))
	if r == 0 {
		return 0, err
	}
	return uint16(r), nil
}

func CreateWindowEx(exStyle uint32, class, title *uint16, style uint32, x, y, w, h int32, parent HWND, menu HANDLE, inst HANDLE, param uintptr) (HWND, error) {
	r, _, err := pCreateWindowExW.Call(uintptr(exStyle), uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)),
		uintptr(style), uintptr(x), uintptr(y), uintptr(w), uintptr(h), uintptr(parent), uintptr(menu), uintptr(inst), param)
	if r == 0 {
		return 0, err
	}
	return HWND(r), nil
}

func DefWindowProc(h HWND, msg uint32, wp, lp uintptr) uintptr {
	r, _, _ := pDefWindowProcW.Call(uintptr(h), uintptr(msg), wp, lp)
	return r
}

func DestroyWindow(h HWND)         { pDestroyWindow.Call(uintptr(h)) }
func ShowWindow(h HWND, cmd int32) { pShowWindow.Call(uintptr(h), uintptr(cmd)) }

func GetMessage(m *MSG) int32 {
	r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(m)), 0, 0, 0)
	return int32(r)
}

func PeekMessage(m *MSG, h HWND, first, last, remove uint32) bool {
	r, _, _ := pPeekMessageW.Call(uintptr(unsafe.Pointer(m)), uintptr(h), uintptr(first), uintptr(last), uintptr(remove))
	return r != 0
}

func TranslateMessage(m *MSG) { pTranslateMessage.Call(uintptr(unsafe.Pointer(m))) }
func DispatchMessage(m *MSG)  { pDispatchMessageW.Call(uintptr(unsafe.Pointer(m))) }

func PostMessage(h HWND, msg uint32, wp, lp uintptr) bool {
	r, _, _ := pPostMessageW.Call(uintptr(h), uintptr(msg), wp, lp)
	return r != 0
}

func SendMessage(h HWND, msg uint32, wp, lp uintptr) uintptr {
	r, _, _ := pSendMessageW.Call(uintptr(h), uintptr(msg), wp, lp)
	return r
}

func SendMessageTimeout(h HWND, msg uint32, wp uintptr, lp unsafe.Pointer, timeoutMS uint32) (uintptr, bool) {
	const smtoBlock = 0x0001
	var result uintptr
	r, _, _ := pSendMessageTimeoutW.Call(uintptr(h), uintptr(msg), wp, uintptr(lp), smtoBlock, uintptr(timeoutMS), uintptr(unsafe.Pointer(&result)))
	return result, r != 0
}

func PostQuitMessage(code int32) { pPostQuitMessage.Call(uintptr(code)) }

func LoadCursor(id uintptr) HANDLE {
	r, _, _ := pLoadCursorW.Call(0, id)
	return HANDLE(r)
}

func SetCursor(c HANDLE) { pSetCursor.Call(uintptr(c)) }

func GetClientRect(h HWND) RECT {
	var r RECT
	pGetClientRect.Call(uintptr(h), uintptr(unsafe.Pointer(&r)))
	return r
}

func GetWindowRect(h HWND) RECT {
	var r RECT
	pGetWindowRect.Call(uintptr(h), uintptr(unsafe.Pointer(&r)))
	return r
}

func SetWindowPos(h, after HWND, x, y, w, ht int32, flags uint32) {
	pSetWindowPos.Call(uintptr(h), uintptr(after), uintptr(x), uintptr(y), uintptr(w), uintptr(ht), uintptr(flags))
}

func AdjustWindowRectExForDpi(r *RECT, style uint32, menu bool, exStyle uint32, dpi uint32) {
	pAdjustWindowRectExForDpi.Call(uintptr(unsafe.Pointer(r)), uintptr(style), boolPtr(menu), uintptr(exStyle), uintptr(dpi))
}

func GetDpiForWindow(h HWND) uint32 {
	r, _, _ := pGetDpiForWindow.Call(uintptr(h))
	if r == 0 {
		return 96
	}
	return uint32(r)
}

func GetDpiForSystem() uint32 {
	r, _, _ := pGetDpiForSystem.Call()
	if r == 0 {
		return 96
	}
	return uint32(r)
}

// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 is the pseudo-handle -4.
const DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = ^uintptr(3)

func SetProcessDpiAwarenessContext(ctx uintptr) bool {
	if pSetProcessDpiAwarenessContext.Find() != nil {
		return false
	}
	r, _, _ := pSetProcessDpiAwarenessContext.Call(ctx)
	return r != 0
}

func SetCapture(h HWND) { pSetCapture.Call(uintptr(h)) }
func ReleaseCapture()   { pReleaseCapture.Call() }
func GetCapture() HWND  { r, _, _ := pGetCapture.Call(); return HWND(r) }

func TrackMouseEvent(t *TRACKMOUSEEVENT) { pTrackMouseEvent.Call(uintptr(unsafe.Pointer(t))) }

// GetPointerType reports the PT_* type of a WM_POINTER* pointer id.
func GetPointerType(id uint32) (uint32, bool) {
	var t uint32
	r, _, _ := pGetPointerType.Call(uintptr(id), uintptr(unsafe.Pointer(&t)))
	return t, r != 0
}

type pointerInfo struct {
	Type, ID, FrameID, Flags uint32
	Source, Hwnd             uintptr
	Pixel, Himetric          POINT
	PixelRaw, HimetricRaw    POINT
	Time, HistoryCount       uint32
	InputData                int32
	KeyStates                uint32
	PerformanceCount         uint64
	ButtonChange             uint32
}

// PointerAge reports how long ago the input of the pointer message being
// handled happened, from its high-resolution timestamp. It reports false when
// the device gives none.
func PointerAge(id uint32) (time.Duration, bool) {
	var pi pointerInfo
	if r, _, _ := pGetPointerInfo.Call(uintptr(id), uintptr(unsafe.Pointer(&pi))); r == 0 || pi.PerformanceCount == 0 {
		return 0, false
	}
	now := Ticks() // first: it loads the counter frequency qpcDuration uses
	age := now - qpcDuration(int64(pi.PerformanceCount))
	return age, age >= 0 && age < time.Second
}

// IsMouseFromTouch reports whether the mouse message being handled was
// synthesized from touch input: Windows marks its extra info with the
// MI_WP_SIGNATURE 0xFF515700 and sets 0x80 for touch, but not for pen.
func IsMouseFromTouch() bool {
	r, _, _ := pGetMessageExtraInfo.Call()
	return uint32(r)&0xFFFFFF80 == 0xFF515780
}

func InvalidateRect(h HWND, erase bool) { pInvalidateRect.Call(uintptr(h), 0, boolPtr(erase)) }
func ValidateRect(h HWND)               { pValidateRect.Call(uintptr(h), 0) }

func ScreenToClient(h HWND, p *POINT) { pScreenToClient.Call(uintptr(h), uintptr(unsafe.Pointer(p))) }
func ClientToScreen(h HWND, p *POINT) { pClientToScreen.Call(uintptr(h), uintptr(unsafe.Pointer(p))) }

func GetKeyState(vk int32) int16 {
	r, _, _ := pGetKeyState.Call(uintptr(vk))
	return int16(r)
}

func SetWindowText(h HWND, s string) {
	p, _ := syscall.UTF16PtrFromString(s)
	pSetWindowTextW.Call(uintptr(h), uintptr(unsafe.Pointer(p)))
}

func GetDoubleClickTime() uint32 {
	r, _, _ := pGetDoubleClickTime.Call()
	return uint32(r)
}

func GetSystemMetricsForDpi(idx int32, dpi uint32) int32 {
	r, _, _ := pGetSystemMetricsForDpi.Call(uintptr(idx), uintptr(dpi))
	return int32(r)
}

func MonitorFromWindow(h HWND, flags uint32) HANDLE {
	r, _, _ := pMonitorFromWindow.Call(uintptr(h), uintptr(flags))
	return HANDLE(r)
}

func SetTimer(h HWND, id uintptr, ms uint32) { pSetTimer.Call(uintptr(h), id, uintptr(ms), 0) }
func KillTimer(h HWND, id uintptr)           { pKillTimer.Call(uintptr(h), id) }

func IsZoomed(h HWND) bool { r, _, _ := pIsZoomed.Call(uintptr(h)); return r != 0 }
func IsIconic(h HWND) bool { r, _, _ := pIsIconic.Call(uintptr(h)); return r != 0 }

func SetForegroundWindow(h HWND) { pSetForegroundWindow.Call(uintptr(h)) }

// RegisterHotKey registers a system-wide hotkey that posts WM_HOTKEY with id to h.
func RegisterHotKey(h HWND, id int32, mods, vk uint32) error {
	r, _, err := pRegisterHotKey.Call(uintptr(h), uintptr(id), uintptr(mods), uintptr(vk))
	if r == 0 {
		return err
	}
	return nil
}

func UnregisterHotKey(h HWND, id int32) bool {
	r, _, _ := pUnregisterHotKey.Call(uintptr(h), uintptr(id))
	return r != 0
}

func GetCursorPos() POINT {
	var p POINT
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return p
}

func GetModuleHandle() HANDLE {
	r, _, _ := pGetModuleHandleW.Call(0)
	return HANDLE(r)
}

func MsgWaitForMultipleObjectsEx(handles []HANDLE, ms uint32, wake, flags uint32) uint32 {
	var p uintptr
	if len(handles) > 0 {
		p = uintptr(unsafe.Pointer(&handles[0]))
	}
	r, _, _ := pMsgWaitForMultipleObjectsEx.Call(uintptr(len(handles)), p, uintptr(ms), uintptr(wake), uintptr(flags))
	return uint32(r)
}

func DwmExtendFrameIntoClientArea(h HWND, m *MARGINS) {
	pDwmExtendFrameIntoClientArea.Call(uintptr(h), uintptr(unsafe.Pointer(m)))
}

func DwmSetWindowAttribute(h HWND, attr uint32, value unsafe.Pointer, size uint32) {
	if pDwmSetWindowAttribute.Find() != nil {
		return
	}
	pDwmSetWindowAttribute.Call(uintptr(h), uintptr(attr), uintptr(value), uintptr(size))
}

func DwmFlush() { pDwmFlush.Call() }

func ImmGetContext(h HWND) HANDLE {
	r, _, _ := pImmGetContext.Call(uintptr(h))
	return HANDLE(r)
}

func ImmReleaseContext(h HWND, c HANDLE) { pImmReleaseContext.Call(uintptr(h), uintptr(c)) }

// ImmGetCompositionString reads a UTF-16 composition string (GCS_COMPSTR or GCS_RESULTSTR).
func ImmGetCompositionString(c HANDLE, index uint32) string {
	n, _, _ := pImmGetCompositionStringW.Call(uintptr(c), uintptr(index), 0, 0)
	if int32(n) <= 0 {
		return ""
	}
	buf := make([]uint16, n/2)
	pImmGetCompositionStringW.Call(uintptr(c), uintptr(index), uintptr(unsafe.Pointer(&buf[0])), n)
	return windows.UTF16ToString(buf)
}

// ImmGetCompositionCursor returns the caret position (in UTF-16 units) inside the composition.
func ImmGetCompositionCursor(c HANDLE) int {
	r, _, _ := pImmGetCompositionStringW.Call(uintptr(c), GCS_CURSORPOS, 0, 0)
	return int(int32(r))
}

func ImmSetCompositionWindow(c HANDLE, f *COMPOSITIONFORM) {
	pImmSetCompositionWindow.Call(uintptr(c), uintptr(unsafe.Pointer(f)))
}

func ImmSetCandidateWindow(c HANDLE, f *CANDIDATEFORM) {
	pImmSetCandidateWindow.Call(uintptr(c), uintptr(unsafe.Pointer(f)))
}

func ImmNotifyIME(c HANDLE, action, index, value uint32) {
	pImmNotifyIME.Call(uintptr(c), uintptr(action), uintptr(index), uintptr(value))
}

func GetStockObject(i int32) HANDLE {
	r, _, _ := pGetStockObject.Call(uintptr(i))
	return HANDLE(r)
}

func SystemParametersInfo(action, param uint32, out unsafe.Pointer) bool {
	r, _, _ := pSystemParametersInfoW.Call(uintptr(action), uintptr(param), uintptr(out), 0)
	return r != 0
}

func CreateIconFromPNG(png []byte, size int32) HANDLE {
	if len(png) == 0 {
		return 0
	}
	r, _, _ := pCreateIconFromResourceEx.Call(uintptr(unsafe.Pointer(&png[0])), uintptr(len(png)), 1, 0x00030000, uintptr(size), uintptr(size), 0)
	return HANDLE(r)
}

// CreateCursorFromBGRA makes a cursor from w*h top-down BGRA pixels with
// straight alpha, or returns 0.
func CreateCursorFromBGRA(pix []byte, w, h, hotX, hotY int) HANDLE {
	// A cursor resource: the hotspot, then a DIB of the color image stacked
	// on its AND mask (left clear, as the alpha channel decides).
	row, maskRow := w*4, (w+31)/32*4
	buf := make([]byte, 4+40+row*h+maskRow*h)
	put16 := func(b []byte, v int) { b[0], b[1] = byte(v), byte(v>>8) }
	put32 := func(b []byte, v int) { put16(b, v); put16(b[2:], v>>16) }
	put16(buf[0:], hotX)
	put16(buf[2:], hotY)
	hd := buf[4:]
	put32(hd[0:], 40)
	put32(hd[4:], w)
	put32(hd[8:], 2*h)
	put16(hd[12:], 1)
	put16(hd[14:], 32)
	px := buf[44:]
	for y := range h {
		copy(px[(h-1-y)*row:(h-y)*row], pix[y*row:(y+1)*row])
	}
	r, _, _ := pCreateIconFromResourceEx.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0, 0x00030000, uintptr(w), uintptr(h), 0)
	return HANDLE(r)
}

// ClipboardHasText reports whether the clipboard holds text.
func ClipboardHasText() bool {
	r, _, _ := pIsClipboardFormatAvailable.Call(CF_UNICODETEXT)
	return r != 0
}

// ClipboardText returns the CF_UNICODETEXT clipboard contents.
func ClipboardText(h HWND) (string, bool) {
	data, ok := ClipboardData(h, CF_UNICODETEXT)
	if !ok {
		return "", false
	}
	u := make([]uint16, len(data)/2)
	for i := range u {
		u[i] = uint16(data[2*i]) | uint16(data[2*i+1])<<8
	}
	return windows.UTF16ToString(u), true
}

// SetClipboardText replaces the clipboard with s as CF_UNICODETEXT.
func SetClipboardText(h HWND, s string) bool {
	u, err := syscall.UTF16FromString(s)
	if err != nil {
		return false
	}
	if r, _, _ := pOpenClipboard.Call(uintptr(h)); r == 0 {
		return false
	}
	defer pCloseClipboard.Call()
	pEmptyClipboard.Call()
	size := uintptr(len(u) * 2)
	hmem, _, _ := pGlobalAlloc.Call(GMEM_MOVEABLE, size)
	if hmem == 0 {
		return false
	}
	p, _, _ := pGlobalLock.Call(hmem)
	if p == 0 {
		pGlobalFree.Call(hmem)
		return false
	}
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(p)), len(u)), u)
	pGlobalUnlock.Call(hmem)
	if r, _, _ := pSetClipboardData.Call(CF_UNICODETEXT, hmem); r == 0 {
		pGlobalFree.Call(hmem)
		return false
	}
	return true
}

func boolPtr(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

func LOWORD(v uintptr) uint16 { return uint16(v) }
func HIWORD(v uintptr) uint16 { return uint16(v >> 16) }

// PointFromLParam decodes signed client coordinates packed in an LPARAM.
func PointFromLParam(lp uintptr) (int32, int32) {
	return int32(int16(uint16(lp))), int32(int16(uint16(lp >> 16)))
}

var pGetUserDefaultLocaleName = kernel32.NewProc("GetUserDefaultLocaleName")

// UserLocale returns the user's default locale name, such as "ko-KR".
func UserLocale() string {
	buf := make([]uint16, 85)
	r, _, _ := pGetUserDefaultLocaleName.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if r == 0 {
		return "en-US"
	}
	return windows.UTF16ToString(buf)
}

var pImmAssociateContextEx = imm32.NewProc("ImmAssociateContextEx")

// ImmEnable turns the IME on (default context) or off for the window.
func ImmEnable(h HWND, on bool) {
	if on {
		pImmAssociateContextEx.Call(uintptr(h), 0, IACE_DEFAULT)
	} else {
		pImmAssociateContextEx.Call(uintptr(h), 0, 0)
	}
}
