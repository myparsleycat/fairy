package fairy

import (
	"testing"

	"github.com/myparsleycat/fairy/internal/darwin"
)

// A backdrop is an NSVisualEffectView behind the content view of a window
// that no longer paints an opaque background.
func TestDarwinBackdrop(t *testing.T) {
	w := newTestWindow(t, func() *El { return Box().Size(100, 100) })
	Do(func() {
		for _, b := range []Backdrop{BackdropMica, BackdropAcrylic} {
			if err := w.SetBackdrop(b); err != nil {
				t.Fatal(err)
			}
			fx := backdropViews[w]
			subs := darwin.Send(darwin.Send(w.view, "superview"), "subviews")
			below := darwin.Get[uint64](subs, "indexOfObject:", fx) < darwin.Get[uint64](subs, "indexOfObject:", w.view)
			if fx == 0 || !below || darwin.Get[int64](fx, "material") != backdropMaterials[b] {
				t.Errorf("%v: effect view %v, below content %v", b, fx, below)
			}
			if darwin.Get[bool](w.nsWindow, "isOpaque") || !w.r.Transparent() || w.clearColor() != [4]float32{} {
				t.Errorf("%v: window opaque or renderer clears to %v", b, w.clearColor())
			}
			w.renderFrame()
		}
		if err := w.SetBackdrop(BackdropNone); err != nil {
			t.Fatal(err)
		}
		if backdropViews[w] != 0 || !darwin.Get[bool](w.nsWindow, "isOpaque") || w.r.Transparent() {
			t.Error("BackdropNone left the effect view or a transparent window")
		}
		_ = w.SetBackdrop(BackdropMica)
	})
}
