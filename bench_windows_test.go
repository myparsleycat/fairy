package fairy

import (
	"strconv"
	"testing"
)

func benchRow(i int) *El {
	s := strconv.Itoa(i)
	return Row(Text("row "+s), Text("detail").Grow(1), Text(s)).Key(s).H(20).Gap(8)
}

// BenchmarkBuildVirtualRows builds a view holding a long virtual list, as
// after any handler or Post in a window showing one.
func BenchmarkBuildVirtualRows(b *testing.B) {
	w := newListWindow(b, func() *El {
		return VirtualList(10000, 20, benchRow).WFull().HFull()
	})
	w.height = 600
	w.build()
	w.placeAll()
	b.ReportAllocs()
	for b.Loop() {
		w.dirty = true
		w.build()
	}
}

// BenchmarkScrollVirtualRows scrolls a virtual list by a view per frame, so
// each placement builds the rows of a new range.
func BenchmarkScrollVirtualRows(b *testing.B) {
	w := newListWindow(b, func() *El {
		return VirtualList(10000, 20, benchRow).WFull().HFull()
	})
	w.height = 600
	w.build()
	w.placeAll()
	b.ReportAllocs()
	dy := float32(600)
	for b.Loop() {
		if s := w.root.state; s.ScrollY+2*dy > w.root.node.ContentH {
			dy = -dy
		} else if s.ScrollY+2*dy < 0 {
			dy = -dy
		}
		w.scrollNow(w.root, 0, dy)
		w.placeAll()
	}
}

// BenchmarkMemoHit builds a view of 200 remembered subtrees whose versions
// hold.
func BenchmarkMemoHit(b *testing.B) {
	keys := make([]string, 200)
	for i := range keys {
		keys[i] = "m" + strconv.Itoa(i)
	}
	w := newListWindow(b, func() *El {
		col := Col()
		for i, k := range keys {
			col.Child(Memo(k, i, func() *El { return Text(k) }))
		}
		return col
	})
	w.build()
	w.placeAll()
	b.ReportAllocs()
	for b.Loop() {
		w.dirty = true
		w.build()
	}
}

// BenchmarkBuildVirtualRowsVersioned is BenchmarkBuildVirtualRows with rows
// whose versions hold.
func BenchmarkBuildVirtualRowsVersioned(b *testing.B) {
	w := newListWindow(b, func() *El {
		return VirtualList(10000, 20, benchRow).RowVersion(func(int) (uint64, bool) { return 0, true }).WFull().HFull()
	})
	w.height = 600
	w.build()
	w.placeAll()
	b.ReportAllocs()
	for b.Loop() {
		w.dirty = true
		w.build()
	}
}
