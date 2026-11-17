// Package dragdrop registers a window as an OLE drop target for files.
package dragdrop

import (
	"syscall"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/com"
	"github.com/myparsleycat/fairy/internal/win"
)

// Handler receives the file drags over a window. Points are in screen
// pixels. Enter, Over and Drop return the DROPEFFECT to show. OLE calls them
// on the UI thread, inside its drag loop.
type Handler interface {
	Enter(paths []string, pt win.POINT) uint32
	Over(pt win.POINT) uint32
	Leave()
	Drop(paths []string, pt win.POINT) uint32
}

// target is an IDropTarget registered for one window.
type target struct {
	vtbl *[7]uintptr
	hwnd win.HWND
}

var (
	vtbl           [7]uintptr
	iidIDropTarget = com.MustGUID("00000122-0000-0000-c000-000000000046")
	iidIUnknown    = com.MustGUID("00000000-0000-0000-c000-000000000046")
	formatHDROP    = win.FORMATETC{Format: win.CF_HDROP, Aspect: win.DVASPECT_CONTENT, Index: -1, Tymed: win.TYMED_HGLOBAL}
	// targets keeps the objects OLE points to reachable; handlers holds
	// what each one calls. Both are used only on the UI thread.
	targets  = map[win.HWND]*target{}
	handlers = map[win.HWND]Handler{}
)

func init() {
	vtbl = [7]uintptr{
		syscall.NewCallback(queryInterface),
		syscall.NewCallback(addRef),
		syscall.NewCallback(release),
		syscall.NewCallback(dragEnter),
		syscall.NewCallback(dragOver),
		syscall.NewCallback(dragLeave),
		syscall.NewCallback(drop),
	}
}

// Register makes hwnd accept dropped files, passing drags to h.
func Register(hwnd win.HWND, h Handler) {
	t := &target{vtbl: &vtbl, hwnd: hwnd}
	if win.RegisterDragDrop(hwnd, unsafe.Pointer(t)) >= 0 {
		targets[hwnd] = t
		handlers[hwnd] = h
	}
}

// Revoke undoes Register.
func Revoke(hwnd win.HWND) {
	if targets[hwnd] != nil {
		win.RevokeDragDrop(hwnd)
		delete(targets, hwnd)
		delete(handlers, hwnd)
	}
}

func handler(this uintptr) Handler {
	return handlers[(*target)(com.Ptr(this)).hwnd]
}

func queryInterface(this, riid, out uintptr) uintptr {
	iid := *(*com.GUID)(com.Ptr(riid))
	if iid == iidIUnknown || iid == iidIDropTarget {
		*(*uintptr)(com.Ptr(out)) = this
		return 0
	}
	*(*uintptr)(com.Ptr(out)) = 0
	return 0x80004002 // E_NOINTERFACE
}

func addRef(this uintptr) uintptr  { return 1 }
func release(this uintptr) uintptr { return 1 }

// paths reads CF_HDROP from an IDataObject.
func paths(dataObj uintptr) []string {
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

// point unpacks a POINTL passed by value: x in the low half, y in the high
// half.
func point(pt uintptr) win.POINT {
	return win.POINT{X: int32(uint32(pt)), Y: int32(uint32(pt >> 32))}
}

func dragEnter(this, dataObj, keys, pt, effect uintptr) uintptr {
	h := handler(this)
	if h == nil {
		setEffect(effect, win.DROPEFFECT_NONE)
		return 0
	}
	setEffect(effect, h.Enter(paths(dataObj), point(pt)))
	return 0
}

func dragOver(this, keys, pt, effect uintptr) uintptr {
	h := handler(this)
	if h == nil {
		setEffect(effect, win.DROPEFFECT_NONE)
		return 0
	}
	setEffect(effect, h.Over(point(pt)))
	return 0
}

func dragLeave(this uintptr) uintptr {
	if h := handler(this); h != nil {
		h.Leave()
	}
	return 0
}

func drop(this, dataObj, keys, pt, effect uintptr) uintptr {
	h := handler(this)
	if h == nil {
		setEffect(effect, win.DROPEFFECT_NONE)
		return 0
	}
	setEffect(effect, h.Drop(paths(dataObj), point(pt)))
	return 0
}
