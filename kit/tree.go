package kit

import (
	"math"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/motion"
	"github.com/myparsleycat/fairy/svg"
)

// TreeItem is what a tree row shows after its indent and chevron: an icon, a
// label and trailing content.
type TreeItem struct {
	Label     string
	Icon      *svg.Vector
	IconColor fairy.Color // MutedForeground when unset
	// LabelColor dims a row, for an ignored file say; Foreground when unset.
	LabelColor fairy.Color
	// Highlight marks the first case-insensitive occurrence of it in the
	// label, as a search does.
	Highlight string
	Badge     string      // a short status letter or count at the end of the row
	Dot       fairy.Color // a small status dot at the end of the row, when set
	Trailing  *fairy.El   // any element at the end of the row
}

// TreeProps describes a Tree.
type TreeProps[T any] struct {
	// Label names a node, for the default row and for typing to jump to it.
	Label func(T) string
	// Row builds a row's content; without it a row shows Label.
	Row func(data T, r TreeRow) TreeItem
	// OnSelect runs when the cursor moves to id by a click or a key.
	OnSelect func(id string)
	// OnOpen runs when a row without children is double-clicked, or Enter or
	// Space is pressed on it.
	OnOpen func(id string)
	// Name labels the tree for assistive technology ("Files").
	Name      string
	RowHeight float32 // 24 when zero
	Indent    float32 // 16 when zero
	// Guides draws a line down each level of the indent.
	Guides bool
}

// Tree draws a TreeModel as a virtualized outline: only the rows on screen are
// built, so a large tree costs what the visible rows do. The tree is one focus
// stop that keeps the keyboard while rows scroll in and out (see
// TreeModel.HandleKey); a click selects a row and opens or closes a folder.
// Bound its height like any scroll container. id must be unique in the window.
func Tree[T any](id string, m *TreeModel[T], p TreeProps[T]) *fairy.El {
	t := &current
	rh := p.RowHeight
	if rh == 0 {
		rh = 24
	}
	indent := p.Indent
	if indent == 0 {
		indent = 16
	}
	listKey := "kit.tree:" + id
	w := fairy.CurrentWindow()
	reveal := func() {
		if r, ok := m.VisibleRow(m.Cursor()); ok && w != nil {
			w.ScrollToRowInstant(listKey, r)
		}
	}
	label := p.Label
	if label == nil {
		label = func(T) string { return "" }
	}
	rows := fairy.VirtualList(m.VisibleLen(), rh, func(i int) *fairy.El {
		data, r, ok := m.Row(i)
		if !ok {
			return nil
		}
		item := TreeItem{Label: label(data)}
		if p.Row != nil {
			item = p.Row(data, r)
		}
		row := treeRow(item, r, rh, indent, p.Guides).Key(r.ID)
		rid := r.ID
		row.OnPointerDown(func(e fairy.PointerEvent) { m.pressMods = e.Mods }).
			OnClick(func() {
				plain := m.Click(rid, m.pressMods)
				if r.Expandable && plain {
					m.Toggle(rid)
				}
				if p.OnSelect != nil {
					p.OnSelect(rid)
				}
			})
		if !r.Expandable && p.OnOpen != nil {
			row.OnDoubleClick(func() { p.OnOpen(rid) })
		}
		return row
	}).Key(listKey).Grow(1).MinH(0).
		OnLayout(func(_, h float32) { m.viewport, m.rowHeight = h, rh })
	root := fairy.Col(rows).Key("kit.tree.root:" + id).Focusable().Role(fairy.RoleTree).MinH(0).
		FontSize(t.FontSize).
		OnKey(func(ev keyboard.KeyEvent) bool {
			before := m.Cursor()
			switch m.HandleKey(ev, label) {
			case TreeKeyHandled:
				reveal()
				if after := m.Cursor(); after != before && after != "" && p.OnSelect != nil {
					p.OnSelect(after)
				}
				return true
			case TreeKeyOpen:
				if p.OnOpen != nil {
					p.OnOpen(m.Cursor())
				}
				return true
			}
			return false
		})
	if p.Name != "" {
		root.Label(p.Name)
	}
	return root
}

// treeRow draws a row: indent guides, a chevron that turns as the folder
// opens, the icon, the label and trailing content.
func treeRow(it TreeItem, r TreeRow, rh, indent float32, guides bool) *fairy.El {
	t := &current
	const pad, chevronW = 4, 16
	e := fairy.Row().H(rh).PadEdges(0, pad, 0, pad+float32(r.Depth)*indent).Gap(6).Align(fairy.Center).
		Radius(t.Radius-2).Role(fairy.RoleTreeItem).Label(it.Label).Level(r.Depth+1).PositionInSet(r.pos, r.size).Selected(r.Selected).
		Animate(motion.Fast)
	if r.Expandable {
		e.Expanded(r.Expanded)
	}
	if r.Selected {
		e.Fill(t.Muted)
	} else {
		e.HoverFill(alpha(t.Muted, 0.5))
	}
	if r.Cursor {
		e.ActiveDescendant()
		if r.multi {
			e.Border(1, t.Ring)
		}
	}
	if guides {
		for level := 0; level < r.Depth; level++ {
			x := pad + float32(level)*indent + chevronW/2 - 0.5
			e.Child(fairy.Box().W(1).Top(0).Bottom(0).Left(x).Fill(t.Border).Role(fairy.RoleHidden))
		}
	}
	chevron := fairy.Box().Size(chevronW, chevronW).Center().Shrink(0).Role(fairy.RoleHidden)
	if r.Expandable {
		turn := float32(0)
		if !r.Expanded {
			turn = -math.Pi / 2
		}
		chevron.Child(fairy.Icon(lucide.ChevronDown).Size(14, 14).Color(t.MutedForeground).
			Rotate(turn).ScaleMotion(motion.Snappy))
	}
	e.Child(chevron)
	if it.Icon != nil {
		c := it.IconColor
		if c.A == 0 {
			c = t.MutedForeground
		}
		e.Child(fairy.Icon(it.Icon).Size(16, 16).Color(c).Shrink(0).Role(fairy.RoleHidden))
	}
	color := it.LabelColor
	if color.A == 0 {
		color = t.Foreground
	}
	var text *fairy.El
	if a, b, ok := findFold(it.Label, it.Highlight); ok {
		text = fairy.RichText(
			fairy.Span{Text: it.Label[:a]},
			fairy.Span{Text: it.Label[a:b], Weight: 650, Background: t.Primary.Alpha(0.22)},
			fairy.Span{Text: it.Label[b:]},
		)
	} else {
		text = fairy.Text(it.Label)
	}
	e.Child(text.Color(color).Grow(1).MinW(0).Ellipsis().TooltipWhenTruncated().Role(fairy.RoleHidden))
	if it.Badge != "" {
		e.Child(fairy.Text(it.Badge).FontSize(t.SmallSize).Medium().Color(t.MutedForeground).Role(fairy.RoleHidden))
	}
	if it.Dot.A > 0 {
		e.Child(fairy.Box().Size(6, 6).Radius(3).Fill(it.Dot).MarginEdges(0, 5, 0, 5).Role(fairy.RoleHidden))
	}
	if it.Trailing != nil {
		e.Child(it.Trailing)
	}
	return e
}

// findFold returns the byte range of q in s ignoring case.
func findFold(s, q string) (int, int, bool) {
	if q == "" {
		return 0, 0, false
	}
	ls, lq := strings.ToLower(s), strings.ToLower(q)
	if len(ls) != len(s) {
		// Lowercasing changed byte lengths; only an exact match maps back.
		if a := strings.Index(s, q); a >= 0 {
			return a, a + len(q), true
		}
		return 0, 0, false
	}
	if a := strings.Index(ls, lq); a >= 0 {
		return a, a + len(lq), true
	}
	return 0, 0, false
}
