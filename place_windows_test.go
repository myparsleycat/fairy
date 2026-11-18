package fairy

import (
	"strconv"
	"testing"
)

// A view that sizes one element by another's OnLayout report shows the
// measured size in the same frame, without waiting for an unrelated rebuild.
func TestOnLayoutChangeRebuildsInSameFrame(t *testing.T) {
	var measured float32
	var follower *El
	w := newListWindow(t, func() *El {
		follower = Box().W(10).H(measured)
		return Col(Box().Size(30, 24).OnLayout(func(_, h float32) bool { measured = h; return true }), follower)
	})
	frame := func() {
		for i := 0; i < 3; i++ {
			if w.dirty {
				w.build()
			}
			w.placeAll()
			if !w.dirty {
				break
			}
		}
	}

	frame()
	if follower.node.H != 24 {
		t.Fatalf("follower height after the first frame = %v, want the measured 24", follower.node.H)
	}
	frame()
	if w.dirty {
		t.Fatal("an unchanged OnLayout size asked for another build")
	}
}

// ScrollStep shows the next child below a covered bottom edge, and
// OnScrollEdges reports the edges when first placed and when they change. A
// pinned Absolute child stays where its insets put it and over the scrolled
// content for the pointer; an unpinned one scrolls.
func TestScrollStepAndPinToView(t *testing.T) {
	var edges [][2]bool
	w := newPointerWindow(t, func() *El {
		list := Col().Size(200, 100).ScrollY().ClipInset(0, 0, 20, 0).OnScrollEdges(func(top, bottom bool) {
			edges = append(edges, [2]bool{top, bottom})
		})
		for range 10 {
			list.Child(Box().Size(100, 28))
		}
		return list.Child(
			Box().Key("pinned").H(20).Left(0).Right(0).Bottom(0).Absolute().PinToView(),
			Box().Key("loose").H(20).Left(0).Right(0).Top(0).Absolute(),
		)
	})
	if w.ScrollStep("gone", false) {
		t.Fatal("ScrollStep reported an element with a key that is not in the view")
	}
	if !w.ScrollStep("pinned", false) {
		t.Fatal("ScrollStep reported no element with key \"pinned\"")
	}
	w.placeAll()
	if w.dirty {
		w.build()
	}
	w.placeAll()
	if y := w.root.state.ScrollY; y != 4 {
		t.Fatalf("scrollY = %v, want 4: the third row's bottom above the covered 20 pixels", y)
	}
	if want := [][2]bool{{true, false}, {false, false}}; len(edges) != 2 || edges[0] != want[0] || edges[1] != want[1] {
		t.Fatalf("OnScrollEdges got %v, want %v", edges, want)
	}
	pinned, loose := w.findKey("pinned"), w.findKey("loose")
	if pinned.absY != 80 {
		t.Fatalf("pinned child at y %v, want 80", pinned.absY)
	}
	if loose.absY != -4 {
		t.Fatalf("unpinned child at y %v, want -4", loose.absY)
	}
	if hit := w.hitTest(10, 90); hit != pinned {
		t.Fatalf("hit %v at the pinned child", hit)
	}
}

// ScrollStep passes over a separator, so a step does not stop on a line a
// pixel tall.
func TestScrollStepSkipsSeparator(t *testing.T) {
	w := newPointerWindow(t, func() *El {
		list := Col().Size(200, 100).ScrollY().ClipInset(0, 0, 20, 0)
		list.Child(Box().Size(100, 40), Box().Size(100, 41), Box().Size(100, 1).Role(RoleSeparator))
		for range 5 {
			list.Child(Box().Size(100, 28))
		}
		return list.Child(Box().Key("pinned").H(20).Left(0).Right(0).Bottom(0).Absolute().PinToView())
	})
	w.ScrollStep("pinned", false)
	w.placeAll()
	if y := w.root.state.ScrollY; y != 30 {
		t.Fatalf("scrollY = %v, want 30: the row after the separator above the covered 20 pixels", y)
	}
}

// ClipInset keeps scrolled children out of a strip along the edge, for
// painting, the pointer and scrolling into view, but not a PinToView child in
// it. A Hidden scroll bar is not placed, so it takes no presses.
func TestClipInsetAndHiddenScrollBar(t *testing.T) {
	w := newPointerWindow(t, func() *El {
		list := Col().Size(200, 100).ScrollY().ClipInset(0, 0, 20, 0).ScrollBarStyle(ScrollBarStyle{Hidden: true})
		for i := range 10 {
			list.Child(Box().Key("row"+strconv.Itoa(i)).Size(100, 28))
		}
		return list.Child(Box().Key("pinned").Size(20, 20).Right(0).Bottom(0).Absolute().PinToView())
	})
	row, pinned := w.findKey("row2"), w.findKey("pinned")
	if got := row.clipR.Y + row.clipR.H; got != 80 {
		t.Fatalf("row clip ends at y %v, want 80", got)
	}
	if got := pinned.clipR.Y + pinned.clipR.H; got != 100 {
		t.Fatalf("pinned clip ends at y %v, want 100", got)
	}
	if hit := w.hitTest(10, 82); hit != w.root {
		t.Fatalf("hit %v in the strip, want the list", hit)
	}
	if hit := w.hitTest(190, 90); hit != pinned {
		t.Fatalf("hit %v at the pinned child", hit)
	}
	if len(w.bars) != 0 {
		t.Fatalf("%d scroll bars placed, want none", len(w.bars))
	}
	w.scrollIntoView(w.findKey("row3"))
	if y := w.root.state.ScrollY; y != 32 {
		t.Fatalf("scrollY = %v after scrolling row 3 into view, want 32: its bottom above the strip", y)
	}
}
