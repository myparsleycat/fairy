package fairy

import "github.com/myparsleycat/fairy/internal/darwin"

// dockProgress is the Dock tile's content while it shows progress: the app
// icon with a bar along its bottom.
var dockProgress struct {
	view, bar darwin.ID
	owner     *Window
}

// SetProgress shows progress on the app's Dock tile, which all its windows
// share: the latest call wins, and ProgressNone from that window removes
// the bar. value is 0 to 1. The Dock draws the tile as a still picture, so
// an indeterminate bar does not move, and paused and error bars keep the
// system's color. It may be called from any goroutine.
func (w *Window) SetProgress(state Progress, value float64) {
	Go(func() {
		if nsApp == 0 || w.closed {
			return
		}
		tile := darwin.Send(nsApp, "dockTile")
		if state == ProgressNone {
			if dockProgress.owner == w {
				dockProgress.owner = nil
				darwin.Send(tile, "setContentView:", darwin.ID(0))
				darwin.Send(tile, "display")
			}
			return
		}
		if dockProgress.view == 0 {
			size := darwin.Get[darwin.CGSize](tile, "size")
			v := darwin.Send(darwin.SendClass("NSImageView", "alloc"), "initWithFrame:", darwin.Rect(0, 0, size.W, size.H))
			darwin.Send(v, "setImage:", darwin.Send(nsApp, "applicationIconImage"))
			bar := darwin.Send(darwin.SendClass("NSProgressIndicator", "alloc"), "initWithFrame:",
				darwin.Rect(size.W*0.1, size.H*0.06, size.W*0.8, size.H*0.12))
			darwin.Send(bar, "setStyle:", uint64(0)) // NSProgressIndicatorStyleBar
			darwin.Send(bar, "setMinValue:", float64(0))
			darwin.Send(bar, "setMaxValue:", float64(1))
			darwin.Send(v, "addSubview:", bar)
			dockProgress.view, dockProgress.bar = v, bar
		}
		dockProgress.owner = w
		indeterminate := state == ProgressIndeterminate
		darwin.Send(dockProgress.bar, "setIndeterminate:", indeterminate)
		if !indeterminate {
			darwin.Send(dockProgress.bar, "setDoubleValue:", min(max(value, 0), 1))
		}
		darwin.Send(tile, "setContentView:", dockProgress.view)
		darwin.Send(tile, "display")
	})
}

// clearDockProgress removes the bar a closing window left on the Dock tile:
// on Windows the bar goes away with the window's taskbar button.
func (w *Window) clearDockProgress() {
	if dockProgress.owner != w || nsApp == 0 {
		return
	}
	dockProgress.owner = nil
	tile := darwin.Send(nsApp, "dockTile")
	darwin.Send(tile, "setContentView:", darwin.ID(0))
	darwin.Send(tile, "display")
}
