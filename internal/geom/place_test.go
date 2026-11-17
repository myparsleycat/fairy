package geom

import "testing"

func TestTipBesideAnchor(t *testing.T) {
	const bw, bh, gap, lw, lh, top = 40, 20, 6, 200, 100, 0
	for _, tc := range []struct {
		name   string
		anchor Rect
		side   Side
		x, y   float32
		used   Side
	}{
		{name: "right", anchor: Rect{0, 40, 20, 20}, side: SideRight, x: 26, y: 40, used: SideRight},
		{name: "right flips left", anchor: Rect{170, 40, 20, 20}, side: SideRight, x: 124, y: 40, used: SideLeft},
		{name: "right clamped to top", anchor: Rect{0, 0, 20, 10}, side: SideRight, x: 26, y: 4, used: SideRight},
		{name: "right clamped to bottom", anchor: Rect{0, 90, 20, 10}, side: SideRight, x: 26, y: 76, used: SideRight},
		{name: "top flips below", anchor: Rect{80, 10, 40, 10}, side: SideTop, x: 80, y: 26, used: SideBottom},
		{name: "right without room stays inside", anchor: Rect{0, 40, 200, 20}, side: SideRight, x: 156, y: 40,
			used: SideRight},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x, y, used := Tip(tc.anchor, tc.side, bw, bh, gap, lw, lh, top)
			if x != tc.x || y != tc.y || used != tc.used {
				t.Errorf("Tip = (%v, %v, %v), want (%v, %v, %v)", x, y, used, tc.x, tc.y, tc.used)
			}
		})
	}
}
