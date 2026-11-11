// Package pancursor draws the autoscroll pointer shapes.
package pancursor

import (
	"image"
	"image/draw"
	"math"

	"golang.org/x/image/vector"

	"github.com/myparsleycat/fairy/internal/vectorpath"
)

// Kind is the pointer shape during autoscroll, as browsers show it on
// Windows: a dot with arrows along the axes that can scroll while the pointer
// rests near the press point, and a dot with one arrow toward the direction
// of scrolling once it moves.
type Kind uint8

const (
	Middle Kind = iota
	MiddleVertical
	MiddleHorizontal
	N
	NE
	E
	SE
	S
	SW
	W
	NW
)

// At is the cursor for scrolling speeds vx and vy, of which only
// the signs count.
func At(vx, vy float32, canX, canY bool) Kind {
	sx, sy := sign32(vx), sign32(vy)
	if sx == 0 && sy == 0 {
		switch {
		case !canX:
			return MiddleVertical
		case !canY:
			return MiddleHorizontal
		}
		return Middle
	}
	// Clockwise from north, with y growing downward.
	dirs := [3][3]Kind{
		{NW, N, NE},
		{W, Middle, E},
		{SW, S, SE},
	}
	return dirs[sy+1][sx+1]
}

func sign32(v float32) int {
	switch {
	case v < 0:
		return -1
	case v > 0:
		return 1
	}
	return 0
}

// arrows lists the arrow directions of c in eighths of a turn clockwise
// from north.
func (c Kind) arrows() []int {
	switch c {
	case Middle:
		return []int{0, 2, 4, 6}
	case MiddleVertical:
		return []int{0, 4}
	case MiddleHorizontal:
		return []int{2, 6}
	}
	return []int{int(c - N)}
}

// Pixels draws c as top-down BGRA with straight alpha: black shapes
// with a white outline, centered in a size*size square.
func Pixels(c Kind, size int) []byte {
	u := float32(size) / 32 // the shapes are laid out on a 32-unit grid
	cx := float32(size) / 2
	var shapes [][]vectorpath.Point
	dot := make([]vectorpath.Point, 24)
	for i := range dot {
		t := 2 * math.Pi * float64(i) / float64(len(dot))
		dot[i] = vectorpath.Point{X: cx + 2.5*u*float32(math.Cos(t)), Y: cx + 2.5*u*float32(math.Sin(t))}
	}
	shapes = append(shapes, dot)
	for _, d := range c.arrows() {
		t := float64(d) * math.Pi / 4
		sin, cos := float32(math.Sin(t)), float32(math.Cos(t))
		// The north arrow, turned by t about the center.
		var tri []vectorpath.Point
		for _, p := range []vectorpath.Point{{X: 0, Y: -12.5}, {X: -4, Y: -7}, {X: 4, Y: -7}} {
			tri = append(tri, vectorpath.Point{X: cx + (p.X*cos-p.Y*sin)*u, Y: cx + (p.X*sin+p.Y*cos)*u})
		}
		shapes = append(shapes, tri)
	}

	rect := image.Rect(0, 0, size, size)
	body, edge := image.NewAlpha(rect), image.NewAlpha(rect)
	var r vector.Rasterizer
	fill := func(dst *image.Alpha) {
		r.Reset(size, size)
		r.DrawOp = draw.Over
		for _, s := range shapes {
			r.MoveTo(s[0].X, s[0].Y)
			for _, p := range s[1:] {
				r.LineTo(p.X, p.Y)
			}
			r.ClosePath()
		}
		r.Draw(dst, rect, image.Opaque, image.Point{})
	}
	fill(body)
	fill(edge)
	r.Reset(size, size)
	r.DrawOp = draw.Over
	for _, s := range shapes {
		vectorpath.Stroke(&r, s, true, 1.25*u, true)
	}
	r.Draw(edge, rect, image.Opaque, image.Point{})

	pix := make([]byte, size*size*4)
	for i := range size * size {
		b, e := float32(body.Pix[i])/255, float32(edge.Pix[i])/255
		// Black body over a white outline.
		a := b + e*(1-b)
		if a <= 0 {
			continue
		}
		v := byte(e*(1-b)/a*255 + 0.5)
		pix[i*4], pix[i*4+1], pix[i*4+2], pix[i*4+3] = v, v, v, byte(a*255+0.5)
	}
	return pix
}
