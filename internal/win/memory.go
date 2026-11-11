//go:build windows

package win

import (
	"sync"

	"golang.org/x/sys/windows"
)

var (
	pEnumDisplayMonitors              = user32.NewProc("EnumDisplayMonitors")
	pCreateMemoryResourceNotification = kernel32.NewProc("CreateMemoryResourceNotification")

	monitorsMu   sync.Mutex
	monitorRects []RECT
	// The callback is made once: Windows allows a process only a limited
	// number of them.
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
