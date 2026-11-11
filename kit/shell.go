package kit

import (
	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/motion"
	"github.com/myparsleycat/fairy/svg"
)

// TitleBarHeight is the height of a SplitView's title band.
const TitleBarHeight float32 = 48

const (
	gripWidth float32 = 6
	gripStep  float32 = 8
)

var panelLeft = svg.MustParse(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect width="18" height="18" x="3" y="3" rx="2"/><path d="M9 3v18"/></svg>`)

const (
	splitToggleKey = "kit.split.toggle"
	splitMainKey   = "kit.split.main"
)

// SplitProps describes a SplitView.
type SplitProps struct {
	// Main is the content of the main pane.
	Main *fairy.El
	// Sidebar is the leading pane's state, owned by the app; nil for no
	// sidebar. The resize handle and the toggle button change it.
	Sidebar        *PaneState
	SidebarContent *fairy.El
	// Inspector is the trailing pane's state; nil for no inspector. It
	// resizes from its leading edge.
	Inspector        *PaneState
	InspectorContent *fairy.El
	// Toolbar fills the title band above the main pane, after the toggle
	// button. The band drags the window; controls in it keep their clicks.
	Toolbar *fairy.El
	// HideSidebarToggle leaves out the sidebar's toggle button.
	HideSidebarToggle bool
	// FrameInset is the background showing around the main pane where no
	// pane borders it: 8 when zero, none when negative.
	FrameInset float32
}

// SplitView is an app shell: a resizable, collapsible sidebar, the main pane
// under a title band, and an optional inspector. The panes sit flush on the
// window background; the main pane is a rounded card beside them. A pane
// resizes from its inner edge: double-clicking the edge resets its width,
// dragging it below half the minimum collapses the pane, and Escape during
// the drag restores it. Panes open and close with a spring that snaps under
// reduced motion. When a pane closes with the focus inside it, the focus
// moves to the toggle button, or to the main pane.
//
// Call IsSidebarToggle from the window's key handler to toggle the sidebar
// with Ctrl+Shift+B.
func SplitView(p SplitProps) *fairy.El {
	t := &current
	frame := p.FrameInset
	switch {
	case frame == 0:
		frame = 8
	case frame < 0:
		frame = 0
	}
	sidebarOpen := p.Sidebar != nil && p.Sidebar.Open()
	inspectorOpen := p.Inspector != nil && p.Inspector.Open()
	toggle := p.Sidebar != nil && !p.HideSidebarToggle

	band := fairy.Row().Key("kit.split.band").H(TitleBarHeight).Align(fairy.Center).DragRegion()
	lead := float32(12)
	if toggle && !sidebarOpen {
		lead = 12 + 24 + 8
	}
	band.Child(fairy.Box().W(lead).Shrink(0).AnimateLayout(motion.Default).Role(fairy.RoleHidden))
	if p.Toolbar != nil {
		band.Child(p.Toolbar.Grow(1).MinW(0))
	}

	card := fairy.Col(p.Main.Grow(1).MinH(0)).Key(splitMainKey).
		Grow(1).MinW(0).MinH(0).
		MarginEdges(0, edge(!inspectorOpen, frame), frame, edge(!sidebarOpen, frame)).
		Fill(t.Background).Radius(t.RadiusXl()).Clip().
		AnimateLayout(motion.Default)
	shadowSm(card)
	below := fairy.Row(card).Grow(1).MinH(0)
	if p.Inspector != nil {
		below.Child(dock(p.Inspector, p.InspectorContent, false, nil))
	}
	main := fairy.Col(band, below).Grow(1).MinW(0).HFull()

	split := fairy.Row().WFull().HFull()
	if p.Sidebar != nil {
		top := fairy.Box().H(TitleBarHeight).Shrink(0).DragRegion()
		split.Child(dock(p.Sidebar, p.SidebarContent, true, top))
	}
	split.Child(main)
	root := fairy.Box(split).WFull().HFull().Fill(t.Sidebar).Color(t.SidebarForeground)
	if toggle {
		label := Messages.ShowSidebar
		if sidebarOpen {
			label = Messages.HideSidebar
		}
		pane := p.Sidebar
		b := IconButton(panelLeft, pane.Toggle, Small, Tooltip(label+" (Ctrl+Shift+B)")).
			Key(splitToggleKey).Label(label).Expanded(sidebarOpen).Size(24, 24)
		root.Child(b.Left(12).Top((TitleBarHeight - 24) / 2))
	}
	return root
}

func edge(on bool, px float32) float32 {
	if on {
		return px
	}
	return 0
}

// dock is a pane's slot: its width springs between 0 and the pane's width
// while the content keeps its full width, clipped and pinned to the outer
// edge. The content fades in and out. The grip on the inner edge stays
// while a drag that collapsed the pane goes on, so the drag can reopen it.
func dock(p *PaneState, content *fairy.El, leading bool, top *fairy.El) *fairy.El {
	name, gripName, ret := Messages.Sidebar, Messages.ResizeSidebar, splitToggleKey
	if !leading {
		name, gripName, ret = Messages.Inspector, Messages.ResizeInspector, splitMainKey
	}
	paneMotion := motion.Default
	if p.Resizing() {
		paneMotion = motion.Instant
	}
	slot := fairy.Box().W(p.ShownWidth()).Shrink(0).HFull().Clip().AnimateLayout(paneMotion).
		Role(fairy.RoleGroup).Label(name)
	if !p.Open() && !p.Resizing() {
		return slot.Role(fairy.RoleHidden)
	}
	if p.Open() {
		body := fairy.Col().Key("kit.split.body").W(p.Width()).Top(0).Bottom(0).FocusReturn(ret).
			Enter(fairy.Fade()).Exit(fairy.Fade().With(motion.Ease(0.12)))
		if leading {
			body.Left(0)
		} else {
			body.Right(0)
		}
		if top != nil {
			body.Child(top)
		}
		if content != nil {
			body.Child(fairy.Col(content.WFull().HFull()).WFull().Grow(1).MinH(0))
		}
		slot.Child(body)
	}
	return slot.Child(paneGrip(p, leading, gripName))
}

func paneGrip(p *PaneState, leading bool, label string) *fairy.El {
	sign := float32(1)
	if !leading {
		sign = -1
	}
	g := fairy.Box().Key("kit.split.grip").W(gripWidth).Top(0).Bottom(0).Cursor(fairy.CursorResizeEW).
		Role(fairy.RoleSeparator).Label(label).
		RangeValue(float64(p.Width()), float64(p.MinWidth()), float64(p.MaxWidth()), float64(gripStep),
			func(v float64) { p.SetWidth(float32(v)) }).
		OnDrag(func(d fairy.DragEvent) { paneDrag(p, sign, d) }).
		OnDoubleClick(p.ResetWidth)
	if leading {
		return g.Right(0)
	}
	return g.Left(0)
}

func abs32(f float32) float32 {
	if f < 0 {
		return -f
	}
	return f
}

// IsSidebarToggle reports whether ev is the shortcut that shows and hides a
// SplitView's sidebar: Ctrl+Shift+B.
func IsSidebarToggle(ev keyboard.KeyEvent) bool {
	return !ev.Repeat && keyboard.ChordOf(ev) == sidebarChord
}

var sidebarChord = keyboard.MustChord("ctrl+shift+b")

// paneDrag applies a drag of a pane's grip.
func paneDrag(p *PaneState, sign float32, d fairy.DragEvent) {
	switch d.Phase {
	case fairy.DragStart:
		p.dragStart = p.Width()
		if !p.Open() {
			p.dragStart = -p.dragStart
		}
		p.resizing = true
	case fairy.DragMove:
		p.resizing = true
		p.DragTo(abs32(p.dragStart) + sign*d.DX)
	case fairy.DragEnd:
		if d.Canceled {
			p.SetWidth(abs32(p.dragStart))
			p.SetOpen(p.dragStart > 0)
		}
		p.resizing = false
	}
}
