package motion

import "testing"

func TestBezierFollowsCSSCurve(t *testing.T) {
	m := Bezier(1, 0.25, 0.1, 0.25, 1) // CSS ease
	tr := NewTrack(1, 0.001)
	tr.Set([4]float32{0})
	tr.Step([4]float32{100}, m, 0)
	// CSS ease is 80.24% of the way at half time.
	if got := tr.Step([4]float32{100}, m, 0.5)[0]; got < 80.0 || got > 80.5 {
		t.Fatalf("ease at half time = %.3f, want 80.24", got)
	}
	tr.Step([4]float32{100}, m, 0.5)
	if tr.moving || tr.cur[0] != 100 {
		t.Fatalf("bezier did not land at its duration: %.3f moving=%v", tr.cur[0], tr.moving)
	}
}

// Springs settle far from the origin, where float32 cannot hold the last
// fraction of a pixel, and stay stable at the shortest duration Spring takes.
func TestSpringSettles(t *testing.T) {
	cases := []struct {
		m        Spec
		from, to float32
	}{
		{Spring(0.5, 0), 400000, 401234.5},
		{Snappy, 400000, 401234.5},
		{Spring(0.01, 0), 0, 100},
		{Spring(0.01, 0.9), 0, 100},
	}
	for _, c := range cases {
		tr := NewTrack(1, 0.1)
		tr.Set([4]float32{c.from})
		far := float32(0)
		for i := 0; i < 600 && (i == 0 || tr.moving); i++ {
			v := tr.Step([4]float32{c.to}, c.m, 1.0/60)[0]
			if d := abs32(v - c.from); !(d <= far) {
				far = d
			}
		}
		if tr.moving || tr.cur[0] != c.to || !(far <= 2*abs32(c.to-c.from)) {
			t.Errorf("Spring(%v, %v) from %v to %v: at %v moving=%v, farthest %v from the start",
				c.m.duration, c.m.bounce, c.from, c.to, tr.cur[0], tr.moving, far)
		}
	}
}
