package fairy

import "testing"

// A view that sizes one element by another's OnLayout report shows the
// measured size in the same frame, without waiting for an unrelated rebuild.
func TestOnLayoutChangeRebuildsInSameFrame(t *testing.T) {
	var measured float32
	var follower *El
	w := newListWindow(t, func() *El {
		follower = Box().W(10).H(measured)
		return Col(Box().Size(30, 24).OnLayout(func(_, h float32) { measured = h }), follower)
	})
	frame := func() {
		for i := 0; i < 3; i++ {
			if w.dirty {
				w.build()
			}
			w.placeAll()
			if !w.dirty {
				break
			}
		}
	}

	frame()
	if follower.node.H != 24 {
		t.Fatalf("follower height after the first frame = %v, want the measured 24", follower.node.H)
	}
	frame()
	if w.dirty {
		t.Fatal("an unchanged OnLayout size asked for another build")
	}
}
