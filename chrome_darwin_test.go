package fairy

import (
	"math"
	"testing"

	"github.com/myparsleycat/fairy/internal/darwin"
)

// A frameless window keeps the traffic lights, centers them in the title
// band it draws, and owns the title bar: its DragRegion moves the window and
// its buttons take clicks.
func TestDarwinFramelessTitleBar(t *testing.T) {
	var w *Window
	var err error
	Do(func() {
		w, err = NewWindow(Options{Width: 400, Height: 300, StartHidden: true, Frameless: true, View: func(*Cx) *El {
			return Col(
				Row(Spacer(), Box().Size(40, 24).OnClick(func() {})).H(48).WFull().DragRegion(),
				Box().Grow(1),
			).WFull().HFull()
		}})
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Do(w.closeNow) })
	var inset float32
	var center, contentH float64
	var opaque darwin.CGRect
	var drag, button bool
	Do(func() {
		w.renderFrame()
		w.placeControls()
		inset = w.ControlsInset()
		b, ok := w.windowButtons()
		if !ok {
			t.Fatal("no window buttons")
		}
		f := darwin.Get[darwin.CGRect](b[0], "frame")
		f = darwin.Get[darwin.CGRect](darwin.Send(b[0], "superview"), "convertRect:toView:", f, darwin.ID(0))
		center = f.Origin.Y + f.Size.H/2
		contentH = darwin.Get[darwin.CGRect](w.view, "bounds").Size.H
		opaque = darwin.Get[darwin.CGRect](w.view, "_opaqueRectForWindowMoveWhenInTitlebar")
		drag, button = w.inDragRegion(200, 20), w.inDragRegion(380, 10)
	})
	if inset < 60 || inset > 120 {
		t.Errorf("ControlsInset = %v", inset)
	}
	// Window coordinates have y up: the band's middle is 24 below the top.
	if math.Abs(center-(contentH-24)) > 1 {
		t.Errorf("close button center y = %v, want %v", center, contentH-24)
	}
	if opaque.Size.W != 400 || opaque.Size.H != contentH {
		t.Errorf("title bar the view owns = %v", opaque)
	}
	if !drag || button {
		t.Errorf("drag region at the band = %v, at its button = %v", drag, button)
	}
}
