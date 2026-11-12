package kit

import "github.com/myparsleycat/fairy/svg"

// Variant is the look of a button or badge, as shadcn/ui's variants.
type Variant uint8

const (
	VariantDefault Variant = iota // filled with Primary
	VariantSecondary
	VariantOutline
	VariantGhost
	VariantDestructive
	VariantLink
)

// Tone colors a badge or progress bar by status.
type Tone uint8

const (
	ToneNone Tone = iota
	ToneSuccess
	ToneWarning
	ToneDestructive
)

// Size scales a control.
type Size uint8

const (
	SizeDefault Size = iota // 32 pixels high
	SizeXS                  // 24
	SizeSmall               // 28
	SizeLarge               // 36
)

type options struct {
	variant       Variant
	variantSet    bool
	tone          Tone
	line          bool
	indeterminate bool
	size          Size
	icon          *svg.Vector
	iconRight     *svg.Vector
	iconInline    bool
	disabled      bool
	loading       bool
	placeholder   string
	tooltip       string
	label         string
	description   string
	invalid       bool
	unmodified    bool
	alpha         bool
	swatchField   bool
	onSubmit      func(string)
	onPaste       func() bool
	rowAction     *rowAction
	onHighlight   func(string)
	onOpenChange  func(bool)
	emptyText     string
	maxWidth      float32
	maxHeight     float32
	width         float32
	strokeWidth   float32
	alignEnd      bool
}

type rowAction struct {
	icon    *svg.Vector
	label   string
	onClick func(value string)
}

// Option configures a component.
type Option func(*options)

func build(opts []Option) options {
	var o options
	for _, f := range opts {
		if f != nil {
			f(&o)
		}
	}
	return o
}

// Variant presets.
var (
	// Default fills a button or badge with Primary.
	Default = WithVariant(VariantDefault)
	// Secondary fills a button or badge with Secondary.
	Secondary = WithVariant(VariantSecondary)
	// Outline draws a bordered button or badge.
	Outline = WithVariant(VariantOutline)
	// Ghost draws a button without a fill until hovered.
	Ghost = WithVariant(VariantGhost)
	// Destructive tints a button or badge with Destructive.
	Destructive = WithVariant(VariantDestructive)
	// Link draws a button as a Primary-colored link.
	Link = WithVariant(VariantLink)
)

// Size presets.
var (
	// XS makes a control 24 pixels high.
	XS = WithSize(SizeXS)
	// Small makes a control 28 pixels high.
	Small = WithSize(SizeSmall)
	// Large makes a control 36 pixels high.
	Large = WithSize(SizeLarge)
)

var (
	// Line draws Tabs as a row of triggers over a line that marks the
	// selected one, as shadcn/ui's line variant.
	Line Option = func(o *options) { o.line = true }

	// IconInline narrows a Button's padding on the side of its icon, as
	// shadcn/ui does for an icon marked data-icon="inline-start" or
	// "inline-end". Without it the padding is the same as a text-only button.
	IconInline Option = func(o *options) { o.iconInline = true }
)

// WithTone colors a badge or progress bar by status.
func WithTone(t Tone) Option { return func(o *options) { o.tone = t } }

// Indeterminate shows a Checkbox as partly checked, with a dash.
func Indeterminate(b bool) Option { return func(o *options) { o.indeterminate = b } }

// WithVariant sets the variant.
func WithVariant(v Variant) Option { return func(o *options) { o.variant, o.variantSet = v, true } }

// WithSize sets the size of a control.
func WithSize(s Size) Option { return func(o *options) { o.size = s } }

// Icon puts an icon before the label.
func Icon(v *svg.Vector) Option { return func(o *options) { o.icon = v } }

// IconRight puts an icon after the label.
func IconRight(v *svg.Vector) Option { return func(o *options) { o.iconRight = v } }

// Disabled grays the control out and ignores input when d is true.
func Disabled(d bool) Option { return func(o *options) { o.disabled = d } }

// Loading shows a spinner and ignores input when l is true.
func Loading(l bool) Option { return func(o *options) { o.loading = l } }

// Placeholder sets the hint shown while a field or select is empty.
func Placeholder(s string) Option { return func(o *options) { o.placeholder = s } }

// Tooltip sets the text shown when hovering.
func Tooltip(s string) Option { return func(o *options) { o.tooltip = s } }

// Label puts a label next to a switch, checkbox or radio, and names a
// slider, select, text field or text area for assistive technology.
func Label(s string) Option { return func(o *options) { o.label = s } }

// Description adds secondary text under a label.
func Description(s string) Option { return func(o *options) { o.description = s } }

// AllowUnmodified lets a ShortcutRecorder propose chords without Ctrl, Alt or
// Win, such as Space or Shift+A, for shortcuts that work only inside the app.
var AllowUnmodified Option = func(o *options) { o.unmodified = true }

// OnSubmit runs with the text when Enter is pressed in a TextField or
// TextArea. In a TextArea, Enter and Ctrl+Enter submit and Shift+Enter
// inserts a newline. The Enter that confirms an input method composition does
// not submit.
func OnSubmit(f func(string)) Option { return func(o *options) { o.onSubmit = f } }

// OnPaste runs when the user pastes into a TextField or TextArea, before the
// clipboard text is inserted; returning true skips the insertion. See
// fairy.El.OnPaste.
func OnPaste(f func() bool) Option { return func(o *options) { o.onPaste = f } }

// RowAction gives each enabled row of an open Select an icon button, shown
// while the pointer is over the row. Clicking it closes the list and runs
// onClick with the row's value instead of choosing the row. label names the
// button and is its tooltip.
func RowAction(icon *svg.Vector, label string, onClick func(value string)) Option {
	return func(o *options) { o.rowAction = &rowAction{icon, label, onClick} }
}

// OnHighlight runs with the value of the row that the arrow keys or the
// pointer highlight in the open list of a Select, Combobox or MultiCombobox,
// and with "" when no row is highlighted or the list closes. A
// MultiCombobox also reports the chip under the pointer while no row is
// highlighted. f runs on the UI thread after the update that changed the
// highlight, so it can change what the rest of the view shows.
func OnHighlight(f func(value string)) Option { return func(o *options) { o.onHighlight = f } }

// OnOpenChange runs when the list of a Select, Combobox or MultiCombobox
// opens or closes, on the UI thread after the update that did it.
func OnOpenChange(f func(open bool)) Option { return func(o *options) { o.onOpenChange = f } }

// EmptyText replaces Messages.NoResults as what a Combobox or MultiCombobox
// shows when no item matches the search.
func EmptyText(s string) Option { return func(o *options) { o.emptyText = s } }

// MaxWidth caps the width of a DropdownMenu, as a max-w class on shadcn/ui's
// DropdownMenuContent. Longer item labels end in an ellipsis.
func MaxWidth(w float32) Option { return func(o *options) { o.maxWidth = w } }

// Width fixes the width of a DropdownMenu or ContextMenu, as a w class on
// shadcn/ui's menu content. Longer item labels end in an ellipsis.
func Width(w float32) Option { return func(o *options) { o.width = w } }

// MaxHeight caps the height of the open list of a Select or the panel of a
// DropdownMenu, below the room beside the trigger. The rows scroll.
func MaxHeight(h float32) Option { return func(o *options) { o.maxHeight = h } }

// StrokeWidth sets the line width of a ProgressCircle in pixels, in place of
// its 2-pixel default. A width that is not positive keeps the default.
func StrokeWidth(px float32) Option { return func(o *options) { o.strokeWidth = px } }

// AlignEnd lines a DropdownMenu's end edge up with its trigger's, as
// align="end" on shadcn/ui's DropdownMenuContent.
var AlignEnd Option = func(o *options) { o.alignEnd = true }

// Invalid marks a field as having an error.
func Invalid(b bool) Option { return func(o *options) { o.invalid = b } }

// controlHeight is the height of buttons, inputs and selects.
func controlHeight(s Size) float32 {
	switch s {
	case SizeXS:
		return 24
	case SizeSmall:
		return 28
	case SizeLarge:
		return 36
	}
	return 32
}

// fontSize is the text size of buttons of size s.
func fontSize(s Size) float32 {
	switch s {
	case SizeXS:
		return current.SmallSize
	case SizeSmall:
		return 12.8
	}
	return current.FontSize
}

func iconSize(s Size) float32 {
	switch s {
	case SizeXS:
		return 12
	case SizeSmall:
		return 14
	}
	return 16
}

// controlRadius is the corner radius of buttons of size s.
func controlRadius(s Size) float32 {
	if s == SizeXS || s == SizeSmall {
		return current.RadiusMd()
	}
	return current.RadiusLg()
}
