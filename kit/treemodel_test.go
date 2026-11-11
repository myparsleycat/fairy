package kit

import (
	"fmt"
	"slices"
	"testing"
)

func TestTreeRowSiblingPositions(t *testing.T) {
	m := treeFixture()
	m.ExpandAll()
	type row struct {
		id        string
		pos, size int
		matched   bool
	}
	check := func(want []row) {
		t.Helper()
		if m.VisibleLen() != len(want) {
			t.Fatalf("visible rows = %d, want %d", m.VisibleLen(), len(want))
		}
		for repeat := range 2 {
			for i, w := range want {
				data, r, ok := m.Row(i)
				wantData, _ := m.Get(w.id)
				if !ok || r.ID != w.id || data != wantData || r.pos != w.pos || r.size != w.size || r.Matched != w.matched {
					t.Errorf("read %d row %d = %q %+v, want %+v", repeat, i, data, r, w)
				}
				if at, visible := m.VisibleRow(w.id); !visible || at != i || m.VisibleID(i) != w.id {
					t.Errorf("row lookup for %q = %d, %v", w.id, at, visible)
				}
			}
		}
	}
	check([]row{{"a", 1, 3, false}, {"a/1", 1, 2, false}, {"a/2", 2, 2, false}, {"b", 2, 3, false}, {"c", 3, 3, false}})
	m.Filter(func(s string) bool { return s == "two" || s == "three" || s == "bravo" })
	check([]row{{"a", 1, 2, false}, {"a/2", 1, 1, true}, {"c", 2, 2, true}})
	m.Insert("a", "a/3", "three")
	m.Insert("", "d", "delta")
	m.SortBy(func(a, b string) bool { return a < b })
	check([]row{{"a", 1, 2, false}, {"a/3", 1, 2, true}, {"a/2", 2, 2, true}, {"c", 2, 2, true}})
	m.SetExpanded("a", false)
	check([]row{{"a", 1, 2, false}, {"c", 2, 2, true}})
	_, r, _ := m.Row(0)
	if !r.Expandable || r.Expanded {
		t.Errorf("closed filtered folder state = %+v", r)
	}
	m.ClearFilter()
	check([]row{{"a", 1, 4, false}, {"a/1", 1, 3, false}, {"a/3", 2, 3, false}, {"a/2", 3, 3, false}, {"b", 2, 4, false}, {"c", 3, 4, false}, {"d", 4, 4, false}})
	for _, i := range []int{-1, m.VisibleLen()} {
		if data, r, ok := m.Row(i); ok || data != "" || r != (TreeRow{}) {
			t.Errorf("out-of-range row %d = %q %+v %v", i, data, r, ok)
		}
	}
}

func TestTreeCollapseAllState(t *testing.T) {
	for _, filtered := range []bool{false, true} {
		for _, mode := range []string{"selected", "unselected", "multi", "root", "no cursor", "empty"} {
			t.Run(fmt.Sprintf("filter=%v/%s", filtered, mode), func(t *testing.T) {
				m := NewTreeModel[string]()
				if mode != "empty" {
					for _, root := range []string{"a", "b"} {
						m.Insert("", root, root)
						m.Insert(root, root+"/1", root+"/1")
						m.Insert(root+"/1", root+"/2", root+"/2")
					}
				}
				m.ExpandAll()
				if filtered {
					m.Filter(func(s string) bool { return s == "a/2" || s == "b/2" })
				}
				wantCursor, wantAnchor := "b", "a"
				var wantSelected []string
				switch mode {
				case "selected", "unselected", "multi":
					m.MultiSelect(mode == "multi")
					m.Select("b/2")
					m.anchor = m.index["a/2"]
					if mode == "selected" {
						wantSelected, wantAnchor = []string{"b"}, "b"
					} else if mode == "unselected" {
						m.ClearSelection()
					} else {
						m.nodes[m.index["a/2"]].selected = true
						wantSelected = []string{"a/2", "b/2"}
					}
				case "root":
					m.Select("b")
					m.anchor = m.index["a/2"]
					wantSelected = []string{"b"}
				case "no cursor":
					m.anchor = m.index["a/2"]
					wantCursor = ""
				case "empty":
					wantCursor, wantAnchor = "", ""
				}
				m.VisibleLen()
				for range 2 {
					m.CollapseAll()
					anchor := ""
					if m.anchor >= 0 {
						anchor = m.nodes[m.anchor].id
					}
					if m.Cursor() != wantCursor || anchor != wantAnchor || !slices.Equal(m.Selected(), wantSelected) {
						t.Fatalf("cursor %q, anchor %q, selected %v; want %q, %q, %v", m.Cursor(), anchor, m.Selected(), wantCursor, wantAnchor, wantSelected)
					}
					for _, n := range m.nodes {
						if m.IsExpanded(n.id) {
							t.Errorf("%q still expanded", n.id)
						}
					}
					if mode != "empty" && (m.VisibleLen() != 2 || m.VisibleID(0) != "a" || m.VisibleID(1) != "b") {
						t.Fatal("collapse did not leave only the roots visible")
					}
				}
				if filtered {
					m.ClearFilter()
					if m.VisibleLen() != m.Len() {
						t.Fatal("filtered collapse changed the unfiltered expansion state")
					}
				}
			})
		}
	}
}

func BenchmarkTreeRowSiblings(b *testing.B) {
	for _, n := range []int{128, 2048, 8192} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			m := NewTreeModel[int]()
			for i := range n {
				m.Insert("", fmt.Sprint(i), i)
			}
			m.VisibleLen()
			for b.Loop() {
				for row := range 32 {
					m.Row(row)
				}
			}
		})
	}
}

func BenchmarkTreeCollapseAll(b *testing.B) {
	for _, n := range []int{128, 1024, 4096} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			m := NewTreeModel[int]()
			for i := range n {
				m.Insert("", fmt.Sprintf("sibling%d", i), i)
			}
			parent := ""
			for i := range n {
				id := fmt.Sprintf("deep%d", i)
				m.Insert(parent, id, i)
				parent = id
			}
			for b.Loop() {
				for i := range m.nodes {
					m.nodes[i].open = true
				}
				m.cursor, m.anchor = len(m.nodes)-1, len(m.nodes)-2
				m.CollapseAll()
			}
		})
	}
}
