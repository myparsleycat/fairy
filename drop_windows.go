package fairy

import (
	"github.com/myparsleycat/fairy/internal/dragdrop"
	"github.com/myparsleycat/fairy/internal/win"
)

func (w *Window) registerDrop() { dragdrop.Register(w.hwnd, windowDrop{w}) }

func (w *Window) revokeDrop() { dragdrop.Revoke(w.hwnd) }

// windowDrop passes the file drags OLE reports for a window to it.
type windowDrop struct{ w *Window }

func (d windowDrop) Enter(paths []string, pt win.POINT) uint32 {
	x, y := d.w.dropPoint(pt)
	return dropEffect(d.w.fileDragEnter(paths, x, y))
}

func (d windowDrop) Over(pt win.POINT) uint32 {
	x, y := d.w.dropPoint(pt)
	return dropEffect(d.w.fileDragOver(x, y))
}

func (d windowDrop) Leave() { d.w.fileDragLeave() }

func (d windowDrop) Drop(paths []string, pt win.POINT) uint32 {
	x, y := d.w.dropPoint(pt)
	return dropEffect(d.w.fileDrop(paths, x, y))
}

// dropPoint converts a point in screen pixels to logical window coordinates.
func (w *Window) dropPoint(p win.POINT) (float32, float32) {
	win.ScreenToClient(w.hwnd, &p)
	return float32(p.X) / w.scale, float32(p.Y) / w.scale
}

// dropEffect is the OLE drop effect for whether the window takes the files.
func dropEffect(copy bool) uint32 {
	if copy {
		return win.DROPEFFECT_COPY
	}
	return win.DROPEFFECT_NONE
}
