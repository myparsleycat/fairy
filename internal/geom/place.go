package geom

// Side is the side of an anchor that a popover or tooltip opens on. Its
// values match fairy.Side.
type Side uint8

const (
	SideBottom Side = iota
	SideTop
	SideRight
	SideLeft
)

// Align is where a popover lines up along its anchor's edge. Its values match
// fairy.Align.
type Align uint8

const (
	AlignStart Align = iota
	AlignCenter
	AlignEnd
)

// Popover places a popover next to its anchor.
type Popover struct {
	Side        Side
	Align       Align
	Gap         float32
	AlignOffset float32
}

// Room returns the height a popover next to a can take inside wr, keeping the
// 4 pixel margin Position keeps: below or above a, whichever is taller, for
// SideBottom and SideTop.
func (o Popover) Room(a, wr Rect) float32 {
	if o.Side == SideLeft || o.Side == SideRight {
		return max(wr.H-8, 0)
	}
	below := wr.Y + wr.H - (a.Y + a.H + o.Gap) - 4
	above := a.Y - o.Gap - 4 - wr.Y
	return max(below, above, 0)
}

// Position places a popover of size cw×ch next to a, and returns the side it
// ended up on after flipping to fit.
func (o Popover) Position(a Rect, cw, ch float32, wr Rect) (float32, float32, Side) {
	along := func(start, length, size float32) float32 {
		switch o.Align {
		case AlignCenter:
			return start + (length-size)/2 + o.AlignOffset
		case AlignEnd:
			return start + length - size - o.AlignOffset
		}
		return start + o.AlignOffset
	}
	var x, y float32
	side := o.Side
	switch o.Side {
	case SideTop, SideBottom:
		x = along(a.X, a.W, cw)
		below, above := a.Y+a.H+o.Gap, a.Y-o.Gap-ch
		y = below
		if o.Side == SideTop {
			y = above
		}
		if o.Side == SideBottom && below+ch > wr.Y+wr.H && above >= wr.Y {
			y, side = above, SideTop
		} else if o.Side == SideTop && above < wr.Y && below+ch <= wr.Y+wr.H {
			y, side = below, SideBottom
		}
	default:
		y = along(a.Y, a.H, ch)
		right, left := a.X+a.W+o.Gap, a.X-o.Gap-cw
		x = right
		if o.Side == SideLeft {
			x = left
		}
		if o.Side == SideRight && right+cw > wr.W && left >= 0 {
			x, side = left, SideLeft
		} else if o.Side == SideLeft && left < 0 && right+cw <= wr.W {
			x, side = right, SideRight
		}
	}
	x = min(max(x, 4), max(wr.W-cw-4, 4))
	y = min(max(y, wr.Y+4), max(wr.Y+wr.H-ch-4, wr.Y+4))
	return x, y, side
}

// Tip puts a bw by bh tooltip gap away from side of anchor a, or on the
// opposite side when it does not fit there and the opposite side does. It is
// centered on a and kept inside an lw by lh window, below top. It returns the
// side used.
func Tip(a Rect, side Side, bw, bh, gap, lw, lh, top float32) (x, y float32, used Side) {
	minY, maxY := top+4, lh-4
	switch side {
	case SideTop:
		if a.Y-gap-bh < minY && a.Y+a.H+gap+bh <= maxY {
			side = SideBottom
		}
	case SideBottom:
		if a.Y+a.H+gap+bh > maxY && a.Y-gap-bh >= minY {
			side = SideTop
		}
	case SideRight:
		if a.X+a.W+gap+bw > lw-4 && a.X-gap-bw >= 4 {
			side = SideLeft
		}
	case SideLeft:
		if a.X-gap-bw < 4 && a.X+a.W+gap+bw <= lw-4 {
			side = SideRight
		}
	}

	// When neither side has room, the tooltip covers part of the anchor rather
	// than leaving the window.
	clampX := func(x float32) float32 { return min(max(x, 4), max(lw-bw-4, 4)) }
	clampY := func(y float32) float32 { return min(max(y, minY), max(maxY-bh, minY)) }
	cx, cy := clampX(a.X+a.W/2-bw/2), clampY(a.Y+a.H/2-bh/2)
	switch side {
	case SideTop:
		return cx, clampY(a.Y - gap - bh), side
	case SideBottom:
		return cx, clampY(a.Y + a.H + gap), side
	case SideRight:
		return clampX(a.X + a.W + gap), cy, side
	default:
		return clampX(a.X - gap - bw), cy, side
	}
}
