package fairy

import (
	"image"
	"slices"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/myparsleycat/fairy/internal/imaging"
)

// scheduleTestLoad starts a load of key whose decoder reports its start on
// order and then waits for release, if any.
func scheduleTestLoad(t *testing.T, c *ImageCache, key string, order chan<- string, release <-chan struct{}) {
	t.Helper()
	data := cacheTestImage(t, func() (image.Image, error) {
		order <- key
		if release != nil {
			<-release
		}
		return image.NewNRGBA(image.Rect(0, 0, 1, 1)), nil
	})
	c.Load(key, 1, 1, func() ([]byte, error) { return data, nil })
	synctest.Wait()
}

func TestImageCacheDecodesLatestRequestFirst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Four read slots let every source queue while hold1 reserves all
		// working memory. Waiting for memory must not occupy a decode slot.
		c := NewImageCache(1<<20, 2)
		order := make(chan string, 5)
		hold1, hold2, holdA := make(chan struct{}), make(chan struct{}), make(chan struct{})
		scheduleTestLoad(t, c, "hold1", order, hold1)
		scheduleTestLoad(t, c, "hold2", order, hold2)
		scheduleTestLoad(t, c, "a", order, holdA)
		scheduleTestLoad(t, c, "b", order, nil)
		scheduleTestLoad(t, c, "c", order, nil)
		c.Load("a", 1, 1, nil) // drawn again while waiting
		// Registered decoders reserve all working memory. Even hold2 has not
		// started yet, so the re-requested a and newer c/b run before it.
		for _, release := range []chan struct{}{hold1, hold2, holdA} {
			close(release)
			synctest.Wait()
		}
		got := []string{<-order, <-order, <-order, <-order, <-order}
		if want := []string{"hold1", "a", "c", "b", "hold2"}; !slices.Equal(got, want) {
			t.Fatalf("decode order %v, want %v", got, want)
		}
	})
}

func TestImageCacheDropsLoadsNobodyRequests(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := NewImageCache(1<<20, 1)
		order := make(chan string, 4)
		hold := make(chan struct{})
		scheduleTestLoad(t, c, "hold", order, hold)
		<-order

		var reads atomic.Int32
		data := pngBytes(t, 2, 2)
		read := func() ([]byte, error) { reads.Add(1); return data, nil }
		c.Load("stale", 1, 1, read)
		c.mu.Lock()
		_, captured, _ := c.request(imageKey{src: "captured", Bound: imaging.Bound{W: 1, H: 1}}, read)
		c.mu.Unlock()
		done := make(chan *imageLoad, 1)
		captured.watch(done)
		synctest.Wait()

		time.Sleep(imageLoadExpiry + time.Second)
		close(hold)
		synctest.Wait()
		if p := <-done; p.img == nil {
			t.Fatalf("a load a capture waits for was dropped: %v", p.err)
		}
		if c.Load("stale", 1, 1, read) != nil || c.Err("stale", 1, 1) != nil {
			t.Fatal("a load nobody requested again was kept")
		}
		synctest.Wait()
		if reads.Load() != 3 || c.Load("stale", 1, 1, read) == nil {
			t.Fatalf("requesting a dropped image again did not load it (%d reads)", reads.Load())
		}
	})
}

func TestImageCacheDecodesWhileASourceReads(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := NewImageCache(1<<20, 1)
		slow := make(chan struct{})
		data := pngBytes(t, 2, 2)
		c.Load("slow", 1, 1, func() ([]byte, error) { <-slow; return data, nil })
		c.Load("fast", 1, 1, func() ([]byte, error) { return data, nil })
		synctest.Wait()
		if c.Load("fast", 1, 1, nil) == nil {
			t.Fatal("a slow read held up decoding of a source already read")
		}
		close(slow)
		synctest.Wait()
		if c.Load("slow", 1, 1, nil) == nil {
			t.Fatal("the slow source did not load")
		}
	})
}

func TestImageCacheExpiresMemoryWaiters(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := NewImageCache(1<<20, 2)
		order := make(chan string, 3)
		a, b := make(chan struct{}), make(chan struct{})
		defer close(b)
		scheduleTestLoad(t, c, "A", order, a)
		scheduleTestLoad(t, c, "B", order, b)
		time.Sleep(imageLoadExpiry + time.Second)
		scheduleTestLoad(t, c, "C", order, nil)
		close(a)
		synctest.Wait()
		if first, next := <-order, <-order; first != "A" || next != "C" {
			t.Fatalf("decode order %s, %s; want A followed by fresh C", first, next)
		}
	})
}

func TestImageCacheDoesNotFinishForgottenReadTwice(t *testing.T) {
	c := NewImageCache(1024, 1)
	k := imageKey{src: "forgotten", Bound: imaging.Bound{W: 1, H: 1}}
	p := &imageLoad{key: k, done: make(chan struct{}), reqAt: time.Now().Add(-3 * time.Second)}
	c.loading[k] = p
	c.reads.waiting[p] = make(chan struct{}, 1)
	c.Forget(k.src)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dispatch(&c.reads)
	if len(c.reads.waiting) != 0 || c.reads.used != 0 {
		t.Fatal("forgotten read retained a slot")
	}
}

func TestImageCacheDoesNotExpireSlowRead(t *testing.T) {
	data := pngBytes(t, 2, 2)
	synctest.Test(t, func(t *testing.T) {
		c := NewImageCache(1024, 1)
		c.SetNativeDecoders(false)
		read := func() ([]byte, error) { time.Sleep(imageLoadExpiry + time.Second); return data, nil }
		c.mu.Lock()
		_, pending, _ := c.request(imageKey{src: "slow", Bound: imaging.Bound{W: 2, H: 2}}, read)
		c.mu.Unlock()
		<-pending.done
		if pending.img == nil || pending.err != nil {
			t.Fatal("source was expired immediately after its slow read")
		}
	})
}
