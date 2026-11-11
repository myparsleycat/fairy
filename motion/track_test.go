package motion

import "testing"

func TestEaseRetargetKeepsVelocity(t *testing.T) {
	m := Ease(1)
	tr := NewTrack(1, 0.001)
	tr.Set([4]float32{0})
	const h = 0.001
	tr.Step([4]float32{100}, m, 0)
	tr.Step([4]float32{100}, m, 0.3)
	before := tr.cur[0]
	tr.Step([4]float32{100}, m, h)
	vBefore := (tr.cur[0] - before) / h

	// Retarget backwards mid-flight: the value must keep moving forward at
	// the same speed before it turns around.
	before = tr.cur[0]
	tr.Step([4]float32{50}, m, h)
	vAfter := (tr.cur[0] - before) / h
	if d := vAfter - vBefore; d > 2 || d < -2 {
		t.Fatalf("velocity jumped from %.1f to %.1f on retarget", vBefore, vAfter)
	}

	for range 2000 {
		tr.Step([4]float32{50}, m, h)
	}
	if tr.moving || tr.cur[0] != 50 {
		t.Fatalf("ease did not settle on the new target: %.3f moving=%v", tr.cur[0], tr.moving)
	}
}

func TestEaseFromRestIsEaseOut(t *testing.T) {
	tr := NewTrack(1, 0.001)
	tr.Set([4]float32{0})
	tr.Step([4]float32{100}, Ease(1), 0)
	got := tr.Step([4]float32{100}, Ease(1), 0.5)[0]
	if want := float32(87.5); got < want-0.01 || got > want+0.01 {
		t.Fatalf("ease-out at half time = %.3f, want %.3f", got, want)
	}
}
