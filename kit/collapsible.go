package kit

import (
	"math"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/motion"
)

// Collapsible is a 32 pixel trigger row with a disclosure arrow, shown on
// hover or while open, over content shown while open. The caller keeps open
// and flips it in onToggle. id must be unique in the window.
func Collapsible(id, title string, open bool, onToggle func(bool), content *fairy.El, opts ...Option) *fairy.El {
	t := &current
	o := build(opts)
	hover := fairy.Local[bool]("kit.collapsible:" + id)
	turn := float32(-math.Pi / 2)
	if open {
		turn = 0
	}
	arrow := fairy.Box(fairy.Icon(lucide.ChevronDown).Size(14, 14).Color(t.MutedForeground).Rotate(turn).ScaleMotion(motion.Snappy)).
		Size(24, 24).Center().Opacity(0).Animate(motion.Snappy)
	if open || *hover {
		arrow.Opacity(1)
	}
	trigger := fairy.Row(fairy.Text(title).Medium().FontSize(t.SmallSize+1), arrow).Gap(8).Align(fairy.Center).H(32).
		Radius(t.Radius).Role(fairy.RoleButton).Label(title).Expanded(open).OnHover(func(in bool) { *hover = in })
	if o.disabled {
		trigger.Color(t.MutedForeground).Disabled(true)
	} else {
		trigger.Focusable().FocusVisibleBorder(t.Ring).FocusRing(3, alpha(t.Ring, 0.5)).Cursor(fairy.CursorPointer).OnClick(func() { onToggle(!open) })
	}
	root := fairy.Col(trigger).Key("kit.collapsible:" + id).Role(fairy.RoleGroup).AnimateHeight(motion.Snappy)
	if open && content != nil {
		root.Child(fairy.Col(content).Key("content").Role(fairy.RoleGroup).Label(title).Exit(fairy.Fade()))
	}
	return root
}

// AccordionItem is one section of an Accordion. The caller keeps Open and
// flips it in OnToggle; whether only one section opens at a time is the
// caller's choice.
type AccordionItem struct {
	Title    string
	Open     bool
	Disabled bool
	Content  *fairy.El
	OnToggle func(open bool)
}

// Accordion stacks bordered sections that share their borders, each a trigger
// with a chevron over its content.
func Accordion(items []AccordionItem) *fairy.El {
	t := &current
	root := fairy.Col().Radius(t.RadiusLg()).Border(1, t.Border).Clip().Role(fairy.RoleGroup)
	for i, it := range items {
		turn := float32(-math.Pi / 2)
		if it.Open {
			turn = 0
		}
		trigger := fairy.Row(
			fairy.Text(it.Title).Medium().Grow(1).MinW(0).Ellipsis(),
			fairy.Icon(lucide.ChevronDown).Size(16, 16).Color(t.MutedForeground).Rotate(turn).ScaleMotion(motion.Snappy),
		).Gap(8).Align(fairy.Center).H(44).PadXY(16, 0).Role(fairy.RoleButton).Label(it.Title).Expanded(it.Open).
			Animate(motion.Snappy)
		if it.Disabled {
			trigger.Color(t.MutedForeground).Disabled(true)
		} else if it.OnToggle != nil {
			f, open := it.OnToggle, it.Open
			trigger.HoverFill(alpha(t.Muted, 0.5)).Focusable().FocusVisibleBorder(t.Ring).FocusRing(3, alpha(t.Ring, 0.5)).Cursor(fairy.CursorPointer).
				OnClick(func() { f(!open) })
		}
		section := fairy.Col(trigger).Key(itemKey(i, it.Title)).AnimateHeight(motion.Snappy)
		if i > 0 {
			section.BorderEdges(1, 0, 0, 0, t.Border)
		}
		if it.Open && it.Content != nil {
			section.Child(fairy.Col(it.Content).Key("content").PadEdges(0, 16, 16, 16).Role(fairy.RoleGroup).Label(it.Title).Exit(fairy.Fade()))
		}
		root.Child(section)
	}
	return root
}
