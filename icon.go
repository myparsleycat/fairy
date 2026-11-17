//go:build windows

package fairy

import (
	"math"
	"time"

	"github.com/myparsleycat/fairy/internal/geom"
	"github.com/myparsleycat/fairy/internal/render"
	"github.com/myparsleycat/fairy/svg"
)

// Icon draws v as a mask tinted with the text color. Without an explicit size
// it is a square a little larger than the inherited font size.
func Icon(v *svg.Vector) *El {
	e := newEl(kindIcon)
	e.vec = v
	return e
}

// Spinner is a busy indicator: an open ring in the text color, drawn like the
// Lucide loader-circle icon, that turns once a second while it is on screen.
// Size it with Size; it defaults to the icon size.
func Spinner() *El { return newEl(kindSpinner) }

type arcProps struct {
	start, sweep, stroke float32
	round                bool
}

// Arc strokes part of a circle in the text color, centered in the element:
// sweep radians clockwise from start, where 0 is 12 o'clock. A sweep of 2π
// or more is the whole circle. Size it with Size; it defaults to the icon
// size. With Animate, changes of start and sweep move with its motion.
func Arc(start, sweep float32) *El {
	e := newEl(kindArc)
	if !finite(start) {
		start = 0
	}
	if !finite(sweep) {
		sweep = 0
	}
	if sweep < 0 {
		start, sweep = start+sweep, -sweep
	}
	e.arc = &arcProps{start: start, sweep: sweep}
	return e
}

// Stroke sets an Arc's line width in pixels. It defaults to an eighth of
// the arc's size.
func (e *El) Stroke(px float32) *El {
	if e.arc != nil {
		e.arc.stroke = max(px, 0)
	}
	return e
}

// RoundCaps rounds the ends of an Arc.
func (e *El) RoundCaps() *El {
	if e.arc != nil {
		e.arc.round = true
	}
	return e
}

type iconKey struct {
	vec  *svg.Vector
	w, h int
}

func measureIcon(e *El, width float32, exact bool) (float32, float32) {
	sz := float32(math.Round(float64(e.tp.size * 1.15)))
	if e.vec == nil {
		return sz, sz
	}
	vw, vh, err := e.vec.Size()
	if err != nil {
		return sz, sz
	}
	if exact && width == width {
		return width, width * vh / vw
	}
	return sz * vw / max(vh, 1), sz
}

func (w *Window) paintIcon(e *El, box geom.Rect, color Color, clip [4]float32, opacity float32) bool {
	if e.vec == nil || box.Empty() {
		return true
	}
	dst := w.snap(box)
	pw, ph := int(dst[2]), int(dst[3])
	if pw <= 0 || ph <= 0 || !render.Fits(pw, ph) {
		return true
	}
	if w.iconGen != w.r.Atlas.Generation {
		clear(w.iconTiles)
		w.iconGen = w.r.Atlas.Generation
	}
	k := iconKey{e.vec, pw, ph}
	tile, ok := w.iconTiles[k]
	if !ok {
		mask := e.vec.Mask(pw, ph)
		if mask == nil {
			return true
		}
		if tile, ok = w.r.Atlas.Insert(pw, ph, mask.Pix); !ok {
			return false
		}
		w.iconTiles[k] = tile
	}
	as := float32(w.r.Atlas.Size())
	w.scene.Add(render.Prim{
		Bounds: dst,
		Clip:   clip,
		Color:  color.arr(opacity),
		UV:     [4]float32{float32(tile.X) / as, float32(tile.Y) / as, float32(tile.W) / as, float32(tile.H) / as},
		Kind:   render.KindGlyph,
		Flags:  render.FlagRawAlpha,
	})
	return true
}

func (w *Window) paintArc(e *El, box geom.Rect, color Color, clip [4]float32, opacity float32) {
	a := e.arc
	start, sweep := a.start, a.sweep
	if m := e.motion; m != nil && e.state != nil {
		v := w.animate(e.state, animArc, 2, 0.0005, [4]float32{start, sweep}, *m)
		start, sweep = v[0], max(v[1], 0)
	}
	size := min(box.W, box.H)
	if sweep <= 0 || size <= 0 {
		return
	}
	stroke := a.stroke
	if stroke <= 0 {
		stroke = size / 8
	}
	s := w.scale
	round := float32(0)
	if a.round {
		round = 1
	}
	w.scene.Add(render.Prim{
		Bounds: [4]float32{(box.X + (box.W-size)/2) * s, (box.Y + (box.H-size)/2) * s, size * s, size * s},
		Clip:   clip,
		Color:  color.arr(opacity),
		Params: [4]float32{start, min(sweep, 2*math.Pi), min(stroke, size/2) * s, round},
		Kind:   render.KindArc,
	})
}

// paintSpinner draws Lucide's loader-circle, M21 12a9 9 0 1 1-6.219-8.56 with
// a stroke of 2 in a 24-unit box: an arc of radius 9 from 3 o'clock clockwise
// to 0.1π, turned once per second like CSS animate-spin. It asks for frames at
// up to 60 per second rather than animating, so a spinner left on screen does
// not repaint the window at the full refresh rate.
func (w *Window) paintSpinner(box geom.Rect, color Color, clip [4]float32, opacity float32) {
	size := min(box.W, box.H)
	if size <= 0 {
		return
	}
	const (
		extent = 20.0 / 24 // diameter plus stroke
		stroke = 2.0 / 24
		start  = math.Pi / 2
		sweep  = 1.6 * math.Pi
		step   = time.Second / 60
	)
	now := w.now()
	since := now.Sub(pulseEpoch)
	turn := float64(since%time.Second) / float64(time.Second)
	s, d := w.scale, size*extent
	w.scene.Add(render.Prim{
		Bounds: [4]float32{(box.X + (box.W-d)/2) * s, (box.Y + (box.H-d)/2) * s, d * s, d * s},
		Clip:   clip,
		Color:  color.arr(opacity),
		Params: [4]float32{float32(start + 2*math.Pi*turn), sweep, size * stroke * s, 1},
		Kind:   render.KindArc,
	})
	w.frameAt(now.Add(step - since%step))
}
