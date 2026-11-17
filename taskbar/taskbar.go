// Package taskbar shows progress on a window's taskbar button.
package taskbar

import (
	"unsafe"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/internal/apphost"
	"github.com/myparsleycat/fairy/internal/com"
	"github.com/myparsleycat/fairy/internal/win"
)

// Progress is the state of a taskbar button's progress bar.
type Progress uint8

const (
	ProgressNone          Progress = iota // no progress bar
	ProgressNormal                        // green bar at value
	ProgressPaused                        // yellow bar at value
	ProgressError                         // red bar at value
	ProgressIndeterminate                 // moving bar, value ignored
)

var (
	clsidTaskbarList = com.MustGUID("56fdf344-fd6d-11d0-958a-006097c9a090")
	iidITaskbarList3 = com.MustGUID("ea1afb91-9e28-4b86-90e9-9e9f8a5eefaf")
	taskbarList      unsafe.Pointer // lives in the apartment of the running Main
)

func init() { apphost.OnShutdown(release) }

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

// release runs before Main uninitializes OLE, which unloads the object's DLL.
func release() {
	if taskbarList != nil {
		com.Release(taskbarList)
		taskbarList = nil
	}
}

// SetProgress shows progress on w's taskbar button. value is 0 to 1. It may
// be called from any goroutine. It does nothing once w is closed, or for a
// state that is not one of the Progress constants.
func SetProgress(w *fairy.Window, state Progress, value float64) {
	flagsOf := [...]uintptr{0, 2, 8, 4, 1} // TBPF_NOPROGRESS, NORMAL, PAUSED, ERROR, INDETERMINATE
	if int(state) >= len(flagsOf) {
		return
	}
	fairy.Go(func() {
		tb := taskbar()
		hwnd := w.HWND()
		if tb == nil || hwnd == 0 {
			return
		}
		flags := flagsOf[state]
		if state != ProgressNone && state != ProgressIndeterminate {
			const total = 10000
			done := uint64(min(max(value, 0), 1) * total)
			com.HR(tb, 9, hwnd, uintptr(done), total) // SetProgressValue
		}
		com.HR(tb, 10, hwnd, flags) // SetProgressState
	})
}
