package fairy

import (
	"runtime"
	"sync"
)

// Local returns state of type T kept under key in the window whose view is
// being built, creating it on first use. It survives rebuilds while some
// View keeps asking for it and is dropped a few builds after it stops.
// Call it from View, on the UI thread. Components use it for state the app
// does not care about, such as whether a dropdown is open.
func Local[T any](key string) *T {
	w := CurrentWindow()
	if w == nil {
		return new(T)
	}
	l, ok := w.locals[key]
	if ok {
		if v, ok := l.value.(*T); ok {
			l.gen = w.gen
			w.recordLocal(key)
			return v
		}
	}
	v := new(T)
	w.locals[key] = &localEntry{value: v, gen: w.gen}
	w.recordLocal(key)
	return v
}

type localEntry struct {
	value any
	gen   int
}

var viewBuilds = struct {
	sync.RWMutex
	windows map[uint32]*Window
}{windows: map[uint32]*Window{}}

func (w *Window) withBuildWindow(build func() *El) *El {
	// Offscreen views can build on different threads, and a view can build
	// another view on the same thread before returning.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	thread := currentThreadID()
	viewBuilds.Lock()
	prev := viewBuilds.windows[thread]
	viewBuilds.windows[thread] = w
	viewBuilds.Unlock()
	defer func() {
		viewBuilds.Lock()
		if prev == nil {
			delete(viewBuilds.windows, thread)
		} else {
			viewBuilds.windows[thread] = prev
		}
		viewBuilds.Unlock()
	}()
	return build()
}

// CurrentWindow returns the window whose view is being built, or nil outside
// View. Components keep it for handlers that act on the window, such as
// scrolling a row into view.
func CurrentWindow() *Window {
	thread := currentThreadID()
	viewBuilds.RLock()
	w := viewBuilds.windows[thread]
	viewBuilds.RUnlock()
	return w
}
