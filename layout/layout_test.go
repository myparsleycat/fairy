package layout

import "testing"

func box(st Style, children ...*Node) *Node {
	if st.Shrink == 0 {
		st.Shrink = 1
	}
	if st.AlignItems == AlignAuto {
		st.AlignItems = AlignStretch
	}
	return &Node{Style: st, Children: children}
}

// textLeaf wraps "words" of 10px each into lines 10px tall.
func textLeaf(words int) *Node {
	n := box(Style{})
	n.Measure = func(w float32, exact bool) (float32, float32) {
		full := float32(words * 10)
		if isNaN(w) || w >= full {
			return full, 10
		}
		perLine := max(int(w/10), 1)
		lines := (words + perLine - 1) / perLine
		return float32(perLine * 10), float32(lines * 10)
	}
	return n
}

func expect(t *testing.T, n *Node, x, y, w, h float32) {
	t.Helper()
	if n.X != x || n.Y != y || n.W != w || n.H != h {
		t.Fatalf("got (%v,%v %vx%v), want (%v,%v %vx%v)", n.X, n.Y, n.W, n.H, x, y, w, h)
	}
}

func TestRowGrowAndFixed(t *testing.T) {
	a := box(Style{Width: PxDim(50)})
	b := box(Style{Grow: 1})
	c := box(Style{Width: PxDim(30)})
	root := box(Style{Direction: Row, Padding: Edges{10, 10, 10, 10}, Gap: 5}, a, b, c)
	Compute(root, 200, 100)
	expect(t, a, 10, 10, 50, 80)
	expect(t, b, 65, 10, 90, 80)
	expect(t, c, 160, 10, 30, 80)
}

func TestShrinkKeepsExplicitSizes(t *testing.T) {
	icon := box(Style{Width: PxDim(20), Height: PxDim(20)})
	label := textLeaf(30)
	root := box(Style{Direction: Row, AlignItems: AlignStart}, icon, label)
	Compute(root, 100, 100)
	expect(t, icon, 0, 0, 20, 20)
	// The label takes the remaining 80px and wraps into 4 lines of 8 words.
	expect(t, label, 20, 0, 80, 40)
}

func TestContentSizedColumnWrapsText(t *testing.T) {
	label := textLeaf(20)
	col := box(Style{Padding: Edges{5, 5, 5, 5}}, label)
	root := box(Style{AlignItems: AlignStart}, col)
	Compute(root, 105, 300)
	// The column is bounded by the 105px parent, so the text wraps at 95px (9 words a line).
	expect(t, col, 0, 0, 100, 40)
	expect(t, label, 5, 5, 90, 30)

	// A maximum width bounds the text too, in a parent and as a fitted root.
	label = textLeaf(20)
	col = box(Style{MaxWidth: PxDim(55), Padding: Edges{0, 5, 0, 0}}, label)
	Compute(box(Style{AlignItems: AlignStart}, col), 300, 300)
	expect(t, col, 0, 0, 55, 40)
	expect(t, label, 0, 0, 50, 40)
	label = textLeaf(20)
	col = box(Style{MaxWidth: PxDim(50)}, label)
	ComputeFit(col, 800, 600)
	expect(t, col, 0, 0, 50, 40)
	a, b := textLeaf(5), textLeaf(5)
	row := box(Style{Direction: Row, MaxWidth: PxDim(60), AlignItems: AlignStart}, a, b)
	Compute(box(Style{AlignItems: AlignStart}, row), 300, 300)
	expect(t, row, 0, 0, 60, 20)
	expect(t, b, 30, 0, 30, 20)
}

func TestMaxWidthBoundsMinContentHeight(t *testing.T) {
	label := textLeaf(20)
	page := box(Style{Width: PctDim(1), MaxWidth: PxDim(50)}, label)
	a := box(Style{}, page)
	root := box(Style{}, a, box(Style{Height: PxDim(100)}))
	Compute(root, 100, 50)
	// a overflows with its sibling, but shrinks no further than the 4 lines of the 50px page.
	expect(t, page, 0, 0, 50, 40)
	expect(t, a, 0, 0, 100, 40)
}

func TestJustifyAndAlignCenter(t *testing.T) {
	a := box(Style{Width: PxDim(20), Height: PxDim(10)})
	root := box(Style{Direction: Row, Justify: JustifyCenter, AlignItems: AlignCenter}, a)
	Compute(root, 100, 50)
	expect(t, a, 40, 20, 20, 10)
}

func TestWrapBreaksLines(t *testing.T) {
	var kids []*Node
	for range 5 {
		kids = append(kids, box(Style{Width: PxDim(40), Height: PxDim(30)}))
	}
	root := box(Style{Direction: Row, Wrap: true, Gap: 10, CrossGap: 10, AlignItems: AlignStart}, kids...)
	Compute(root, 140, 200)
	expect(t, kids[2], 100, 0, 40, 30)
	expect(t, kids[3], 0, 40, 40, 30)
}

func TestScrollContainerKeepsContentSize(t *testing.T) {
	var kids []*Node
	for range 10 {
		kids = append(kids, box(Style{Height: PxDim(30)}))
	}
	list := box(Style{ScrollY: true, Grow: 1}, kids...)
	root := box(Style{}, list)
	Compute(root, 100, 120)
	expect(t, list, 0, 0, 100, 120)
	expect(t, kids[9], 0, 270, 100, 30)
	if list.ContentH != 300 {
		t.Fatalf("content height %v, want 300", list.ContentH)
	}
}

func TestMaxWidthBoundsContentHeightInScroll(t *testing.T) {
	label := textLeaf(20)
	page := box(Style{Width: PctDim(1), MaxWidth: PxDim(50)}, label)
	list := box(Style{ScrollY: true, Grow: 1, AlignItems: AlignCenter}, page)
	root := box(Style{}, list)
	Compute(root, 100, 30)
	// The page is measured at its 50px maximum, not the 100px it asks for, so the text takes 4 lines.
	expect(t, page, 25, 0, 50, 40)
	if list.ContentH != 40 {
		t.Fatalf("content height %v, want 40", list.ContentH)
	}
}

func TestAbsoluteInsets(t *testing.T) {
	badge := box(Style{Absolute: true, Width: PxDim(10), Height: PxDim(10), Inset: Inset{Top: 2, Right: 2, Bottom: nan, Left: nan}})
	root := box(Style{}, badge)
	Compute(root, 100, 50)
	expect(t, badge, 88, 2, 10, 10)
}

func TestMinMaxClamp(t *testing.T) {
	a := box(Style{Grow: 1, MaxWidth: PxDim(60)})
	b := box(Style{Grow: 1})
	root := box(Style{Direction: Row}, a, b)
	Compute(root, 200, 10)
	expect(t, a, 0, 0, 60, 10)
	expect(t, b, 60, 0, 140, 10)
}

func TestGrowingScrollAreaDoesNotSquishHeader(t *testing.T) {
	header := textLeaf(3) // 30x10
	var rows []*Node
	for range 100 {
		rows = append(rows, box(Style{Height: PxDim(30)}))
	}
	list := box(Style{ScrollY: true, Grow: 1}, rows...)
	page := box(Style{}, header, list)
	Compute(page, 200, 100)
	expect(t, header, 0, 0, 200, 10)
	expect(t, list, 0, 10, 200, 90)
}

func TestColumnOverflowKeepsContentHeights(t *testing.T) {
	a, b := textLeaf(1), textLeaf(1)
	inner := box(Style{Padding: Edges{5, 5, 5, 5}}, textLeaf(1)) // 20 tall
	root := box(Style{}, a, inner, b)
	Compute(root, 100, 25)
	expect(t, a, 0, 0, 100, 10)
	expect(t, inner, 0, 10, 100, 20)
	expect(t, b, 0, 30, 100, 10)
}

// In an overflowing column, a row's minimum height must measure wrapped text at
// the width left beside its siblings, or the text overlaps the row padding.
func TestOverflowingColumnRowWrapsBesideSibling(t *testing.T) {
	label := textLeaf(20)
	label.Style.Grow = 1
	control := box(Style{Width: PxDim(40), Height: PxDim(10)})
	row := box(Style{Direction: Row, Padding: Edges{12, 0, 12, 0}, Gap: 10, AlignItems: AlignStart}, label, control)
	root := box(Style{}, row, box(Style{Height: PxDim(100)}))
	Compute(root, 100, 50)
	// The label gets 100-40-10 = 50px, so 20 words wrap into 4 lines.
	expect(t, row, 0, 0, 100, 64)
	expect(t, label, 0, 12, 50, 40)
}

// A percentage height is definite in a fixed-height column, so content taller
// than it, such as a portrait image filling a square, does not stretch the item.
func TestPercentHeightCapsContentMinimum(t *testing.T) {
	portrait := box(Style{Width: PctDim(1), Height: PctDim(1)})
	portrait.Measure = func(w float32, exact bool) (float32, float32) {
		if isNaN(w) {
			w = 20
		}
		return w, w * 2
	}
	frame := box(Style{Width: PxDim(40), Height: PxDim(40), AlignItems: AlignCenter}, portrait)
	Compute(box(Style{AlignItems: AlignStart}, frame), 100, 100)
	expect(t, portrait, 0, 0, 40, 40)
}

func TestComputeAtWidthHonorsRootHeight(t *testing.T) {
	content := box(Style{Padding: Edges{Top: 5, Bottom: 5}}, textLeaf(25), box(Style{Height: PxDim(20)}))
	ComputeAtWidth(content, 100)
	expect(t, content, 0, 0, 100, 60)

	fixed := box(Style{Height: PxDim(70)}, textLeaf(5))
	ComputeAtWidth(fixed, 100)
	expect(t, fixed, 0, 0, 100, 70)

	capped := box(Style{MaxHeight: PxDim(25)}, textLeaf(50))
	ComputeAtWidth(capped, 100)
	expect(t, capped, 0, 0, 100, 25)
}

func TestComputeFitHonorsRootWidth(t *testing.T) {
	panel := box(Style{Width: PxDim(100)}, textLeaf(25))
	ComputeFit(panel, 800, 600)
	expect(t, panel, 0, 0, 100, 30)

	tall := box(Style{Height: PxDim(40)}, textLeaf(5))
	ComputeFit(tall, 800, 600)
	expect(t, tall, 0, 0, 50, 40)

	narrow := box(Style{Width: PxDim(300)}, textLeaf(25))
	ComputeFit(narrow, 120, 600)
	expect(t, narrow, 0, 0, 120, 30)
}
