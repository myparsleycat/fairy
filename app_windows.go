package fairy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/apphost"
	"github.com/myparsleycat/fairy/internal/com"
	"github.com/myparsleycat/fairy/internal/win"
)

// The app window is a hidden top-level window owned by the UI thread. It
// receives calls posted from other goroutines, tray icon notifications and
// launches forwarded by later instances. Unlike thread messages, its messages
// keep arriving while a modal loop (dialog, menu, window move) runs. It is not
// message-only because those get no broadcasts, such as TaskbarCreated, and
// cannot own the foreground, which tray menus need.
var app struct {
	main    sync.Mutex
	thread  atomic.Uint32
	hwnd    win.HWND
	closing bool
	done    chan struct{} // closed when this Main run begins shutting down

	mu    sync.Mutex
	calls []appCall
	quiet []appCall // like calls, but no window updates afterwards

	instanceID string
	onLaunch   func(Launch)
	mutex      win.HANDLE
}

type appCall struct {
	run     func()
	cancel  func()
	session <-chan struct{}
}

const (
	msgAppCall  = win.WM_APP + 10
	msgAppQuiet = win.WM_APP + 12 // WM_APP + 11 is apphost.MsgTray
)

var appProcPtr = syscall.NewCallback(appProc)
var errUIUnavailable = apphost.ErrUIUnavailable

func init() {
	apphost.HWND = func() win.HWND { return app.hwnd }
	apphost.OnUIThread = onUIThread
	apphost.Running = func() bool { return app.thread.Load() != 0 }
	apphost.Do = func(f func()) bool { return doAppCall(f, msgAppCall) }
	apphost.Go = Go
	apphost.UpdateWindows = updateWindows
	apphost.QuitIfIdle = quitIfIdle
}

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

// Go runs f on the UI thread without waiting, then updates every window.
// It may be called from any goroutine; before Main starts, f is queued until it does.
// Calls still queued when Main exits are discarded.
func Go(f func()) {
	enqueueAppCall(appCall{run: f}, msgAppCall)
}

// Do runs f on the UI thread and waits for it to return. On the UI thread it
// calls f directly. If no UI loop is running, or it exits before f runs, Do
// returns without calling f.
func Do(f func()) {
	doAppCall(f, msgAppCall)
}

// doQuiet runs f on the UI thread and waits, without updating windows
// afterwards. It is for queries that change no state.
func doQuiet(f func()) {
	doAppCall(f, msgAppQuiet)
}

func doAppCall(f func(), msg uint32) bool {
	if onUIThread() {
		f()
		return true
	}
	done := make(chan bool, 1)
	call := appCall{
		run: func() {
			defer func() { done <- true }()
			f()
		},
		cancel: func() { done <- false },
	}
	return enqueueAppCall(call, msg) && <-done
}

func enqueueAppCall(call appCall, msg uint32) bool {
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.closing || (app.hwnd == 0 && call.cancel != nil) || (call.session != nil && call.session != app.done) {
		return false
	}
	queue := &app.calls
	if msg == msgAppQuiet {
		queue = &app.quiet
	}
	if len(*queue) == 0 && app.hwnd != 0 && !win.PostMessage(app.hwnd, msg, 0, 0) {
		return false
	}
	*queue = append(*queue, call)
	return true
}

func cancelAppCalls(calls []appCall) {
	for _, call := range calls {
		if call.cancel != nil {
			call.cancel()
		}
	}
}

func onUIThread() bool {
	thread := app.thread.Load()
	return thread != 0 && win.GetCurrentThreadId() == thread
}

func runAppCalls() {
	app.mu.Lock()
	calls := app.calls
	app.calls = nil
	app.mu.Unlock()
	for _, call := range calls {
		t := time.Now()
		call.run()
		if debugSlow > 0 {
			slow("Go call", t)
		}
	}
	// Calls usually change app state, so every window rebuilds its view.
	updateWindows()
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
		app.mu.Lock()
		calls := app.quiet
		app.quiet = nil
		app.mu.Unlock()
		for _, call := range calls {
			call.run()
		}
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
	}
	if h := apphost.Handler(msg); h != nil {
		return h(wp, lp)
	}
	return win.DefWindowProc(hwnd, msg, wp, lp)
}

// Launch is a later start of the program, forwarded to the running instance.
type Launch struct {
	Args []string // os.Args of the new process
	Dir  string   // its working directory
}

const launchMagic = 0x6775696c // "guil"

func instanceClass(id string) string { return "nahida.fairy.instance." + id }

func findInstanceWindow(id string) win.HWND { return win.FindWindowByClass(instanceClass(id)) }

// SingleInstance makes the program run once per user session. Call it before
// Main. In the first process it returns true, and onLaunch later receives the
// arguments of every further start on the UI thread. In a further process it
// forwards the launch to the first one, allows it to take the foreground, and
// returns false; the caller should then exit.
func SingleInstance(id string, onLaunch func(Launch)) (bool, error) {
	if id == "" {
		return false, errors.New("fairy: SingleInstance needs an id")
	}
	h, existed, err := win.CreateMutex(`Local\nahida.fairy.` + id)
	if err != nil {
		return false, fmt.Errorf("fairy: single instance mutex: %w", err)
	}
	if !existed {
		app.mutex = h
		app.instanceID = id
		app.onLaunch = onLaunch
		return true, nil
	}
	dir, _ := os.Getwd()
	data, _ := json.Marshal(Launch{Args: os.Args, Dir: dir})
	// The first process may still be starting up; give it a few seconds.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if target := findInstanceWindow(id); target != 0 {
			win.AllowSetForegroundWindow(win.WindowProcessID(target))
			remaining := time.Until(deadline)
			if remaining <= 0 {
				break
			}
			timeoutMS := uint32((remaining + time.Millisecond - 1) / time.Millisecond)
			cds := win.COPYDATASTRUCT{Data: launchMagic, Size: uint32(len(data)), Ptr: uintptr(unsafe.Pointer(&data[0]))}
			reply, ok := win.SendMessageTimeout(target, win.WM_COPYDATA, 0, unsafe.Pointer(&cds), timeoutMS)
			runtime.KeepAlive(data)
			if ok && reply != 0 {
				return false, nil
			}
		}
		time.Sleep(min(100*time.Millisecond, time.Until(deadline)))
	}
	return false, errors.New("fairy: the running instance did not answer")
}
