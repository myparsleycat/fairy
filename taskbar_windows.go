package fairy

import (
	"unsafe"

	"github.com/myparsleycat/fairy/internal/com"
	"github.com/myparsleycat/fairy/internal/win"
)

var (
	clsidTaskbarList = com.MustGUID("56fdf344-fd6d-11d0-958a-006097c9a090")
	iidITaskbarList3 = com.MustGUID("ea1afb91-9e28-4b86-90e9-9e9f8a5eefaf")
	taskbarList      unsafe.Pointer // lives in the apartment of the running Main
)

func taskbar() unsafe.Pointer {
	if taskbarList == nil {
		p, hr := win.CoCreateInstance(unsafe.Pointer(&clsidTaskbarList), unsafe.Pointer(&iidITaskbarList3))
		if hr < 0 {
			return nil
		}
		if com.HR(p, 3).Failed() { // HrInit
			com.Release(p)
			return nil
		}
		taskbarList = p
	}
	return taskbarList
}

// releaseTaskbar runs before Main uninitializes OLE, which unloads the
// object's DLL.
func releaseTaskbar() {
	if taskbarList != nil {
		com.Release(taskbarList)
		taskbarList = nil
	}
}

// SetProgress shows progress on the window's taskbar button. value is 0 to 1.
// It may be called from any goroutine.
func (w *Window) SetProgress(state Progress, value float64) {
	Go(func() {
		tb := taskbar()
		if tb == nil || w.r == nil {
			return
		}
		flags := [...]uintptr{0, 2, 8, 4, 1}[state] // TBPF_NOPROGRESS, NORMAL, PAUSED, ERROR, INDETERMINATE
		if state != ProgressNone && state != ProgressIndeterminate {
			const total = 10000
			done := uint64(min(max(value, 0), 1) * total)
			com.HR(tb, 9, uintptr(w.hwnd), uintptr(done), total) // SetProgressValue
		}
		com.HR(tb, 10, uintptr(w.hwnd), flags) // SetProgressState
	})
}
