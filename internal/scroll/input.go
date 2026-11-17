package scroll

import (
	"time"

	"github.com/myparsleycat/fairy/keyboard"
)

// WheelPageScroll is WHEEL_PAGESCROLL, the wheel setting that scrolls a
// screen at a time.
const WheelPageScroll = ^uint32(0)

// WheelStep returns how far delta notches scroll a view of the given size
// when a notch is set to scroll lines lines. A setting of 0 lines scrolls
// nothing.
func WheelStep(delta float32, lines uint32, view float32) float32 {
	if lines == WheelPageScroll {
		return delta * view
	}
	return delta * float32(lines) * 100 / 3
}

const (
	// LatchTimeout is the pause in wheel input that ends a scroll gesture, as
	// Chromium's wheel scroll latching transaction.
	LatchTimeout = 500 * time.Millisecond
	// latchSlop is how far, in logical pixels, the pointer may move during a
	// gesture before the next wheel step starts a new one.
	latchSlop = 4
)

// Latch is the scroll container a wheel gesture scrolled. Until the gesture
// pauses or the pointer moves, the wheel scrolls only it: an inner list that
// reaches its end swallows the rest of the gesture instead of handing it to
// the page, and an inner list that passes under the pointer while the page
// scrolls does not take it over. A press or key ends the gesture.
type Latch struct {
	State      *State
	X, Y       float32 // pointer position when the gesture started
	Horizontal bool
	Mods       keyboard.Mods
	At         time.Time // last wheel step of the gesture
}

// Continues reports whether a wheel step at (x, y) at now, along the given
// axis and with mods held, belongs to the latched gesture.
func (l Latch) Continues(x, y float32, now time.Time, horizontal bool, mods keyboard.Mods) bool {
	dx, dy := x-l.X, y-l.Y
	return l.State != nil && l.Horizontal == horizontal && l.Mods == mods && now.Sub(l.At) < LatchTimeout &&
		dx*dx+dy*dy <= latchSlop*latchSlop
}

// StepTarget returns the offset that shows one more of spans, the tops and
// bottoms of a container's children in order, toward the top (up) or bottom
// of a view viewH tall at offset y whose edge that way is covered by inset:
// the child before the first one shown whole lines up below the inset, or the
// child after the last one shown whole above it. Next to the first or last
// child it goes to that end.
func StepTarget(spans [][2]float32, y, viewH, maxY, inset float32, up bool) float32 {
	const tolerance = 1
	n := len(spans)
	if n == 0 {
		return y
	}
	if up {
		first := n
		for i, s := range spans {
			if s[0] >= y+inset-tolerance {
				first = i
				break
			}
		}
		if first <= 1 {
			return 0
		}
		return min(max(spans[first-1][0]-inset, 0), maxY)
	}
	last := -1
	for i := n - 1; i >= 0; i-- {
		if spans[i][1] <= y+viewH-inset+tolerance {
			last = i
			break
		}
	}
	if last >= n-2 {
		return maxY
	}
	return min(max(spans[last+1][1]-(viewH-inset), 0), maxY)
}

// ThumbLength is the length of a scroll bar thumb on a track for a view that
// shows view of content.
func ThumbLength(track, view, content float32) float32 {
	return min(track, max(track*view/content, 24))
}

// ThumbPos is how far along its track a thumb of length thumb lies at offset
// of maxOffset.
func ThumbPos(track, thumb, offset, maxOffset float32) float32 {
	return (track - thumb) * (offset / max(maxOffset, 1))
}

// TrackOffset is the offset that centers a thumb of length thumb at p along
// its track.
func TrackOffset(p, track, thumb, maxOffset float32) float32 {
	return (p - thumb/2) / max(track-thumb, 1) * maxOffset
}

// DragRatio is how far the offset moves per pixel the thumb is dragged.
func DragRatio(track, thumb, maxOffset float32) float32 {
	return maxOffset / max(track-thumb, 1)
}

// AutoDeadZone is the distance from the autoscroll press point, in logical
// pixels along each axis, within which the view does not scroll.
const AutoDeadZone = 10

// AutoSpeed is the autoscroll speed in logical pixels per second for a
// pointer d pixels from the press point along one axis; it grows faster than
// the distance so that a long reach covers long content.
func AutoSpeed(d float32) float32 {
	m := abs(d) - AutoDeadZone
	if m <= 0 {
		return 0
	}
	v := m * (5 + m/20)
	if d < 0 {
		return -v
	}
	return v
}
