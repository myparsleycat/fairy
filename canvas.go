//go:build windows

package fairy

import (
	"math"

	"github.com/myparsleycat/fairy/internal/geom"
	"github.com/myparsleycat/fairy/internal/render"
	"github.com/myparsleycat/fairy/internal/text"
)

// Canvas is an element the app draws on with a Painter, for charts,
// sparklines, waveforms and other drawings made of many shapes, which cost
// far less this way than as elements of their own. draw runs when the window
// paints the element, with coordinates in logical pixels from the top-left
// corner of its content box; drawing is clipped to that box. Size the element
// like a box. draw may read state that changes without a build, such as a
// Live value or a field the app updates, as long as something asks for the
// paint, as Live.Set or Window.Tick do.
//
// With Retain, the drawing is kept and draw runs again only when version
// changes, when the element's size, position within a device pixel, scale or
// opacity changes, and after Window.InvalidateSurface with its key.
func Canvas(draw func(p *Painter)) *El {
	e := newEl(kindCanvas)
	e.canvas = draw
	return e
}

// Painter draws shapes and text on a Canvas. Its methods take logical
// pixels relative to the canvas's content box; angles are in radians,
// clockwise from 12 o'clock for arcs.
type Painter struct {
	w       *Window
	e       *El
	ox, oy  float32 // the content box's top-left corner in device pixels
	scale   float32
	width   float32
	height  float32
	clip    [4]float32
	opacity float32
	ok      bool          // false once the glyph atlas overflowed
	images  []canvasImage // the image prims drawn, for a retained drawing
}

// Size returns the size of the canvas's content box.
func (p *Painter) Size() (w, h float32) { return p.width, p.height }

func (p *Painter) add(pr render.Prim) {
	pr.Clip = p.clip
	p.w.scene.Add(pr)
}

func (p *Painter) rect(x, y, w, h, r float32, fill, border Color, bw float32) {
	if w <= 0 || h <= 0 {
		return
	}
	s := p.scale
	r = min(max(r, 0), w/2, h/2) * s
	pr := render.Prim{
		Bounds: [4]float32{p.ox + x*s, p.oy + y*s, w * s, h * s},
		Color:  fill.arr(p.opacity),
		Radii:  [4]float32{r, r, r, r},
		Kind:   render.KindRect,
	}
	if bw > 0 {
		b := bw * s
		pr.Border = [4]float32{b, b, b, b}
		pr.BorderColor = border.arr(p.opacity)
	}
	p.add(pr)
}

// Rect fills a rectangle.
func (p *Painter) Rect(x, y, w, h float32, c Color) { p.rect(x, y, w, h, 0, c, Color{}, 0) }

// RoundRect fills a rectangle with corners of radius r.
func (p *Painter) RoundRect(x, y, w, h, r float32, c Color) { p.rect(x, y, w, h, r, c, Color{}, 0) }

// StrokeRect draws the outline of a rectangle with corners of radius r, width
// pixels wide inside its edges.
func (p *Painter) StrokeRect(x, y, w, h, r, width float32, c Color) {
	p.rect(x, y, w, h, r, Color{}, c, width)
}

// Circle fills a circle of radius r about (cx, cy).
func (p *Painter) Circle(cx, cy, r float32, c Color) { p.rect(cx-r, cy-r, 2*r, 2*r, r, c, Color{}, 0) }

// Arc strokes an arc of radius r about (cx, cy), from start through sweep,
// width pixels wide and centered on the radius. A negative sweep runs
// counterclockwise.
func (p *Painter) Arc(cx, cy, r, start, sweep, width float32, c Color) {
	if sweep == 0 || r <= 0 || width <= 0 {
		return
	}
	if sweep < 0 {
		start, sweep = start+sweep, -sweep
	}
	s := p.scale
	d := (2*r + width) * s
	p.add(render.Prim{
		Bounds: [4]float32{p.ox + (cx-r-width/2)*s, p.oy + (cy-r-width/2)*s, d, d},
		Color:  c.arr(p.opacity),
		Params: [4]float32{start, min(sweep, 2*math.Pi), min(width*s, d/2), 0},
		Kind:   render.KindArc,
	})
}

// Line draws a line from (x0, y0) to (x1, y1), width pixels wide with round
// ends.
func (p *Painter) Line(x0, y0, x1, y1, width float32, c Color) {
	if width <= 0 {
		return
	}
	s := p.scale
	ax, ay := p.ox+x0*s, p.oy+y0*s
	bx, by := p.ox+x1*s, p.oy+y1*s
	wd := width * s
	dx, dy := bx-ax, by-ay
	length := float32(math.Hypot(float64(dx), float64(dy)))
	cx, cy := (ax+bx)/2, (ay+by)/2
	// The line is a capsule along x about its center, turned into place.
	l, r := length+wd, wd/2
	pr := render.Prim{
		Bounds: [4]float32{cx - l/2, cy - r, l, wd},
		Color:  c.arr(p.opacity),
		Radii:  [4]float32{r, r, r, r},
		Kind:   render.KindRect,
	}
	switch {
	case dy == 0:
	case dx == 0:
		pr.Bounds = [4]float32{cx - r, cy - l/2, wd, l}
	default:
		// About the origin, so that moving the line moves only Xlate.
		cos, sin := dx/length, dy/length
		pr.Bounds = [4]float32{-l / 2, -r, l, wd}
		pr.Flags = render.FlagXform
		pr.Xform = [4]float32{cos, sin, -sin, cos}
		pr.Xlate = [2]float32{cx, cy}
		pr.Clip2 = render.NoClip
	}
	p.add(pr)
}

// Polyline draws lines through pts in order, width pixels wide with round
// ends and joins. Translucent colors darken where the lines meet; draw such a
// line in an Isolate element at full alpha and give the element the opacity.
func (p *Painter) Polyline(pts []Point, width float32, c Color) {
	for i := 1; i < len(pts); i++ {
		p.Line(pts[i-1].X, pts[i-1].Y, pts[i].X, pts[i].Y, width, c)
	}
}

func (p *Painter) textStyle(size float32) text.Style {
	tp := p.e.tp
	tp.size = size
	tp.noWrap, tp.ellipsis = true, false
	return p.w.textStyle(tp)
}

// MeasureText returns the size of s drawn by Text at font size size.
func (p *Painter) MeasureText(s string, size float32) (w, h float32) {
	tw, th := p.w.ts.Measure(s, p.textStyle(size), text.Unbounded)
	return tw / p.scale, th / p.scale
}

// Text draws s on one line with its top-left corner at (x, y), at font size
// size in the canvas's font.
func (p *Painter) Text(x, y float32, s string, size float32, c Color) {
	if s == "" {
		return
	}
	l := p.w.ts.Layout(s, p.textStyle(size), text.Unbounded)
	p.ok = l.Draw(&p.w.scene, p.ox+x*p.scale, p.oy+y*p.scale, c.arr(p.opacity), p.clip) && p.ok
}

// maxRecordPixels bounds the device pixels a retained canvas draws its
// paths for beyond the part in view, keeping each path texture within 16 MB.
const maxRecordPixels = 4 << 20

type canvasKey struct {
	version      uint64
	scale, w, h  float32
	fracX, fracY float32    // the origin's position within a device pixel
	clip         [4]float32 // the part drawn, from the origin, when not all of it
	opacity      float32
	atlas        int
	tp           textProps // the font Text draws with
}

type canvasEntry struct {
	key     canvasKey
	ox, oy  float32
	prims   []render.Prim // as drawn, without the scene's clip index
	images  []canvasImage // indices into prims
	frame   int
	replays int // times prims were replayed since they were drawn
}

func (w *Window) paintCanvas(e *El, box geom.Rect, clip [4]float32, opacity float32) bool {
	if e.canvas == nil || box.W <= 0 || box.H <= 0 {
		return true
	}
	s := w.scale
	ox, oy := box.X*s, box.Y*s
	full := [4]float32{ox, oy, box.W * s, box.H * s}
	visible := intersect4(clip, full)
	p := &Painter{
		w: w, e: e, ox: ox, oy: oy, scale: s, width: box.W, height: box.H,
		clip: visible, opacity: opacity, ok: true,
	}
	if e.retain == nil {
		e.canvas(p)
		return p.ok
	}
	atlas := 0
	if w.r != nil {
		atlas = w.r.Atlas.Generation
	}
	fx, fy := ox-float32(math.Floor(float64(ox))), oy-float32(math.Floor(float64(oy)))
	key := canvasKey{
		version: *e.retain, scale: s, w: box.W, h: box.H, fracX: fx, fracY: fy,
		opacity: opacity, atlas: atlas, tp: e.tp,
	}
	if w.canvases == nil {
		w.canvases = map[uint64]*canvasEntry{}
	}
	ce := w.canvases[e.id]
	if ce == nil {
		ce = &canvasEntry{}
		w.canvases[e.id] = ce
	}
	ce.frame = w.frame
	invalidated := e.key != "" && w.invalidSurfaces[e.key]
	// Paths and text are drawn for the whole canvas, so that the drawing
	// replays whatever part of it is in view. A canvas larger than one path
	// texture may be, or than maxRecordPixels, is drawn for the part in view,
	// and again when that changes; so is one drawn again for a new version or
	// an invalidation before its last drawing was replayed.
	fw := int(math.Ceil(float64(ox+full[2]))) - int(math.Floor(float64(ox)))
	fh := int(math.Ceil(float64(oy+full[3]))) - int(math.Floor(float64(oy)))
	churning := ce.prims != nil && ce.replays == 0 && (ce.key.version != key.version || invalidated)
	record := full
	if churning || fw > maxPathSide || fh > maxPathSide || fw*fh > maxRecordPixels {
		record = visible
		key.clip = [4]float32{visible[0] - ox, visible[1] - oy, visible[2], visible[3]}
	}
	if ce.prims != nil && ce.key == key && !invalidated {
		ce.replays++
		dx, dy := ox-ce.ox, oy-ce.oy
		images := ce.images
		for i, pr := range ce.prims {
			if pr.Flags&render.FlagXform != 0 {
				pr.Xlate[0] += dx
				pr.Xlate[1] += dy
			} else {
				pr.Bounds[0] += dx
				pr.Bounds[1] += dy
			}
			pr.Clip = p.clip
			b := intersect4(pr.Bounds, pr.Clip)
			shown := pr.Flags&render.FlagXform != 0 || b[2] > 0 && b[3] > 0
			if len(images) > 0 && images[0].index == i {
				// The texture is kept while the image is out of view.
				images[0].tex.frame = w.frame
				if shown {
					w.scene.AddImage(pr, images[0].tex.tex)
				}
				images = images[1:]
				continue
			}
			if shown {
				w.scene.Add(pr)
			}
		}
		return true
	}
	start := len(w.scene.Prims)
	p.clip = record
	e.canvas(p)
	for i := start; i < len(w.scene.Prims); i++ {
		w.scene.Prims[i].Clip = visible
	}
	if !p.ok {
		ce.prims = nil
		return false
	}
	ce.key, ce.ox, ce.oy, ce.replays = key, ox, oy, 0
	ce.prims = append(ce.prims[:0], w.scene.Prims[start:]...)
	ce.images = ce.images[:0]
	for _, im := range p.images {
		ce.images = append(ce.images, canvasImage{im.index - start, im.tex})
	}
	for i := range ce.prims {
		ce.prims[i].Flags &= 1<<render.ClipShift - 1
	}
	return true
}

// sweepCanvases forgets the drawings of canvases not painted this frame.
func (w *Window) sweepCanvases() {
	for id, ce := range w.canvases {
		if ce.frame != w.frame {
			delete(w.canvases, id)
		}
	}
}

func intersect4(a, b [4]float32) [4]float32 {
	x0, y0 := max(a[0], b[0]), max(a[1], b[1])
	x1, y1 := min(a[0]+a[2], b[0]+b[2]), min(a[1]+a[3], b[1]+b[3])
	return [4]float32{x0, y0, max(x1-x0, 0), max(y1-y0, 0)}
}
