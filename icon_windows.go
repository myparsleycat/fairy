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

// paintSpinner draws Lucide's loader-circle, M21 12a9 9 0 1 1-6.219-8.56 with
// a stroke of 2 in a 24-unit box: an arc of radius 9 from 3 o'clock clockwise
// to 0.1π, turned once per second like CSS animate-spin. It asks for frames at
// up to 60 per second rather than animating, so a spinner left on screen does
// not repaint the window at the full refresh rate.
func (w *Window) paintSpinner(box rectF, color Color, clip [4]float32, opacity float32) {
	size := min(box.w, box.h)
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
		Bounds: [4]float32{(box.x + (box.w-d)/2) * s, (box.y + (box.h-d)/2) * s, d * s, d * s},
		Clip:   clip,
		Color:  color.arr(opacity),
		Params: [4]float32{float32(start + 2*math.Pi*turn), sweep, size * stroke * s, 1},
		Kind:   render.KindArc,
	})
	w.frameAt(now.Add(step - since%step))
}
