//go:build windows

package fairy

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/myparsleycat/fairy/motion"
)

// Color is a straight-alpha sRGB color with components in 0..1.
type Color struct{ R, G, B, A float32 }

// Transparent is the zero color.
var Transparent = Color{}

// RGB builds an opaque color from 0..255 components.
func RGB(r, g, b uint8) Color {
	return Color{float32(r) / 255, float32(g) / 255, float32(b) / 255, 1}
}

// RGBA builds a color from 0..255 components and a 0..1 alpha.
func RGBA(r, g, b uint8, a float32) Color {
	return Color{float32(r) / 255, float32(g) / 255, float32(b) / 255, a}
}

// Hex parses 0xRRGGBB.
func Hex(v uint32) Color {
	return RGB(uint8(v>>16), uint8(v>>8), uint8(v))
}

// HexA parses 0xRRGGBBAA.
func HexA(v uint32) Color {
	return RGBA(uint8(v>>24), uint8(v>>16), uint8(v>>8), float32(uint8(v))/255)
}

// Alpha returns c with alpha a.
func (c Color) Alpha(a float32) Color { c.A = a; return c }

// Mix interpolates from c to d by t in premultiplied space.
func (c Color) Mix(d Color, t float32) Color {
	t = float32(math.Max(0, math.Min(1, float64(t))))
	a := c.A + (d.A-c.A)*t
	if a <= 0 {
		return Transparent
	}
	mix := func(x, y float32) float32 { return (x*c.A + (y*d.A-x*c.A)*t) / a }
	return Color{mix(c.R, d.R), mix(c.G, d.G), mix(c.B, d.B), a}
}

// Over composites c on top of d.
func (c Color) Over(d Color) Color {
	a := c.A + d.A*(1-c.A)
	if a <= 0 {
		return Transparent
	}
	f := func(x, y float32) float32 { return (x*c.A + y*d.A*(1-c.A)) / a }
	return Color{f(c.R, d.R), f(c.G, d.G), f(c.B, d.B), a}
}

func (c Color) arr(opacity float32) [4]float32 {
	return [4]float32{c.R, c.G, c.B, c.A * opacity}
}

func (c Color) isSet() bool { return c.A > 0 }

// TooltipStyle is how tooltips look and move in every window. The zero value
// of a field takes the default noted beside it.
type TooltipStyle struct {
	Fill     Color         // background
	Text     Color         // text color
	Radius   float32       // corner radius
	PadX     float32       // horizontal padding
	PadY     float32       // vertical padding
	FontSize float32       // text size
	MaxW     float32       // the width text wraps at; 320 when zero
	Gap      float32       // distance from the element
	Arrow    bool          // draws an arrow pointing at the element
	Delay    time.Duration // how long the pointer rests before the tooltip shows
	// Motion runs the tooltip in and out: it fades, grows from 0.95 and
	// slides 8 pixels from the element's side. The zero value appears at once.
	Motion motion.Spec
}

var tooltipStyle atomic.Pointer[TooltipStyle]

// SetTooltipStyle places tooltips above the element they belong to (below
// when there is no room), centered on it, and draws them with s. Without it,
// tooltips show beside the pointer after 600ms.
func SetTooltipStyle(s TooltipStyle) {
	if s.MaxW <= 0 {
		s.MaxW = 320
	}
	tooltipStyle.Store(&s)
}

// ScrollBarStyle describes the thumb of a scroll container.
type ScrollBarStyle struct {
	Thickness float32 // thumb width; 6 when zero
	Inset     float32 // distance from the container's edge; 2 when zero
	// TrackInset overrides Inset at each end of the track when positive.
	TrackInset float32
	// AlwaysVisible shows the thumb whenever the container overflows.
	AlwaysVisible bool
	// Color is the thumb's color; zero uses the container's text color at
	// 30% opacity, and 50% while the thumb is dragged.
	Color Color
	// DragColor is the thumb's color while it is dragged; zero uses Color.
	DragColor Color
	// Hidden draws no thumb and takes no presses on the track; the container
	// still scrolls by wheel, touch and keys.
	Hidden bool
}

var scrollBarStyle atomic.Pointer[ScrollBarStyle]

// SetScrollBarStyle sets the scroll bar style of every window.
func SetScrollBarStyle(s ScrollBarStyle) { scrollBarStyle.Store(&s) }

// ScrollBarStyle overrides the window-wide style for this element's scroll bars.
func (e *El) ScrollBarStyle(s ScrollBarStyle) *El { e.scrollBars = &s; return e }

func (e *El) effectiveScrollBarStyle() ScrollBarStyle {
	var s ScrollBarStyle
	if p := e.scrollBars; p != nil {
		s = *p
	} else if p := scrollBarStyle.Load(); p != nil {
		s = *p
	}
	if s.Thickness <= 0 {
		s.Thickness = 6
	}
	if s.Inset <= 0 {
		s.Inset = 2
	}
	if s.TrackInset <= 0 {
		s.TrackInset = s.Inset
	}
	return s
}

// scrollBarFade is how scroll bars fade in and out, and scrollBarLinger how
// long they stay after scrolling stops.
var (
	scrollBarFade   = motion.Bezier(0.2, 0.4, 0, 0.2, 1)
	scrollBarLinger = 500 * time.Millisecond
)
