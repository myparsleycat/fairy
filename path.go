//go:build windows

package fairy

import (
	"image"
	"math"

	"github.com/myparsleycat/fairy/internal/fnvhash"
	"github.com/myparsleycat/fairy/internal/render"
	"github.com/myparsleycat/fairy/internal/vectorpath"
	"golang.org/x/image/vector"
)

// Path is a shape of lines and curves that a Painter fills or strokes, in
// logical pixels. The zero value is an empty path. A path built once can be
// drawn on any number of canvases.
type Path struct {
	cmds []pathCmd
}

type pathOp uint8

const (
	pathMove pathOp = iota
	pathLine
	pathQuad
	pathCubic
	pathArc
	pathClose
)

type pathCmd struct {
	op      pathOp
	a, b, c Point // pathArc: center, (radius, start), (sweep, 0)
}

// MoveTo starts a new subpath at (x, y).
func (p *Path) MoveTo(x, y float32) { p.cmds = append(p.cmds, pathCmd{op: pathMove, a: Point{x, y}}) }

// LineTo adds a line to (x, y). Without a current point, it starts a
// subpath at (x, y).
func (p *Path) LineTo(x, y float32) { p.cmds = append(p.cmds, pathCmd{op: pathLine, a: Point{x, y}}) }

// QuadTo adds a quadratic Bézier curve through control point (cx, cy) to
// (x, y). Without a current point, the curve starts at the control point.
func (p *Path) QuadTo(cx, cy, x, y float32) {
	p.cmds = append(p.cmds, pathCmd{op: pathQuad, a: Point{cx, cy}, b: Point{x, y}})
}

// CubicTo adds a cubic Bézier curve through control points (c1x, c1y) and
// (c2x, c2y) to (x, y). Without a current point, the curve starts at the
// first control point.
func (p *Path) CubicTo(c1x, c1y, c2x, c2y, x, y float32) {
	p.cmds = append(p.cmds, pathCmd{op: pathCubic, a: Point{c1x, c1y}, b: Point{c2x, c2y}, c: Point{x, y}})
}

// Arc adds a line to the start of a circular arc of radius r about
// (cx, cy), then the arc, from start through sweep in radians clockwise from
// 12 o'clock, as Painter.Arc. A negative sweep runs counterclockwise, and
// one beyond a full turn draws one turn.
// Without a current point, the subpath starts at the start of the arc.
func (p *Path) Arc(cx, cy, r, start, sweep float32) {
	p.cmds = append(p.cmds, pathCmd{op: pathArc, a: Point{cx, cy}, b: Point{r, start}, c: Point{sweep, 0}})
}

// Close ends the subpath with a line back to its start.
func (p *Path) Close() { p.cmds = append(p.cmds, pathCmd{op: pathClose}) }

// Reset empties the path, keeping its memory for reuse.
func (p *Path) Reset() { p.cmds = p.cmds[:0] }

type subpath struct {
	pts    []vectorpath.Point
	closed bool
}

// flatten turns the path into polylines in device pixels.
func (p *Path) flatten(ox, oy, scale float32) []subpath {
	const tol = 0.2 // device pixels
	var subs []subpath
	open := false // the last subpath takes more points
	var pen vectorpath.Point
	hasPen := false
	dev := func(q Point) vectorpath.Point { return vectorpath.Point{X: ox + q.X*scale, Y: oy + q.Y*scale} }
	for _, c := range p.cmds {
		if c.op == pathMove {
			pen, hasPen, open = dev(c.a), true, false
			continue
		}
		if c.op == pathClose {
			if open {
				subs[len(subs)-1].closed = true
				pen, open = subs[len(subs)-1].pts[0], false
			}
			continue
		}
		if !hasPen && (c.op == pathQuad || c.op == pathCubic) {
			pen, hasPen = dev(c.a), true
		}
		if !open {
			subs = append(subs, subpath{})
			if hasPen {
				subs[len(subs)-1].pts = []vectorpath.Point{pen}
			}
			open = true
		}
		s := &subs[len(subs)-1]
		switch c.op {
		case pathLine:
			s.pts = append(s.pts, dev(c.a))
		case pathQuad:
			s.pts = vectorpath.Quad(s.pts, pen, dev(c.a), dev(c.b), tol)
		case pathCubic:
			s.pts = vectorpath.Cubic(s.pts, pen, dev(c.a), dev(c.b), dev(c.c), tol)
		case pathArc:
			ctr := dev(c.a)
			s.pts = vectorpath.Arc(s.pts, ctr.X, ctr.Y, c.b.X*scale, c.b.Y, c.c.X, tol)
		}
		pen, hasPen = s.pts[len(s.pts)-1], true
	}
	return subs
}

// FillPath fills the inside of path, by the nonzero rule; open subpaths are
// closed with a line.
func (p *Painter) FillPath(path *Path, c Color) { p.drawPath(path, 0, c) }

// StrokePath draws the lines of path, width pixels wide with round joins and
// ends.
func (p *Painter) StrokePath(path *Path, width float32, c Color) {
	if width > 0 {
		p.drawPath(path, width, c)
	}
}

// drawPath rasterizes path into a texture of its device-pixel bounds within
// the clip, filled when width is 0 and stroked otherwise. Textures are kept
// by content, so a path drawn the same way again is not rasterized again.
func (p *Painter) drawPath(path *Path, width float32, c Color) {
	if path == nil || len(path.cmds) == 0 || c.A <= 0 || p.w.r == nil {
		return
	}
	subs := path.flatten(p.ox, p.oy, p.scale)
	half := width * p.scale / 2
	x0, y0 := float32(math.Inf(1)), float32(math.Inf(1))
	x1, y1 := float32(math.Inf(-1)), float32(math.Inf(-1))
	for _, s := range subs {
		for _, q := range s.pts {
			x0, y0, x1, y1 = min(x0, q.X), min(y0, q.Y), max(x1, q.X), max(y1, q.Y)
		}
	}
	if x1 < x0 {
		return
	}
	pad := half + 1
	b := intersect4([4]float32{x0 - pad, y0 - pad, x1 - x0 + 2*pad, y1 - y0 + 2*pad}, p.clip)
	ix0, iy0 := int(math.Floor(float64(b[0]))), int(math.Floor(float64(b[1])))
	iw := int(math.Ceil(float64(b[0]+b[2]))) - ix0
	ih := int(math.Ceil(float64(b[1]+b[3]))) - iy0
	if iw <= 0 || ih <= 0 || iw > maxPathSide || ih > maxPathSide {
		return
	}
	key := pathKey(subs, half, c, ix0, iy0, iw, ih)
	pt := p.w.pathTextures[key]
	if pt == nil {
		tex := p.rasterize(subs, half, c, ix0, iy0, iw, ih)
		if tex == nil {
			return
		}
		pt = &pathTexture{tex: tex}
		if p.w.pathTextures == nil {
			p.w.pathTextures = map[uint64]*pathTexture{}
		}
		p.w.pathTextures[key] = pt
	}
	pt.frame = p.w.frame
	p.images = append(p.images, canvasImage{len(p.w.scene.Prims), pt})
	p.w.scene.AddImage(render.Prim{
		Bounds: [4]float32{float32(ix0), float32(iy0), float32(iw), float32(ih)},
		Clip:   p.clip,
		Color:  [4]float32{1, 1, 1, p.opacity},
		UV:     [4]float32{0, 0, 1, 1},
		Flags:  render.FlagPremultiplied,
	}, pt.tex)
}

// maxPathSide bounds a path texture's side, below Direct3D 10's 8192.
const maxPathSide = 8192

func (p *Painter) rasterize(subs []subpath, half float32, c Color, x0, y0, w, h int) *render.Texture {
	z := vector.NewRasterizer(w, h)
	ox, oy := float32(x0), float32(y0)
	for _, s := range subs {
		pts := make([]vectorpath.Point, len(s.pts))
		for i, q := range s.pts {
			pts[i] = vectorpath.Point{X: q.X - ox, Y: q.Y - oy}
		}
		if half > 0 {
			vectorpath.Stroke(z, pts, s.closed, half, true)
			continue
		}
		if len(pts) < 3 {
			continue
		}
		z.MoveTo(pts[0].X, pts[0].Y)
		for _, q := range pts[1:] {
			z.LineTo(q.X, q.Y)
		}
		z.ClosePath()
	}
	mask := image.NewAlpha(image.Rect(0, 0, w, h))
	z.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
	pix := make([]byte, w*h*4)
	for i, a := range mask.Pix {
		if a == 0 {
			continue
		}
		f := c.A * float32(a) / 255
		pix[4*i] = byte(c.R*f*255 + 0.5)
		pix[4*i+1] = byte(c.G*f*255 + 0.5)
		pix[4*i+2] = byte(c.B*f*255 + 0.5)
		pix[4*i+3] = byte(f*255 + 0.5)
	}
	tex, err := p.w.r.NewTexture(w, h, pix, false)
	if err != nil {
		return nil
	}
	return tex
}

// pathKey identifies a path texture by what it shows, with points relative
// to its top-left device pixel, so a path moved by whole pixels reuses it.
func pathKey(subs []subpath, half float32, c Color, x, y, w, h int) uint64 {
	k := fnvhash.New().Float(half).Float(c.R).Float(c.G).Float(c.B).Float(c.A).
		Uint(uint64(w)<<32 | uint64(uint32(h)))
	ox, oy := float32(x), float32(y)
	for _, s := range subs {
		k = k.Uint(uint64(len(s.pts))).Bool(s.closed)
		for _, q := range s.pts {
			k = k.Float(q.X - ox).Float(q.Y - oy)
		}
	}
	return uint64(k)
}

type pathTexture struct {
	tex   *render.Texture
	frame int // the last frame that drew it
}

// canvasImage is an image prim a Painter added, and the texture it samples.
type canvasImage struct {
	index int // into the scene's prims
	tex   *pathTexture
}

// sweepPathTextures frees the path textures no canvas drew this frame.
func (w *Window) sweepPathTextures() {
	for k, pt := range w.pathTextures {
		if pt.frame != w.frame {
			pt.tex.Release()
			delete(w.pathTextures, k)
		}
	}
}

// releasePathTextures frees every path texture, and the retained drawings
// that sample them.
func (w *Window) releasePathTextures() {
	for _, pt := range w.pathTextures {
		pt.tex.Release()
	}
	w.pathTextures = nil
	clear(w.canvases)
}
