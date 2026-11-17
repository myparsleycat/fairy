package imagecache

import (
	"runtime"
	"strconv"
	"testing"

	"github.com/myparsleycat/fairy/internal/imaging"
)

// BenchmarkImageCacheEvictAtCapacity inserts into a cache at its entry limit
// whose evicted images are all still held, so every insert evicts one entry
// and records it as revivable.
func BenchmarkImageCacheEvictAtCapacity(b *testing.B) {
	c := New(1<<30, 1)
	var held []*Image
	next := 0
	add := func() {
		img := NewImage(&imaging.Bitmap{W: 1, H: 1, Pixels: make([]byte, 4)})
		held = append(held, img)
		k := imageKey{src: "img" + strconv.Itoa(next), Bound: imaging.Bound{W: 1, H: 1}}
		next++
		c.mu.Lock()
		c.insert(&imageEntry{key: k, img: img, direct: true})
		c.mu.Unlock()
	}
	for range 3 * maxImageEntries {
		add()
	}
	b.ResetTimer()
	for b.Loop() {
		add()
	}
	runtime.KeepAlive(held)
}
