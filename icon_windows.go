package fairy

import (
	"math"
	"time"

	"github.com/myparsleycat/fairy/internal/render"
	"github.com/myparsleycat/fairy/layout"
	"github.com/myparsleycat/fairy/svg"
)

type iconKey struct {
	vec  *svg.Vector
	w, h int
}

func (w *Window) iconMeasure(e *El) layout.MeasureFunc {
	return func(width float32, exact bool) (float32, float32) {
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
}

func (w *Window) paintIcon(e *El, box rectF, color Color, clip [4]float32, opacity float32) bool {
	if e.vec == nil || box.empty() {
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

func (w *Window) paintArc(e *El, box rectF, color Color, clip [4]float32, opacity float32) {
	a := e.arc
	start, sweep := a.start, a.sweep
	if m := e.motion; m != nil && e.state != nil {
		v := w.animate(e.state, animArc, 2, 0.0005, [4]float32{start, sweep}, *m)
		start, sweep = v[0], max(v[1], 0)
	}
	size := min(box.w, box.h)
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
		Bounds: [4]float32{(box.x + (box.w-size)/2) * s, (box.y + (box.h-size)/2) * s, size * s, size * s},
		Clip:   clip,
		Color:  color.arr(opacity),
		Params: [4]float32{start, min(sweep, 2*math.Pi), min(stroke, size/2) * s, round},
		Kind:   render.KindArc,
	})
}

// paintSpinner draws eight dots on a circle whose brightness rotates once per
// second, and asks for a frame at the next step while it is drawn.
func (w *Window) paintSpinner(box rectF, color Color, clip [4]float32, opacity float32) {
	const dots = 8
	s := w.scale
	size := min(box.w, box.h) * s
	if size <= 0 {
		return
	}
	cx, cy := (box.x+box.w/2)*s, (box.y+box.h/2)*s
	dot := size * 0.2
	r := size/2 - dot/2
	const step = time.Second / dots
	now := w.now()
	since := now.Sub(pulseEpoch)
	head := int(since % time.Second / step)
	for i := 0; i < dots; i++ {
		a := float64(i) / dots * 2 * math.Pi
		x, y := cx+r*float32(math.Sin(a))-dot/2, cy-r*float32(math.Cos(a))-dot/2
		age := (head - i + dots) % dots
		alpha := 1 - float32(age)/dots*0.85
		w.scene.Add(primRect([4]float32{x, y, dot, dot}, clip, color.arr(opacity*alpha), dot/2))
	}
	w.frameAt(now.Add(step - since%step))
}
