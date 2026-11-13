package fairy

// Frameless windows on macOS keep the system's close, minimize and zoom
// buttons (the traffic lights) over a full-size content view, with the title
// bar itself transparent. The view takes over moving the window from the
// title bar, so its DragRegion elements decide where a drag moves it and the
// rest of the title bar takes clicks at once.

import (
	"github.com/myparsleycat/fairy/internal/darwin"
)

// SystemWindowControls reports whether frameless windows keep the system's
// own close, minimize and zoom buttons, as on macOS. Draw your own only
// where it is false; Window.ControlsInset is the room the system's take.
const SystemWindowControls = true

const nsFullSizeContentView = 1 << 15

// setupFrameless makes the title bar transparent and hides the title.
func (w *Window) setupFrameless() {
	darwin.Send(w.nsWindow, "setTitlebarAppearsTransparent:", true)
	darwin.Send(w.nsWindow, "setTitleVisibility:", int64(1)) // NSWindowTitleHidden
}

// windowButtons returns the close, minimize and zoom buttons.
func (w *Window) windowButtons() (b [3]darwin.ID, ok bool) {
	for i := range b {
		b[i] = darwin.Send(w.nsWindow, "standardWindowButton:", uint64(i)) // close, miniaturize, zoom
		if b[i] == 0 {
			return b, false
		}
	}
	return b, true
}

// titleBand is the height of the DragRegion along the top edge under the
// window controls, or 0.
func (w *Window) titleBand() float32 {
	for e := w.pointerHitTest(30, 1); e != nil; e = e.parent {
		if e.dragRegion {
			if e.absY <= 1 && e.h >= 20 && e.h <= 120 {
				return e.absY + e.h
			}
			return 0
		}
	}
	return 0
}

// placeControls centers the window controls in the title band the view
// draws. AppKit puts them back on some layout passes, so this runs after
// every frame and changes their frames only when they moved.
func (w *Window) placeControls() {
	if !w.opts.Frameless || w.closed || darwin.Get[uint64](w.nsWindow, "styleMask")&(1<<14) != 0 { // full screen
		return
	}
	band := w.titleBand()
	if band == 0 {
		return
	}
	b, ok := w.windowButtons()
	if !ok {
		return
	}
	closeFrame := darwin.Get[darwin.CGRect](b[0], "frame")
	gap := darwin.Get[darwin.CGRect](b[1], "frame").Origin.X - closeFrame.Origin.X - closeFrame.Size.W
	bh := closeFrame.Size.H
	y := (float64(band) - bh) / 2
	x := max(y, 7)
	if closeFrame.Origin.X == x && closeFrame.Origin.Y == y {
		return
	}
	container := darwin.Send(darwin.Send(b[0], "superview"), "superview")
	if container == 0 {
		return
	}
	cf := darwin.Get[darwin.CGRect](container, "frame")
	wh := darwin.Get[darwin.CGRect](w.nsWindow, "frame").Size.H
	cf.Size.H = float64(band)
	cf.Origin.Y = wh - cf.Size.H
	darwin.Send(container, "setFrame:", cf)
	for i, btn := range b {
		darwin.Send(btn, "setFrameOrigin:", darwin.CGPoint{X: x + float64(i)*(closeFrame.Size.W+gap), Y: y})
	}
	darwin.Send(container, "updateTrackingAreas")
	for _, btn := range b {
		darwin.Send(btn, "updateTrackingAreas")
	}
	// Views that read ControlsInset while building laid out for the
	// buttons' old place. AppKit can move them back, so rebuild only when
	// the inset differs from the one last placed.
	if inset := w.ControlsInset(); inset != w.placedInset {
		w.placedInset = inset
		w.Update()
	}
}

// ControlsInset returns how far the system's window controls reach into a
// frameless window from its left edge, in logical pixels: the traffic
// lights and the margin after them on macOS. It is 0 for windows with a
// system title bar, and on Windows, where frameless windows draw their own.
func (w *Window) ControlsInset() float32 {
	if !w.opts.Frameless || !w.hasNativeWindow() {
		return 0
	}
	b, ok := w.windowButtons()
	if !ok || darwin.Get[bool](b[2], "isHidden") {
		return 0
	}
	f := darwin.Get[darwin.CGRect](b[2], "frame")
	first := darwin.Get[darwin.CGRect](b[0], "frame")
	return float32(f.Origin.X + f.Size.W + first.Origin.X)
}

// inDragRegion reports whether a press at (x, y) moves the window. As on
// Windows, DragRegion only applies to frameless windows: a framed window
// is moved by its title bar.
func (w *Window) inDragRegion(x, y float32) bool {
	if !w.opts.Frameless {
		return false
	}
	for e := w.pointerHitTest(x, y); e != nil; e = e.parent {
		if e.interactive() {
			return false
		}
		if e.dragRegion {
			return true
		}
	}
	return false
}

// titleBarDoubleClick does what the system settings ask of a double click
// on a title bar: zoom (the default), minimize or nothing.
func (w *Window) titleBarDoubleClick() {
	var action string
	darwin.WithPool(func() {
		d := darwin.Send(darwin.SendClass("NSUserDefaults", "standardUserDefaults"), "persistentDomainForName:",
			darwin.String("NSGlobalDomain"))
		if d != 0 {
			action = darwin.GoString(darwin.Send(d, "objectForKey:", darwin.String("AppleActionOnDoubleClick")))
		}
	})
	switch action {
	case "None":
	case "Minimize":
		w.Minimize()
	default:
		w.ToggleMaximize()
	}
}

// opaqueTitleBarRect is the part of the title bar the view owns: all of it
// in a frameless window, which moves itself from its DragRegion elements.
func (w *Window) opaqueTitleBarRect() darwin.CGRect {
	if w == nil || !w.opts.Frameless {
		return darwin.CGRect{}
	}
	return darwin.Get[darwin.CGRect](w.view, "bounds")
}
