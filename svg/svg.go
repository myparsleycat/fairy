// Package svg parses the subset of SVG that icon sets use and renders it as
// a single-color mask.
package svg

import (
	"encoding/xml"
	"fmt"
	"image"
	"image/draw"
	"math"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/image/vector"

	"github.com/myparsleycat/fairy/internal/vectorpath"
)

// Vector is a parsed SVG drawing rendered as a single-color mask, tinted with
// the text color where it is drawn. It supports the subset icon sets use:
// path, circle, ellipse, rect, line, polyline and polygon, with fill, stroke,
// stroke-width, stroke-linecap and stroke-linejoin, inherited through <g>.
type Vector struct {
	vw, vh float32 // viewBox size
	vx, vy float32 // viewBox origin
	shapes []vshape
	once   sync.Once
	src    string
	err    error
}

type vshape struct {
	subpaths [][]vpt // in viewBox units
	closed   []bool
	fill     bool
	stroke   bool
	width    float32
	roundCap bool
}

type vpt = vectorpath.Point

// Parse parses SVG markup.
func Parse(src string) (*Vector, error) {
	v := &Vector{src: src}
	v.once.Do(v.parse)
	return v, v.err
}

// MustParse parses SVG markup and panics on error.
func MustParse(src string) *Vector {
	v, err := Parse(src)
	if err != nil {
		panic(err)
	}
	return v
}

// Lazy returns a Vector parsed on first use; parse errors draw nothing.
// Generated icon packages use it so that unused icons cost no startup time.
func Lazy(src string) *Vector {
	return &Vector{src: src}
}

func (v *Vector) ensure() bool {
	v.once.Do(v.parse)
	return v.err == nil
}

type svgStyle struct {
	fill, stroke bool
	width        float32
	roundCap     bool
}

// svgDefinitions are elements whose content is only drawn where referenced,
// which Vector does not support, so their shapes are skipped.
var svgDefinitions = map[string]bool{
	"defs": true, "symbol": true, "clipPath": true, "mask": true, "pattern": true,
	"marker": true, "linearGradient": true, "radialGradient": true,
}

func (v *Vector) parse() {
	dec := xml.NewDecoder(strings.NewReader(v.src))
	stack := []svgStyle{{fill: true, width: 1}}
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if svgDefinitions[t.Name.Local] {
				dec.Skip()
				continue
			}
			st := stack[len(stack)-1]
			attrs := map[string]string{}
			for _, a := range t.Attr {
				attrs[a.Name.Local] = a.Value
			}
			if f, ok := attrs["fill"]; ok {
				st.fill = f != "none"
			}
			if s, ok := attrs["stroke"]; ok {
				st.stroke = s != "none"
			}
			if s, ok := attrs["stroke-width"]; ok {
				st.width = atof(s)
			}
			if s, ok := attrs["stroke-linecap"]; ok {
				st.roundCap = s == "round"
			}
			stack = append(stack, st)
			if t.Name.Local == "svg" {
				v.parseViewBox(attrs)
				continue
			}
			sh := vshape{fill: st.fill, stroke: st.stroke, width: st.width, roundCap: st.roundCap}
			if err := sh.build(t.Name.Local, attrs); err != nil {
				v.err = err
				return
			}
			if len(sh.subpaths) > 0 {
				v.shapes = append(v.shapes, sh)
			}
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if v.vw <= 0 || v.vh <= 0 {
		v.vw, v.vh = 24, 24
	}
}

func (v *Vector) parseViewBox(a map[string]string) {
	if vb := strings.Fields(strings.ReplaceAll(a["viewBox"], ",", " ")); len(vb) == 4 {
		v.vx, v.vy, v.vw, v.vh = atof(vb[0]), atof(vb[1]), atof(vb[2]), atof(vb[3])
		return
	}
	v.vw, v.vh = atof(a["width"]), atof(a["height"])
}

func atof(s string) float32 {
	f, _ := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(s), "px"), 32)
	return float32(f)
}

func (sh *vshape) build(tag string, a map[string]string) error {
	f := func(k string) float32 { return atof(a[k]) }
	switch tag {
	case "path":
		return sh.parsePath(a["d"])
	case "circle":
		sh.ellipse(f("cx"), f("cy"), f("r"), f("r"))
	case "ellipse":
		sh.ellipse(f("cx"), f("cy"), f("rx"), f("ry"))
	case "line":
		sh.add([]vpt{{X: f("x1"), Y: f("y1")}, {X: f("x2"), Y: f("y2")}}, false)
	case "polyline", "polygon":
		nums := parseNumbers(a["points"])
		var pts []vpt
		for i := 0; i+1 < len(nums); i += 2 {
			pts = append(pts, vpt{X: nums[i], Y: nums[i+1]})
		}
		sh.add(pts, tag == "polygon")
	case "rect":
		x, y, w, h := f("x"), f("y"), f("width"), f("height")
		rx, ry := f("rx"), f("ry")
		if rx == 0 {
			rx = ry
		}
		if ry == 0 {
			ry = rx
		}
		rx, ry = min(rx, w/2), min(ry, h/2)
		if rx <= 0 {
			sh.add([]vpt{{X: x, Y: y}, {X: x + w, Y: y}, {X: x + w, Y: y + h}, {X: x, Y: y + h}}, true)
			return nil
		}
		var p pathBuilder
		p.move(x+rx, y)
		p.line(x+w-rx, y)
		p.arc(rx, ry, 0, false, true, x+w, y+ry)
		p.line(x+w, y+h-ry)
		p.arc(rx, ry, 0, false, true, x+w-rx, y+h)
		p.line(x+rx, y+h)
		p.arc(rx, ry, 0, false, true, x, y+h-ry)
		p.line(x, y+ry)
		p.arc(rx, ry, 0, false, true, x+rx, y)
		p.close()
		sh.subpaths, sh.closed = p.subpaths, p.closed
	}
	return nil
}

func (sh *vshape) add(pts []vpt, closed bool) {
	if len(pts) > 0 {
		sh.subpaths = append(sh.subpaths, pts)
		sh.closed = append(sh.closed, closed)
	}
}

func (sh *vshape) ellipse(cx, cy, rx, ry float32) {
	n := 48
	pts := make([]vpt, n)
	for i := range pts {
		a := float64(i) / float64(n) * 2 * math.Pi
		pts[i] = vpt{X: cx + rx*float32(math.Cos(a)), Y: cy + ry*float32(math.Sin(a))}
	}
	sh.add(pts, true)
}

func parseNumbers(s string) []float32 {
	var out []float32
	t := pathTokens{s: s}
	for {
		t.skipSep()
		if t.i >= len(s) {
			return out
		}
		f, ok := t.number()
		if !ok {
			return out
		}
		out = append(out, f)
	}
}

type pathTokens struct {
	s string
	i int
}

func (t *pathTokens) skipSep() {
	for t.i < len(t.s) && strings.IndexByte(" \t\r\n\f,", t.s[t.i]) >= 0 {
		t.i++
	}
}

func (t *pathTokens) number() (float32, bool) {
	t.skipSep()
	start := t.i
	if t.i < len(t.s) && (t.s[t.i] == '-' || t.s[t.i] == '+') {
		t.i++
	}
	dot, digits := false, false
	for t.i < len(t.s) {
		c := t.s[t.i]
		switch {
		case c >= '0' && c <= '9':
			digits = true
		case c == '.' && !dot:
			dot = true
		case (c == 'e' || c == 'E') && digits:
			t.i++
			if t.i < len(t.s) && (t.s[t.i] == '-' || t.s[t.i] == '+') {
				t.i++
			}
			continue
		default:
			goto done
		}
		t.i++
	}
done:
	if !digits {
		t.i = start
		return 0, false
	}
	f, err := strconv.ParseFloat(t.s[start:t.i], 32)
	return float32(f), err == nil
}

// flag reads an arc flag, which may be written without a separator ("a1 1 0 01 1 1").
func (t *pathTokens) flag() (float32, bool) {
	t.skipSep()
	if t.i < len(t.s) && (t.s[t.i] == '0' || t.s[t.i] == '1') {
		t.i++
		return float32(t.s[t.i-1] - '0'), true
	}
	return 0, false
}

type pathBuilder struct {
	subpaths [][]vpt
	closed   []bool
	cur      []vpt
	X, Y     float32
	sx, sy   float32
}

func (p *pathBuilder) flush(closed bool) {
	if len(p.cur) > 1 {
		p.subpaths = append(p.subpaths, p.cur)
		p.closed = append(p.closed, closed)
	} else if len(p.cur) == 1 {
		// A lone point draws a dot with round caps.
		p.subpaths = append(p.subpaths, []vpt{p.cur[0], p.cur[0]})
		p.closed = append(p.closed, false)
	}
	p.cur = nil
}

func (p *pathBuilder) move(x, y float32) {
	p.flush(false)
	p.X, p.Y, p.sx, p.sy = x, y, x, y
	p.cur = []vpt{{X: x, Y: y}}
}

func (p *pathBuilder) line(x, y float32) {
	if p.cur == nil {
		p.cur = []vpt{{X: p.X, Y: p.Y}}
	}
	p.cur = append(p.cur, vpt{X: x, Y: y})
	p.X, p.Y = x, y
}

func (p *pathBuilder) cubic(x1, y1, x2, y2, x, y float32) {
	x0, y0 := p.X, p.Y
	n := 16
	for i := 1; i <= n; i++ {
		t := float32(i) / float32(n)
		u := 1 - t
		px := u*u*u*x0 + 3*u*u*t*x1 + 3*u*t*t*x2 + t*t*t*x
		py := u*u*u*y0 + 3*u*u*t*y1 + 3*u*t*t*y2 + t*t*t*y
		p.line(px, py)
	}
}

func (p *pathBuilder) quad(x1, y1, x, y float32) {
	x0, y0 := p.X, p.Y
	n := 12
	for i := 1; i <= n; i++ {
		t := float32(i) / float32(n)
		u := 1 - t
		p.line(u*u*x0+2*u*t*x1+t*t*x, u*u*y0+2*u*t*y1+t*t*y)
	}
}

// arc appends an SVG elliptical arc using the endpoint-to-center conversion of SVG 1.1 F.6.5.
func (p *pathBuilder) arc(rx, ry, rot float32, large, sweep bool, x, y float32) {
	x1, y1 := float64(p.X), float64(p.Y)
	x2, y2 := float64(x), float64(y)
	if x1 == x2 && y1 == y2 {
		return
	}
	frx, fry := math.Abs(float64(rx)), math.Abs(float64(ry))
	if frx == 0 || fry == 0 {
		p.line(x, y)
		return
	}
	phi := float64(rot) * math.Pi / 180
	cos, sin := math.Cos(phi), math.Sin(phi)
	dx, dy := (x1-x2)/2, (y1-y2)/2
	x1p := cos*dx + sin*dy
	y1p := -sin*dx + cos*dy
	lambda := x1p*x1p/(frx*frx) + y1p*y1p/(fry*fry)
	if lambda > 1 {
		s := math.Sqrt(lambda)
		frx, fry = frx*s, fry*s
	}
	num := frx*frx*fry*fry - frx*frx*y1p*y1p - fry*fry*x1p*x1p
	den := frx*frx*y1p*y1p + fry*fry*x1p*x1p
	coef := 0.0
	if den != 0 && num > 0 {
		coef = math.Sqrt(num / den)
	}
	if large == sweep {
		coef = -coef
	}
	cxp, cyp := coef*frx*y1p/fry, -coef*fry*x1p/frx
	cx := cos*cxp - sin*cyp + (x1+x2)/2
	cy := sin*cxp + cos*cyp + (y1+y2)/2
	angle := func(ux, uy, vx, vy float64) float64 {
		a := math.Atan2(ux*vy-uy*vx, ux*vx+uy*vy)
		return a
	}
	t1 := angle(1, 0, (x1p-cxp)/frx, (y1p-cyp)/fry)
	dt := angle((x1p-cxp)/frx, (y1p-cyp)/fry, (-x1p-cxp)/frx, (-y1p-cyp)/fry)
	if !sweep && dt > 0 {
		dt -= 2 * math.Pi
	} else if sweep && dt < 0 {
		dt += 2 * math.Pi
	}
	n := max(4, int(math.Ceil(math.Abs(dt)/(math.Pi/16))))
	for i := 1; i <= n; i++ {
		t := t1 + dt*float64(i)/float64(n)
		ex, ey := frx*math.Cos(t), fry*math.Sin(t)
		p.line(float32(cos*ex-sin*ey+cx), float32(sin*ex+cos*ey+cy))
	}
	p.X, p.Y = x, y
}

func (p *pathBuilder) close() {
	if p.cur != nil {
		p.flush(true)
	}
	p.X, p.Y = p.sx, p.sy
}

var pathArgs = map[byte]int{'m': 2, 'l': 2, 'h': 1, 'v': 1, 'c': 6, 's': 4, 'q': 4, 't': 2, 'a': 7, 'z': 0}

// parsePath draws the path up to its first error, as SVG renderers do.
func (sh *vshape) parsePath(d string) error {
	var p pathBuilder
	t := pathTokens{s: d}
	var cmd byte
	var lcx, lcy float32 // last control point for S/T
	var lastCmd byte
parse:
	for {
		t.skipSep()
		if t.i >= len(d) {
			break
		}
		c := d[t.i]
		explicit := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if explicit {
			cmd = c
			t.i++
		} else if cmd == 0 {
			return fmt.Errorf("svg: path data must start with a command: %q", d)
		}
		n, ok := pathArgs[cmd|0x20]
		if !ok {
			return fmt.Errorf("svg: unsupported path command %q", cmd)
		}
		if !explicit && n == 0 {
			break
		}
		var a [7]float32
		for i := range n {
			if cmd|0x20 == 'a' && (i == 3 || i == 4) {
				a[i], ok = t.flag()
			} else {
				a[i], ok = t.number()
			}
			if !ok {
				break parse
			}
		}
		rel := cmd >= 'a'
		ox, oy := float32(0), float32(0)
		if rel {
			ox, oy = p.X, p.Y
		}
		switch cmd | 0x20 {
		case 'm':
			p.move(a[0]+ox, a[1]+oy)
			// Further pairs after M are implicit L.
			if rel {
				cmd = 'l'
			} else {
				cmd = 'L'
			}
		case 'l':
			p.line(a[0]+ox, a[1]+oy)
		case 'h':
			p.line(a[0]+ox, p.Y)
		case 'v':
			p.line(p.X, a[0]+oy)
		case 'c':
			p.cubic(a[0]+ox, a[1]+oy, a[2]+ox, a[3]+oy, a[4]+ox, a[5]+oy)
			lcx, lcy = a[2]+ox, a[3]+oy
		case 's':
			x1, y1 := p.X, p.Y
			if l := lastCmd | 0x20; l == 'c' || l == 's' {
				x1, y1 = 2*p.X-lcx, 2*p.Y-lcy
			}
			p.cubic(x1, y1, a[0]+ox, a[1]+oy, a[2]+ox, a[3]+oy)
			lcx, lcy = a[0]+ox, a[1]+oy
		case 'q':
			p.quad(a[0]+ox, a[1]+oy, a[2]+ox, a[3]+oy)
			lcx, lcy = a[0]+ox, a[1]+oy
		case 't':
			x1, y1 := p.X, p.Y
			if l := lastCmd | 0x20; l == 'q' || l == 't' {
				x1, y1 = 2*p.X-lcx, 2*p.Y-lcy
			}
			p.quad(x1, y1, a[0]+ox, a[1]+oy)
			lcx, lcy = x1, y1
		case 'a':
			p.arc(a[0], a[1], a[2], a[3] != 0, a[4] != 0, a[5]+ox, a[6]+oy)
		case 'z':
			p.close()
		}
		lastCmd = cmd
	}
	p.flush(false)
	sh.subpaths, sh.closed = p.subpaths, p.closed
	return nil
}

// Mask renders the vector into a w*h coverage mask, as Icon draws it, so that an
// app can composite icons into its own images. It returns nil when the SVG did
// not parse or the size is empty.
func (v *Vector) Mask(w, h int) *image.Alpha {
	pix := v.rasterize(w, h)
	if pix == nil {
		return nil
	}
	return &image.Alpha{Pix: pix, Stride: w, Rect: image.Rect(0, 0, w, h)}
}

// rasterize draws the vector into a w*h coverage mask.
func (v *Vector) rasterize(w, h int) []byte {
	if !v.ensure() || w <= 0 || h <= 0 {
		return nil
	}
	sx, sy := float32(w)/v.vw, float32(h)/v.vh
	dst := image.NewAlpha(image.Rect(0, 0, w, h))
	var r vector.Rasterizer
	tf := func(p vpt) (float32, float32) { return (p.X - v.vx) * sx, (p.Y - v.vy) * sy }
	for _, sh := range v.shapes {
		if sh.fill {
			r.Reset(w, h)
			r.DrawOp = draw.Over
			for _, sp := range sh.subpaths {
				x, y := tf(sp[0])
				r.MoveTo(x, y)
				for _, p := range sp[1:] {
					x, y := tf(p)
					r.LineTo(x, y)
				}
				r.ClosePath()
			}
			r.Draw(dst, dst.Bounds(), image.Opaque, image.Point{})
		}
		if sh.stroke && sh.width > 0 {
			r.Reset(w, h)
			r.DrawOp = draw.Over
			half := sh.width * (sx + sy) / 4
			for i, sp := range sh.subpaths {
				pts := make([]vpt, len(sp))
				for j, p := range sp {
					x, y := tf(p)
					pts[j] = vpt{X: x, Y: y}
				}
				vectorpath.Stroke(&r, pts, sh.closed[i], half, sh.roundCap)
			}
			r.Draw(dst, dst.Bounds(), image.Opaque, image.Point{})
		}
	}
	return dst.Pix
}

// Size parses a lazy vector if necessary and returns its viewBox dimensions.
func (v *Vector) Size() (width, height float32, err error) {
	v.once.Do(v.parse)
	return v.vw, v.vh, v.err
}
