package fairy

import "github.com/myparsleycat/fairy/svg"

// Icon draws v as a mask tinted with the text color. Without an explicit size
// it is a square a little larger than the inherited font size.
func Icon(v *svg.Vector) *El {
	e := newEl(kindIcon)
	e.vec = v
	return e
}

// Spinner is a busy indicator: an open ring in the text color, drawn like the
// Lucide loader-circle icon, that turns once a second while it is on screen.
// Size it with Size; it defaults to the icon size.
func Spinner() *El { return newEl(kindSpinner) }

type arcProps struct {
	start, sweep, stroke float32
	round                bool
}

// Arc strokes part of a circle in the text color, centered in the element:
// sweep radians clockwise from start, where 0 is 12 o'clock. A sweep of 2π
// or more is the whole circle. Size it with Size; it defaults to the icon
// size. With Animate, changes of start and sweep move with its motion.
func Arc(start, sweep float32) *El {
	e := newEl(kindArc)
	if !finite(start) {
		start = 0
	}
	if !finite(sweep) {
		sweep = 0
	}
	if sweep < 0 {
		start, sweep = start+sweep, -sweep
	}
	e.arc = &arcProps{start: start, sweep: sweep}
	return e
}

// Stroke sets an Arc's line width in pixels. It defaults to an eighth of
// the arc's size.
func (e *El) Stroke(px float32) *El {
	if e.arc != nil {
		e.arc.stroke = max(px, 0)
	}
	return e
}

// RoundCaps rounds the ends of an Arc.
func (e *El) RoundCaps() *El {
	if e.arc != nil {
		e.arc.round = true
	}
	return e
}
