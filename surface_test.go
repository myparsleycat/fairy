package fairy

import "testing"

func TestSurfaceNeedsDraw(t *testing.T) {
	v := func(n uint64) *uint64 { return &n }
	drawn := surfaceEntry{drawn: true, version: 3, scale: 1.5, gen: 2}
	cases := []struct {
		name        string
		se          surfaceEntry
		retain      *uint64
		scale       float32
		gen         uint64
		invalidated bool
		want        bool
	}{
		{"not retained", drawn, nil, 1.5, 2, false, true},
		{"unchanged", drawn, v(3), 1.5, 2, false, false},
		{"new version", drawn, v(4), 1.5, 2, false, true},
		{"never drawn", surfaceEntry{version: 3, scale: 1.5, gen: 2}, v(3), 1.5, 2, false, true},
		{"scale changed", drawn, v(3), 2, 2, false, true},
		{"device recreated", drawn, v(3), 1.5, 3, false, true},
		{"invalidated", drawn, v(3), 1.5, 2, true, true},
		{"asked again", surfaceEntry{drawn: true, again: true, version: 3, scale: 1.5, gen: 2}, v(3), 1.5, 2, false, true},
	}
	for _, c := range cases {
		if got := surfaceNeedsDraw(&c.se, c.retain, c.scale, c.gen, c.invalidated); got != c.want {
			t.Errorf("%s: surfaceNeedsDraw = %v, want %v", c.name, got, c.want)
		}
	}
}
