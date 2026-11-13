package fairy

// Files dragged from Finder and other apps: the view is an
// NSDraggingDestination for file URLs.

import (
	"github.com/ebitengine/purego/objc"

	"github.com/myparsleycat/fairy/internal/darwin"
)

const (
	nsDragOperationNone = 0
	nsDragOperationCopy = 1
)

// addDropTarget makes the view class take dragged files.
func addDropTarget(c objc.Class) {
	type id = darwin.ID
	type sel = darwin.SEL
	op := func(copy bool) uint64 {
		if copy {
			return nsDragOperationCopy
		}
		return nsDragOperationNone
	}
	add := func(name, types string, fn any) {
		if !c.AddMethod(darwin.Sel(name), objc.NewIMP(fn), types) {
			panic("fairy: add method " + name)
		}
	}
	add("draggingEntered:", "Q@:@", func(self id, _ sel, info id) uint64 {
		w := viewWindow(self)
		if w == nil {
			return nsDragOperationNone
		}
		x, y := w.dragPoint(info)
		return op(w.fileDragEnter(draggedPaths(info), x, y))
	})
	add("draggingUpdated:", "Q@:@", func(self id, _ sel, info id) uint64 {
		w := viewWindow(self)
		if w == nil {
			return nsDragOperationNone
		}
		return op(w.fileDragOver(w.dragPoint(info)))
	})
	add("draggingExited:", "v@:@", func(self id, _ sel, info id) {
		if w := viewWindow(self); w != nil {
			w.fileDragLeave()
		}
	})
	add("prepareForDragOperation:", "B@:@", func(self id, _ sel, info id) bool { return viewWindow(self) != nil })
	add("performDragOperation:", "B@:@", func(self id, _ sel, info id) bool {
		w := viewWindow(self)
		if w == nil {
			return false
		}
		x, y := w.dragPoint(info)
		return w.fileDrop(draggedPaths(info), x, y)
	})
	add("wantsPeriodicDraggingUpdates", "B@:", func(id, sel) bool { return false })
}

// registerDrop makes the window's view accept dragged files.
func (w *Window) registerDrop() {
	types := darwin.SendClass("NSArray", "arrayWithObject:", darwin.SymbolID(darwin.AppKit, "NSPasteboardTypeFileURL"))
	darwin.Send(w.view, "registerForDraggedTypes:", types)
}

// dragPoint is where a drag is, in logical view coordinates.
func (w *Window) dragPoint(info darwin.ID) (float32, float32) {
	return w.viewPoint(darwin.Get[darwin.CGPoint](info, "draggingLocation"))
}

// draggedPaths returns the paths of the files a drag carries.
func draggedPaths(info darwin.ID) []string {
	var paths []string
	darwin.WithPool(func() {
		pb := darwin.Send(info, "draggingPasteboard")
		classes := darwin.SendClass("NSArray", "arrayWithObject:", darwin.ID(darwin.Cls("NSURL")))
		opts := darwin.SendClass("NSDictionary", "dictionaryWithObject:forKey:",
			darwin.SendClass("NSNumber", "numberWithBool:", true),
			darwin.SymbolID(darwin.AppKit, "NSPasteboardURLReadingFileURLsOnlyKey"))
		urls := darwin.Send(pb, "readObjectsForClasses:options:", classes, opts)
		if urls == 0 {
			return
		}
		for i := range darwin.Get[uint64](urls, "count") {
			u := darwin.Send(urls, "objectAtIndex:", i)
			if p := darwin.GoString(darwin.Send(u, "path")); p != "" {
				paths = append(paths, p)
			}
		}
	})
	return paths
}
