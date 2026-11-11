// Package motion describes how animated values move (springs, ease-out and
// CSS cubic-bezier curves) and steps them frame by frame with Track.
package motion

type motionKind uint8

const (
	motionSpring motionKind = iota
	motionEase
	motionNone
	motionBezier
)

// Spec describes how an animated value moves toward its target.
type Spec struct {
	kind           motionKind
	duration       float32
	bounce         float32
	x1, y1, x2, y2 float32 // control points of a Bezier motion
}

// Spring is a spring that settles in about duration seconds. bounce is 0 for no
// overshoot and up to 0.9 for a springy wobble.
func Spring(duration, bounce float32) Spec {
	return Spec{kind: motionSpring, duration: max(duration, 0.01), bounce: min(max(bounce, 0), 0.9)}
}

// Ease moves to the target over duration seconds with an ease-out curve.
func Ease(duration float32) Spec { return Spec{kind: motionEase, duration: max(duration, 0.01)} }

// Bezier moves to the target over duration seconds along the CSS timing
// function cubic-bezier(x1, y1, x2, y2). x1 and x2 are clamped to [0, 1]. A
// change of target starts a new curve from the current value, as a CSS
// transition does.
func Bezier(duration, x1, y1, x2, y2 float32) Spec {
	return Spec{kind: motionBezier, duration: max(duration, 0.01),
		x1: min(max(x1, 0), 1), y1: y1, x2: min(max(x2, 0), 1), y2: y2}
}

// Common motions.
var (
	Snappy  = Spring(0.25, 0)
	Default = Spring(0.35, 0)
	Gentle  = Spring(0.5, 0)
	Bouncy  = Spring(0.45, 0.3)
	// Instant jumps to the target without animating.
	Instant = Spec{kind: motionNone}

	// Fast is a CSS transition's default: 150ms cubic-bezier(0.4, 0, 0.2, 1).
	Fast = Bezier(0.15, 0.4, 0, 0.2, 1)
	// Popup is 100ms of the CSS ease curve, for menus and popovers opening and closing.
	Popup = Bezier(0.1, 0.25, 0.1, 0.25, 1)
	// Tooltip is 150ms of the CSS ease curve.
	Tooltip = Bezier(0.15, 0.25, 0.1, 0.25, 1)
)

// Duration returns the settling duration in seconds.
func (m Spec) Duration() float32 { return m.duration }

// IsInstant reports whether m is Instant.
func (m Spec) IsInstant() bool { return m.kind == motionNone }

// IsBezier reports whether m was made by Bezier.
func (m Spec) IsBezier() bool { return m.kind == motionBezier }
