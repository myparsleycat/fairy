package kit

import (
	"math"
	"strconv"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/motion"
)

type rangeState struct {
	// active is the thumb the pointer moves, or -1 until a drag away from
	// two thumbs at the same value says which.
	active int
}

// RangeSlider picks a range between min and max with two thumbs, rounded to
// step (0 for none). A press on the track moves the nearer thumb, and a
// thumb stops at the other one. Each thumb takes focus on its own; the arrow
// keys, Page Up, Page Down, Home and End move it. id must be unique in the
// window.
func RangeSlider(
	id string,
	value [2]float64,
	minV, maxV, step float64,
	onChange func([2]float64),
	opts ...Option,
) *fairy.El {
	o := build(opts)
	t := &current
	st := fairy.Local[rangeState]("kit.range:" + id)
	if value[0] > value[1] {
		value[0], value[1] = value[1], value[0]
	}
	frac := func(v float64) float32 {
		if maxV <= minV {
			return 0
		}
		return float32(min(max((v-minV)/(maxV-minV), 0), 1))
	}
	f0, f1 := frac(value[0]), frac(value[1])
	setThumb := func(i int, v float64) {
		if next := rangeSet(value, i, v, minV, maxV, step); next != value {
			onChange(next)
		}
	}
	small := step
	if small <= 0 {
		small = (maxV - minV) / 100
	}
	large := max((maxV-minV)/10, small)

	const thumbSize = 12
	w := fairy.CurrentWindow()
	thumbKey := func(i int) string { return "kit.range:" + id + ":" + strconv.Itoa(i) }
	var thumbs [2]*fairy.El
	for i := range thumbs {
		thumb := fairy.Box().Size(thumbSize, thumbSize).Shrink(0).Radius(thumbSize/2).Fill(fairy.Hex(0xffffff)).
			Border(1, t.Ring).Animate(motion.Fast).Role(fairy.RoleSlider).Key(thumbKey(i)).
			RangeValue(value[i], minV, maxV, step, func(v float64) { setThumb(i, v) })
		if o.label != "" {
			thumb.Label(o.label)
		}
		if !o.disabled {
			thumb.HoverRing(3, alpha(t.Ring, 0.5)).FocusRing(3, alpha(t.Ring, 0.5)).Focusable().
				OnKey(func(k keyboard.KeyEvent) bool {
					if k.Mods != 0 {
						return false
					}
					switch k.Key {
					case keyboard.KeyLeft, keyboard.KeyDown:
						setThumb(i, value[i]-small)
					case keyboard.KeyRight, keyboard.KeyUp:
						setThumb(i, value[i]+small)
					case keyboard.KeyPageDown:
						setThumb(i, value[i]-large)
					case keyboard.KeyPageUp:
						setThumb(i, value[i]+large)
					case keyboard.KeyHome:
						setThumb(i, minV)
					case keyboard.KeyEnd:
						setThumb(i, maxV)
					default:
						return false
					}
					return true
				})
		}
		thumbs[i] = thumb
	}

	// The track runs under the thumbs' centers at both ends, as in Slider.
	// Each thumb sits in a lane of its own over the track, so they can pass
	// over each other's place; the lanes let the pointer through to e.
	track := fairy.Row(
		fairy.Box().W(thumbSize/2),
		fairy.Box().W(0).Grow(f0+1e-4),
		fairy.Box().W(0).HFull().Grow(f1-f0+1e-4).Fill(t.Primary),
		fairy.Box().W(0).Grow(1-f1+1e-4),
		fairy.Box().W(thumbSize/2),
	).H(4).Grow(1).Radius(2).Fill(t.Muted).Clip()
	lane := func(f float32, thumb *fairy.El) *fairy.El {
		return fairy.Row(
			fairy.Box().W(0).Grow(f+1e-4).PassThrough(),
			thumb,
			fairy.Box().W(0).Grow(1-f+1e-4).PassThrough(),
		).Absolute().Left(0).Right(0).Top(0).H(16).Align(fairy.Center).PassThrough()
	}
	e := fairy.Row(
		fairy.Box(track).Absolute().Left(0).Right(0).Top(6).H(4),
		lane(f0, thumbs[0]),
		lane(f1, thumbs[1]),
	).H(16).MinW(80).Role(fairy.RoleGroup)
	if o.label != "" {
		e.Label(o.label)
	}
	if o.disabled {
		return e.Opacity(0.5).Isolate().Disabled(true)
	}

	at := func(x, width float32) float64 {
		f := min(max(float64((x-thumbSize/2)/max(width-thumbSize, 1)), 0), 1)
		return minV + f*(maxV-minV)
	}
	move := func(i int, v float64) {
		if w != nil && !w.Focused(thumbKey(i)) {
			w.Focus(thumbKey(i))
		}
		setThumb(i, v)
	}
	return e.Cursor(fairy.CursorPointer).
		OnPointerDown(func(p fairy.PointerEvent) {
			v := at(p.LocalX, p.W)
			if st.active = rangeThumb(value, v); st.active >= 0 {
				move(st.active, v)
			}
		}).
		OnDrag(func(d fairy.DragEvent) {
			if st.active < 0 {
				switch {
				case d.DX < 0:
					st.active = 0
				case d.DX > 0:
					st.active = 1
				default:
					return
				}
			}
			move(st.active, at(d.LocalX, d.W))
		})
}

// rangeThumb returns the thumb of a RangeSlider at value that a press at v
// moves: the nearer one, the lower one halfway between them, or -1 when both
// sit at v, since only the direction of the drag tells which.
func rangeThumb(value [2]float64, v float64) int {
	if value[0] == value[1] {
		switch {
		case v < value[0]:
			return 0
		case v > value[1]:
			return 1
		}
		return -1
	}
	if math.Abs(v-value[1]) < math.Abs(v-value[0]) {
		return 1
	}
	return 0
}

// rangeSet returns value with thumb i moved to v, rounded to step, within
// min and max, and stopped at the other thumb.
func rangeSet(value [2]float64, i int, v, minV, maxV, step float64) [2]float64 {
	if step > 0 {
		v = minV + math.Round((v-minV)/step)*step
	}
	v = min(max(v, minV), maxV)
	if i == 0 {
		v = min(v, value[1])
	} else {
		v = max(v, value[0])
	}
	value[i] = v
	return value
}
