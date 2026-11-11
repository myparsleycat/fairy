package kit

import (
	"math"

	"github.com/myparsleycat/fairy/keyboard"
)

// TableState is a table's selection and keyboard cursor, kept in the app's
// state. Rows are named by the ids TableProps.RowID gives, so the selection
// follows a row through a re-sort. The zero value selects one row at a time;
// set Multi to select several (Shift, Ctrl+click, Ctrl+A).
type TableState struct {
	Multi bool

	selected   map[string]bool
	cursor     string
	anchor     string
	cursorHint int // where the cursor's row was last seen, to skip a scan
	anchorHint int
	viewport   float32
	rowHeight  float32
	// pressMods are the modifiers of the last press on a row, for its click.
	pressMods keyboard.Mods
}

// IsSelected reports whether row id is selected.
func (s *TableState) IsSelected(id string) bool { return s.selected[id] }

// Selected returns the selected row ids, in no particular order.
func (s *TableState) Selected() []string {
	ids := make([]string, 0, len(s.selected))
	for id := range s.selected {
		ids = append(ids, id)
	}
	return ids
}

// SelectedLen returns the number of selected rows.
func (s *TableState) SelectedLen() int { return len(s.selected) }

// Cursor is the keyboard cursor's row id, or "".
func (s *TableState) Cursor() string { return s.cursor }

// Select selects row id alone and puts the cursor on it.
func (s *TableState) Select(id string) {
	clear(s.selected)
	if s.selected == nil {
		s.selected = map[string]bool{}
	}
	s.selected[id] = true
	s.cursor, s.anchor = id, id
}

// ClearSelection deselects every row; the cursor stays.
func (s *TableState) ClearSelection() { clear(s.selected) }

func findRow(id string, hint *int, n int, idAt func(int) string) int {
	if id == "" {
		return -1
	}
	if *hint >= 0 && *hint < n && idAt(*hint) == id {
		return *hint
	}
	for i := range n {
		if idAt(i) == id {
			*hint = i
			return i
		}
	}
	return -1
}

// click applies a click on row i of n with mods: Shift extends from the
// anchor and Ctrl adds or removes the row when Multi is set.
func (s *TableState) click(i, n int, mods keyboard.Mods, idAt func(int) string) {
	if s.Multi && mods&keyboard.ModShift != 0 {
		s.extend(i, n, idAt)
		return
	}
	s.cursorHint, s.anchorHint = i, i
	id := idAt(i)
	if s.Multi && mods&keyboard.ModCtrl != 0 {
		if s.selected[id] {
			delete(s.selected, id)
		} else {
			if s.selected == nil {
				s.selected = map[string]bool{}
			}
			s.selected[id] = true
		}
		s.cursor, s.anchor = id, id
		return
	}
	s.Select(id)
}

// extend selects the rows from the anchor to row i.
func (s *TableState) extend(i, n int, idAt func(int) string) {
	a := findRow(s.anchor, &s.anchorHint, n, idAt)
	if a < 0 {
		a, s.anchor, s.anchorHint = i, idAt(i), i
	}
	lo, hi := min(a, i), max(a, i)
	s.selected = make(map[string]bool, hi-lo+1)
	for j := lo; j <= hi; j++ {
		s.selected[idAt(j)] = true
	}
	s.cursor, s.cursorHint = idAt(i), i
}

type tableKeyResult uint8

const (
	tableKeyIgnored tableKeyResult = iota
	tableKeyHandled
	tableKeyMoved // the cursor moved to another row
	tableKeyOpen  // Enter on the cursor's row
)

// handleKey applies a key to n rows named by idAt. It returns the row it moved
// to or opened, or -1.
func (s *TableState) handleKey(ev keyboard.KeyEvent, n int, idAt func(int) string) (tableKeyResult, int) {
	if n == 0 || ev.Mods&(keyboard.ModAlt|keyboard.ModSuper) != 0 {
		return tableKeyIgnored, -1
	}
	if ev.Mods&keyboard.ModCtrl != 0 {
		if ev.Is(keyboard.KeyA, keyboard.ModCtrl) && s.Multi {
			s.selected = make(map[string]bool, n)
			for i := range n {
				s.selected[idAt(i)] = true
			}
			return tableKeyHandled, -1
		}
		return tableKeyIgnored, -1
	}
	at := findRow(s.cursor, &s.cursorHint, n, idAt)
	page := 1
	if s.rowHeight > 0 {
		page = max(int(math.Floor(float64(s.viewport/s.rowHeight)))-1, 1)
	}
	var target int
	switch ev.Key {
	case keyboard.KeyUp:
		target = n - 1
		if at >= 0 {
			target = max(at-1, 0)
		}
	case keyboard.KeyDown:
		target = 0
		if at >= 0 {
			target = min(at+1, n-1)
		}
	case keyboard.KeyHome:
		target = 0
	case keyboard.KeyEnd:
		target = n - 1
	case keyboard.KeyPageUp:
		if at >= 0 {
			target = max(at-page, 0)
		}
	case keyboard.KeyPageDown:
		if at >= 0 {
			target = min(at+page, n-1)
		}
	case keyboard.KeyEnter:
		switch {
		case at < 0:
			return tableKeyIgnored, -1
		case ev.Repeat:
			return tableKeyHandled, -1
		}
		return tableKeyOpen, at
	case keyboard.KeyEscape:
		if !s.Multi || len(s.selected) <= 1 {
			return tableKeyIgnored, -1
		}
		if at >= 0 {
			s.cursorHint, s.anchorHint = at, at
			s.Select(idAt(at))
		} else {
			s.ClearSelection()
		}
		return tableKeyHandled, -1
	default:
		return tableKeyIgnored, -1
	}
	before := s.cursor
	if ev.Mods&keyboard.ModShift != 0 && s.Multi {
		s.extend(target, n, idAt)
	} else {
		s.cursorHint, s.anchorHint = target, target
		s.Select(idAt(target))
	}
	if s.cursor != before {
		return tableKeyMoved, target
	}
	return tableKeyHandled, target
}
