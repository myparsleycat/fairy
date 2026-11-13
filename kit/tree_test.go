package kit

import (
	"slices"
	"testing"
	"time"

	"github.com/myparsleycat/fairy/keyboard"
)

func treeFixture() *TreeModel[string] {
	m := NewTreeModel[string]()
	m.Insert("", "a", "alpha")
	m.Insert("a", "a/1", "one")
	m.Insert("a", "a/2", "two")
	m.Insert("", "b", "beta")
	m.Insert("", "c", "bravo")
	return m
}

func label(s string) string { return s }

func TestTreeTypeSelect(t *testing.T) {
	m := treeFixture()
	if r := m.typeSelect("b", true, label); r != 1 {
		t.Errorf("b = row %d, want 1 (beta): rows are alpha, beta, bravo", r)
	}
	m.Select("b")
	if r := m.typeSelect("b", false, label); r != 2 {
		t.Errorf("b again = row %d, want 2 (bravo)", r)
	}
	m.Select("c")
	if r := m.typeSelect("r", false, label); r != -1 {
		t.Errorf(`"bbr" = row %d, want none`, r)
	}
	if r := m.typeSelect("b", true, label); r != 1 {
		t.Errorf("after a pause = row %d, want 1", r)
	}
	m.Select("a")
	m.typeSelect("b", true, label)
	if r := m.typeSelect("r", false, label); r != 2 {
		t.Errorf(`"br" = row %d, want 2 (bravo)`, r)
	}
}

// Typed text comes from KeyEvent.Text, so names in any script and typed with
// Shift match.
func TestTreeTypeSelectText(t *testing.T) {
	m := NewTreeModel[string]()
	m.Insert("", "x", "가방")
	m.Insert("", "y", "나무")
	m.Insert("", "z", "Zebra")
	m.HandleKey(keyboard.KeyEvent{Key: keyboard.Key('S'), Text: "나"}, label)
	if m.Cursor() != "y" {
		t.Errorf(`"나" = %q, want y (나무)`, m.Cursor())
	}
	m.typedAt = m.typedAt.Add(-time.Hour) // a pause starts a new prefix
	m.HandleKey(keyboard.KeyEvent{Key: keyboard.Key('Z'), Mods: keyboard.ModShift, Text: "Z"}, label)
	if m.Cursor() != "z" {
		t.Errorf(`Shift+Z = %q, want z (Zebra)`, m.Cursor())
	}
}

func TestTreeKeys(t *testing.T) {
	m := treeFixture()
	key := func(k keyboard.Key, mods keyboard.Mods) TreeKeyResult {
		return m.HandleKey(keyboard.KeyEvent{Key: k, Mods: mods}, label)
	}

	key(keyboard.KeyDown, 0)
	if m.Cursor() != "a" || m.VisibleLen() != 3 {
		t.Fatalf("Down from nothing: cursor %q, %d rows", m.Cursor(), m.VisibleLen())
	}
	key(keyboard.KeyRight, 0) // opens a
	key(keyboard.KeyRight, 0) // goes to its first child
	if m.Cursor() != "a/1" || m.VisibleLen() != 5 {
		t.Fatalf("Right twice: cursor %q, %d rows", m.Cursor(), m.VisibleLen())
	}
	if got := key(keyboard.KeyEnter, 0); got != TreeKeyOpen {
		t.Errorf("Enter on a leaf = %v, want open", got)
	}
	key(keyboard.KeyLeft, 0) // a leaf goes to its parent
	if m.Cursor() != "a" {
		t.Fatalf("Left on a leaf: cursor %q, want a", m.Cursor())
	}
	key(keyboard.KeyDown, 0)
	m.SetExpanded("a", false) // closing the folder moves the cursor inside it up to it
	if m.Cursor() != "a" || !m.IsSelected("a") {
		t.Fatalf("closing a: cursor %q, a selected %v", m.Cursor(), m.IsSelected("a"))
	}
	key(keyboard.KeyEnd, 0)
	if m.Cursor() != "c" {
		t.Fatalf("End: cursor %q", m.Cursor())
	}
}

func TestTreeFilterAndRange(t *testing.T) {
	m := treeFixture().MultiSelect(true)
	m.Filter(func(s string) bool { return s == "two" })
	var rows []string
	for i := range m.VisibleLen() {
		rows = append(rows, m.VisibleID(i))
	}
	if !slices.Equal(rows, []string{"a", "a/2"}) || m.IsMatch("a") || !m.IsMatch("a/2") {
		t.Fatalf("filtered rows = %v, match a %v, a/2 %v", rows, m.IsMatch("a"), m.IsMatch("a/2"))
	}
	m.ClearFilter()
	m.ExpandAll()
	m.Click("a/1", 0)
	m.Click("c", keyboard.ModShift)
	if got := m.Selected(); !slices.Equal(got, []string{"a/1", "a/2", "b", "c"}) {
		t.Fatalf("Shift-click range = %v", got)
	}
	m.Click("b", keyboard.ModPrimary)
	if got := m.Selected(); !slices.Equal(got, []string{"a/1", "a/2", "c"}) {
		t.Fatalf("Ctrl-click = %v", got)
	}
}
