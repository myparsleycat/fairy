//go:build windows

package text

import (
	"slices"
	"sync"
	"testing"

	"github.com/myparsleycat/fairy/internal/render"
)

// Systems on different goroutines, as RenderImage uses off the UI thread,
// must each receive only their own layout's glyph runs.
func TestConcurrentDrawLayout(t *testing.T) {
	texts := []string{"Hello world", "ABCDEFGHIJKLMNOP", "0123456789", "Zz Yy Xx Ww"}
	var wg sync.WaitGroup
	for _, str := range texts {
		s, err := NewSystem(&render.Atlas{})
		if err != nil {
			t.Fatal(err)
		}
		defer s.Release()
		l := s.Layout(str, Style{Family: "Segoe UI", Size: 14}, Unbounded)
		want := glyphsOf(l)
		wg.Go(func() {
			for range 200 {
				if got := glyphsOf(l); !slices.Equal(got, want) {
					t.Errorf("%q drew glyphs %v, want %v", str, got, want)
					return
				}
			}
		})
	}
	wg.Wait()
}
