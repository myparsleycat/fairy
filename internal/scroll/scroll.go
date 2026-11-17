// Package scroll holds the offset of a scroll container and the rules that
// move it: smooth wheel scrolling, glides to a row or the bottom, touch
// flings, wheel gesture latching, autoscroll speed and scroll bar geometry.
package scroll

import (
	"math"
	"time"

	"github.com/myparsleycat/fairy/motion"
)

// Edge is how a row that ScrollToRow reveals is lined up with the view.
type Edge uint8

const (
	EdgeUndecided Edge = iota
	EdgeTop
	EdgeBottom
	EdgeKeep
)

// State is a scroll container's offset and the motion moving it.
type State struct {
	ScrollX float32
	ScrollY float32
	// Wheel scrolling animates toward these; other scrolls set both.
	ScrollTX, ScrollTY float32
	Smooth             bool
	EndY               float32 // the bottom offset when last placed, for StickToBottom
	Reveal             int     // the row ScrollToRow is bringing into view
	Revealing          bool
	RevealEdge         Edge    // how the row is lined up with the view
	RevealOff          float32 // for EdgeKeep, the row's distance below the top of the view
	RevealInstant      bool    // the ScrollToRow request jumps instead of gliding
	ToBottom           bool    // a ScrollToBottom request waits to be applied
	Glide              bool    // smooth scrolling runs on GlideMotion, as for ScrollToRow
	RevealGlide        bool    // gliding to Reveal, retargeted as rows are measured
	BottomGlide        bool    // gliding to the bottom, retargeted as content grows
	Fling              bool    // a touch pan let go and the view coasts at FlingVX, FlingVY
	FlingVX, FlingVY   float32 // logical pixels per second
	BarScroll          float32 // ScrollY when the thumb was last placed
	BarScrollX         float32 // ScrollX when the thumb was last placed
	ScrolledAt         time.Time

	track *motion.Track // the offset shown, easing toward ScrollTX, ScrollTY
}

// GlideMotion carries the view to a row or the bottom; it is gentler than
// the wheel's motion.Snappy.
var GlideMotion = motion.Spring(0.5, 0)

// GlideReach is how many views away from its end a glide starts at most, so a
// jump across a long list glides only the last stretch.
const GlideReach = 2

func (s *State) offsetTrack() *motion.Track {
	if s.track == nil {
		s.track = motion.NewTrack(2, 0.1)
	}
	return s.track
}

// ResetMotion forgets the eased offset, as for an element that appears afresh.
func (s *State) ResetMotion() { s.track = nil }

// Base is the offset a new scroll step starts from: where smooth scrolling is
// heading while it runs, else the current offset.
func (s *State) Base() (float32, float32) {
	if s.Smooth {
		return s.ScrollTX, s.ScrollTY
	}
	return s.ScrollX, s.ScrollY
}

// StopGlide ends a glide or a fling.
func (s *State) StopGlide() {
	s.Glide, s.RevealGlide, s.BottomGlide, s.Fling = false, false, false, false
}

// ShiftY moves the offset, and any wheel scrolling toward a target, by d.
func (s *State) ShiftY(d float32) {
	s.ScrollY += d
	s.ScrollTY += d
	if t := s.track; t != nil && t.Initialized() {
		t.Shift([4]float32{0, d})
	}
}

// MoveY moves the vertical offset to y at once and reports whether it changed.
func (s *State) MoveY(y float32) bool {
	if y == s.ScrollY && !s.Smooth {
		return false
	}
	s.ScrollY, s.ScrollTX, s.ScrollTY, s.Smooth = y, s.ScrollX, y, false
	s.StopGlide()
	s.offsetTrack().Set([4]float32{s.ScrollX, y})
	return true
}

// Set moves the offset to (x, y), kept within (0, 0) and (maxX, maxY), and
// reports whether it changed.
func (s *State) Set(x, y, maxX, maxY float32) bool {
	x = min(max(x, 0), max(maxX, 0))
	y = min(max(y, 0), max(maxY, 0))
	if x == s.ScrollX && y == s.ScrollY {
		return false
	}
	s.ScrollX, s.ScrollY = x, y
	s.Smooth = false
	s.StopGlide()
	return true
}

// Wheel moves the target offset by (dx, dy); the offset follows it smoothly.
// It reports whether the target moved.
func (s *State) Wheel(dx, dy, maxX, maxY float32) bool {
	maxX, maxY = max(maxX, 0), max(maxY, 0)
	bx, by := s.Base()
	tx := min(max(bx+dx, 0), maxX)
	ty := min(max(by+dy, 0), maxY)
	if tx == bx && ty == by {
		return false
	}
	if s.Glide || s.Fling {
		// The wheel takes over from a glide or a fling, from where the view is now.
		s.ScrollTX, s.ScrollTY = s.ScrollX, s.ScrollY
		tx = min(max(s.ScrollX+dx, 0), maxX)
		ty = min(max(s.ScrollY+dy, 0), maxY)
		s.StopGlide()
	}
	s.ScrollTX, s.ScrollTY, s.Smooth = tx, ty, true
	return true
}

// StartGlide turns a jump of the vertical offset, from from to where it is
// now, into a glide that starts at most GlideReach views of viewH away. It
// reports whether a glide started.
func (s *State) StartGlide(from, viewH float32, instant bool) bool {
	to := s.ScrollY
	if instant || abs(to-from) < 1 {
		return false
	}
	reach := GlideReach * viewH
	start := min(max(from, to-reach), to+reach)
	s.ScrollY, s.ScrollTY, s.Smooth, s.Glide = start, to, true, true
	s.offsetTrack().Set([4]float32{s.ScrollX, start})
	return true
}

// RevealTarget returns the offset that shows content from top to top+h in a
// view viewH tall that scrolls up to maxY. The first call for a ScrollToRow
// request picks the least distance, lining the span up with the top or bottom
// edge of the view, or leaving it where it is; later calls, made as the rows
// around it are measured, keep that.
func (s *State) RevealTarget(top, h, viewH, maxY float32) float32 {
	if s.RevealEdge == EdgeUndecided {
		_, y := s.Base()
		switch {
		case top < y || h > viewH:
			s.RevealEdge = EdgeTop
		case top+h > y+viewH:
			s.RevealEdge = EdgeBottom
		default:
			s.RevealEdge, s.RevealOff = EdgeKeep, top-y
		}
	}
	var y float32
	switch s.RevealEdge {
	case EdgeTop:
		y = top
	case EdgeBottom:
		y = top + h - viewH
	default:
		y = top - s.RevealOff
	}
	return min(max(y, 0), max(maxY, 0))
}

// Step clamps the offset to (maxX, maxY) and advances smooth scrolling or a
// fling by dt seconds. stickBottom keeps a view that was at the bottom there
// as maxY changes; reduced makes smooth scrolling arrive at once. moved
// reports that motion moved the offset this frame, and moving that it goes
// on.
func (s *State) Step(maxX, maxY, dt float32, stickBottom, reduced bool) (moved, moving bool) {
	maxX, maxY = max(maxX, 0), max(maxY, 0)
	if stickBottom && maxY != s.EndY {
		if _, by := s.Base(); by >= s.EndY-0.5 {
			s.ShiftY(maxY - s.EndY)
		}
	}
	if s.Smooth {
		s.Fling = false // the wheel or a glide took over
	}
	if s.Fling {
		return true, s.stepFling(maxX, maxY, dt)
	}
	if s.Smooth {
		if s.BottomGlide {
			s.ScrollTY = maxY // content that grows on the way extends the glide
		}
		s.ScrollTX = min(max(s.ScrollTX, 0), maxX)
		s.ScrollTY = min(max(s.ScrollTY, 0), maxY)
		m := motion.Snappy
		if s.Glide {
			m = GlideMotion
		}
		if reduced {
			m = motion.Instant
		}
		tr := s.offsetTrack()
		v := tr.Step([4]float32{s.ScrollTX, s.ScrollTY}, m, dt)
		s.ScrollX, s.ScrollY = min(max(v[0], 0), maxX), min(max(v[1], 0), maxY)
		moving = tr.Moving()
		if !moving {
			s.Smooth = false
			s.StopGlide()
		}
		return true, moving
	}
	s.ScrollX = min(max(s.ScrollX, 0), maxX)
	s.ScrollY = min(max(s.ScrollY, 0), maxY)
	s.ScrollTX, s.ScrollTY = s.ScrollX, s.ScrollY
	s.offsetTrack().Set([4]float32{s.ScrollX, s.ScrollY})
	return false, false
}

// A fling slows by flingFriction every millisecond, as Firefox's
// apz.fling_friction. Speeds are in logical pixels per second.
const (
	flingFriction = 0.002
	flingMinStart = 500
	flingStop     = 10
	flingMax      = 8000
)

// flingDecay is flingFriction as an exponential decay rate per second.
var flingDecay = -math.Log1p(-flingFriction) * 1000

// StartFling keeps the view moving at (vx, vy) logical pixels per second once
// a pan lets go, slowing by flingFriction. It reports whether the release was
// fast enough to fling.
func (s *State) StartFling(vx, vy float32) bool {
	speed := float32(math.Hypot(float64(vx), float64(vy)))
	if speed < flingMinStart {
		return false
	}
	if speed > flingMax {
		vx, vy = vx*flingMax/speed, vy*flingMax/speed
	}
	s.ScrollTX, s.ScrollTY, s.Smooth = s.ScrollX, s.ScrollY, false
	s.StopGlide()
	s.Fling, s.FlingVX, s.FlingVY = true, vx, vy
	return true
}

// stepFling advances a fling by dt seconds and reports whether it goes on.
// Integrating the exponential decay keeps the distance the same at any frame
// rate.
func (s *State) stepFling(maxX, maxY, dt float32) bool {
	if dt > 0 {
		left := math.Exp(-flingDecay * float64(dt))
		travel := float32((1 - left) / flingDecay)
		x := s.ScrollX + s.FlingVX*travel
		y := s.ScrollY + s.FlingVY*travel
		s.FlingVX *= float32(left)
		s.FlingVY *= float32(left)
		// An edge stops the fling along its axis.
		if x <= 0 && s.FlingVX < 0 || x >= maxX && s.FlingVX > 0 {
			s.FlingVX = 0
		}
		if y <= 0 && s.FlingVY < 0 || y >= maxY && s.FlingVY > 0 {
			s.FlingVY = 0
		}
		s.ScrollX, s.ScrollY = min(max(x, 0), maxX), min(max(y, 0), maxY)
	}
	moving := math.Hypot(float64(s.FlingVX), float64(s.FlingVY)) >= flingStop
	if !moving {
		s.Fling = false
	}
	s.ScrollTX, s.ScrollTY = s.ScrollX, s.ScrollY
	s.offsetTrack().Set([4]float32{s.ScrollX, s.ScrollY})
	return moving
}

func abs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
