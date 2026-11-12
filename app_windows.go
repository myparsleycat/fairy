package fairy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/apphost"
	"github.com/myparsleycat/fairy/internal/com"
	"github.com/myparsleycat/fairy/internal/win"
)

// appPlat is the Win32 part of app. The app window is a hidden top-level
// window owned by the UI thread. It receives calls posted from other
// goroutines, tray icon notifications and launches forwarded by later
// instances. Unlike thread messages, its messages keep arriving while a modal
// loop (dialog, menu, window move) runs. It is not message-only because those
// get no broadcasts, such as TaskbarCreated, and cannot own the foreground,
// which tray menus need.
type appPlat struct {
	hwnd       win.HWND
	instanceID string
	mutex      win.HANDLE
}

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

func init() { apphost.HWND = func() win.HWND { return app.hwnd } }

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
	defer releaseTaskbar()
	if err := createAppWindow(); err != nil {
		return err
	}
	defer func() {
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
	Go(func() { win.PostQuitMessage(0) })
}

// quitIfIdle ends Main once no window and no tray icon is left.
func quitIfIdle() {
	if !app.closing && len(windows) == 0 && apphost.Holds() == 0 {
		win.PostQuitMessage(0)
	}
}

func updateWindows() {
	for _, w := range windows {
		w.markDirty(dirtyGo)
		w.invalidate()
	}
}

func createAppWindow() error {
	class := "nahida.fairy.app"
	if app.instanceID != "" {
		class = instanceClass(app.instanceID)
	}
	cls, _ := syscall.UTF16PtrFromString(class)
	wc := win.WNDCLASSEXW{
		Size:      uint32(unsafe.Sizeof(win.WNDCLASSEXW{})),
		WndProc:   appProcPtr,
		Instance:  win.GetModuleHandle(),
		ClassName: cls,
	}
	win.RegisterClassEx(&wc) // already registered after a previous Main
	const wsExToolWindow = 0x80
	hwnd, err := win.CreateWindowEx(wsExToolWindow, cls, cls, win.WS_POPUP, 0, 0, 0, 0, 0, 0, win.GetModuleHandle(), 0)
	if err != nil {
		return fmt.Errorf("fairy: app window: %w", err)
	}
	app.mu.Lock()
	if len(app.calls) != 0 && !win.PostMessage(hwnd, msgAppCall, 0, 0) {
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
	case win.WM_COPYDATA:
		cds := (*win.COPYDATASTRUCT)(com.Ptr(lp))
		if cds.Data == launchMagic && app.onLaunch != nil {
			var l Launch
			if json.Unmarshal(unsafe.Slice((*byte)(com.Ptr(cds.Ptr)), cds.Size), &l) == nil {
				// Reply first so the sending process can exit, then handle it.
				f := app.onLaunch
				Go(func() { f(l) })
			}
			return 1
		}
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

const launchMagic = 0x6775696c // "guil"

func instanceClass(id string) string { return "nahida.fairy.instance." + id }

func findInstanceWindow(id string) win.HWND { return win.FindWindowByClass(instanceClass(id)) }

// instanceWait bounds how long a further process waits for the first one. The first process has no instance window
// while it starts up before Main and while it shuts down after Main returns.
const instanceWait = 30 * time.Second

// SingleInstance makes the program run once per user session. Call it before
// Main. In the first process it returns true, and onLaunch later receives the
// arguments of every further start on the UI thread. In a further process it
// forwards the launch to the first one, allows it to take the foreground, and
// returns false; the caller should then exit. A further process waits while the
// first one is not running Main, and returns true in its place once it has exited.
func SingleInstance(id string, onLaunch func(Launch)) (bool, error) {
	if id == "" {
		return false, errors.New("fairy: SingleInstance needs an id")
	}
	name := `Local\nahida.fairy.` + id
	h, existed, err := win.CreateMutex(name)
	if err != nil {
		return false, fmt.Errorf("fairy: single instance mutex: %w", err)
	}
	dir, _ := os.Getwd()
	data, _ := json.Marshal(Launch{Args: os.Args, Dir: dir})
	deadline := time.Now().Add(instanceWait)
	for existed {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false, errors.New("fairy: the running instance did not answer")
		}
		if target := findInstanceWindow(id); target != 0 {
			win.AllowSetForegroundWindow(win.WindowProcessID(target))
			timeoutMS := uint32((remaining + time.Millisecond - 1) / time.Millisecond)
			cds := win.COPYDATASTRUCT{Data: launchMagic, Size: uint32(len(data)), Ptr: uintptr(unsafe.Pointer(&data[0]))}
			reply, ok := win.SendMessageTimeout(target, win.WM_COPYDATA, 0, unsafe.Pointer(&cds), timeoutMS)
			runtime.KeepAlive(data)
			if ok && reply != 0 {
				return false, nil
			}
		} else {
			// The mutex is gone once the first process has exited, since this handle was the only other one.
			win.CloseHandle(h)
			h, existed, err = win.CreateMutex(name)
			if err != nil {
				return false, fmt.Errorf("fairy: single instance mutex: %w", err)
			}
			if !existed {
				break
			}
		}
		time.Sleep(min(100*time.Millisecond, time.Until(deadline)))
	}
	app.mutex = h
	app.instanceID = id
	app.onLaunch = onLaunch
	return true, nil
}
