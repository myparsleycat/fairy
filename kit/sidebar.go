package kit

import (
	"strconv"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/motion"
	"github.com/myparsleycat/fairy/svg"
)

// Sidebar row metrics.
const (
	SidebarRowHeight        float32 = 28
	SidebarTwoLineRowHeight float32 = 40
	sidebarIndent           float32 = 16
	sidebarInset            float32 = 8
)

// SidebarProps describes a Sidebar.
type SidebarProps struct {
	// Label names the list for assistive technology; Messages.Sidebar when empty.
	Label  string
	Header []*fairy.El
	// Items are SidebarSection and SidebarRow elements, in order.
	Items  []*fairy.El
	Footer []*fairy.El
	// Count, Key and Row make the list virtual instead of Items: only the rows
	// in view are built. Key names row i, as in fairy.VirtualListMeasured.
	Count int
	Key   func(i int) string
	Row   func(i int) *fairy.El
}

// Sidebar is a source list for a SplitView's sidebar: a header, a scrolling
// list of sections and rows, and a footer. The list is one Tab stop that
// lands on the selected row. Up and Down move the selection, stopping at the
// ends; Home and End go to the ends; typing a row's first letters jumps to
// it; Enter presses the focused row.
func Sidebar(p SidebarProps) *fairy.El {
	label := p.Label
	if label == "" {
		label = Messages.Sidebar
	}
	list := fairy.Col().Key("kit.sidebar.list").WFull().PadEdges(0, sidebarInset, sidebarInset, sidebarInset).
		Role(fairy.RoleList).Label(label).FocusGroup().FocusFollows()
	if p.Row != nil {
		list.Child(fairy.VirtualListMeasured(p.Count, SidebarRowHeight, p.Key, p.Row).WFull().Grow(1).MinH(0))
		list.Grow(1).MinH(0)
	} else {
		list.Child(p.Items...)
	}
	root := fairy.Col().WFull().HFull().MinH(0)
	if len(p.Header) > 0 {
		root.Child(fairy.Col(p.Header...).WFull().PadEdges(0, sidebarInset, sidebarInset, sidebarInset).Gap(2))
	}
	if p.Row != nil {
		root.Child(list)
	} else {
		root.Child(fairy.Col(list).WFull().Grow(1).MinH(0).ScrollY())
	}
	if len(p.Footer) > 0 {
		root.Child(fairy.Col(p.Footer...).WFull().PadXY(sidebarInset, sidebarInset).Gap(2))
	}
	return root
}

// SidebarSection is a titled group of sidebar rows. With onToggle, its
// header shows a disclosure triangle and collapses the rows when pressed;
// whether it is collapsed is the app's state. count, when at least 0, is
// shown after the title.
func SidebarSection(title string, count int, collapsed bool, onToggle func(), rows ...*fairy.El) *fairy.El {
	t := &current
	head := fairy.Row(fairy.Text(title).FontSize(t.SmallSize).Medium().Color(t.MutedForeground).Ellipsis().Shrink(1)).
		Gap(4).Align(fairy.Center).Shrink(1).MinW(0).Role(fairy.RoleHidden)
	if count >= 0 {
		head.Child(fairy.Text(strconv.Itoa(count)).FontSize(t.SmallSize).Medium().Color(t.MutedForeground))
	}
	header := fairy.Row(head, fairy.Spacer()).WFull().H(SidebarRowHeight).PadEdges(4, sidebarInset, 0, sidebarInset).
		Align(fairy.Center).Radius(t.Radius).Label(title)
	if onToggle != nil {
		glyph := lucide.ChevronDown
		if collapsed {
			glyph = lucide.ChevronRight
		}
		head.Child(fairy.Icon(glyph).Size(12, 12).Color(t.MutedForeground))
		// Pointer only: the list's arrows step through rows, and with
		// selection following focus they would toggle sections in passing.
		header.Role(fairy.RoleButton).Expanded(!collapsed).Cursor(fairy.CursorPointer).OnClick(onToggle)
	} else {
		header.Role(fairy.RoleText).Heading(2)
	}
	s := fairy.Col(header).WFull().Gap(2).Role(fairy.RoleGroup).Label(title).AnimateLayout(motion.Default)
	if !collapsed {
		s.Child(rows...)
	}
	return s
}

// RowStatus is what a SidebarRow's status slot shows.
type RowStatus uint8

const (
	StatusNone RowStatus = iota
	// StatusBusy shows a small spinner.
	StatusBusy
	// StatusAttention shows a dot in the warning color.
	StatusAttention
	// StatusUnread shows a dot in the accent color.
	StatusUnread
)

// SidebarRowProps describes a SidebarRow.
type SidebarRowProps struct {
	Label    string
	Icon     *svg.Vector
	Subtitle string
	// Detail is trailing muted text, such as a time.
	Detail string
	Badge  string
	Status RowStatus
	// Trailing is any element after the badge.
	Trailing *fairy.El
	Selected bool
	// Depth indents the row 16 pixels per level.
	Depth int
	// Height defaults to 28, or 40 with a Subtitle.
	Height   float32
	OnSelect func()
	// OnDoubleClick runs on a double click, such as to open the item.
	OnDoubleClick func()
}

// SidebarRow is a selectable sidebar row: icon, label, then trailing detail,
// badge, element and status. Key it by its item's id.
func SidebarRow(p SidebarRowProps) *fairy.El {
	t := &current
	h := p.Height
	if h <= 0 {
		h = SidebarRowHeight
		if p.Subtitle != "" {
			h = SidebarTwoLineRowHeight
		}
	}
	name := p.Label
	if p.Subtitle != "" {
		name += ", " + p.Subtitle
	}
	var said []string
	for _, s := range []string{p.Detail, p.Badge, statusWord(p.Status)} {
		if s != "" {
			said = append(said, s)
		}
	}
	if len(said) > 0 {
		name += ", " + strings.Join(said, ", ")
	}
	r := fairy.Row().WFull().H(h).PadEdges(0, sidebarInset, 0, sidebarInset+float32(max(p.Depth, 0))*sidebarIndent).
		Gap(8).Align(fairy.Center).Radius(t.RadiusMd()).Color(t.SidebarForeground).Animate(motion.Fast).
		Focusable().FocusSelected(p.Selected).FocusRing(2, t.SidebarRing).
		Role(fairy.RoleListItem).Label(name).Selected(p.Selected).Cursor(fairy.CursorPointer)
	if p.Selected {
		r.Fill(t.SidebarAccent).Color(t.SidebarAccentForeground).Medium()
	} else {
		r.HoverFill(t.SidebarAccent).HoverColor(t.SidebarAccentForeground)
	}
	if p.OnSelect != nil {
		r.OnClick(p.OnSelect)
	}
	if p.OnDoubleClick != nil {
		r.OnDoubleClick(p.OnDoubleClick)
	}
	var lead *fairy.El
	if p.Icon != nil {
		lead = fairy.Icon(p.Icon).Size(16, 16).Shrink(0)
	}
	label := fairy.Text(p.Label).Ellipsis().Grow(1).Shrink(1).MinW(0)
	if p.Subtitle != "" {
		label.Medium()
		first := fairy.Row(label).WFull().Gap(6).Align(fairy.Center)
		if lead != nil {
			first = fairy.Row(lead, label).WFull().Gap(6).Align(fairy.Center)
		}
		sub := fairy.Text(p.Subtitle).FontSize(t.SmallSize).Color(t.MutedForeground).Ellipsis().WFull()
		second := fairy.Row(sub).WFull()
		if lead != nil {
			second.PadEdges(0, 0, 0, 16+6)
		}
		r.Child(fairy.Col(first, second).Grow(1).Shrink(1).MinW(0).Role(fairy.RoleHidden))
	} else {
		if lead != nil {
			r.Child(lead.Role(fairy.RoleHidden))
		}
		r.Child(label.Role(fairy.RoleHidden))
	}
	if p.Detail != "" {
		r.Child(fairy.Text(p.Detail).FontSize(t.SmallSize).Color(t.MutedForeground).NoWrap().Role(fairy.RoleHidden))
	}
	if p.Badge != "" {
		r.Child(fairy.Row(fairy.Text(p.Badge).FontSize(t.SmallSize).Medium().Color(t.MutedForeground).NoWrap()).
			H(20).PadXY(6, 0).Center().Radius(999).Fill(t.Muted).Role(fairy.RoleHidden))
	}
	if p.Trailing != nil {
		r.Child(p.Trailing)
	}
	if p.Status != StatusNone {
		dot := func(c fairy.Color) *fairy.El { return fairy.Box().Size(7, 7).Radius(3.5).Fill(c) }
		var mark *fairy.El
		switch p.Status {
		case StatusBusy:
			mark = fairy.Spinner().Size(12, 12).Color(t.MutedForeground)
		case StatusAttention:
			mark = dot(t.Warning)
		case StatusUnread:
			mark = dot(t.Primary)
		}
		r.Child(fairy.Row(mark).Size(20, 20).Shrink(0).Center().Role(fairy.RoleHidden))
	}
	return r
}

func statusWord(s RowStatus) string {
	switch s {
	case StatusBusy:
		return Messages.Working
	case StatusAttention:
		return Messages.NeedsAttention
	case StatusUnread:
		return Messages.Unread
	}
	return ""
}
