package diff

import (
	"fmt"
	"strings"
	"testing"
)

// bigState is 200 files of 1300 lines with every fifth line changed.
func bigState() *State {
	files := make([]FileDiff, 200)
	for f := range files {
		var old, new strings.Builder
		for i := range 1300 {
			fmt.Fprintf(&old, "line %d\n", i)
			if i%5 == 0 {
				fmt.Fprintf(&new, "line %d changed\n", i)
			} else {
				fmt.Fprintf(&new, "line %d\n", i)
			}
		}
		files[f] = FromTexts(fmt.Sprint("f", f), old.String(), new.String())
	}
	return NewState(files)
}

// BenchmarkCollapse collapses or expands one file, then all of them one by
// one; ScrollToFile reads the rows each change leaves.
func BenchmarkCollapse(b *testing.B) {
	s := bigState()
	b.Run("one", func(b *testing.B) {
		for b.Loop() {
			s.SetCollapsed(7, !s.Collapsed(7))
			s.ScrollToFile(0)
		}
	})
	b.Run("all", func(b *testing.B) {
		collapsed := false
		for b.Loop() {
			collapsed = !collapsed
			for f := range s.Files() {
				s.SetCollapsed(f, collapsed)
			}
			s.ScrollToFile(0)
		}
	})
}
