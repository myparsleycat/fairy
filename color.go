package fairy

import "math"

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
