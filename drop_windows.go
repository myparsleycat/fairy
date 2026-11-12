package fairy

import (
	"syscall"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/com"
	"github.com/myparsleycat/fairy/internal/win"
)

// dropTarget is an IDropTarget registered for one window. OLE calls it on the
// UI thread, inside its drag loop.
type dropTarget struct {
	vtbl *[7]uintptr
	hwnd win.HWND
}

var (
	dropVtbl         [7]uintptr
	iidIDropTarget   = com.MustGUID("00000122-0000-0000-c000-000000000046")
	iidIUnknownGUID  = com.MustGUID("00000000-0000-0000-c000-000000000046")
	formatHDROP      = win.FORMATETC{Format: win.CF_HDROP, Aspect: win.DVASPECT_CONTENT, Index: -1, Tymed: win.TYMED_HGLOBAL}
	dropTargetsAlive = map[win.HWND]*dropTarget{} // keeps the objects OLE points to reachable
)

func init() {
	dropVtbl = [7]uintptr{
		syscall.NewCallback(dtQueryInterface),
		syscall.NewCallback(dtAddRef),
		syscall.NewCallback(dtRelease),
		syscall.NewCallback(dtDragEnter),
		syscall.NewCallback(dtDragOver),
		syscall.NewCallback(dtDragLeave),
		syscall.NewCallback(dtDrop),
	}
}

func (w *Window) registerDrop() {
	t := &dropTarget{vtbl: &dropVtbl, hwnd: w.hwnd}
	if win.RegisterDragDrop(w.hwnd, unsafe.Pointer(t)) >= 0 {
		dropTargetsAlive[w.hwnd] = t
	}
}

func (w *Window) revokeDrop() {
	if dropTargetsAlive[w.hwnd] != nil {
		win.RevokeDragDrop(w.hwnd)
		delete(dropTargetsAlive, w.hwnd)
	}
}

func dropWindow(this uintptr) *Window {
	return windows[(*dropTarget)(com.Ptr(this)).hwnd]
}

func dtQueryInterface(this, riid, out uintptr) uintptr {
	iid := *(*com.GUID)(com.Ptr(riid))
	if iid == iidIUnknownGUID || iid == iidIDropTarget {
		*(*uintptr)(com.Ptr(out)) = this
		return 0
	}
	*(*uintptr)(com.Ptr(out)) = 0
	return 0x80004002 // E_NOINTERFACE
}

func dtAddRef(this uintptr) uintptr  { return 1 }
func dtRelease(this uintptr) uintptr { return 1 }

// droppedPaths reads CF_HDROP from an IDataObject.
func droppedPaths(dataObj uintptr) []string {
	if dataObj == 0 {
		return nil
	}
	f := formatHDROP
	var m win.STGMEDIUM
	if com.HR(com.Ptr(dataObj), 3, uintptr(unsafe.Pointer(&f)), uintptr(unsafe.Pointer(&m))).Failed() {
		return nil
	}
	defer win.ReleaseStgMedium(&m)
	return win.DropFiles(m.Handle)
}

func setEffect(p uintptr, e uint32) { *(*uint32)(com.Ptr(p)) = e }

func dropEffect(copy bool) uint32 {
	if copy {
		return win.DROPEFFECT_COPY
	}
	return win.DROPEFFECT_NONE
}

func dtDragEnter(this, dataObj, keys, pt, effect uintptr) uintptr {
	w := dropWindow(this)
	if w == nil {
		setEffect(effect, win.DROPEFFECT_NONE)
		return 0
	}
	x, y := w.dropPoint(pt)
	setEffect(effect, dropEffect(w.fileDragEnter(droppedPaths(dataObj), x, y)))
	return 0
}

func dtDragOver(this, keys, pt, effect uintptr) uintptr {
	w := dropWindow(this)
	if w == nil {
		setEffect(effect, win.DROPEFFECT_NONE)
		return 0
	}
	x, y := w.dropPoint(pt)
	setEffect(effect, dropEffect(w.fileDragOver(x, y)))
	return 0
}

func dtDragLeave(this uintptr) uintptr {
	if w := dropWindow(this); w != nil {
		w.fileDragLeave()
	}
	return 0
}

func dtDrop(this, dataObj, keys, pt, effect uintptr) uintptr {
	w := dropWindow(this)
	if w == nil {
		setEffect(effect, win.DROPEFFECT_NONE)
		return 0
	}
	x, y := w.dropPoint(pt)
	setEffect(effect, dropEffect(w.fileDrop(droppedPaths(dataObj), x, y)))
	return 0
}

// dropPoint converts a POINTL passed by value (x in the low half, y in the
// high half) from screen pixels to logical window coordinates.
func (w *Window) dropPoint(pt uintptr) (float32, float32) {
	p := win.POINT{X: int32(uint32(pt)), Y: int32(uint32(pt >> 32))}
	win.ScreenToClient(w.hwnd, &p)
	return float32(p.X) / w.scale, float32(p.Y) / w.scale
}
