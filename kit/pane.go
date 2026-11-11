package kit

import "math"

// PaneState is a side pane's width and whether it shows: a SplitView's
// sidebar or inspector. The app owns it, so it can save Width and Open and
// restore them with NewPane(defaultWidth).WithWidth(saved), plus Collapsed.
// Widths are whole pixels within the pane's range.
type PaneState struct {
	width    float32
	open     bool
	min, max float32
	resizing bool
	// dragStart is the width a drag on the grip began at, negative when the
	// pane was closed, for Escape to restore.
	dragStart float32
	// asked holds the width and the default as given, before clamping, so a
	// later Range can widen to them.
	asked [2]float32
}

// Default range of a pane's width.
const (
	PaneMinWidth float32 = 160
	PaneMaxWidth float32 = 480
)

// NewPane returns an open pane width pixels wide, which double-clicking its
// edge also returns to.
func NewPane(width float32) PaneState {
	p := PaneState{width: width, open: true, min: PaneMinWidth, max: PaneMaxWidth, asked: [2]float32{width, width}}
	p.clamp()
	return p
}

// Range sets the width range. The width and the default clamp into it from
// the values given to NewPane and WithWidth, so NewPane(640).Range(360, 960)
// is 640 wide.
func (p PaneState) Range(lo, hi float32) PaneState {
	p.min = max(lo, 1)
	p.max = max(hi, p.min)
	p.width = p.asked[0]
	p.clamp()
	return p
}

// Collapsed starts the pane closed.
func (p PaneState) Collapsed() PaneState { p.open = false; return p }

// WithWidth starts the pane at a saved width, clamped and rounded. The
// default width stays.
func (p PaneState) WithWidth(width float32) PaneState {
	if finite32(width) {
		p.asked[0] = width
	}
	p.SetWidth(width)
	return p
}

// Width returns the pane's width, kept while it is closed.
func (p *PaneState) Width() float32 { return p.width }

// Open reports whether the pane is open.
func (p *PaneState) Open() bool { return p.open }

// MinWidth returns the lower end of the width range.
func (p *PaneState) MinWidth() float32 { return p.min }

// MaxWidth returns the upper end of the width range.
func (p *PaneState) MaxWidth() float32 { return p.max }

// DefaultWidth is the width the pane was made with, clamped to its range.
func (p *PaneState) DefaultWidth() float32 {
	return min(max(float32(math.Round(float64(p.asked[1]))), p.min), p.max)
}

// Resizing reports whether the pane's edge is being dragged.
func (p *PaneState) Resizing() bool { return p.resizing }

// ShownWidth is the width the pane takes in the layout: Width when open, else 0.
func (p *PaneState) ShownWidth() float32 {
	if p.open {
		return p.width
	}
	return 0
}

// SetWidth clamps width to the range and rounds it to a whole pixel. It
// doesn't open the pane.
func (p *PaneState) SetWidth(width float32) {
	if finite32(width) {
		p.width = min(max(float32(math.Round(float64(width))), p.min), p.max)
	}
}

// SetOpen opens or closes the pane, keeping its width.
func (p *PaneState) SetOpen(open bool) { p.open = open }

// Toggle opens the pane if it is closed and closes it if it is open.
func (p *PaneState) Toggle() { p.open = !p.open }

// ResetWidth returns to the default width.
func (p *PaneState) ResetWidth() { p.width = p.DefaultWidth() }

// DragTo is where dragging the pane's edge to width leaves it: clamped, or
// collapsed (keeping its last width) once dragged below half its minimum.
// Dragging back out opens it again.
func (p *PaneState) DragTo(width float32) {
	if width < p.min*0.5 {
		p.open = false
		return
	}
	p.open = true
	p.SetWidth(width)
}

func (p *PaneState) clamp() {
	p.width = min(max(float32(math.Round(float64(p.width))), p.min), p.max)
}

func finite32(f float32) bool {
	return !math.IsNaN(float64(f)) && !math.IsInf(float64(f), 0)
}
