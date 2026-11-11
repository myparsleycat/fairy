package fairy

import (
	"testing"

	"github.com/myparsleycat/fairy/internal/render"
)

// Setting a Live value redraws with it without building the view again.
func TestLiveSetPaintsWithoutBuild(t *testing.T) {
	fill := NewLive(Hex(0xff0000))
	builds := 0
	w := newListWindow(t, func() *El {
		builds++
		return Box().Size(20, 20).FillLive(fill)
	})
	painted := func() [4]float32 {
		w.placeAll()
		w.scene.Reset()
		w.paint(w.root, 1, w.opts.TextColor)
		for _, p := range w.scene.Prims {
			if p.Kind == render.KindRect {
				return p.Color
			}
		}
		t.Fatal("no fill drawn")
		return [4]float32{}
	}
	w.build()
	painted()
	fill.Set(Hex(0x0000ff))
	if w.dirty {
		t.Fatal("Set marked the view for building")
	}
	if c := painted(); c != Hex(0x0000ff).arr(1) || builds != 1 {
		t.Fatalf("after Set: fill %v with %d builds, want blue with 1", c, builds)
	}
}
