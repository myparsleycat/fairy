package kit

import (
	"fmt"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/motion"
)

// CellAlign positions cell content horizontally.
type CellAlign uint8

const (
	CellStart CellAlign = iota
	CellCenter
	CellEnd
)

// Column describes a table column. A column has a fixed Width, or takes a
// Grow share of the remaining width (1 when both are zero).
type Column struct {
	Title    string
	Width    float32
	Grow     float32
	Align    CellAlign
	Sortable bool
	// Resizable puts a handle on the column's right edge that sets its width
	// by dragging, between MinWidth (48 when zero) and MaxWidth (no limit
	// when zero). Escape during the drag puts the width back; a double click
	// on the handle returns the column to its declared size. The table needs
	// an ID to keep the widths. A growing column keeps MinWidth too.
	Resizable          bool
	MinWidth, MaxWidth float32
}

func (c Column) clamp(w float32) float32 {
	lo := c.MinWidth
	if lo == 0 {
		lo = 48
	}
	w = max(w, lo)
	if c.MaxWidth > 0 {
		w = min(w, max(c.MaxWidth, lo))
	}
	return w
}

// TableProps describes a Table. Only the visible rows are built, so Rows may
// be large.
type TableProps struct {
	Columns []Column
	Rows    int
	// Cell builds the content of a cell; a plain fairy.Text is typical.
	Cell      func(row, col int) *fairy.El
	RowHeight float32 // 36 when zero
	// Selected reports whether a row is highlighted.
	Selected   func(row int) bool
	OnRowClick func(row int)
	// SortColumn is the index of the sorted column, or -1.
	SortColumn int
	SortDesc   bool
	// OnSort runs when a sortable header is clicked, with the column and the
	// requested direction. The caller sorts its data.
	OnSort func(col int, desc bool)
	// ID keeps the widths set on resizable columns.
	ID string
	// OnColumnResize runs when a drag on a column's handle ends, with the new
	// width, or 0 when a double click returned the column to its declared size.
	OnColumnResize func(col int, width float32)

	// State and RowID make the table keep the selection by row id and take
	// keys. It is one Tab stop: Up and Down move the selection, stopping at
	// the ends; Home, End, Page Up and Page Down jump; Enter opens the row.
	// With State.Multi, Shift extends the selection, Ctrl+click toggles a
	// row, Ctrl+A selects every row and Escape keeps only the cursor's row.
	// Selected and OnRowClick are not used then.
	State *TableState
	RowID func(row int) string
	// OnSelect runs when a click or a key selects a row, with its id.
	OnSelect func(id string)
	// OnOpen runs on a double click or Enter on a row.
	OnOpen func(id string)
	// Zebra shades every other row.
	Zebra bool
	// Label names the table for assistive technology.
	Label string
}

// tableState keeps a table's column widths: the widths set by dragging
// override the declared ones.
type tableState struct {
	widths      map[int]float32 // set by dragging
	laidOut     map[int]float32 // as last laid out, where a drag starts from
	resizeStart float32         // the override before the drag; 0 when there was none
	dragBase    float32
}

func sizeColumn(e *fairy.El, c Column, override float32) *fairy.El {
	if override > 0 {
		// Dragged columns give way, down to their minimum, when the row runs
		// out of room, so the columns after them stay in view.
		return e.W(override).Shrink(1).MinW(c.clamp(0))
	}
	if c.Width > 0 {
		return e.W(c.Width).Shrink(0)
	}
	g := c.Grow
	if g == 0 {
		g = 1
	}
	return e.Grow(g).Basis(0).MinW(c.clamp(0))
}

// Table is a header row over a virtualized list of rows. Bound its height
// like any scroll container.
func Table(p TableProps) *fairy.El {
	t := &current
	rh := p.RowHeight
	if rh == 0 {
		rh = 36
	}
	header := fairy.Row().H(40).Align(fairy.Center).
		BorderEdges(0, 0, 1, 0, t.Border).Color(t.Foreground).Medium().
		Role(fairy.RoleRow)
	var st *tableState
	if p.ID != "" {
		st = fairy.Local[tableState]("kit.table:" + p.ID)
		if st.widths == nil {
			st.widths, st.laidOut = map[int]float32{}, map[int]float32{}
		}
	}
	override := func(i int) float32 {
		if st == nil {
			return 0
		}
		return st.widths[i]
	}
	for i, c := range p.Columns {
		cell := fairy.Row(fairy.Text(c.Title).Ellipsis().TooltipWhenTruncated()).Gap(4).Align(fairy.Center).PadXY(8, 0).HFull().
			Role(fairy.RoleColumnHeader).Label(c.Title)
		alignCell(cell, c.Align)
		if c.Sortable && p.OnSort != nil {
			icon := lucide.ArrowUpDown
			desc := false
			if p.SortColumn == i {
				icon, desc = lucide.ChevronUp, true
				if p.SortDesc {
					icon, desc = lucide.ChevronDown, false
				}
				cell.Color(t.Foreground)
			}
			cell.Child(fairy.Icon(icon).Size(13, 13)).HoverColor(t.Foreground).Cursor(fairy.CursorPointer).
				OnClick(func() { p.OnSort(i, desc) })
		}
		if st != nil {
			cell.OnLayout(func(w, _ float32) { st.laidOut[i] = w })
			if c.Resizable {
				cell.Child(resizeHandle(st, p, i, c))
			}
		}
		if (st == nil || !c.Resizable) && i < len(p.Columns)-1 {
			cell.Child(fairy.Box().W(1).Top(8).Bottom(8).Right(0).Fill(t.Border).Role(fairy.RoleHidden))
		}
		header.Child(sizeColumn(cell, c, override(i)))
	}
	ts := p.State
	if p.RowID == nil {
		ts = nil
	}
	id := p.ID
	if id == "" && ts != nil {
		// The app's state is one per table, so its address keeps the keys of
		// tables without an ID apart.
		id = fmt.Sprintf("%p", ts)
	}
	listKey := "kit.table.rows:" + id
	stripe := alpha(t.Muted, 0.5)
	body := fairy.VirtualList(p.Rows, rh, func(r int) *fairy.El {
		row := fairy.Row().H(rh).Align(fairy.Center).BorderEdges(0, 0, 1, 0, t.Border).
			Animate(motion.Fast).Role(fairy.RoleRow)
		sel := false
		switch {
		case ts != nil:
			id := p.RowID(r)
			sel = ts.IsSelected(id)
			row.Key(id).Selected(sel)
			if id == ts.cursor {
				row.ActiveDescendant()
				if ts.Multi {
					row.Border(1, t.Ring)
				}
			}
			row.OnPointerDown(func(e fairy.PointerEvent) { ts.pressMods = e.Mods }).
				OnClick(func() {
					ts.click(r, p.Rows, ts.pressMods, p.RowID)
					if p.OnSelect != nil {
						p.OnSelect(id)
					}
				})
			if p.OnOpen != nil {
				row.OnDoubleClick(func() { p.OnOpen(id) })
			}
		case p.Selected != nil:
			sel = p.Selected(r)
			row.Selected(sel)
		}
		switch {
		case sel:
			row.Fill(t.Muted)
		case p.Zebra && r%2 == 1:
			row.Fill(stripe).HoverFill(t.Muted)
		default:
			row.HoverFill(alpha(t.Muted, 0.5))
		}
		if ts == nil && p.OnRowClick != nil {
			row.Cursor(fairy.CursorPointer).OnClick(func() { p.OnRowClick(r) })
		}
		for i, c := range p.Columns {
			cell := fairy.Row(p.Cell(r, i)).Align(fairy.Center).PadXY(8, 0).HFull().Clip().Role(fairy.RoleGridCell)
			alignCell(cell, c.Align)
			row.Child(sizeColumn(cell, c, override(i)))
		}
		return row
	}).Key(listKey).Grow(1).MinH(0)
	root := fairy.Col(header, body).Radius(t.RadiusLg()).Border(1, t.Border).Clip().
		Role(fairy.RoleGrid)
	if p.Label != "" {
		root.Label(p.Label)
	}
	if ts == nil {
		return root
	}
	body.OnLayout(func(_, h float32) { ts.viewport, ts.rowHeight = h, rh })
	w := fairy.CurrentWindow()
	return root.Key("kit.table:"+id).Focusable().FocusVisibleBorder(t.Ring).FocusRing(3, alpha(t.Ring, 0.5)).
		OnKey(func(ev keyboard.KeyEvent) bool {
			res, row := ts.handleKey(ev, p.Rows, p.RowID)
			if row >= 0 && w != nil {
				w.ScrollToRowInstant(listKey, row)
			}
			switch res {
			case tableKeyIgnored:
				return false
			case tableKeyMoved:
				if p.OnSelect != nil {
					p.OnSelect(ts.cursor)
				}
			case tableKeyOpen:
				if p.OnOpen != nil {
					p.OnOpen(p.RowID(row))
				}
			}
			return true
		})
}

// resizeHandle is the drag handle on column i's right edge: the drag starts
// from the column's current width, Escape puts the width from before the drag
// back, and a double click resets it.
func resizeHandle(st *tableState, p TableProps, i int, c Column) *fairy.El {
	t := &current
	notify := func(w float32) {
		if p.OnColumnResize != nil {
			p.OnColumnResize(i, w)
		}
	}
	return fairy.Box().W(8).Top(4).Bottom(4).Right(0).Radius(2).Cursor(fairy.CursorResizeEW).
		HoverFill(t.Border).Role(fairy.RoleSeparator).Label(Messages.ResizeColumn).
		OnDrag(func(ev fairy.DragEvent) {
			switch ev.Phase {
			case fairy.DragStart:
				st.resizeStart = st.widths[i]
				st.dragBase = st.laidOut[i]
				if st.dragBase == 0 {
					st.dragBase = st.widths[i]
				}
				if st.dragBase == 0 {
					st.dragBase = c.clamp(c.Width)
				}
			case fairy.DragMove:
				st.widths[i] = c.clamp(st.dragBase + ev.DX)
			case fairy.DragEnd:
				if ev.Canceled {
					if st.resizeStart > 0 {
						st.widths[i] = st.resizeStart
					} else {
						delete(st.widths, i)
					}
					return
				}
				st.widths[i] = c.clamp(st.dragBase + ev.DX)
				notify(st.widths[i])
			}
		}).
		OnDoubleClick(func() {
			delete(st.widths, i)
			notify(0)
		})
}

func alignCell(e *fairy.El, a CellAlign) {
	switch a {
	case CellCenter:
		e.Justify(fairy.JustifyCenter)
	case CellEnd:
		e.Justify(fairy.JustifyEnd)
	}
}
