// Package tooltip keeps the state of a window's tooltip and times how a
// styled tooltip eases in when shown and out when hidden.
package tooltip

import (
	"time"

	"github.com/myparsleycat/fairy/internal/geom"
	"github.com/myparsleycat/fairy/motion"
)

// Timing is how a styled tooltip moves. The zero Timing shows and hides it
// at once.
type Timing struct {
	d     time.Duration
	curve motion.Spec
}

// NewTiming returns the timing of a tooltip that moves by m. It shows and
// hides at once when m is nil, instant or has no duration, or when reduced is
// set.
func NewTiming(m *motion.Spec, reduced bool) Timing {
	if m == nil || m.IsInstant() || m.Duration() <= 0 || reduced {
		return Timing{}
	}
	return Timing{d: time.Duration(float64(m.Duration()) * float64(time.Second)), curve: *m}
}

// Duration is how long the motion takes.
func (t Timing) Duration() time.Duration { return t.d }

// At returns how far the motion has come after elapsed, from 0 to 1.
func (t Timing) At(elapsed time.Duration) float32 {
	if t.d <= 0 || elapsed >= t.d {
		return 1
	}
	x := float32(max(elapsed, 0)) / float32(t.d)
	if t.curve.IsBezier() {
		y, _ := t.curve.BezierAt(x)
		return y
	}
	u := 1 - x
	return 1 - u*u*u
}

// ElapsedAt returns how long the motion takes to reach progress p, the
// inverse of At.
func (t Timing) ElapsedAt(p float32) time.Duration {
	lo, hi := time.Duration(0), t.d
	for range 24 {
		mid := (lo + hi) / 2
		if t.At(mid) < p {
			lo = mid
		} else {
			hi = mid
		}
	}
	return hi
}

// State is the tooltip a window shows or is hiding.
type State struct {
	Shown   bool
	Text    string
	Anchor  geom.Rect // the element a styled tooltip points at
	Side    geom.Side // where TooltipSide placed the tooltip
	SideSet bool      // the element set TooltipSide
	At      time.Time // when the styled tooltip showed
	Left    time.Time // when it began to leave; zero while shown or gone
	From    float32   // its progress when it began to leave
}

// Visible reports whether the tooltip is shown or still leaving.
func (s *State) Visible() bool {
	return s.Text != "" && (s.Shown || !s.Left.IsZero())
}

// Progress returns how much of a styled tooltip shows at now, from 0 to 1.
func (s *State) Progress(now time.Time, t Timing) float32 {
	if !s.Shown {
		if s.Left.IsZero() {
			return 0
		}
		return s.From * (1 - t.At(now.Sub(s.Left)))
	}
	return t.At(now.Sub(s.At))
}

// Show shows text at anchor. The same tooltip shown again while it leaves
// turns around from where it has faded to.
func (s *State) Show(text string, anchor geom.Rect, side geom.Side, sideSet bool, now time.Time, t Timing) {
	if s.Left.IsZero() || s.Text != text || s.Anchor != anchor {
		s.At = now
	} else {
		s.At = now.Add(-t.ElapsedAt(s.Progress(now, t)))
	}
	s.Text, s.Shown, s.Left = text, true, time.Time{}
	s.Anchor, s.Side, s.SideSet = anchor, side, sideSet
}

// Hide starts hiding the tooltip, letting it fade out when fades is set, and
// reports whether it was shown.
func (s *State) Hide(now time.Time, t Timing, fades bool) bool {
	if !s.Shown {
		return false
	}
	if fades {
		s.From = s.Progress(now, t) // read while it still counts as shown
		s.Left = now
	}
	s.Shown = false
	return true
}
