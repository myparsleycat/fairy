package kit

import (
	"slices"
	"testing"

	"github.com/myparsleycat/fairy/keyboard"
)

func TestTableKeys(t *testing.T) {
	ids := []string{"a", "b", "c", "d", "e"}
	idAt := func(i int) string { return ids[i] }
	key := func(s *TableState, k keyboard.Key, mods keyboard.Mods) (tableKeyResult, int) {
		return s.handleKey(keyboard.KeyEvent{Key: k, Mods: mods}, len(ids), idAt)
	}
	var s TableState
	if res, row := key(&s, keyboard.KeyUp, 0); res != tableKeyMoved || row != 4 || s.Cursor() != "e" {
		t.Fatalf("Up with no cursor: %v %d %q, want the last row", res, row, s.Cursor())
	}
	if res, _ := key(&s, keyboard.KeyDown, 0); res != tableKeyHandled || s.Cursor() != "e" {
		t.Fatalf("Down at the end: %v %q, want it to stop", res, s.Cursor())
	}
	key(&s, keyboard.KeyHome, 0)
	if res, _ := key(&s, keyboard.KeyUp, 0); res != tableKeyHandled || s.Cursor() != "a" {
		t.Fatalf("Up at the start: %v %q, want it to stop", res, s.Cursor())
	}
	s.viewport, s.rowHeight = 100, 25 // four rows in view: a page moves three
	if key(&s, keyboard.KeyPageDown, 0); s.Cursor() != "d" {
		t.Fatalf("Page Down went to %q, want d", s.Cursor())
	}
	if res, row := key(&s, keyboard.KeyEnter, 0); res != tableKeyOpen || row != 3 {
		t.Fatalf("Enter: %v %d, want open row 3", res, row)
	}
	if res, _ := s.handleKey(keyboard.KeyEvent{Key: keyboard.KeyEnter, Repeat: true}, len(ids), idAt); res != tableKeyHandled {
		t.Fatalf("a repeated Enter opened again: %v", res)
	}
	if res, _ := key(&s, keyboard.KeyDown, keyboard.ModShift); s.SelectedLen() != 1 || res != tableKeyMoved {
		t.Fatalf("Shift+Down without Multi selected %d rows", s.SelectedLen())
	}
	if res, _ := key(&s, keyboard.KeyA, keyboard.ModPrimary); res != tableKeyIgnored {
		t.Fatalf("Ctrl+A without Multi: %v, want ignored", res)
	}
}

func TestTableMultiSelect(t *testing.T) {
	ids := []string{"a", "b", "c", "d", "e"}
	idAt := func(i int) string { return ids[i] }
	s := TableState{Multi: true}
	s.click(1, len(ids), 0, idAt)
	s.handleKey(keyboard.KeyEvent{Key: keyboard.KeyDown, Mods: keyboard.ModShift}, len(ids), idAt)
	s.handleKey(keyboard.KeyEvent{Key: keyboard.KeyDown, Mods: keyboard.ModShift}, len(ids), idAt)
	if got := sorted(s.Selected()); !slices.Equal(got, []string{"b", "c", "d"}) || s.Cursor() != "d" {
		t.Fatalf("Shift+Down twice from b: %v cursor %q", got, s.Cursor())
	}
	s.click(0, len(ids), keyboard.ModPrimary, idAt)
	if !s.IsSelected("a") || s.SelectedLen() != 4 {
		t.Fatalf("Ctrl+click added nothing: %v", sorted(s.Selected()))
	}
	s.click(4, len(ids), keyboard.ModShift, idAt)
	if got := sorted(s.Selected()); !slices.Equal(got, ids) {
		t.Fatalf("Shift+click from the anchor a: %v", got)
	}
	s.handleKey(keyboard.KeyEvent{Key: keyboard.KeyEscape}, len(ids), idAt)
	if got := s.Selected(); !slices.Equal(got, []string{"e"}) {
		t.Fatalf("Escape kept %v, want only the cursor's row e", got)
	}
	s.handleKey(keyboard.KeyEvent{Key: keyboard.KeyA, Mods: keyboard.ModPrimary}, len(ids), idAt)
	if s.SelectedLen() != 5 {
		t.Fatalf("Ctrl+A selected %d rows", s.SelectedLen())
	}
}

// The selection and cursor are row ids, so they follow their rows when the
// app re-sorts them.
func TestTableSelectionFollowsSort(t *testing.T) {
	ids := []string{"a", "b", "c", "d"}
	idAt := func(i int) string { return ids[i] }
	s := TableState{Multi: true}
	s.click(1, len(ids), 0, idAt)
	s.click(2, len(ids), keyboard.ModShift, idAt)
	slices.Reverse(ids) // d c b a
	s.handleKey(keyboard.KeyEvent{Key: keyboard.KeyDown}, len(ids), idAt)
	if s.Cursor() != "b" {
		t.Fatalf("Down from c after the sort went to %q, want b", s.Cursor())
	}
}

func sorted(ids []string) []string {
	slices.Sort(ids)
	return ids
}
