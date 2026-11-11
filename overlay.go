package fairy

// Side is where a popover opens relative to its anchor.
type Side uint8

const (
	SideBottom Side = iota
	SideTop
	SideRight
	SideLeft
)

// PopoverOptions places a popover. The zero value opens below the anchor,
// aligned to its start edge, 4 pixels away.
type PopoverOptions struct {
	Side  Side
	Align Align
	Gap   float32
	// AlignOffset moves the popover along the anchor's edge from where Align
	// puts it: toward the end for AlignStart and AlignCenter, toward the start
	// for AlignEnd. A negative offset moves it the other way.
	AlignOffset float32
	// OnDismiss runs on Escape or an outside press. By default, the anchor
	// counts as inside the popover for dismissal.
	OnDismiss func()
	// DismissOnAnchorPress counts a press on the anchor outside the panel as
	// an outside press. Dismissal does not consume the press.
	DismissOnAnchorPress bool
	// AtPoint anchors the popover to (X, Y) in the anchor element's
	// coordinates instead of to the whole element, as for a context menu.
	AtPoint bool
	X, Y    float32
}

// Align positions a popover along the anchor edge.
type Align uint8

const (
	AlignStart Align = iota
	AlignCenter
	AlignEnd
)

type popover struct {
	content *El
	opts    PopoverOptions
}

// Popover draws content above everything else, next to this element. Pass nil
// content to close it. The popover flips to the other side when it would not fit.
func (e *El) Popover(content *El, opts PopoverOptions) *El {
	if content == nil {
		e.popover = nil
		return e
	}
	if opts.Gap == 0 {
		opts.Gap = 4
	}
	e.popover = &popover{content: content, opts: opts}
	return e
}

type layer struct {
	root       *El
	anchor     uint64 // 0 for window overlays
	pop        *popover
	modal      bool
	onDismiss  func()
	hidden     bool
	focusStart int
	focusEnd   int
}
