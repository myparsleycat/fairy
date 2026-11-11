package fairy

type rectF struct{ x, y, w, h float32 }

func (r rectF) contains(x, y float32) bool {
	return x >= r.x && y >= r.y && x < r.x+r.w && y < r.y+r.h
}

// containsRounded is contains for r with corner radii top-left, top-right,
// bottom-right and bottom-left, each limited to half the shorter side as when
// drawn.
func (r rectF) containsRounded(x, y float32, radii [4]float32) bool {
	if !r.contains(x, y) {
		return false
	}
	left, top := x < r.x+r.w/2, y < r.y+r.h/2
	corner := 3
	switch {
	case top && left:
		corner = 0
	case top:
		corner = 1
	case !left:
		corner = 2
	}
	rad := min(radii[corner], min(r.w, r.h)/2)
	if rad <= 0 {
		return true
	}
	// dx and dy are how far the point lies past the corner circle's center,
	// towards the corner.
	dx, dy := r.x+rad-x, r.y+rad-y
	if !left {
		dx = x - (r.x + r.w - rad)
	}
	if !top {
		dy = y - (r.y + r.h - rad)
	}
	return dx <= 0 || dy <= 0 || dx*dx+dy*dy <= rad*rad
}

func (r rectF) intersect(o rectF) rectF {
	x0, y0 := max(r.x, o.x), max(r.y, o.y)
	x1, y1 := min(r.x+r.w, o.x+o.w), min(r.y+r.h, o.y+o.h)
	return rectF{x0, y0, max(x1-x0, 0), max(y1-y0, 0)}
}

func (r rectF) empty() bool { return r.w <= 0 || r.h <= 0 }
