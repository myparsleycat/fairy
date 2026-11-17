package fairy

import (
	"math"
	"sync/atomic"

	"github.com/myparsleycat/fairy/motion"
)

// Transition is the state an element enters from or exits to: its opacity,
// offset and scale.
type Transition struct {
	Opacity float32
	X, Y    float32
	// Slide is the distance the root of a popover moves from, toward the
	// side of its anchor: from above when it opened below the anchor, from
	// the left when it opened to the right. Other elements ignore it.
	Slide float32
	// Scale is the size relative to the resting size, about the element's
	// Origin; 0 means 1 (no scaling).
	Scale  float32
	Motion motion.Spec
}

// Fade enters from (or exits to) transparent.
func Fade() Transition { return Transition{Motion: motion.Default} }

// SlideY fades in while moving dy pixels into place.
func SlideY(dy float32) Transition { return Transition{Y: dy, Motion: motion.Default} }

// SlideX fades in while moving dx pixels into place.
func SlideX(dx float32) Transition { return Transition{X: dx, Motion: motion.Default} }

// ScaleFade fades in while growing from s times the resting size, or exits
// by shrinking to it.
func ScaleFade(s float32) Transition { return Transition{Scale: s, Motion: motion.Default} }

// With returns t using motion m.
func (t Transition) With(m motion.Spec) Transition { t.Motion = m; return t }

var reducedMotion atomic.Bool

// ReducedMotion reports whether the user turned off animation effects in the
// system settings. Layout, position and scroll animations then finish at
// once, and enter and exit transitions only fade.
func ReducedMotion() bool { return reducedMotion.Load() }

// sided returns t with Slide turned into an offset away from the popover's
// anchor; side is the Side the popover opened on plus one, 0 for elements that
// are not a popover's root.
func (t Transition) sided(side uint8) Transition {
	if side == 0 || t.Slide == 0 {
		return t
	}
	switch Side(side - 1) {
	case SideBottom:
		t.Y -= t.Slide
	case SideTop:
		t.Y += t.Slide
	case SideRight:
		t.X -= t.Slide
	case SideLeft:
		t.X += t.Slide
	}
	return t
}

// Pulse fades the element's opacity from 1 to low and back over period
// seconds, repeating while it is drawn, like Tailwind's animate-pulse (2
// seconds to 0.5). Every pulsing element in a window keeps the same phase. With
// ReducedMotion it stays at 1.
func (e *El) Pulse(period, low float32) *El {
	if period > 0 && finite(period) && finite(low) {
		e.pulse = &pulseProps{period: period, low: min(max(low, 0), 1)}
	} else {
		e.pulse = nil
	}
	return e
}

type pulseProps struct{ period, low float32 }

// Shimmer sweeps highlight across the text of a Text element from left to
// right every period seconds, repeating while it is drawn, as a gradient
// clipped to the text that slides over it (CSS background-clip: text). The
// highlight peaks in a band 0.6 of the text wide and fades to the text color
// at its edges. Each glyph takes the color at its center. With ReducedMotion
// the text keeps its color.
func (e *El) Shimmer(period float32, highlight Color) *El {
	if period > 0 && finite(period) {
		e.shimmer = &shimmerProps{period: period, highlight: highlight}
	} else {
		e.shimmer = nil
	}
	return e
}

type shimmerProps struct {
	period    float32
	highlight Color
}

// at returns how far toward the highlight a glyph centered at u goes, where u
// is 0 at the text's left edge and 1 at its right, at elapsed seconds.
func (p *shimmerProps) at(elapsed float64, u float32) float32 {
	phase := float32(math.Mod(elapsed, float64(p.period))) / p.period
	peak := -0.25 + 1.5*phase
	return max(0, 1-abs32(u-peak)/0.3)
}

// Animate makes paint changes of this element (fill, border, text color and
// opacity, including hover and press styles) move with m instead of jumping.
func (e *El) Animate(m motion.Spec) *El { e.motion = &m; return e }

// AnimateHeight makes changes of this element's laid-out height, as when
// content inside it appears or goes, grow and shrink with m. The content is
// laid out at its new size at once and clipped to the shown height; the
// auto-height boxes around the element follow the shown height, the
// elements after it move with it, and a scroll container holding it scrolls
// over the shown content. A scroll container keeps its own height.
func (e *El) AnimateHeight(m motion.Spec) *El { e.heightMotion = &m; return e }

// MatchID gives the element an identity shared across views. When it is
// placed where a different element with the same id was placed in the frame
// before, as a thumbnail in a list that opens into a detail view, it flies
// from that element's place and size to its own, with AnimateLayout's motion
// or motion.Default, drawn above the rest of the window but inside its own
// clip. The element that had the id goes at once, without its Exit. Hit
// testing uses the element's own place. Give an id to one element at a
// time; elements inside a scaled or turned element do not fly, nor any with
// ReducedMotion.
func (e *El) MatchID(id string) *El { e.matchID = id; return e }

// AnimateLayout makes changes of this element's position and size move with m.
func (e *El) AnimateLayout(m motion.Spec) *El { e.layoutMotion = &m; return e }

// Enter animates the element in from t when it first appears. Give list items
// a Key so that items keep their identity when others are added or removed.
func (e *El) Enter(t Transition) *El { e.enter = &t; return e }

// Exit keeps the element drawn after it is removed, animating it out to t. It
// runs only while the element's parent stays. Elements are identified by their
// position and Key, so give containers whose content is replaced wholesale
// (pages, tabs) a Key that changes with the content.
func (e *El) Exit(t Transition) *El { e.exit = &t; return e }
