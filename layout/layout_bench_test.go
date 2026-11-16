package layout

import (
	"strconv"
	"testing"
)

func countingLeaf(calls *int, words int) *Node {
	n := textLeaf(words)
	inner := n.Measure
	n.Measure = MeasureFunc(func(w float32, exact bool) (float32, float32) {
		*calls++
		return inner.Measure(w, exact)
	})
	return n
}

// nestedTree alternates content-sized rows and columns, each level holding
// two text leaves beside the next level.
func nestedTree(calls *int, depth int) *Node {
	var inner *Node = countingLeaf(calls, 8)
	for d := range depth {
		dir := Row
		if d%2 == 1 {
			dir = Column
		}
		inner = box(Style{Direction: dir, Gap: 4, Padding: Edges{4, 4, 4, 4}},
			countingLeaf(calls, 5), inner, countingLeaf(calls, 12))
	}
	return box(Style{Direction: Column, Width: PxDim(800), Height: PxDim(600)}, inner)
}

// shellTree is a sidebar beside a header and a column of cards, each card
// wrapped in extra content-sized boxes.
func shellTree(calls *int, cards, wrappers int) *Node {
	side := box(Style{Direction: Column, Width: PxDim(200)})
	for range 20 {
		side.Children = append(side.Children, countingLeaf(calls, 3))
	}
	list := box(Style{Direction: Column, Gap: 8, Grow: 1, ScrollY: true})
	for range cards {
		card := box(Style{Direction: Row, Gap: 8, Padding: Edges{8, 8, 8, 8}},
			box(Style{Width: PxDim(32), Height: PxDim(32)}),
			box(Style{Direction: Column, Grow: 1}, countingLeaf(calls, 6), countingLeaf(calls, 30)),
			countingLeaf(calls, 2))
		for w := range wrappers {
			dir := Column
			if w%2 == 1 {
				dir = Row
			}
			card = box(Style{Direction: dir}, card)
		}
		list.Children = append(list.Children, card)
	}
	header := box(Style{Direction: Row, Gap: 8, Padding: Edges{8, 8, 8, 8}},
		countingLeaf(calls, 4), box(Style{Grow: 1}), countingLeaf(calls, 2))
	main := box(Style{Direction: Column, Grow: 1}, header, list)
	return box(Style{Direction: Row, Width: PxDim(1200), Height: PxDim(800)}, side, main)
}

func BenchmarkComputeNested(b *testing.B) {
	for _, depth := range []int{8, 16} {
		b.Run("depth"+strconv.Itoa(depth), func(b *testing.B) {
			calls := 0
			root := nestedTree(&calls, depth)
			Compute(root, 800, 600)
			calls = 0
			for b.Loop() {
				Compute(root, 800, 600)
			}
			b.ReportMetric(float64(calls)/float64(b.N), "measures/op")
		})
	}
}

func BenchmarkComputeShell(b *testing.B) {
	for _, wrappers := range []int{0, 4} {
		b.Run("wrappers"+strconv.Itoa(wrappers), func(b *testing.B) {
			calls := 0
			root := shellTree(&calls, 200, wrappers)
			Compute(root, 1200, 800)
			calls = 0
			for b.Loop() {
				Compute(root, 1200, 800)
			}
			b.ReportMetric(float64(calls)/float64(b.N), "measures/op")
		})
	}
}
