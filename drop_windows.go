package fairy

import (
	"syscall"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/com"
	"github.com/myparsleycat/fairy/internal/win"
)

// fileDrag is the state of files being dragged over a window.
type fileDrag struct {
	paths  []string
	x, y   float32 // last pointer position
	target uint64  // element that would receive the drop, or 0
	key    string  // the target's keyboard.Key
	// rejected is set when the target's AcceptFiles turned the files down.
	rejected bool
}

// retarget finds the drop target again after the tree changed under a
// stationary pointer, such as when the view shows a drop zone.
func (w *Window) retarget() {
	if w.drag == nil {
		return
	}
	w.drag.target, w.drag.key, w.drag.rejected = 0, "", false
	if t := w.dropTargetAt(w.drag.x, w.drag.y); t != nil {
		w.drag.target, w.drag.key, w.drag.rejected = t.id, t.key, !t.accepts(w.drag.paths)
	}
}

func (e *El) accepts(paths []string) bool { return e.acceptFiles == nil || e.acceptFiles(paths) }

// FileDrag returns the paths of files being dragged over the window, or nil.
// Use it to show a drop zone while a drag is in progress.
func (cx *Cx) FileDrag() []string {
	if cx.w.drag == nil {
		return nil
	}
	return cx.w.drag.paths
}

// FileDropTarget returns the Key of the element that would receive the files
// being dragged, or "" when there is none or it has no Key. The view is built
// again when the target changes, so it can show where the files will go.
func (cx *Cx) FileDropTarget() string {
	if cx.w.drag == nil {
		return ""
	}
	return cx.w.drag.key
}

// FileDropRejected reports whether the element under files being dragged
// turned them down with AcceptFiles.
func (cx *Cx) FileDropRejected() bool { return cx.w.drag != nil && cx.w.drag.rejected }

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

func dtDragEnter(this, dataObj, keys, pt, effect uintptr) uintptr {
	w := dropWindow(this)
	if w == nil {
		setEffect(effect, win.DROPEFFECT_NONE)
		return 0
	}
	x, y := w.dropPoint(pt)
	setEffect(effect, w.fileDragEnter(droppedPaths(dataObj), x, y))
	return 0
}

func dtDragOver(this, keys, pt, effect uintptr) uintptr {
	w := dropWindow(this)
	if w == nil {
		setEffect(effect, win.DROPEFFECT_NONE)
		return 0
	}
	x, y := w.dropPoint(pt)
	setEffect(effect, w.fileDragOver(x, y))
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
	setEffect(effect, w.fileDrop(droppedPaths(dataObj), x, y))
	return 0
}

func (w *Window) fileDragEnter(paths []string, x, y float32) uint32 {
	if len(paths) == 0 {
		w.drag = nil
		return win.DROPEFFECT_NONE
	}
	w.drag = &fileDrag{paths: paths}
	w.Update()
	return w.fileDragOver(x, y)
}

func (w *Window) fileDragOver(x, y float32) uint32 {
	if w.drag == nil {
		return win.DROPEFFECT_NONE
	}
	w.drag.x, w.drag.y = x, y
	var id uint64
	key := ""
	t := w.dropTargetAt(x, y)
	if t != nil {
		id, key = t.id, t.key
	}
	if id != w.drag.target {
		w.drag.target = id
		rejected := t != nil && !t.accepts(w.drag.paths)
		if key != w.drag.key || rejected != w.drag.rejected {
			w.drag.key, w.drag.rejected = key, rejected
			w.Update()
		} else {
			w.invalidate()
		}
	}
	if id == 0 || w.drag.rejected {
		return win.DROPEFFECT_NONE
	}
	return win.DROPEFFECT_COPY
}

func (w *Window) fileDragLeave() {
	if w.drag != nil {
		w.drag = nil
		w.Update()
	}
}

func (w *Window) fileDrop(paths []string, x, y float32) uint32 {
	if w.drag == nil {
		return win.DROPEFFECT_NONE
	}
	if len(paths) == 0 {
		paths = w.drag.paths
	}
	target := w.dropTargetAt(x, y)
	w.drag = nil
	w.Update()
	if target == nil || !target.accepts(paths) {
		return win.DROPEFFECT_NONE
	}
	target.onFileDrop(FileDrop{Paths: paths, X: x, Y: y})
	return win.DROPEFFECT_COPY
}

// dropPoint converts a POINTL passed by value (x in the low half, y in the
// high half) from screen pixels to logical window coordinates.
func (w *Window) dropPoint(pt uintptr) (float32, float32) {
	p := win.POINT{X: int32(uint32(pt)), Y: int32(uint32(pt >> 32))}
	win.ScreenToClient(w.hwnd, &p)
	return float32(p.X) / w.scale, float32(p.Y) / w.scale
}

func (w *Window) dropTargetAt(x, y float32) *El {
	for e := w.hitTest(x, y); e != nil; e = e.parent {
		if e.onFileDrop != nil {
			return e
		}
	}
	return nil
}
