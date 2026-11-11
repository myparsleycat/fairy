package kit

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/motion"
)

// ColorAlpha adds an opacity slider to a ColorPicker, and its hex field and
// label show #RRGGBBAA. Without it the picker keeps the value's alpha.
var ColorAlpha Option = func(o *options) { o.alpha = true }

// ColorSwatchField draws a ColorPicker's button as a field filled by a swatch,
// without the hex text, as a browser draws an <input type="color"> styled as a
// text field. The swatch has the browser's gray frame.
var ColorSwatchField Option = func(o *options) { o.swatchField = true }

// HexString formats c as #RRGGBB, or #RRGGBBAA when alpha is true.
func HexString(c fairy.Color, alpha bool) string {
	s := fmt.Sprintf("#%02X%02X%02X", byte8(c.R), byte8(c.G), byte8(c.B))
	if alpha {
		s += fmt.Sprintf("%02X", byte8(c.A))
	}
	return s
}

// ParseHexColor reads #RGB, #RGBA, #RRGGBB or #RRGGBBAA, with or without the
// "#". It reports false for anything else.
func ParseHexColor(s string) (fairy.Color, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	switch len(s) {
	case 3, 4:
		var b strings.Builder
		for _, r := range s {
			b.WriteRune(r)
			b.WriteRune(r)
		}
		s = b.String()
	case 6, 8:
	default:
		return fairy.Color{}, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return fairy.Color{}, false
	}
	if len(s) == 6 {
		return fairy.Hex(uint32(v)), true
	}
	return fairy.HexA(uint32(v)), true
}

func byte8(f float32) uint8 { return uint8(math.Round(float64(min(max(f, 0), 1)) * 255)) }

// quantize rounds c to 8 bits per channel, as a hex string holds it.
func quantize(c fairy.Color) fairy.Color {
	q := func(f float32) float32 { return float32(byte8(f)) / 255 }
	return fairy.Color{R: q(c.R), G: q(c.G), B: q(c.B), A: q(c.A)}
}

func sameColor(a, b fairy.Color) bool { return quantize(a) == quantize(b) }

// hsv is a color as hue (0 to 360), saturation and value (0 to 1) and alpha.
type hsv struct{ h, s, v, a float64 }

func (c hsv) color() fairy.Color {
	h := math.Mod(c.h, 360) / 60
	f := h - math.Floor(h)
	p, q, t := c.v*(1-c.s), c.v*(1-c.s*f), c.v*(1-c.s*(1-f))
	var r, g, b float64
	switch int(h) {
	case 0:
		r, g, b = c.v, t, p
	case 1:
		r, g, b = q, c.v, p
	case 2:
		r, g, b = p, c.v, t
	case 3:
		r, g, b = p, q, c.v
	case 4:
		r, g, b = t, p, c.v
	default:
		r, g, b = c.v, p, q
	}
	return quantize(fairy.Color{R: float32(r), G: float32(g), B: float32(b), A: float32(c.a)})
}

// toHSV converts c, keeping hue (and saturation for black) where c does not
// determine them, so dragging through gray does not lose the hue.
func toHSV(c fairy.Color, keep hsv) hsv {
	r, g, b := float64(c.R), float64(c.G), float64(c.B)
	hi, lo := max(r, g, b), min(r, g, b)
	out := hsv{h: keep.h, s: keep.s, v: hi, a: float64(c.A)}
	d := hi - lo
	if hi > 0 {
		out.s = d / hi
	}
	if d > 0 {
		switch hi {
		case r:
			out.h = math.Mod((g-b)/d+6, 6) * 60
		case g:
			out.h = ((b-r)/d + 2) * 60
		default:
			out.h = ((r-g)/d + 4) * 60
		}
	}
	return out
}

type colorPickerState struct {
	open   bool
	hsv    hsv
	last   fairy.Color // the color hsv stands for; another value resets it
	synced bool
	from   fairy.Color // the color when a drag began, for Escape to restore
	down   hsv         // the color under the last press, which a click commits
	hex    string      // the hex field's text while it is being edited
	typing bool
}

const (
	pickerW     = 224
	areaH       = 150
	areaInset   = 3 // the focus border around the saturation and brightness plane
	thumbSize   = 14
	channelH    = 14
	checkerCell = 5
)

// ColorPicker is a color swatch button that opens a popover with a
// saturation and brightness area, a hue slider, an opacity slider with
// ColorAlpha, and a hex field, in place of HTML's <input type="color">.
//
// onChange receives each new color, rounded to 8 bits per channel. final is
// false while the pointer drags the area or a slider and true once a change is
// complete: the drag ends or is canceled with Escape (with the color from
// before the drag), a click or key moves a value, or the hex field is
// committed with Enter or by leaving it. id must be unique in the window.
//
// The area takes arrow keys (Shift moves ten times as far): left and right
// change saturation, up and down brightness. The sliders take arrows, Page Up,
// Page Down, Home and End. Label names the button for assistive technology.
func ColorPicker(id string, value fairy.Color, onChange func(c fairy.Color, final bool), opts ...Option) *fairy.El {
	o := build(opts)
	t := &current
	st := fairy.Local[colorPickerState]("kit.color:" + id)
	if !st.synced || !sameColor(value, st.last) {
		st.hsv = toHSV(value, st.hsv)
		st.last, st.synced = quantize(value), true
	}
	label := o.label
	if label == "" {
		label = Messages.Color
	}
	hexText := HexString(value, o.alpha)
	var trigger *fairy.El
	if o.swatchField {
		// Chromium insets the swatch by the field's 10px by 4px padding and the
		// swatch wrapper's 2px by 4px padding, inside the border.
		trigger = fairy.Row(fairy.Box().Grow(1).HFull().Fill(value).Border(1, fairy.Hex(0x777777))).
			H(controlHeight(o.size)).PadXY(12, 8).MinW(48)
	} else {
		trigger = fairy.Row(
			swatch(value, 18, 4),
			fairy.Text(hexText).Font(t.MonoFont).Grow(1).MinW(0).NoWrap(),
		).H(controlHeight(o.size)).PadXY(8, 0).Gap(8).Align(fairy.Center).MinW(112)
	}
	trigger.Radius(t.RadiusLg()).Fill(fieldFill()).Border(1, t.Input).FontSize(t.FontSize).
		Animate(motion.Fast).Role(fairy.RoleComboBox).Label(label)
	if o.tooltip != "" {
		trigger.Tooltip(o.tooltip)
	}
	if o.disabled {
		return trigger.Opacity(0.5).Isolate().Disabled(true)
	}
	focusRing(trigger).Focusable().Cursor(fairy.CursorPointer).
		OnClick(func() {
			st.open = !st.open
			st.typing = false
		}).Expanded(st.open)
	if !st.open {
		return trigger
	}

	setColor := func(c fairy.Color, final bool) {
		st.hsv = toHSV(c, st.hsv)
		st.last, st.typing = c, false
		onChange(c, final)
	}
	set := func(c hsv, final bool) {
		st.hsv = c
		st.last, st.typing = c.color(), false
		onChange(st.last, final)
	}
	// drag runs a pointer gesture on a control: a click commits the color
	// under the press, the moves of a drag preview it and the release commits
	// it, and Escape goes back to the color from before the drag. A press that
	// ends any other way changes nothing.
	drag := func(e *fairy.El, at func(x, y, w, h float32) hsv) *fairy.El {
		return e.OnPointerDown(func(p fairy.PointerEvent) {
			if p.Button != 0 {
				return
			}
			st.from = st.last
			st.down = at(p.LocalX, p.LocalY, p.W, p.H)
		}).OnClick(func() { set(st.down, true) }).
			OnDrag(func(d fairy.DragEvent) {
				switch {
				case d.Canceled:
					setColor(st.from, true)
				case d.Phase == fairy.DragEnd:
					set(at(d.LocalX, d.LocalY, d.W, d.H), true)
				default:
					set(at(d.LocalX, d.LocalY, d.W, d.H), false)
				}
			})
	}

	c := st.hsv
	area := colorArea(c, func(s, v float64, final bool) { c := st.hsv; c.s, c.v = s, v; set(c, final) })
	drag(area, func(x, y, w, h float32) hsv {
		c := st.hsv
		c.s = clamp01(float64((x - areaInset) / max(w-2*areaInset, 1)))
		c.v = 1 - clamp01(float64((y-areaInset)/max(h-2*areaInset, 1)))
		return c
	})
	pure := hsv{h: c.h, s: 1, v: 1, a: 1}.color()
	hue := channelSlider(Messages.Hue, c.h, 360, 1, hueTrack(), pure, func(v float64, final bool) {
		c := st.hsv
		c.h = math.Mod(v, 360)
		if v >= 360 {
			c.h = 359.999 // the right end is the same red, kept at the end
		}
		set(c, final)
	})
	drag(hue, func(x, _, w, _ float32) hsv {
		c := st.hsv
		c.h = min(channelFrac(x, w)*360, 359.999)
		return c
	})
	panel := popupSurface(fairy.Col(area, hue).Gap(12).Pad(10), false)
	if o.alpha {
		opaque := c
		opaque.a = 1
		alpha := channelSlider(Messages.Opacity, c.a*100, 100, 1, alphaTrack(opaque.color()), fairy.Transparent, func(v float64, final bool) {
			c := st.hsv
			c.a = clamp01(v / 100)
			set(c, final)
		})
		drag(alpha, func(x, _, w, _ float32) hsv {
			c := st.hsv
			c.a = channelFrac(x, w)
			return c
		})
		panel.Child(alpha)
	}

	draft := hexText
	if st.typing {
		draft = st.hex
	}
	commit := func() {
		if !st.typing {
			return
		}
		st.typing = false
		nc, ok := ParseHexColor(st.hex)
		if !ok {
			return
		}
		if !o.alpha {
			nc.A = value.A
		}
		setColor(quantize(nc), true)
	}
	field := fairy.Input(draft, func(s string) { st.hex, st.typing = s, true }).OnSubmit(func(string) { commit() }).
		OnBlur(commit).Label(Messages.Hex).Font(t.MonoFont).Grow(1).MinW(0).
		H(controlHeight(SizeSmall)).PadXY(8, 0).Justify(fairy.JustifyCenter)
	fieldBox(field, options{})
	panel.Child(fairy.Row(swatch(st.last, 28, t.RadiusMd()), field).Gap(8).Align(fairy.Center))

	return trigger.Popover(panel.W(pickerW+20), fairy.PopoverOptions{OnDismiss: func() {
		st.open, st.typing = false, false
	}})
}

func clamp01(f float64) float64 { return min(max(f, 0), 1) }

// channelFrac maps x on a channel slider of width w to 0..1; the thumb's
// center travels between half a thumb from either end.
func channelFrac(x, w float32) float64 {
	return clamp01(float64((x - thumbSize/2) / max(w-thumbSize, 1)))
}

// swatch shows c over a checkerboard, so that transparency shows.
func swatch(c fairy.Color, size, radius float32) *fairy.El {
	t := &current
	return fairy.Box(
		checker(size, size, radius),
		fairy.Box().Inset(0).Fill(c).Radius(radius).Border(1, t.Border),
	).Size(size, size).Role(fairy.RoleHidden)
}

// checker is a light and dark checkerboard of w×h.
func checker(w, h, radius float32) *fairy.El {
	k := [2]float32{w, h}
	img := checkers[k]
	if img == nil {
		// Drawn at twice the logical size, so the cells stay sharp on
		// high-DPI screens.
		const res = 2
		pw, ph := int(math.Ceil(float64(w*res))), int(math.Ceil(float64(h*res)))
		m := image.NewNRGBA(image.Rect(0, 0, pw, ph))
		for y := range ph {
			for x := range pw {
				c := color.NRGBA{0xff, 0xff, 0xff, 0xff}
				if (x/(checkerCell*res)+y/(checkerCell*res))%2 == 1 {
					c = color.NRGBA{0xd4, 0xd4, 0xd8, 0xff}
				}
				m.SetNRGBA(x, y, c)
			}
		}
		img = fairy.NewImage(m)
		checkers[k] = img
	}
	return fairy.ImageEl(img).Fit(fairy.FitFill).Inset(0).Radius(radius)
}

// checkers holds the checkerboard images by logical size. Views build on the
// UI thread, so it needs no lock.
var checkers = map[[2]float32]*fairy.Image{}

func thumb(fill fairy.Color, size float32) *fairy.El {
	return fairy.Box().Size(size, size).Radius(size/2).Fill(fill).Border(2, fairy.Hex(0xffffff)).
		Shadow(0, 0, 0, 1, fairy.RGBA(0, 0, 0, 0.35)).Shadow(0, 1, 3, 0, fairy.RGBA(0, 0, 0, 0.3))
}

// colorArea is the saturation (across) and brightness (up) plane of hue c.h.
func colorArea(c hsv, set func(s, v float64, final bool)) *fairy.El {
	t := &current
	pure := hsv{h: c.h, s: 1, v: 1, a: 1}.color()
	planeW, planeH := float32(pickerW-2*areaInset), float32(areaH-2*areaInset)
	plane := fairy.Box(
		fairy.Box().Inset(0).Radius(t.Radius-1).Gradient(90, fairy.Hex(0xffffff), fairy.Hex(0xffffff).Alpha(0)),
		fairy.Box().Inset(0).Radius(t.Radius-1).Gradient(180, fairy.Hex(0x000000).Alpha(0), fairy.Hex(0x000000)),
	).Inset(areaInset).Radius(t.Radius - 1).Fill(pure)
	opaque := c
	opaque.a = 1
	knob := thumb(opaque.color(), thumbSize).
		Left(areaInset + float32(c.s)*planeW - thumbSize/2).Top(areaInset + float32(1-c.v)*planeH - thumbSize/2)
	pct := func(f float64) int { return int(math.Round(f * 100)) }
	return fairy.Box(plane, knob).Size(pickerW, areaH).Radius(t.Radius+2).Border(2, fairy.Transparent).
		FocusBorder(t.Primary).Focusable().AutoFocus().Cursor(fairy.CursorCrosshair).
		Role(fairy.RoleSlider).Label(Messages.SaturationBrightness).
		RangeValue(float64(pct(c.s)), 0, 100, 1, func(v float64) { set(clamp01(v/100), c.v, true) }).
		ValueText(fmt.Sprintf(Messages.SaturationBrightnessValue, pct(c.s), pct(c.v))).
		OnKey(func(k keyboard.KeyEvent) bool {
			if k.Mods&^keyboard.ModShift != 0 {
				return false
			}
			if k.Key == keyboard.KeyEnter || k.Key == keyboard.KeySpace {
				return true // the click that commits a pointer press has nothing to commit
			}
			step := 0.01
			if k.Mods == keyboard.ModShift {
				step = 0.1
			}
			s, v := c.s, c.v
			switch k.Key {
			case keyboard.KeyLeft:
				s -= step
			case keyboard.KeyRight:
				s += step
			case keyboard.KeyUp:
				v += step
			case keyboard.KeyDown:
				v -= step
			default:
				return false
			}
			set(clamp01(s), clamp01(v), true)
			return true
		})
}

// channelSlider is a horizontal slider over a painted track for a value from
// 0 to maxV. Pointer handling is added by the caller.
func channelSlider(label string, value, maxV, step float64, track *fairy.El, knobFill fairy.Color, set func(v float64, final bool)) *fairy.El {
	t := &current
	frac := float32(clamp01(value / maxV))
	trackW := float32(pickerW - thumbSize)
	knob := thumb(knobFill, thumbSize).Left(frac * trackW).Top(0)
	move := func(v float64) { set(min(max(v, 0), maxV), true) }
	return fairy.Box(track, knob).Size(pickerW, channelH).Radius(channelH/2).Border(1, fairy.Transparent).
		FocusBorder(t.Primary).Focusable().Cursor(fairy.CursorPointer).
		Role(fairy.RoleSlider).Label(label).RangeValue(math.Round(value), 0, maxV, step, move).
		OnKey(func(k keyboard.KeyEvent) bool {
			if k.Mods&^keyboard.ModShift != 0 {
				return false
			}
			if k.Key == keyboard.KeyEnter || k.Key == keyboard.KeySpace {
				return true // the click that commits a pointer press has nothing to commit
			}
			small := step
			if k.Mods == keyboard.ModShift {
				small *= 10
			}
			switch k.Key {
			case keyboard.KeyLeft, keyboard.KeyDown:
				move(math.Round(value) - small)
			case keyboard.KeyRight, keyboard.KeyUp:
				move(math.Round(value) + small)
			case keyboard.KeyPageDown:
				move(math.Round(value) - maxV/10)
			case keyboard.KeyPageUp:
				move(math.Round(value) + maxV/10)
			case keyboard.KeyHome:
				move(0)
			case keyboard.KeyEnd:
				move(maxV)
			default:
				return false
			}
			return true
		})
}

// trackBox positions a channel's track so that its ends line up with the
// thumb's center at either end of the slider.
func trackBox() *fairy.El {
	return fairy.Row().Left(thumbSize / 2).Right(thumbSize / 2).Top(3).Bottom(3)
}

func hueTrack() *fairy.El {
	row := trackBox()
	for i := range 6 {
		from := hsv{h: float64(i) * 60, s: 1, v: 1, a: 1}.color()
		to := hsv{h: float64(i+1) * 60, s: 1, v: 1, a: 1}.color()
		seg := fairy.Box().Grow(1).Basis(0).Gradient(90, from, to)
		switch i {
		case 0:
			seg.Radii(4, 0, 0, 4)
		case 5:
			seg.Radii(0, 4, 4, 0)
		}
		row.Child(seg)
	}
	return row
}

func alphaTrack(c fairy.Color) *fairy.El {
	h := float32(channelH - 6)
	return trackBox().Child(fairy.Box(
		checker(pickerW-thumbSize, h, 4),
		fairy.Box().Inset(0).Radius(4).Gradient(90, c.Alpha(0), c),
	).Grow(1))
}
