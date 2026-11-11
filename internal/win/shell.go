//go:build windows

package win

import (
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ole32 = windows.NewLazySystemDLL("ole32.dll")

	pOleInitialize      = ole32.NewProc("OleInitialize")
	pOleUninitialize    = ole32.NewProc("OleUninitialize")
	pCoCreateInstance   = ole32.NewProc("CoCreateInstance")
	pCoInitializeEx     = ole32.NewProc("CoInitializeEx")
	pCoTaskMemFree      = ole32.NewProc("CoTaskMemFree")
	pRegisterDragDrop   = ole32.NewProc("RegisterDragDrop")
	pRevokeDragDrop     = ole32.NewProc("RevokeDragDrop")
	pReleaseStgMedium   = ole32.NewProc("ReleaseStgMedium")
	pSHCreateItemFromPN = shell32.NewProc("SHCreateItemFromParsingName")
	pDragQueryFileW     = shell32.NewProc("DragQueryFileW")
	pShellNotifyIconW   = shell32.NewProc("Shell_NotifyIconW")

	pMessageBoxW                = user32.NewProc("MessageBoxW")
	pCreatePopupMenu            = user32.NewProc("CreatePopupMenu")
	pAppendMenuW                = user32.NewProc("AppendMenuW")
	pTrackPopupMenuEx           = user32.NewProc("TrackPopupMenuEx")
	pDestroyMenu                = user32.NewProc("DestroyMenu")
	pGetSystemMenu              = user32.NewProc("GetSystemMenu")
	pEnableMenuItem             = user32.NewProc("EnableMenuItem")
	pSetMenuDefaultItem         = user32.NewProc("SetMenuDefaultItem")
	pRegisterWindowMessageW     = user32.NewProc("RegisterWindowMessageW")
	pRegisterClipboardFormatW   = user32.NewProc("RegisterClipboardFormatW")
	pFindWindowExW              = user32.NewProc("FindWindowExW")
	pGetWindowThreadProcessId   = user32.NewProc("GetWindowThreadProcessId")
	pAllowSetForegroundWindow   = user32.NewProc("AllowSetForegroundWindow")
	pGetWindowPlacement         = user32.NewProc("GetWindowPlacement")
	pSetWindowPlacement         = user32.NewProc("SetWindowPlacement")
	pBringWindowToTop           = user32.NewProc("BringWindowToTop")
	pIsWindowVisible            = user32.NewProc("IsWindowVisible")
	pDestroyIcon                = user32.NewProc("DestroyIcon")
	pGetCurrentThreadId         = kernel32.NewProc("GetCurrentThreadId")
	pCreateMutexW               = kernel32.NewProc("CreateMutexW")
	pGlobalSize                 = kernel32.NewProc("GlobalSize")
	pGetClipboardSequenceNumber = user32.NewProc("GetClipboardSequenceNumber")
)

const (
	WM_NULL        = 0x0000
	WM_COPYDATA    = 0x004A
	WM_CONTEXTMENU = 0x007B

	HWND_MESSAGE = ^uintptr(2) // (HWND)-3

	CLSCTX_INPROC_SERVER = 0x1

	MB_OK              = 0x0
	MB_OKCANCEL        = 0x1
	MB_YESNOCANCEL     = 0x3
	MB_YESNO           = 0x4
	MB_ICONERROR       = 0x10
	MB_ICONQUESTION    = 0x20
	MB_ICONWARNING     = 0x30
	MB_ICONINFORMATION = 0x40
	MB_TASKMODAL       = 0x2000
	MB_SETFOREGROUND   = 0x10000

	MF_STRING    = 0x0
	MF_ENABLED   = 0x0
	MF_GRAYED    = 0x1
	MF_CHECKED   = 0x8
	MF_POPUP     = 0x10
	MF_SEPARATOR = 0x800

	TPM_RIGHTBUTTON = 0x2
	TPM_RETURNCMD   = 0x100
	TPM_NONOTIFY    = 0x80

	NIM_ADD        = 0
	NIM_MODIFY     = 1
	NIM_DELETE     = 2
	NIM_SETVERSION = 4

	NIF_MESSAGE  = 0x1
	NIF_ICON     = 0x2
	NIF_TIP      = 0x4
	NIF_INFO     = 0x10
	NIF_SHOWTIP  = 0x80
	NIIF_INFO    = 0x1
	NIIF_WARNING = 0x2
	NIIF_ERROR   = 0x3

	NOTIFYICON_VERSION_4 = 4
	NIN_SELECT           = WM_USER + 0
	NIN_KEYSELECT        = WM_USER + 1
	NIN_BALLOONUSERCLICK = WM_USER + 5

	CF_DIB           = 8
	CF_HDROP         = 15
	CF_DIBV5         = 17
	DVASPECT_CONTENT = 1
	TYMED_HGLOBAL    = 1

	DROPEFFECT_NONE = 0
	DROPEFFECT_COPY = 1

	ERROR_ALREADY_EXISTS = 183
)

// NOTIFYICONDATAW is the Shell_NotifyIconW argument.
type NOTIFYICONDATAW struct {
	Size            uint32
	Wnd             HWND
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            HANDLE
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GUIDItem        [16]byte
	BalloonIcon     HANDLE
}

// FORMATETC and STGMEDIUM are the IDataObject::GetData arguments.
type FORMATETC struct {
	Format uint16
	_      [6]byte
	Ptd    uintptr
	Aspect uint32
	Index  int32
	Tymed  uint32
	_      [4]byte
}

type STGMEDIUM struct {
	Tymed   uint32
	_       [4]byte
	Handle  uintptr
	Release uintptr
}

type COPYDATASTRUCT struct {
	Data uintptr
	Size uint32
	_    [4]byte
	Ptr  uintptr
}

type WINDOWPLACEMENT struct {
	Length         uint32
	Flags          uint32
	ShowCmd        uint32
	MinPosition    POINT
	MaxPosition    POINT
	NormalPosition RECT
}

func OleInitialize() error {
	r, _, _ := pOleInitialize.Call(0)
	if int32(r) < 0 {
		return syscall.Errno(r)
	}
	return nil
}

func OleUninitialize() { pOleUninitialize.Call() }

const COINIT_MULTITHREADED = 0

// CoInitializeEx initializes COM on the calling thread.
func CoInitializeEx(coinit uint32) error {
	r, _, _ := pCoInitializeEx.Call(0, uintptr(coinit))
	if int32(r) < 0 {
		return syscall.Errno(r)
	}
	return nil
}

// CoCreateInstance creates an in-process COM object.
func CoCreateInstance(clsid, iid unsafe.Pointer) (unsafe.Pointer, int32) {
	var out unsafe.Pointer
	r, _, _ := pCoCreateInstance.Call(uintptr(clsid), 0, CLSCTX_INPROC_SERVER, uintptr(iid), uintptr(unsafe.Pointer(&out)))
	return out, int32(r)
}

// CoTaskString copies and frees a CoTaskMemAlloc'd UTF-16 string.
func CoTaskString(p uintptr) string {
	if p == 0 {
		return ""
	}
	s := windows.UTF16PtrToString((*uint16)(unsafe.Pointer(p)))
	pCoTaskMemFree.Call(p)
	return s
}

func RegisterDragDrop(h HWND, target unsafe.Pointer) int32 {
	r, _, _ := pRegisterDragDrop.Call(uintptr(h), uintptr(target))
	return int32(r)
}

func RevokeDragDrop(h HWND) { pRevokeDragDrop.Call(uintptr(h)) }

func ReleaseStgMedium(m *STGMEDIUM) { pReleaseStgMedium.Call(uintptr(unsafe.Pointer(m))) }

// SHCreateItemFromParsingName returns an IShellItem for a path.
func SHCreateItemFromParsingName(path string, iid unsafe.Pointer) (unsafe.Pointer, int32) {
	p, _ := syscall.UTF16PtrFromString(path)
	var out unsafe.Pointer
	r, _, _ := pSHCreateItemFromPN.Call(uintptr(unsafe.Pointer(p)), 0, uintptr(iid), uintptr(unsafe.Pointer(&out)))
	return out, int32(r)
}

// DropFiles returns the paths in an HDROP.
func DropFiles(hdrop uintptr) []string {
	n, _, _ := pDragQueryFileW.Call(hdrop, 0xFFFFFFFF, 0, 0)
	paths := make([]string, 0, n)
	for i := uintptr(0); i < n; i++ {
		l, _, _ := pDragQueryFileW.Call(hdrop, i, 0, 0)
		buf := make([]uint16, l+1)
		pDragQueryFileW.Call(hdrop, i, uintptr(unsafe.Pointer(&buf[0])), l+1)
		paths = append(paths, windows.UTF16ToString(buf))
	}
	return paths
}

func ShellNotifyIcon(msg uint32, d *NOTIFYICONDATAW) bool {
	d.Size = uint32(unsafe.Sizeof(*d))
	r, _, _ := pShellNotifyIconW.Call(uintptr(msg), uintptr(unsafe.Pointer(d)))
	return r != 0
}

func MessageBox(owner HWND, text, caption string, flags uint32) int32 {
	t, _ := syscall.UTF16PtrFromString(text)
	c, _ := syscall.UTF16PtrFromString(caption)
	r, _, _ := pMessageBoxW.Call(uintptr(owner), uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), uintptr(flags))
	return int32(r)
}

func CreatePopupMenu() HANDLE {
	r, _, _ := pCreatePopupMenu.Call()
	return HANDLE(r)
}

func AppendMenu(m HANDLE, flags uint32, id uintptr, text string) {
	var p *uint16
	if text != "" {
		p, _ = syscall.UTF16PtrFromString(text)
	}
	pAppendMenuW.Call(uintptr(m), uintptr(flags), id, uintptr(unsafe.Pointer(p)))
}

// TrackPopupMenu shows m at screen point (x, y) and returns the chosen command, or 0.
func TrackPopupMenu(m HANDLE, flags uint32, x, y int32, owner HWND) uintptr {
	r, _, _ := pTrackPopupMenuEx.Call(uintptr(m), uintptr(flags), uintptr(x), uintptr(y), uintptr(owner), 0)
	return r
}

func DestroyMenu(m HANDLE) { pDestroyMenu.Call(uintptr(m)) }

func GetSystemMenu(h HWND) HANDLE {
	r, _, _ := pGetSystemMenu.Call(uintptr(h), 0)
	return HANDLE(r)
}

// EnableMenuItem sets the enabled state of the item whose command is id.
func EnableMenuItem(m HANDLE, id uintptr, enabled bool) {
	flags := uintptr(MF_GRAYED)
	if enabled {
		flags = MF_ENABLED
	}
	pEnableMenuItem.Call(uintptr(m), id, flags)
}

// SetMenuDefaultItem makes the item whose command is id the bold default.
func SetMenuDefaultItem(m HANDLE, id uintptr) { pSetMenuDefaultItem.Call(uintptr(m), id, 0) }

func RegisterWindowMessage(name string) uint32 {
	p, _ := syscall.UTF16PtrFromString(name)
	r, _, _ := pRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(p)))
	return uint32(r)
}

func RegisterClipboardFormat(name string) uint32 {
	p, _ := syscall.UTF16PtrFromString(name)
	r, _, _ := pRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(p)))
	return uint32(r)
}

// FindWindowByClass finds a top-level or message-only window by class name.
func FindWindowByClass(class string) HWND {
	c, _ := syscall.UTF16PtrFromString(class)
	r, _, _ := pFindWindowExW.Call(0, 0, uintptr(unsafe.Pointer(c)), 0)
	return HWND(r)
}

func WindowProcessID(h HWND) uint32 {
	var pid uint32
	pGetWindowThreadProcessId.Call(uintptr(h), uintptr(unsafe.Pointer(&pid)))
	return pid
}

func AllowSetForegroundWindow(pid uint32) { pAllowSetForegroundWindow.Call(uintptr(pid)) }

func GetWindowPlacement(h HWND) WINDOWPLACEMENT {
	p := WINDOWPLACEMENT{Length: uint32(unsafe.Sizeof(WINDOWPLACEMENT{}))}
	pGetWindowPlacement.Call(uintptr(h), uintptr(unsafe.Pointer(&p)))
	return p
}

func SetWindowPlacement(h HWND, p *WINDOWPLACEMENT) {
	p.Length = uint32(unsafe.Sizeof(*p))
	pSetWindowPlacement.Call(uintptr(h), uintptr(unsafe.Pointer(p)))
}

func BringWindowToTop(h HWND) { pBringWindowToTop.Call(uintptr(h)) }

func IsWindowVisible(h HWND) bool {
	r, _, _ := pIsWindowVisible.Call(uintptr(h))
	return r != 0
}

func DestroyIcon(h HANDLE) { pDestroyIcon.Call(uintptr(h)) }

func GetCurrentThreadId() uint32 {
	r, _, _ := pGetCurrentThreadId.Call()
	return uint32(r)
}

// CreateMutex creates or opens a named mutex. existed reports whether another
// process created it first.
func CreateMutex(name string) (h HANDLE, existed bool, err error) {
	p, _ := syscall.UTF16PtrFromString(name)
	r, _, e := pCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(p)))
	if r == 0 {
		return 0, false, e
	}
	return HANDLE(r), e == syscall.Errno(ERROR_ALREADY_EXISTS), nil
}

func ClipboardSequence() uint32 {
	r, _, _ := pGetClipboardSequenceNumber.Call()
	return uint32(r)
}

// ClipboardData returns a copy of the clipboard data in format, if present.
func ClipboardData(h HWND, format uint32) ([]byte, bool) {
	if r, _, _ := pIsClipboardFormatAvailable.Call(uintptr(format)); r == 0 {
		return nil, false
	}
	if r, _, _ := pOpenClipboard.Call(uintptr(h)); r == 0 {
		return nil, false
	}
	defer pCloseClipboard.Call()
	hmem, _, _ := pGetClipboardData.Call(uintptr(format))
	if hmem == 0 {
		return nil, false
	}
	size, _, _ := pGlobalSize.Call(hmem)
	p, _, _ := pGlobalLock.Call(hmem)
	if p == 0 {
		return nil, false
	}
	defer pGlobalUnlock.Call(hmem)
	return append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(p)), size)...), true
}

// SetClipboardData replaces the clipboard with the given formats.
func SetClipboardData(h HWND, formats map[uint32][]byte) bool {
	if r, _, _ := pOpenClipboard.Call(uintptr(h)); r == 0 {
		return false
	}
	defer pCloseClipboard.Call()
	pEmptyClipboard.Call()
	ok := true
	for format, data := range formats {
		hmem, _, _ := pGlobalAlloc.Call(GMEM_MOVEABLE, uintptr(len(data)))
		if hmem == 0 {
			ok = false
			continue
		}
		p, _, _ := pGlobalLock.Call(hmem)
		if p == 0 {
			pGlobalFree.Call(hmem)
			ok = false
			continue
		}
		copy(unsafe.Slice((*byte)(unsafe.Pointer(p)), len(data)), data)
		pGlobalUnlock.Call(hmem)
		if r, _, _ := pSetClipboardData.Call(uintptr(format), hmem); r == 0 {
			pGlobalFree.Call(hmem)
			ok = false
		}
	}
	return ok
}

var (
	pQueryPerformanceCounter   = kernel32.NewProc("QueryPerformanceCounter")
	pQueryPerformanceFrequency = kernel32.NewProc("QueryPerformanceFrequency")
	qpcFreq                    int64
)

// Ticks returns a high-resolution monotonic time for measuring short
// intervals; time.Now is coarser on Windows.
func Ticks() time.Duration {
	if qpcFreq == 0 {
		pQueryPerformanceFrequency.Call(uintptr(unsafe.Pointer(&qpcFreq)))
	}
	var c int64
	pQueryPerformanceCounter.Call(uintptr(unsafe.Pointer(&c)))
	return time.Duration(c/qpcFreq)*time.Second + time.Duration(c%qpcFreq)*time.Second/time.Duration(qpcFreq)
}
