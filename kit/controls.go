package kit

import (
	"math"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/motion"
	"github.com/myparsleycat/fairy/svg"
)

// Item is an entry of a select, combobox, radio group, tabs or segmented control.
type Item struct {
	Value       string
	Label       string
	Description string
	Icon        *svg.Vector
	Disabled    bool
	Tooltip     string
	Trailing    *fairy.El
	OnHover     func(bool)
	// Badge is a count or short tag a tab or segment shows after its label, in a secondary Badge.
	Badge string
	// Heading makes the item a section title in a Select list, which cannot be picked.
	Heading bool
}

// focusRing gives e the focus-visible style of shadcn/ui controls: a Ring
// colored border and a 3 pixel ring around it.
func focusRing(e *fairy.El) *fairy.El {
	t := &current
	return e.FocusVisibleBorder(t.Ring).FocusRing(3, alpha(t.Ring, 0.5))
}

// invalidRing gives e the style of a control with an error: a destructive
// border and ring, which stay while it has focus.
func invalidRing(e *fairy.El) *fairy.El {
	t := &current
	border, ring := t.Destructive, alpha(t.Destructive, 0.2)
	if t.Dark {
		border, ring = alpha(t.Destructive, 0.5), alpha(t.Destructive, 0.4)
	}
	return e.Border(1, border).Ring(3, ring).FocusVisibleBorder(border).FocusRing(3, ring)
}

// fieldFill is the background of inputs and select triggers.
func fieldFill() fairy.Color {
	if current.Dark {
		return alpha(current.Input, 0.3)
	}
	return fairy.Transparent
}

type buttonColors struct{ fill, hover, text, hoverText, border fairy.Color }

func variantColors(v Variant) buttonColors {
	t := &current
	switch v {
	case VariantSecondary:
		return buttonColors{t.Secondary, t.SecondaryHover, t.SecondaryForeground, fairy.Color{}, fairy.Transparent}
	case VariantOutline:
		if t.Dark {
			return buttonColors{alpha(t.Input, 0.3), alpha(t.Input, 0.5), t.Foreground, t.Foreground, t.Input}
		}
		return buttonColors{t.Background, t.Muted, t.Foreground, t.Foreground, t.Border}
	case VariantGhost:
		hover := t.Muted
		if t.Dark {
			hover = alpha(t.Muted, 0.5)
		}
		return buttonColors{fairy.Transparent, hover, fairy.Color{}, t.Foreground, fairy.Transparent}
	case VariantDestructive:
		if t.Dark {
			return buttonColors{alpha(t.Destructive, 0.2), alpha(t.Destructive, 0.3), t.Destructive, fairy.Color{}, fairy.Transparent}
		}
		return buttonColors{alpha(t.Destructive, 0.1), alpha(t.Destructive, 0.2), t.Destructive, fairy.Color{}, fairy.Transparent}
	case VariantLink:
		return buttonColors{fairy.Transparent, fairy.Transparent, t.Primary, alpha(t.Primary, 0.8), fairy.Transparent}
	}
	return buttonColors{t.Primary, alpha(t.Primary, 0.8), t.PrimaryForeground, fairy.Color{}, fairy.Transparent}
}

// pressable styles e as a shadcn/ui button: variant colors that fade on
// hover, a focus ring, a one pixel push while pressed, and onClick unless
// disabled.
func pressable(e *fairy.El, v Variant, o options, onClick func()) *fairy.El {
	t := &current
	c := variantColors(v)
	e.Fill(c.fill).Border(1, c.border).FillPadding().Animate(motion.Fast).Role(fairy.RoleButton)
	if c.text.A > 0 {
		e.Color(c.text)
	}
	if o.tooltip != "" {
		e.Tooltip(o.tooltip)
	}
	if o.disabled || o.loading {
		return e.Opacity(0.5).Isolate().Cursor(fairy.CursorNotAllowed).Disabled(true)
	}
	e.HoverFill(c.hover).PressOffset(0, 1).Focusable().Cursor(fairy.CursorPointer)
	if c.hoverText.A > 0 {
		e.HoverColor(c.hoverText)
	}
	if v == VariantDestructive {
		ring := alpha(t.Destructive, 0.2)
		if t.Dark {
			ring = alpha(t.Destructive, 0.4)
		}
		e.FocusVisibleBorder(alpha(t.Destructive, 0.4)).FocusRing(3, ring)
	} else {
		focusRing(e)
	}
	if onClick != nil {
		e.OnClick(onClick)
	}
	return e
}

// Button is a labeled button. The default variant is Default, filled with
// Primary.
func Button(label string, onClick func(), opts ...Option) *fairy.El {
	o := build(opts)
	is := iconSize(o.size)
	var kids []*fairy.El
	switch {
	case o.loading:
		kids = append(kids, fairy.Spinner().Size(16, 16))
	case o.icon != nil:
		kids = append(kids, fairy.Icon(o.icon).Size(is, is))
	}
	if label != "" {
		kids = append(kids, fairy.Text(label).NoWrap())
	}
	if o.iconRight != nil && !o.loading {
		kids = append(kids, fairy.Icon(o.iconRight).Size(is, is))
	}
	pad, iconPad, gap := float32(10), float32(8), float32(6)
	switch o.size {
	case SizeXS:
		pad, iconPad, gap = 8, 6, 4
	case SizeSmall:
		pad, iconPad, gap = 10, 6, 4
	}
	left, right := pad, pad
	if o.iconInline && (o.icon != nil || o.loading) {
		left = iconPad
	}
	if o.iconInline && o.iconRight != nil {
		right = iconPad
	}
	e := fairy.Row(kids...).H(controlHeight(o.size)).PadEdges(0, right, 0, left).Gap(gap).
		Align(fairy.Center).Justify(fairy.JustifyCenter).Shrink(0).
		Radius(controlRadius(o.size)).FontSize(fontSize(o.size)).LineHeight(fontSize(o.size) * 20 / 14).Medium()
	if o.size == SizeXS {
		e.LineHeight(fontSize(o.size) * 4 / 3)
	}
	return pressable(e, o.variant, o, onClick)
}

// IconButton is a square button showing only an icon. The default variant is
// Ghost; give it a Tooltip that names the action.
func IconButton(icon *svg.Vector, onClick func(), opts ...Option) *fairy.El {
	o := build(opts)
	if !o.variantSet {
		o.variant = VariantGhost
	}
	h := controlHeight(o.size)
	is := float32(16)
	if o.size == SizeXS {
		is = 12
	}
	inner := fairy.Icon(icon).Size(is, is)
	if o.loading {
		inner = fairy.Spinner().Size(16, 16)
	}
	e := fairy.Box(inner).Size(h, h).Shrink(0).Center().Radius(controlRadius(o.size))
	return pressable(e, o.variant, o, onClick)
}

// labeled puts a label and description next to a control; clicking the
// label acts like clicking the control. The label names the control for
// assistive technology.
func labeled(control *fairy.El, o options, onClick func()) *fairy.El {
	t := &current
	if o.disabled {
		control.Disabled(true)
	}
	if o.label == "" {
		return control
	}
	control.Label(o.label)
	text := fairy.Col(fairy.Text(o.label).Medium().LineHeight(t.FontSize + 2).Role(fairy.RoleHidden)).Gap(2).Grow(1).MinW(0)
	if o.description != "" {
		text.Child(fairy.Text(o.description).Color(t.MutedForeground).LineHeight(t.FontSize * 1.5))
	}
	row := fairy.Row(control, text).Gap(8).Align(fairy.Start).Role(fairy.RoleNone)
	if o.disabled {
		// The control fades itself; fading the row as well would fade it twice.
		text.Opacity(0.5)
		return row
	}
	return row.Cursor(fairy.CursorPointer).OnClick(onClick)
}

// Switch is an on/off toggle. Small makes it 24 by 14 pixels.
func Switch(on bool, onChange func(bool), opts ...Option) *fairy.El {
	o := build(opts)
	t := &current
	w, h, knob := float32(32), float32(18.4), float32(16)
	if o.size == SizeSmall || o.size == SizeXS {
		w, h, knob = 24, 14, 12
	}
	fill, thumb := t.Input, t.Background
	if t.Dark {
		fill, thumb = alpha(t.Input, 0.8), t.Foreground
	}
	left := float32(1)
	if on {
		fill, left = t.Primary, 1+w-2-knob
		if t.Dark {
			thumb = t.PrimaryForeground
		}
	}
	toggle := func() { onChange(!on) }
	track := fairy.Box(
		fairy.Box().Size(knob, knob).Radius(knob/2).Fill(thumb).Animate(motion.Fast).
			Left(left).Top((h-knob)/2).AnimateLayout(motion.Fast),
	).Size(w, h).Shrink(0).Radius(h/2).Fill(fill).Border(1, fairy.Transparent).Animate(motion.Fast).
		Role(fairy.RoleSwitch).Checked(on)
	if o.label != "" {
		track.MarginEdges(1, 0, 0, 0)
	}
	if o.invalid {
		invalidRing(track)
	}
	if o.disabled {
		track.Opacity(0.5).Isolate().Cursor(fairy.CursorNotAllowed)
	} else {
		focusRing(track).Focusable().Cursor(fairy.CursorPointer).OnClick(toggle)
	}
	return labeled(track, o, toggle)
}

// Checkbox is a box that is checked or not; Indeterminate shows a dash.
func Checkbox(checked bool, onChange func(bool), opts ...Option) *fairy.El {
	o := build(opts)
	t := &current
	toggle := func() { onChange(!checked) }
	box := fairy.Box().Size(16, 16).Shrink(0).Radius(4).Center().Animate(motion.Fast).MarginEdges(1, 0, 0, 0).
		Role(fairy.RoleCheckBox).Checked(checked || o.indeterminate)
	switch {
	case checked || o.indeterminate:
		mark := lucide.Check
		if o.indeterminate {
			mark = lucide.Minus
		}
		box.Fill(t.Primary).Border(1, t.Primary).Child(fairy.Icon(mark).Size(14, 14).Color(t.PrimaryForeground))
	default:
		box.Fill(fieldFill()).Border(1, t.Input)
		if o.invalid {
			invalidRing(box)
		}
	}
	if o.disabled {
		box.Opacity(0.5).Isolate().Cursor(fairy.CursorNotAllowed)
	} else {
		focusRing(box).Focusable().Cursor(fairy.CursorPointer).OnClick(toggle)
	}
	return labeled(box, o, toggle)
}

// Radio is one choice of a group; onSelect runs when it is chosen.
func Radio(selected bool, onSelect func(), opts ...Option) *fairy.El {
	o := build(opts)
	t := &current
	dot := fairy.Box().Size(16, 16).Shrink(0).Radius(8).Center().Animate(motion.Fast).MarginEdges(1, 0, 0, 0).
		Role(fairy.RoleRadio).Selected(selected)
	if selected {
		dot.Fill(t.Primary).Border(1, t.Primary).Child(fairy.Box().Size(6, 6).Radius(3).Fill(t.PrimaryForeground))
	} else {
		dot.Fill(fieldFill()).Border(1, t.Input)
		if o.invalid {
			invalidRing(dot)
		}
	}
	if o.disabled {
		dot.Opacity(0.5).Isolate().Cursor(fairy.CursorNotAllowed)
	} else {
		focusRing(dot).Focusable().FocusSelected(selected).Cursor(fairy.CursorPointer).OnClick(onSelect)
	}
	return labeled(dot, o, onSelect)
}

// RadioGroup lists one radio per item. Tab enters the group on the selected
// radio and the arrow keys select the previous or next one.
func RadioGroup(value string, items []Item, onChange func(string)) *fairy.El {
	col := fairy.Col().Gap(12).Role(fairy.RoleGroup).FocusGroup()
	for _, it := range items {
		col.Child(Radio(it.Value == value, func() {
			if it.Value != value {
				onChange(it.Value)
			}
		},
			Label(it.Label), Description(it.Description), Disabled(it.Disabled)).Key(it.Value))
	}
	return col
}

// Slider picks a number between min and max, rounded to step (0 for none).
// The arrow keys, Page Up, Page Down, Home and End move it.
func Slider(value, minV, maxV, step float64, onChange func(float64), opts ...Option) *fairy.El {
	o := build(opts)
	t := &current
	frac := 0.0
	if maxV > minV {
		frac = min(max((value-minV)/(maxV-minV), 0), 1)
	}
	setValue := func(v float64) {
		if step > 0 {
			v = minV + math.Round((v-minV)/step)*step
		}
		if v = min(max(v, minV), maxV); v != value {
			onChange(v)
		}
	}
	const thumbSize = 12
	set := func(x, w float32) {
		f := min(max(float64((x-thumbSize/2)/max(w-thumbSize, 1)), 0), 1)
		setValue(minV + f*(maxV-minV))
	}
	small := step
	if small <= 0 {
		small = (maxV - minV) / 100
	}
	large := max((maxV-minV)/10, small)
	thumb := fairy.Box().Size(thumbSize, thumbSize).Shrink(0).Radius(thumbSize/2).Fill(fairy.Hex(0xffffff)).Border(1, t.Ring).
		Animate(motion.Fast)
	// The track runs under the thumb's center at both ends, as in CSS where
	// the thumb overhangs the track by half its width.
	track := fairy.Row(
		fairy.Box().W(thumbSize/2).HFull().Fill(t.Primary),
		fairy.Box().W(0).HFull().Grow(float32(frac)+1e-4).Fill(t.Primary),
		fairy.Box().W(0).Grow(float32(1-frac)+1e-4),
		fairy.Box().W(thumbSize/2),
	).H(4).Grow(1).Radius(2).Fill(t.Muted).Clip()
	e := fairy.Row(
		fairy.Box(track).Absolute().Left(0).Right(0).Top(6).H(4),
		fairy.Box().W(0).Grow(float32(frac)+1e-4),
		thumb,
		fairy.Box().W(0).Grow(float32(1-frac)+1e-4),
	).H(16).Align(fairy.Center).MinW(80).Role(fairy.RoleSlider).RangeValue(value, minV, maxV, step, setValue)
	if o.label != "" {
		e.Label(o.label)
	}
	if o.disabled {
		return e.Opacity(0.5).Isolate().Disabled(true)
	}
	// The row takes focus, so a press anywhere on the track focuses it; the
	// thumb shows the ring.
	thumb.HoverRing(3, alpha(t.Ring, 0.5)).FocusRing(3, alpha(t.Ring, 0.5)).FocusFromParent()
	return e.Cursor(fairy.CursorPointer).Focusable().
		OnPointerDown(func(p fairy.PointerEvent) { set(p.LocalX, p.W) }).
		OnDrag(func(d fairy.DragEvent) { set(d.LocalX, d.W) }).
		OnKey(func(k keyboard.KeyEvent) bool {
			if k.Mods != 0 {
				return false
			}
			switch k.Key {
			case keyboard.KeyLeft, keyboard.KeyDown:
				setValue(value - small)
			case keyboard.KeyRight, keyboard.KeyUp:
				setValue(value + small)
			case keyboard.KeyPageDown:
				setValue(value - large)
			case keyboard.KeyPageUp:
				setValue(value + large)
			case keyboard.KeyHome:
				setValue(minV)
			case keyboard.KeyEnd:
				setValue(maxV)
			default:
				return false
			}
			return true
		})
}

func toneColor(tone Tone) fairy.Color {
	t := &current
	switch tone {
	case ToneSuccess:
		return t.Success
	case ToneWarning:
		return t.Warning
	case ToneDestructive:
		return t.Destructive
	}
	return fairy.Color{}
}

// Badge is a small label, such as a status or a count. The default variant is
// Default; WithTone colors it by status instead. A MaxW on the badge cuts its
// text with an ellipsis.
func Badge(text string, opts ...Option) *fairy.El {
	o := build(opts)
	t := &current
	soft := float32(0.1)
	if t.Dark {
		soft = 0.2
	}
	bg, fg, border := t.Primary, t.PrimaryForeground, fairy.Transparent
	switch o.variant {
	case VariantSecondary:
		bg, fg = t.Secondary, t.SecondaryForeground
	case VariantDestructive:
		bg, fg = alpha(t.Destructive, soft), t.Destructive
	case VariantOutline:
		bg, fg, border = fairy.Transparent, t.Foreground, t.Border
	case VariantGhost, VariantLink:
		bg, fg = fairy.Transparent, t.Foreground
	}
	if c := toneColor(o.tone); c.A > 0 {
		bg, fg = alpha(c, soft), c
	}
	left, right := float32(8), float32(8)
	if o.icon != nil {
		left = 6
	}
	e := fairy.Row().Gap(4).Align(fairy.Center).H(20).PadEdges(0, right, 0, left).Shrink(0).Radius(999).
		Fill(bg).Border(1, border).Color(fg).FontSize(t.SmallSize).LineHeight(t.SmallSize * 4 / 3).Medium()
	if o.icon != nil {
		e.Child(fairy.Icon(o.icon).Size(12, 12).Shrink(0))
	}
	return e.Child(fairy.Text(text).MinW(0).Ellipsis().TooltipWhenTruncated())
}

// Progress is a bar filled to value (0 to 1). WithTone colors it by status.
func Progress(value float64, opts ...Option) *fairy.El {
	o := build(opts)
	t := &current
	color := t.Primary
	if c := toneColor(o.tone); c.A > 0 {
		color = c
	}
	v := float32(min(max(value, 0), 1))
	return fairy.Row(
		fairy.Box().Grow(v+1e-4).Fill(color).AnimateLayout(motion.Fast),
		fairy.Box().Grow(1-v+1e-4),
	).H(4).Radius(2).Fill(t.Muted).Clip().MinW(40).
		Role(fairy.RoleProgressBar).RangeValue(min(max(value, 0), 1)*100, 0, 100, 1, nil)
}

// ProgressCircle is a small ring that fills clockwise from 12 o'clock to
// value (0 to 1), sweeping to a new value as Progress moves. It is 16 pixels
// unless sized with Size; the Label option names what is progressing.
func ProgressCircle(value float64, size float32, opts ...Option) *fairy.El {
	o := build(opts)
	if size <= 0 {
		size = 16
	}
	v := min(max(value, 0), 1)
	ring := func(sweep float32) *fairy.El {
		return fairy.Arc(0, sweep).Stroke(size / 8).Absolute().Inset(0).Pad(size / 16)
	}
	e := fairy.Box(
		ring(2*math.Pi).Opacity(0.2),
		ring(float32(v*2*math.Pi)).Animate(motion.Fast),
	).Size(size, size).Shrink(0).Color(current.MutedForeground).
		Role(fairy.RoleProgressBar).RangeValue(math.Round(v*100), 0, 100, 1, nil)
	if o.label != "" {
		e.Label(o.label)
	}
	return e
}

// Spinner is a busy indicator in the surrounding text color.
func Spinner(size float32) *fairy.El { return fairy.Spinner().Size(size, size) }

// Skeleton is a placeholder block that pulses while content loads; size it
// with W and H.
func Skeleton() *fairy.El {
	return fairy.Box().Radius(current.RadiusMd()).Fill(current.Muted).Pulse(2, 0.5)
}

// Separator is a thin horizontal line.
func Separator() *fairy.El { return fairy.Box().H(1).Shrink(0).Fill(current.Border) }

// VSeparator is a thin vertical line.
func VSeparator() *fairy.El {
	return fairy.Box().W(1).Shrink(0).AlignSelf(fairy.Stretch).Fill(current.Border)
}

// Card is a surface for grouping content.
func Card(children ...*fairy.El) *fairy.El {
	t := &current
	return fairy.Col(children...).Gap(16).Pad(16).Radius(t.RadiusXl()).Fill(t.Card).Color(t.CardForeground).
		Ring(1, alpha(t.Foreground, 0.1))
}

// Kbd shows a keyboard key.
func Kbd(keys string) *fairy.El {
	t := &current
	return fairy.Row(fairy.Text(keys).NoWrap()).H(20).MinW(20).PadXY(4, 0).Shrink(0).Center().
		Radius(t.RadiusSm()).Fill(t.Muted).Color(t.MutedForeground).FontSize(t.SmallSize).Medium()
}

// KbdChord shows a chord as one keycap per key, as in "Ctrl" "Shift" "T".
func KbdChord(c keyboard.Chord) *fairy.El {
	row := fairy.Row().Gap(4).Align(fairy.Center).Role(fairy.RoleText).Label(c.Label())
	for _, k := range c.Keycaps() {
		row.Child(Kbd(k))
	}
	return row
}

// fieldBox styles e as the box of a text field or select trigger.
func fieldBox(e *fairy.El, o options) *fairy.El {
	t := &current
	e.Radius(t.RadiusLg()).Fill(fieldFill()).Border(1, t.Border).FocusBorder(t.Ring).
		FocusRing(3, alpha(t.Ring, 0.5)).Animate(motion.Fast).FontSize(t.FontSize)
	if o.invalid {
		invalidRing(e)
	}
	if o.disabled {
		disabled := alpha(t.Input, 0.5)
		if t.Dark {
			disabled = alpha(t.Input, 0.8)
		}
		e.Fill(disabled).Opacity(0.5).Isolate().Cursor(fairy.CursorNotAllowed)
	}
	return e
}

// TextField is a single-line text input.
func TextField(value string, onChange func(string), opts ...Option) *fairy.El {
	o := build(opts)
	t := &current
	in := fairy.Input(value, onChange).Placeholder(o.placeholder).PlaceholderColor(t.MutedForeground).
		FontSize(t.FontSize).LineHeight(t.FontSize * 20 / 14).Grow(1).MinW(0)
	if o.onSubmit != nil {
		in.OnSubmit(o.onSubmit)
	}
	if o.onPaste != nil {
		in.OnPaste(o.onPaste)
	}
	if o.disabled {
		in = fairy.Text(value).Grow(1).MinW(0).Ellipsis().Role(fairy.RoleEdit).Disabled(true)
	}
	if o.label != "" {
		in.Label(o.label)
	}
	if o.icon == nil {
		return fieldBox(in.H(controlHeight(o.size)).PadXY(10, 0).Justify(fairy.JustifyCenter), o)
	}
	return fieldBox(fairy.Row(fairy.Icon(o.icon).Size(16, 16).Color(t.MutedForeground), in).
		H(controlHeight(o.size)).PadXY(10, 0).Gap(8).Align(fairy.Center).FocusWithin(), o)
}

// TextArea is a multi-line text input that grows with its text.
func TextArea(value string, onChange func(string), opts ...Option) *fairy.El {
	o := build(opts)
	t := &current
	var submit func()
	if o.onSubmit != nil && onChange != nil {
		// Several keys can be handled before the next build, so Enter submits
		// the latest edit rather than the value this build was given.
		latest, change, f := value, onChange, o.onSubmit
		onChange = func(s string) {
			latest = s
			change(s)
		}
		submit = func() { f(latest) }
	}
	e := fairy.Input(value, onChange).Multiline().Placeholder(o.placeholder).PlaceholderColor(t.MutedForeground).
		MinH(64).PadXY(10, 8).LineHeight(20)
	fieldBox(e, o)
	if submit != nil {
		e.OnInputKey(func(k keyboard.KeyEvent) bool {
			if k.Key != keyboard.KeyEnter || k.Mods&^keyboard.ModCtrl != 0 {
				return false
			}
			submit()
			return true
		})
	}
	if o.onPaste != nil {
		e.OnPaste(o.onPaste)
	}
	if o.label != "" {
		e.Label(o.label)
	}
	if o.disabled {
		e.Disabled(true)
	}
	return e
}

// Field labels a form control, with an optional Description, or an error
// message when Invalid is set together with Description. The label also
// names the control for assistive technology.
func Field(label string, control *fairy.El, opts ...Option) *fairy.El {
	o := build(opts)
	t := &current
	control.Label(label)
	head := fairy.Text(label).Medium().LineHeight(t.FontSize + 2).Role(fairy.RoleHidden)
	if o.invalid {
		head.Color(t.Destructive)
	}
	col := fairy.Col(head, control).Gap(8)
	if o.description != "" {
		c := t.MutedForeground
		if o.invalid {
			c = t.Destructive
		}
		col.Child(fairy.Text(o.description).Color(c).LineHeight(t.FontSize * 1.5))
	}
	return col
}
