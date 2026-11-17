// Package geom holds the rectangle type and placement rules shared by
// layout, hit testing, popovers and tooltips.
package geom

type Rect struct{ X, Y, W, H float32 }

func (r Rect) Contains(x, y float32) bool {
	return x >= r.X && y >= r.Y && x < r.X+r.W && y < r.Y+r.H
}

// ContainsRounded is Contains for r with corner radii top-left, top-right,
// bottom-right and bottom-left, each limited to half the shorter side as when
// drawn.
func (r Rect) ContainsRounded(x, y float32, radii [4]float32) bool {
	if !r.Contains(x, y) {
		return false
	}
	left, top := x < r.X+r.W/2, y < r.Y+r.H/2
	corner := 3
	switch {
	case top && left:
		corner = 0
	case top:
		corner = 1
	case !left:
		corner = 2
	}
	rad := min(radii[corner], min(r.W, r.H)/2)
	if rad <= 0 {
		return true
	}
	// dx and dy are how far the point lies past the corner circle's center,
	// towards the corner.
	dx, dy := r.X+rad-x, r.Y+rad-y
	if !left {
		dx = x - (r.X + r.W - rad)
	}
	if !top {
		dy = y - (r.Y + r.H - rad)
	}
	return dx <= 0 || dy <= 0 || dx*dx+dy*dy <= rad*rad
}

func (r Rect) Intersect(o Rect) Rect {
	x0, y0 := max(r.X, o.X), max(r.Y, o.Y)
	x1, y1 := min(r.X+r.W, o.X+o.W), min(r.Y+r.H, o.Y+o.H)
	return Rect{x0, y0, max(x1-x0, 0), max(y1-y0, 0)}
}

func (r Rect) Empty() bool { return r.W <= 0 || r.H <= 0 }
