//go:build windows

package fairy

import "slices"

type ticker struct {
	key string
	f   func(dt float32) bool
	gen int // counts replacements, so a ticker replaced while it runs is kept
}

// Tick runs f before each frame is laid out, with the seconds since the
// frame before, for as long as f returns true. Use it for values the program
// advances itself, such as a simulation drawn through Live values or a
// position that follows the pointer with its own easing.
//
// Frames come at the display rate while a Tick runs, about ten a second
// while the window is covered, and not at all while it is minimized or
// hidden. dt is 0 on the first frame after the window was idle. f calls
// Update when it changed what the view shows, and that frame builds it.
//
// Tick with the same key replaces f, and starts it again after it returned
// false; a nil f stops it. View may call Tick while the value it advances is
// moving, but a call on every build starts it again with each build. Call it
// on the UI thread.
func (w *Window) Tick(key string, f func(dt float32) bool) {
	i := slices.IndexFunc(w.tickers, func(t ticker) bool { return t.key == key })
	switch {
	case f == nil:
		if i >= 0 {
			w.tickers = slices.Delete(w.tickers, i, i+1)
		}
		return
	case i >= 0:
		w.tickers[i].f = f
		w.tickers[i].gen++
	default:
		w.tickers = append(w.tickers, ticker{key: key, f: f})
	}
	w.invalidate()
}

// runTicks runs the tickers for a frame advancing animations by dt.
func (w *Window) runTicks(dt float32) {
	if len(w.tickers) == 0 {
		return
	}
	// The frame being drawn serves the invalidations of Update calls.
	w.needPaint = true
	w.tickRun = append(w.tickRun[:0], w.tickers...)
	for _, t := range w.tickRun {
		if t.f(dt) {
			continue
		}
		i := slices.IndexFunc(w.tickers, func(c ticker) bool { return c.key == t.key })
		if i >= 0 && w.tickers[i].gen == t.gen {
			w.tickers = slices.Delete(w.tickers, i, i+1)
		}
	}
	clear(w.tickRun)
	w.needPaint = false
	if len(w.tickers) > 0 {
		w.animating = true
	}
}
