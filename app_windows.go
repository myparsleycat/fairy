package fairy

import (
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"syscall"
	"unsafe"

	xwin "golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/myparsleycat/fairy/internal/apphost"
	"github.com/myparsleycat/fairy/internal/frameprof"
	"github.com/myparsleycat/fairy/internal/win"
)

// appPlat is the Win32 part of the app loop's state.
//
// The app window is a hidden top-level window owned by the UI thread. It
// receives calls posted from other goroutines, tray icon notifications and
// launches forwarded by later instances. Unlike thread messages, its messages
// keep arriving while a modal loop (dialog, menu, window move) runs. It is not
// message-only because those get no broadcasts, such as TaskbarCreated, and
// cannot own the foreground, which tray menus need.
type appPlat struct {
	hwnd win.HWND
}

func init() { apphost.HWND = func() win.HWND { return app.hwnd } }

// appLoopReady reports whether the UI loop can be woken.
func appLoopReady() bool { return app.hwnd != 0 }

// wakeAppLoop asks the UI loop to run the queued calls, or the quiet ones.
func wakeAppLoop(quiet bool) bool {
	msg := uint32(msgAppCall)
	if quiet {
		msg = msgAppQuiet
	}
	return win.PostMessage(app.hwnd, msg, 0, 0)
}

const (
	msgAppCall  = win.WM_APP + 10
	msgAppQuiet = win.WM_APP + 12 // WM_APP + 11 is apphost.MsgTray
)

var appProcPtr = syscall.NewCallback(appProc)

// Main runs the UI message loop on the calling goroutine's OS thread. start
// runs on that thread first and typically opens windows and a tray icon.
// Main returns after Quit, or once no window and no tray icon is left.
func Main(start func() error) error {
	if !app.main.TryLock() {
		return errors.New("fairy: Main is already running")
	}
	defer app.main.Unlock()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := win.OleInitialize(); err != nil {
		return fmt.Errorf("fairy: OleInitialize: %w", err)
	}
	defer win.OleUninitialize()
	if err := createAppWindow(); err != nil {
		return err
	}
	instance.accepting.Store(true)
	defer func() {
		// Launches that arrive from here on wait for this process to exit or release its claim.
		instance.accepting.Store(false)
		app.mu.Lock()
		app.closing = true
		close(app.done)
		calls, quiet := app.calls, app.quiet
		app.calls, app.quiet = nil, nil
		app.mu.Unlock()
		cancelAppCalls(calls)
		cancelAppCalls(quiet)
		for hwnd := range windows {
			win.DestroyWindow(hwnd)
		}
		apphost.Shutdown()
		win.DestroyWindow(app.hwnd)
		app.mu.Lock()
		app.hwnd = 0
		app.done = nil
		app.thread.Store(0)
		app.closing = false
		app.mu.Unlock()
	}()
	if err := start(); err != nil {
		return err
	}
	if len(windows) == 0 && apphost.Holds() == 0 {
		return nil
	}
	var m win.MSG
	for {
		r := win.GetMessage(&m)
		if r == 0 || r == -1 {
			break
		}
		win.TranslateMessage(&m)
		win.DispatchMessage(&m)
	}
	return nil
}

// Quit ends Main. It may be called from any goroutine.
func Quit() {
	// A launch taken from here on would run on a UI loop that is about to end.
	instance.accepting.Store(false)
	Go(func() { win.PostQuitMessage(0) })
}

// quitIfIdle ends Main once no window and no tray icon is left.
func quitIfIdle() {
	if !app.closing && len(windows) == 0 && apphost.Holds() == 0 {
		instance.accepting.Store(false)
		win.PostQuitMessage(0)
	}
}

func updateWindows() {
	for _, w := range windows {
		w.markDirty(frameprof.CauseGo)
		w.invalidate()
	}
}

// createHiddenWindow creates a hidden top-level tool window of a class it registers on first use.
func createHiddenWindow(class string, proc uintptr) (win.HWND, error) {
	cls, _ := syscall.UTF16PtrFromString(class)
	wc := win.WNDCLASSEXW{
		Size:      uint32(unsafe.Sizeof(win.WNDCLASSEXW{})),
		WndProc:   proc,
		Instance:  win.GetModuleHandle(),
		ClassName: cls,
	}
	win.RegisterClassEx(&wc) // fails harmlessly once the class is registered
	const wsExToolWindow = 0x80
	return win.CreateWindowEx(wsExToolWindow, cls, cls, win.WS_POPUP, 0, 0, 0, 0, 0, 0, win.GetModuleHandle(), 0)
}

func createAppWindow() error {
	hwnd, err := createHiddenWindow("nahida.fairy.app", appProcPtr)
	if err != nil {
		return fmt.Errorf("fairy: app window: %w", err)
	}
	app.mu.Lock()
	if len(app.calls) != 0 && !win.PostMessage(hwnd, msgAppCall, 0, 0) ||
		len(app.quiet) != 0 && !win.PostMessage(hwnd, msgAppQuiet, 0, 0) {
		app.mu.Unlock()
		win.DestroyWindow(hwnd)
		return errors.New("fairy: could not post queued UI calls")
	}
	app.hwnd = hwnd
	app.done = make(chan struct{})
	app.thread.Store(win.GetCurrentThreadId())
	app.mu.Unlock()
	return nil
}

func appProc(hwnd win.HWND, msg uint32, wp, lp uintptr) uintptr {
	switch msg {
	case msgAppCall:
		runAppCalls()
		return 0
	case msgAppQuiet:
		runQuietCalls()
		return 0
	case win.WM_CLOSE:
		// Sent to top-level windows by taskkill and installers; the app window
		// lives until Main returns.
		return 0
	case win.WM_ENDSESSION:
		app.mu.Lock()
		f := app.onSessionEnd
		app.mu.Unlock()
		if wp != 0 && f != nil {
			f()
		}
		return 0
	}
	if h := apphost.Handler(msg); h != nil {
		return h(wp, lp)
	}
	return win.DefWindowProc(hwnd, msg, wp, lp)
}

var systemDark atomic.Bool

func init() { loadSystemDark() }

// SystemDark reports whether Windows is set to the dark mode for apps. When
// the user changes it, every window builds its view again, so a View that
// follows the system theme can read it on each build.
func SystemDark() bool { return systemDark.Load() }

func loadSystemDark() {
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		systemDark.Store(false)
		return
	}
	defer k.Close()
	light, _, err := k.GetIntegerValue("AppsUseLightTheme")
	systemDark.Store(err == nil && light == 0)
}

// isColorSetChange reports whether a WM_SETTINGCHANGE lParam announces a
// change of the app color mode.
func isColorSetChange(lp uintptr) bool {
	return lp != 0 && xwin.UTF16PtrToString((*uint16)(unsafe.Pointer(lp))) == "ImmersiveColorSet"
}
