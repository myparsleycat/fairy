package fairy

import (
	"math"

	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/layout"
	"github.com/myparsleycat/fairy/motion"
	"github.com/myparsleycat/fairy/svg"
)

type elKind uint8

const (
	kindBox elKind = iota
	kindText
	kindImage
	kindInput
	kindIcon
	kindSpinner
	kindSurface
	kindVideo
	kindWebView
	kindArc
)

// Cursor is a system pointer shape.
type Cursor uint8

const (
	CursorInherit Cursor = iota
	CursorDefault
	CursorPointer
	CursorText
	CursorMove
	CursorNotAllowed
	CursorResizeEW
	CursorResizeNS
	CursorWait
	CursorCrosshair
)

// TextAlign aligns lines inside a text element.
type TextAlign uint8

const (
	TextStart TextAlign = iota
	TextCenter
	TextEnd
)

// Shadow is a CSS-style box shadow. Blur follows CSS (sigma = blur / 2).
type Shadow struct {
	X, Y, Blur, Spread float32
	Color              Color
	Inset              bool
}

// ring is a solid band drawn outside the element's border box, as a CSS
// box-shadow of 0 0 0 width.
type ring struct {
	width float32
	color Color
}

type gradient struct {
	angle    float32
	from, to Color
}

type textProps struct {
	color         Color
	size          float32
	weight        int
	italic        int8 // 0 inherit, 1 yes, -1 no
	family        string
	lineHeight    float32
	align         TextAlign
	alignSet      bool
	noWrap        bool
	ellipsis      bool
	letterSpacing float32
}

// PointerEvent describes a pointer press in window and element coordinates.
type PointerEvent struct {
	X, Y           float32 // window logical pixels
	LocalX, LocalY float32 // relative to the element's top-left
	W, H           float32 // the element's size
	Button         int     // 0 left, 1 right, 2 middle
	Clicks         int
	Mods           keyboard.Mods
}

// DragPhase is the stage of a drag gesture.
type DragPhase uint8

const (
	DragStart DragPhase = iota
	DragMove
	DragEnd
)

// DragEvent reports pointer movement while a press that started on the element is held.
type DragEvent struct {
	Phase          DragPhase
	X, Y           float32 // window logical pixels
	DX, DY         float32 // total movement since the press
	LocalX, LocalY float32
	W, H           float32       // the element's size
	Button         int           // the held button: 0 left, 1 right, 2 middle
	Mods           keyboard.Mods // modifier keys held at the time of the event
	// Canceled is set on the DragEnd of a drag that Escape, or the window
	// losing the pointer, ended: the element should go back to how it was
	// before the drag.
	Canceled bool
}

// MouseButtons is a set of mouse buttons, as for DragButtons.
type MouseButtons uint8

const (
	MouseLeft MouseButtons = 1 << iota
	MouseRight
	MouseMiddle
)

// WheelEvent reports a mouse wheel or touchpad scroll over an element.
type WheelEvent struct {
	X, Y           float32 // window logical pixels
	LocalX, LocalY float32 // relative to the element's top-left
	W, H           float32 // the element's size
	// DX and DY are the scroll amount in wheel notches, positive to the right
	// and down, as a browser's WheelEvent deltaX and deltaY: turning the wheel
	// towards the user gives DY > 0. Touchpads and high-resolution wheels
	// give fractions of a notch. Shift does not turn DY into DX here.
	DX, DY float32
	Mods   keyboard.Mods
	// Precise is set for touchpad and high-resolution wheel input, which
	// arrives in small steps and needs no smoothing.
	Precise bool
}

// El is a node of the view tree. Views build a fresh tree on every update;
// the framework keeps per-element state (hover, scroll, focus, editing) by identity.
type El struct {
	kind     elKind
	key      string
	style    layout.Style
	children []*El

	fill         Color
	grad         *gradient
	border       layout.Edges
	borderColor  Color
	borderDashed bool
	fillPadding  bool
	radii        [4]float32
	shadows      []Shadow
	opacity      float32
	clip         bool
	edgeFade     float32
	scrollBars   *ScrollBarStyle
	// heightMotion and grow are AnimateHeight and what this frame's
	// growPass changed in the laid-out node.
	heightMotion *motion.Spec
	grow         growDelta
	kidClip      rectF // the clip place gave the children, for leaving ones
	memo         *memoEntry
	live         *liveProps
	isolate      bool
	matchID      string
	flying       bool // drawn in flight from where its MatchID was
	blur         float32
	backdropBlur float32
	// ownTP is tp as the view set it, before prepare filled in inherited
	// values, so a subtree that Memo reuses inherits afresh.
	ownTP   textProps
	tpSaved bool

	hoverFill, hoverBorder, hoverText Color
	pressFill                         Color
	focusBorder, focusFill            Color
	focusVisBorder                    Color
	ring, focusRing, hoverRing        ring
	focusWithin, focusParent          bool
	dropFill, dropBorder              Color
	cursor                            Cursor
	onFileDrop                        func(FileDrop)
	acceptFiles                       func([]string) bool
	dropReject                        Color

	text  string
	spans []Span
	links bool

	autoFocus bool
	tp        textProps
	image     *Image
	fit       Fit
	surface   func(SurfaceFrame) bool
	retain    *uint64 // Retain's version; nil redraws the surface every frame
	video     *Video
	webview   *WebView
	vec       *svg.Vector
	arc       *arcProps

	input *inputProps

	onClick       func()
	onDoubleClick func()
	onRightClick  func(PointerEvent)
	onPointerDown func(PointerEvent)
	onHover       func(bool)
	onPointerMove func(PointerEvent) bool
	onWheel       func(WheelEvent) bool
	onDrag        func(DragEvent)
	dragButtons   MouseButtons // 0 means MouseLeft
	onKey         func(keyboard.KeyEvent) bool
	onBlur        func()
	onScroll      func(x, y float32)
	onLayout      func(w, h float32)
	focusable     bool
	focus         focusProps
	dragRegion    bool
	windowChrome  bool
	chromeExclude bool
	inChrome      bool
	excludeChrome bool
	maxButton     bool
	tooltip       string
	tipTruncated  bool

	motion       *motion.Spec
	layoutMotion *motion.Spec
	enter        *Transition
	exit         *Transition
	pulse        *pulseProps
	shimmer      *shimmerProps
	passThrough  bool
	selectable   bool
	popover      *popover
	virtual      *virtualList
	stickBottom  bool
	a11y         *a11yProps
	rowIndex     int32 // index+1 of a virtual list row; 0 for other elements
	xfp          *transformProps

	// Runtime, filled during a frame.
	id     uint64
	parent *El
	node   layout.Node
	absX   float32
	absY   float32
	w, h   float32 // painted size; differs from the layout size while AnimateLayout runs
	fade   float32 // opacity multiplier from enter/exit transitions
	xf     xform   // maps the element's layout space to the window; includes ancestors' transforms
	ownXf  xform   // the element's own Scale, Rotate and transition scale, when xfOwn
	xfOwn  bool
	xfK    float32 // the drawn scale and turn, for an exit transition to continue from
	xfRot  float32
	// popSide is the Side a popover root opened on plus one, and popOX, popOY
	// the anchor's center on the edge facing it, in units of the root's size.
	popSide      uint8
	popOX, popOY float32
	clipR        rectF
	culled       bool
	state        *elState
}

func newEl(kind elKind) *El {
	e := &El{kind: kind, opacity: 1}
	e.style.Shrink = 1
	e.style.AlignItems = layout.AlignStretch
	e.style.Inset = layout.NoInset
	return e
}

// Box is a column container. Nil children are skipped, as with Child.
func Box(children ...*El) *El {
	return newEl(kindBox).Child(children...)
}

// Col is a container that stacks children vertically.
func Col(children ...*El) *El { return Box(children...) }

// Row is a container that places children horizontally.
func Row(children ...*El) *El {
	e := Box(children...)
	e.style.Direction = layout.Row
	return e
}

// Spacer takes the free space of its parent.
func Spacer() *El { return Box().Grow(1) }

// Text is a run of text. Text properties not set on it are inherited from ancestors.
func Text(s string) *El {
	e := newEl(kindText)
	e.text = s
	return e
}

// Child appends children; nil entries are skipped.
func (e *El) Child(children ...*El) *El {
	for _, c := range children {
		if c != nil {
			e.children = append(e.children, c)
		}
	}
	return e
}

// Key gives the element a stable identity among its siblings. Without one, an
// element is known by its kind and position, so inserting a sibling of the
// same kind before it hands its state, such as scrolling, edit history and
// focus, to the inserted one.
func (e *El) Key(k string) *El { e.key = k; return e }

// Layout. Sizes are in logical pixels and include padding and border.

// W sets the width in pixels.
func (e *El) W(px float32) *El { e.style.Width = layout.PxDim(px); return e }

// H sets the height in pixels.
func (e *El) H(px float32) *El { e.style.Height = layout.PxDim(px); return e }

// Size sets the width and height in pixels.
func (e *El) Size(w, h float32) *El { return e.W(w).H(h) }

// WPct sets the width as a fraction of the parent's content width: 0.5 is
// half, 1 is all of it.
func (e *El) WPct(f float32) *El { e.style.Width = layout.PctDim(f); return e }

// HPct sets the height as a fraction of the parent's content height.
func (e *El) HPct(f float32) *El { e.style.Height = layout.PctDim(f); return e }

// WFull is WPct(1).
func (e *El) WFull() *El { return e.WPct(1) }

// HFull is HPct(1).
func (e *El) HFull() *El { return e.HPct(1) }

// MinW sets the least width in pixels.
func (e *El) MinW(px float32) *El { e.style.MinWidth = layout.PxDim(px); return e }

// MinH sets the least height in pixels.
func (e *El) MinH(px float32) *El { e.style.MinHeight = layout.PxDim(px); return e }

// MaxW sets the greatest width in pixels.
func (e *El) MaxW(px float32) *El { e.style.MaxWidth = layout.PxDim(px); return e }

// MaxH sets the greatest height in pixels.
func (e *El) MaxH(px float32) *El { e.style.MaxHeight = layout.PxDim(px); return e }

// Grow sets the share of the parent's free main-axis space the element takes,
// as CSS flex-grow.
func (e *El) Grow(g float32) *El { e.style.Grow = g; return e }

// Shrink sets how much the element gives up when its siblings do not fit, as
// CSS flex-shrink. The default is 1; 0 keeps its size.
func (e *El) Shrink(s float32) *El { e.style.Shrink = s; return e }

// Basis sets the main-axis size the element starts from before growing or
// shrinking, as CSS flex-basis.
func (e *El) Basis(px float32) *El { e.style.Basis = layout.PxDim(px); return e }

// Gap sets the space between children and, with Wrap, between lines.
func (e *El) Gap(px float32) *El { e.style.Gap = px; e.style.CrossGap = px; return e }

// RowGap sets the space between wrapped lines only.
func (e *El) RowGap(px float32) *El { e.style.CrossGap = px; return e }

// Wrap moves children that do not fit onto further lines, as CSS flex-wrap.
func (e *El) Wrap() *El { e.style.Wrap = true; return e }

// Hidden, when h is true, leaves the element and its subtree out of layout
// and drawing, as CSS display: none.
func (e *El) Hidden(h bool) *El { e.style.Hidden = h; return e }

// Clip clips the children to the inside of the border, with its corners
// rounded by Radius less the border width.
func (e *El) Clip() *El { e.clip = true; return e }

// Isolate draws the element and its subtree into an offscreen layer and
// then onto the window at the element's opacity, so children that overlap
// fade as one picture instead of showing through each other. A scaled or
// turned element that isolates is drawn and then turned as one picture.
func (e *El) Isolate() *El { e.isolate = true; return e }

// Blur draws the element and its subtree blurred by a Gaussian of standard
// deviation px. It isolates the element.
func (e *El) Blur(px float32) *El { e.blur = max(px, 0); return e }

// BackdropBlur blurs what the window drew behind the element, inside its
// rounded box, by a Gaussian of standard deviation px, as for a frosted
// panel; the element's fill goes on top. The desktop and a system backdrop
// such as Mica are not blurred, and a window draws at most four.
func (e *El) BackdropBlur(px float32) *El { e.backdropBlur = max(px, 0); return e }

// ScrollEdgeFade fades the children out over px toward each edge that has
// content scrolled past it. It implies Clip.
func (e *El) ScrollEdgeFade(px float32) *El { e.clip, e.edgeFade = true, px; return e }

// Pad sets equal padding on all sides.
func (e *El) Pad(px float32) *El {
	e.style.Padding = layout.Edges{Top: px, Right: px, Bottom: px, Left: px}
	return e
}

// PadXY sets horizontal and vertical padding.
func (e *El) PadXY(x, y float32) *El {
	e.style.Padding = layout.Edges{Top: y, Right: x, Bottom: y, Left: x}
	return e
}

// PadEdges sets padding per side.
func (e *El) PadEdges(top, right, bottom, left float32) *El {
	e.style.Padding = layout.Edges{Top: top, Right: right, Bottom: bottom, Left: left}
	return e
}

// padding is the inset of the content from the box edge: the padding inside the border, as CSS
// box-sizing: border-box lays out a box. A set size includes both.
func (e *El) padding() layout.Edges {
	p, b := e.style.Padding, e.border
	return layout.Edges{
		Top:    p.Top + b.Top,
		Right:  p.Right + b.Right,
		Bottom: p.Bottom + b.Bottom,
		Left:   p.Left + b.Left,
	}
}

// Margin sets equal margin on all sides.
func (e *El) Margin(px float32) *El {
	e.style.Margin = layout.Edges{Top: px, Right: px, Bottom: px, Left: px}
	return e
}

// MarginEdges sets margin per side.
func (e *El) MarginEdges(top, right, bottom, left float32) *El {
	e.style.Margin = layout.Edges{Top: top, Right: right, Bottom: bottom, Left: left}
	return e
}

// Alignment values for Align, AlignSelf and Justify.
const (
	Stretch = layout.AlignStretch
	Start   = layout.AlignStart
	Center  = layout.AlignCenter
	End     = layout.AlignEnd

	JustifyStart   = layout.JustifyStart
	JustifyCenter  = layout.JustifyCenter
	JustifyEnd     = layout.JustifyEnd
	JustifyBetween = layout.JustifyBetween
	JustifyAround  = layout.JustifyAround
	JustifyEvenly  = layout.JustifyEvenly
)

// Align sets how children are placed on the cross axis.
func (e *El) Align(a layout.Align) *El { e.style.AlignItems = a; return e }

// AlignSelf overrides the parent's cross-axis alignment for this element.
func (e *El) AlignSelf(a layout.Align) *El { e.style.AlignSelf = a; return e }

// Justify sets how children are distributed on the main axis. On a Text
// element, JustifyCenter and JustifyEnd place the text vertically in its box.
func (e *El) Justify(j layout.Justify) *El { e.style.Justify = j; return e }

// Center centers children on both axes. On a Text element, it centers the text
// vertically; use TextAlign(TextCenter) to center it horizontally too.
//
//	fairy.Text("OK").Size(48, 24).Center().TextAlign(fairy.TextCenter)
func (e *El) Center() *El {
	e.style.Justify = layout.JustifyCenter
	e.style.AlignItems = layout.AlignCenter
	return e
}

// Absolute takes the element out of flow; position it with Top, Right,
// Bottom and Left, which each also make it absolute.
func (e *El) Absolute() *El { e.style.Absolute = true; return e }

// Top places an absolute element px below the top of its parent's padding box.
func (e *El) Top(px float32) *El { e.style.Absolute = true; e.style.Inset.Top = px; return e }

// Right places an absolute element px from the right of its parent's padding box.
func (e *El) Right(px float32) *El { e.style.Absolute = true; e.style.Inset.Right = px; return e }

// Bottom places an absolute element px above the bottom of its parent's padding box.
func (e *El) Bottom(px float32) *El { e.style.Absolute = true; e.style.Inset.Bottom = px; return e }

// Left places an absolute element px from the left of its parent's padding box.
func (e *El) Left(px float32) *El { e.style.Absolute = true; e.style.Inset.Left = px; return e }

// Inset pins all four sides of an absolutely positioned element.
func (e *El) Inset(px float32) *El { return e.Top(px).Right(px).Bottom(px).Left(px) }

// ScrollY makes the element a vertical scroll container. Bound its height
// (H, Grow inside a bounded parent, or MaxH) so there is something to scroll.
func (e *El) ScrollY() *El { e.style.ScrollY = true; e.clip = true; return e }

// ScrollX makes the element a horizontal scroll container.
func (e *El) ScrollX() *El { e.style.ScrollX = true; e.clip = true; return e }

// StickToBottom keeps a vertical scroll container at the bottom while it is
// there: when the content grows or the container shrinks, the offset follows
// the bottom. It starts at the bottom. Scrolling up stops following, and
// scrolling back to the bottom resumes it. Use it for logs and chat.
func (e *El) StickToBottom() *El { e.stickBottom = true; return e }

// Fill paints the element's box with c.
func (e *El) Fill(c Color) *El { e.fill = c; return e }

// Gradient paints a linear gradient; angle is in degrees with CSS semantics (0 = to top, 90 = to right).
func (e *El) Gradient(angle float32, from, to Color) *El {
	e.grad = &gradient{angle: angle * math.Pi / 180, from: from, to: to}
	return e
}

// Border draws a border of width px inside the element's box. As with CSS
// box-sizing: border-box, the border sits outside the padding: it grows a
// box sized by its content and narrows the content of a box with a set size.
func (e *El) Border(px float32, c Color) *El {
	e.border = layout.Edges{Top: px, Right: px, Bottom: px, Left: px}
	e.borderColor = c
	return e
}

// BorderEdges sets per-side border widths.
func (e *El) BorderEdges(top, right, bottom, left float32, c Color) *El {
	e.border = layout.Edges{Top: top, Right: right, Bottom: bottom, Left: left}
	e.borderColor = c
	return e
}

// BorderDashed draws the border as dashes, as CSS border-style: dashed. The
// corners stay solid, and the whole border turns solid while the element is
// focused and has a FocusBorder.
func (e *El) BorderDashed() *El { e.borderDashed = true; return e }

// FillPadding stops the fill at the inside of the border, as CSS
// background-clip: padding-box: a translucent border shows what is behind the
// element instead of its fill, and a transparent border leaves a gap.
func (e *El) FillPadding() *El { e.fillPadding = true; return e }

// Radius rounds all four corners by r pixels.
func (e *El) Radius(r float32) *El { e.radii = [4]float32{r, r, r, r}; return e }

// Radii sets corner radii: top-left, top-right, bottom-right, bottom-left.
func (e *El) Radii(tl, tr, br, bl float32) *El { e.radii = [4]float32{tl, tr, br, bl}; return e }

// Shadow adds a drop shadow.
func (e *El) Shadow(x, y, blur, spread float32, c Color) *El {
	e.shadows = append(e.shadows, Shadow{X: x, Y: y, Blur: blur, Spread: spread, Color: c})
	return e
}

// InsetShadow adds a shadow drawn inside the element.
func (e *El) InsetShadow(x, y, blur, spread float32, c Color) *El {
	e.shadows = append(e.shadows, Shadow{X: x, Y: y, Blur: blur, Spread: spread, Color: c, Inset: true})
	return e
}

// Opacity multiplies the alpha of the element and its subtree.
func (e *El) Opacity(a float32) *El { e.opacity = a; return e }

// HoverFill replaces the fill while the pointer is over the element.
func (e *El) HoverFill(c Color) *El { e.hoverFill = c; return e }

// HoverBorder replaces the border color while the pointer is over the element.
func (e *El) HoverBorder(c Color) *El { e.hoverBorder = c; return e }

// HoverColor replaces the text color while the pointer is over the element.
func (e *El) HoverColor(c Color) *El { e.hoverText = c; return e }

// PressFill replaces the fill while a press on the element is held.
func (e *El) PressFill(c Color) *El { e.pressFill = c; return e }

// FocusBorder replaces the border color while the element has focus.
func (e *El) FocusBorder(c Color) *El { e.focusBorder = c; return e }

// FocusFill replaces the fill while the element has focus.
func (e *El) FocusFill(c Color) *El { e.focusFill = c; return e }

// Cursor sets the pointer shape shown over the element.
func (e *El) Cursor(c Cursor) *El { e.cursor = c; return e }

// Ring draws a band px wide around the outside of the element's box, following
// its corners, as CSS box-shadow: 0 0 0 px. It takes no layout space.
func (e *El) Ring(px float32, c Color) *El { e.ring = ring{px, c}; return e }

// FocusRing draws a Ring while the element's focus is visible: focus that the
// keyboard moved, or any focus of an Input, as CSS :focus-visible. It replaces
// the element's own Ring meanwhile.
func (e *El) FocusRing(px float32, c Color) *El { e.focusRing = ring{px, c}; return e }

// HoverRing draws a Ring while the pointer is over the element, unless its
// FocusRing shows.
func (e *El) HoverRing(px float32, c Color) *El { e.hoverRing = ring{px, c}; return e }

// FocusVisibleBorder changes the border color while the element's focus is
// visible, as for FocusRing.
func (e *El) FocusVisibleBorder(c Color) *El { e.focusVisBorder = c; return e }

// FocusFromParent applies the element's FocusVisibleBorder and FocusRing
// while its parent's focus is visible, as for the thumb of a slider whose
// track takes focus.
func (e *El) FocusFromParent() *El { e.focusParent = true; return e }

// FocusWithin applies the element's FocusBorder, FocusFill, FocusVisibleBorder
// and FocusRing while it or any element inside it has focus, as for a field that
// wraps an Input together with an icon.
func (e *El) FocusWithin() *El { e.focusWithin = true; return e }

// Tooltip shows text after the pointer rests on the element.
func (e *El) Tooltip(s string) *El { e.tooltip = s; return e }

// TooltipWhenTruncated shows the tooltip only while the text is cut short by
// Ellipsis. Without a Tooltip it shows the element's whole text.
func (e *El) TooltipWhenTruncated() *El { e.tipTruncated = true; return e }

// Text properties. Set on a container, they are inherited by descendant text.

// Color sets the text and icon color.
func (e *El) Color(c Color) *El { e.tp.color = c; return e }

// FontSize sets the font size in pixels.
func (e *El) FontSize(px float32) *El { e.tp.size = px; return e }

// Weight sets the font weight, from 100 (thin) to 900 (black); 400 is regular.
func (e *El) Weight(w int) *El { e.tp.weight = w; return e }

// Bold is Weight(700).
func (e *El) Bold() *El { e.tp.weight = 700; return e }

// Medium is Weight(500).
func (e *El) Medium() *El { e.tp.weight = 500; return e }

// SemiBold is Weight(600).
func (e *El) SemiBold() *El { e.tp.weight = 600; return e }

// Italic sets the italic style.
func (e *El) Italic() *El { e.tp.italic = 1; return e }

// Font sets the font family by name, such as one registered with RegisterFont.
func (e *El) Font(family string) *El { e.tp.family = family; return e }

// LineHeight sets the distance between baselines in pixels.
func (e *El) LineHeight(px float32) *El { e.tp.lineHeight = px; return e }

// LetterSpacing adds px after each character, as CSS letter-spacing. Descendants inherit it.
func (e *El) LetterSpacing(px float32) *El { e.tp.letterSpacing = px; return e }

// TextAlign sets the horizontal alignment of text lines.
func (e *El) TextAlign(a TextAlign) *El { e.tp.align = a; e.tp.alignSet = true; return e }

// NoWrap keeps text on one line.
func (e *El) NoWrap() *El { e.tp.noWrap = true; return e }

// Ellipsis keeps text on one line and trims it with "…" when it overflows.
func (e *El) Ellipsis() *El { e.tp.noWrap = true; e.tp.ellipsis = true; return e }

// Events.

// OnClick runs when a primary press is released over the element.
func (e *El) OnClick(f func()) *El { e.onClick = f; return e }

// OnDoubleClick runs on the second click of a double click.
func (e *El) OnDoubleClick(f func()) *El { e.onDoubleClick = f; return e }

// OnRightClick runs when the secondary button is released over the element.
func (e *El) OnRightClick(f func(PointerEvent)) *El { e.onRightClick = f; return e }

// OnPointerDown runs when any button is pressed on the element.
func (e *El) OnPointerDown(f func(PointerEvent)) *El { e.onPointerDown = f; return e }

// OnHover runs when the pointer enters (true) or leaves (false) the element.
func (e *El) OnHover(f func(bool)) *El { e.onHover = f; return e }

// OnPointerMove reports the pointer's position while it moves over the
// element with no button held, at most once per frame, as for hover picking
// in a 3D view. Button is -1 and Clicks 0. OnHover(false) tells when the
// pointer has left. Return true when the handler changed what the view shows,
// so that View runs again; returning false keeps the move from rebuilding.
func (e *El) OnPointerMove(f func(PointerEvent) bool) *El { e.onPointerMove = f; return e }

// OnWheel receives wheel and touchpad scrolling over the element, as for
// zooming a 3D view about the pointer. The innermost element under the
// pointer gets it first. Return true when handled: nothing scrolls. Return
// false to pass it on to the scroll containers and OnWheel handlers outside.
func (e *El) OnWheel(f func(WheelEvent) bool) *El { e.onWheel = f; return e }

// OnDrag reports pointer movement while a press that began on the element is held.
// Only the left button drags unless DragButtons says otherwise.
func (e *El) OnDrag(f func(DragEvent)) *El { e.onDrag = f; return e }

// DragButtons sets which buttons start OnDrag, as MouseLeft|MouseRight for
// a 3D view that turns with the left button and pans with the right. The
// default is MouseLeft. A right press that is released before it moves far
// enough to drag is still a right click.
func (e *El) DragButtons(b MouseButtons) *El { e.dragButtons = b; return e }

func (e *El) dragsWith(button int) bool {
	if e.onDrag == nil {
		return false
	}
	b := e.dragButtons
	if b == 0 {
		b = MouseLeft
	}
	return b&(1<<button) != 0
}

// OnKey receives key presses while the element or a descendant has focus.
// Return true to stop propagation.
func (e *El) OnKey(f func(keyboard.KeyEvent) bool) *El { e.onKey = f; return e }

// OnBlur runs when the element loses keyboard focus to another element or to none, as when the
// user clicks elsewhere or presses Tab. It does not run when the element leaves the view while focused.
func (e *El) OnBlur(f func()) *El { e.onBlur = f; return e }

// OnScroll reports the scroll offset of a scroll container when it changes.
func (e *El) OnScroll(f func(x, y float32)) *El { e.onScroll = f; return e }

// OnLayout reports the element's laid-out size when it is first placed and
// whenever it changes, as for a table that records its column widths.
// Changes f makes to state appear on the next build.
func (e *El) OnLayout(f func(w, h float32)) *El { e.onLayout = f; return e }

// Focusable lets the element take keyboard focus by click or Tab.
func (e *El) Focusable() *El { e.focusable = true; return e }

// DragRegion makes the element move the window when dragged (for custom title bars).
// Interactive children (with click handlers or focusable) still receive input.
func (e *El) DragRegion() *El { e.dragRegion = true; return e }

// WindowChrome gives this subtree pointer priority over overlays without changing
// its paint order or the keyboard focus scope. Use it for custom window controls.
// Popovers and tooltips are placed below chrome along the window's top edge,
// where they could not take presses.
func (e *El) WindowChrome() *El { e.windowChrome = true; return e }

// WindowChromeExclude keeps this subtree in normal hit-test order, even inside
// WindowChrome. Use it for title bar actions that modal overlays must block.
func (e *El) WindowChromeExclude() *El { e.chromeExclude = true; return e }

// MaximizeButton marks the maximize button of a frameless window's own title
// bar, so Windows 11 opens the snap layout menu when the pointer rests on it.
// Give it an OnClick that calls Window.ToggleMaximize.
func (e *El) MaximizeButton() *El { e.maxButton = true; return e }

// AutoFocus gives the element keyboard focus when it appears, as for the
// search field of a dropdown. Use it on focusable elements and inputs. When
// several appear at once, the first in the window or in an overlay wins.
func (e *El) AutoFocus() *El { e.autoFocus = true; return e }

// PassThrough lets pointer input go through this element to whatever is under it.
// Its children still receive input. Use it for full-window layers such as toasts.
func (e *El) PassThrough() *El { e.passThrough = true; return e }

// FileDrop describes files dropped on an element.
type FileDrop struct {
	Paths []string
	X, Y  float32 // window position in logical pixels
}

// OnFileDrop makes the element accept files dragged from Explorer or other
// programs. The deepest accepting element under the pointer receives them.
func (e *El) OnFileDrop(f func(FileDrop)) *El { e.onFileDrop = f; return e }

// DropFill and DropBorder style the element while files are dragged over it
// and it would receive them.
func (e *El) DropFill(c Color) *El   { e.dropFill = c; return e }
func (e *El) DropBorder(c Color) *El { e.dropBorder = c; return e }

// AcceptFiles decides whether an OnFileDrop element takes the files being
// dragged, for example by their extensions. Files it turns down show the
// "not allowed" cursor and are not dropped; DropRejectBorder styles the
// element meanwhile, and Cx.FileDropRejected reports it to the view.
func (e *El) AcceptFiles(f func(paths []string) bool) *El { e.acceptFiles = f; return e }

// DropRejectBorder is the border color while files that AcceptFiles turned
// down are dragged over the element.
func (e *El) DropRejectBorder(c Color) *El { e.dropReject = c; return e }

func (e *El) interactive() bool {
	return e.onClick != nil || e.onDoubleClick != nil || e.onRightClick != nil || e.onPointerDown != nil ||
		e.onDrag != nil || e.onPointerMove != nil || e.focusable || e.kind == kindInput
}
