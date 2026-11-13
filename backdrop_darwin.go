package fairy

import (
	"errors"

	"github.com/myparsleycat/fairy/internal/darwin"
)

// errBackdrop reports a backdrop that macOS does not draw.
var errBackdrop = errors.New("fairy: window backdrop not supported on this platform")

// backdropMaterials are the NSVisualEffectView materials that stand in for
// the Windows backdrops: the window background material for Mica, the
// title bar's, tinted more by the wallpaper, for Mica Alt, and the popover's
// for Acrylic.
var backdropMaterials = [...]int64{
	BackdropMica:    21, // NSVisualEffectMaterialUnderWindowBackground
	BackdropMicaAlt: 3,  // NSVisualEffectMaterialTitlebar
	BackdropAcrylic: 6,  // NSVisualEffectMaterialPopover
}

// backdropViews are the NSVisualEffectViews behind the windows' content
// views.
var backdropViews = map[*Window]darwin.ID{}

// BackdropSupported reports whether macOS draws b behind a window. Every
// backdrop is drawn with NSVisualEffectView, behind the window.
func BackdropSupported(b Backdrop) bool { return b <= BackdropAcrylic }

// applyBackdrop puts an NSVisualEffectView with b's material behind the
// content view, or removes it for BackdropNone. Like the Windows backdrops
// it follows the window's appearance and is drawn while the window is
// active.
func (w *Window) applyBackdrop(b Backdrop) {
	if !w.hasNativeWindow() {
		return
	}
	darwin.WithPool(func() {
		fx := backdropViews[w]
		if b == BackdropNone || int(b) >= len(backdropMaterials) {
			if fx != 0 {
				darwin.Send(fx, "removeFromSuperview")
				darwin.Release(fx)
				delete(backdropViews, w)
			}
			darwin.Send(w.nsWindow, "setOpaque:", true)
			w.setWindowBackground()
			if !w.opts.Background.isSet() {
				darwin.Send(w.nsWindow, "setBackgroundColor:", darwin.SendClass("NSColor", "windowBackgroundColor"))
			}
			return
		}
		if fx == 0 {
			frame := darwin.Get[darwin.CGRect](w.view, "frame")
			fx = darwin.Send(darwin.SendClass("NSVisualEffectView", "alloc"), "initWithFrame:", frame)
			darwin.Send(fx, "setAutoresizingMask:", uint64(2|16))                                                     // NSViewWidthSizable | NSViewHeightSizable
			darwin.Send(fx, "setBlendingMode:", int64(0))                                                             // NSVisualEffectBlendingModeBehindWindow
			darwin.Send(fx, "setState:", int64(0))                                                                    // NSVisualEffectStateFollowsWindowActiveState
			darwin.Send(darwin.Send(w.view, "superview"), "addSubview:positioned:relativeTo:", fx, int64(-1), w.view) // NSWindowBelow
			backdropViews[w] = fx
		}
		darwin.Send(fx, "setMaterial:", backdropMaterials[b])
		darwin.Send(w.nsWindow, "setOpaque:", false)
		darwin.Send(w.nsWindow, "setBackgroundColor:", darwin.SendClass("NSColor", "clearColor"))
	})
}

// releaseBackdrop forgets the backdrop of a closing window.
func (w *Window) releaseBackdrop() {
	if fx := backdropViews[w]; fx != 0 {
		darwin.Release(fx)
		delete(backdropViews, w)
	}
}
