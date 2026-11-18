package render

import (
	"math"
	"testing"
)

// The extent of a turned rect covers the device pixel the shader grows its
// quad by, so that damage redraws the antialiased fringe of its edges.
func TestXformRectExtentCoversFringe(t *testing.T) {
	c, s := float32(math.Cos(math.Pi/4)), float32(math.Sin(math.Pi/4))
	p := Prim{
		Bounds: [4]float32{-10, -1, 20, 2}, Clip: NoClip, Clip2: NoClip, Kind: KindRect, Flags: FlagXform,
		Xform: [4]float32{c, s, -s, c}, Xlate: [2]float32{50, 50},
	}
	got, ok := primsBounds([]Prim{p})
	tight := xformBounds(p.Bounds, p.Xform, p.Xlate)
	if !ok || got[0] > tight[0]-1 || got[1] > tight[1]-1 || got[2] < tight[2]+2 || got[3] < tight[3]+2 {
		t.Fatalf("extent %v does not cover a pixel around %v", got, tight)
	}
}
