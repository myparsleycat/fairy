//go:build windows

package win

import (
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

var (
	pEnumDisplayMonitors              = user32.NewProc("EnumDisplayMonitors")
	pCreateMemoryResourceNotification = kernel32.NewProc("CreateMemoryResourceNotification")

	monitorsMu   sync.Mutex
	monitorRects []RECT
	// The callback is made once: the Go runtime allows a process only a
	// limited number of windows.NewCallback callbacks.
	monitorEnumProc = sync.OnceValue(func() uintptr {
		return windows.NewCallback(func(m, hdc uintptr, rc *RECT, lp uintptr) uintptr {
			monitorRects = append(monitorRects, *rc)
			return 1
		})
	})
)

// MonitorRects returns the rectangles of the attached monitors in virtual
// screen coordinates, or nil when they cannot be enumerated.
func MonitorRects() []RECT {
	monitorsMu.Lock()
	defer monitorsMu.Unlock()
	monitorRects = nil
	if r, _, _ := pEnumDisplayMonitors.Call(0, 0, monitorEnumProc(), 0); r == 0 {
		return nil
	}
	return monitorRects
}

const LowMemoryResourceNotification = 0

// CreateMemoryResourceNotification returns an event that is signaled while
// the system's available physical memory is low (or high), or 0 on failure.
func CreateMemoryResourceNotification(kind uint32) windows.Handle {
	r, _, _ := pCreateMemoryResourceNotification.Call(uintptr(kind))
	return windows.Handle(r)
}

// ScreenPixels returns the area of all attached monitors in device pixels.
func ScreenPixels() int64 {
	var n int64
	for _, r := range MonitorRects() {
		n += int64(r.Right-r.Left) * int64(r.Bottom-r.Top)
	}
	return n
}

// WatchLowMemory calls onLow, on a goroutine of its own, whenever Windows
// reports that physical memory is low, and then waits interval before
// looking again: the notification stays signaled for as long as memory is
// low. Each call starts another watcher.
func WatchLowMemory(interval time.Duration, onLow func()) {
	h := CreateMemoryResourceNotification(LowMemoryResourceNotification)
	if h == 0 {
		return
	}
	go func() {
		for {
			if ev, err := windows.WaitForSingleObject(h, windows.INFINITE); err != nil || ev != windows.WAIT_OBJECT_0 {
				return
			}
			onLow()
			time.Sleep(interval)
		}
	}()
}
