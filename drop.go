package fairy

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

// fileDragEnter starts a drag of files over the window. It and fileDragOver
// and fileDrop report whether the files would be copied where they are.
func (w *Window) fileDragEnter(paths []string, x, y float32) bool {
	if len(paths) == 0 {
		w.drag = nil
		return false
	}
	w.drag = &fileDrag{paths: paths}
	w.Update()
	return w.fileDragOver(x, y)
}

func (w *Window) fileDragOver(x, y float32) bool {
	if w.drag == nil {
		return false
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
		return false
	}
	return true
}

func (w *Window) fileDragLeave() {
	if w.drag != nil {
		w.drag = nil
		w.Update()
	}
}

func (w *Window) fileDrop(paths []string, x, y float32) bool {
	if w.drag == nil {
		return false
	}
	if len(paths) == 0 {
		paths = w.drag.paths
	}
	target := w.dropTargetAt(x, y)
	w.drag = nil
	w.Update()
	if target == nil || !target.accepts(paths) {
		return false
	}
	target.onFileDrop(FileDrop{Paths: paths, X: x, Y: y})
	return true
}

func (w *Window) dropTargetAt(x, y float32) *El {
	for e := w.hitTest(x, y); e != nil; e = e.parent {
		if e.onFileDrop != nil {
			return e
		}
	}
	return nil
}
