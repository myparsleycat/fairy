package fairy

import "testing"

// A ticker runs once a frame until it returns false, keeps frames coming
// meanwhile, builds the view in its frame when it calls Update, and leaves
// the window able to ask for frames afterwards.
func TestTickRunsUntilFalse(t *testing.T) {
	w := newListWindow(t, func() *El { return Box() })
	w.build()
	runs := 0
	tick := func(float32) bool {
		runs++
		w.Update()
		return runs < 3
	}
	w.Tick("t", tick)
	w.Tick("t", tick) // the same key replaces the ticker
	for frame := 1; frame <= 5; frame++ {
		w.dirty, w.animating = false, false
		w.runTicks(1.0 / 60)
		if w.needPaint {
			t.Fatalf("frame %d left needPaint set", frame)
		}
		ticking := frame < 3
		if w.animating != ticking || w.dirty != (frame <= 3) {
			t.Fatalf("frame %d: animating %v, dirty %v", frame, w.animating, w.dirty)
		}
	}
	if runs != 3 || len(w.tickers) != 0 {
		t.Fatalf("ran %d times with %d tickers left, want 3 and none", runs, len(w.tickers))
	}
}
