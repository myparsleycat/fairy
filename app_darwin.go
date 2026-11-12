package fairy

import (
	"errors"
	"runtime"
)

// errNoBackend is returned until the AppKit backend exists.
var errNoBackend = errors.New("fairy: macOS support is not implemented yet")

// appPlat is the macOS part of app.
type appPlat struct{}

func init() {
	// AppKit runs only on the main thread. Package init runs on it, so
	// locking here keeps the main goroutine there for Main.
	runtime.LockOSThread()
}

// appLoopReady reports whether the UI loop can be woken.
func appLoopReady() bool { return false }

// wakeAppLoop asks the UI loop to run the queued calls, or the quiet ones.
func wakeAppLoop(quiet bool) bool { return false }

// Main runs the UI loop on the main thread. start runs on that thread first
// and typically opens windows and a tray icon. Main returns after Quit, or
// once no window and no tray icon is left.
func Main(start func() error) error { return errNoBackend }

// Quit ends Main. It may be called from any goroutine.
func Quit() {}

// quitIfIdle ends Main once no window and no tray icon is left.
func quitIfIdle() {}

// updateWindows rebuilds every window's view, as after a Go call.
func updateWindows() {}

// SingleInstance makes the program run once per user session. Call it
// before Main. In the first process it returns true, and onLaunch later
// receives the arguments of every further start on the UI thread.
func SingleInstance(id string, onLaunch func(Launch)) (bool, error) {
	if id == "" {
		return false, errors.New("fairy: SingleInstance needs an id")
	}
	return false, errNoBackend
}
