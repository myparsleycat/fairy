package fairy

import (
	"image"
	"math"
	"runtime"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/myparsleycat/fairy/internal/imaging"
)

func TestBlurredKeepsUniformImageAndEdges(t *testing.T) {
	t.Run("zero value", func(t *testing.T) {
		var img Image
		if w, h := img.Size(); w != 0 || h != 0 || img.Frames() != 1 || img.Duration() != 0 {
			t.Fatal("zero-value image has nonzero size, animation frames or duration")
		}
		if img.Blurred(0) != nil || img.Blurred(2.5) != nil {
			t.Fatal("zero-value image returned blurred pixels")
		}
	})
	const w, h = 7, 5
	pixels := make([]byte, w*h*4)
	for p := range w * h {
		copy(pixels[p*4:], []byte{200, 40, 90, 255})
	}
	img := &Image{data: imaging.Bitmap{W: w, H: h, Pixels: pixels}}

	blurred := img.Blurred(2.5)
	for p := range w * h {
		if got := blurred.data.Pixels[p*4 : p*4+4]; string(got) != string(pixels[p*4:p*4+4]) {
			t.Fatalf("pixel %d = %v, want %v", p, got, pixels[p*4:p*4+4])
		}
	}
	if img.Blurred(2.5) != blurred {
		t.Fatal("Blurred did not reuse the result for the same sigma")
	}
}

func TestImageCacheBlurredSharesSourceAndBudget(t *testing.T) {
	data := pngBytes(t, 96, 48)
	synctest.Test(t, func(t *testing.T) {
		c := NewImageCache(4096, 2)
		c.SetNativeDecoders(false)
		hold := make(chan struct{})
		var reads atomic.Int32
		read := func() ([]byte, error) { reads.Add(1); <-hold; return data, nil }
		if c.Blurred("photo", 24, 24, 1.5, read) != nil {
			t.Fatal("pending blur returned an image")
		}
		synctest.Wait()
		if c.Load("photo", 24, 24, read) != nil || reads.Load() != 1 {
			t.Fatal("blur did not share its pending source")
		}
		c.mu.Lock()
		work, slots := c.work, c.decodes.used
		c.mu.Unlock()
		if work != 0 || slots != 0 {
			t.Fatal("waiting for source consumed decode resources")
		}
		close(hold)
		synctest.Wait()
		img := c.Blurred("photo", 24, 24, 1.5, read)
		if img == nil || img.data.W != 24 || img.data.H != 12 || reads.Load() != 1 {
			t.Fatalf("blur size or shared read: image=%v, reads=%d, err=%v", img, reads.Load(), c.BlurredErr("photo", 24, 24, 1.5))
		}
		if c.Blurred("photo", 24, 24, 1.5, read) != img {
			t.Fatal("identical blur did not reuse its result")
		}
		if c.bytes != 2*24*12*4 || c.bytes > c.maxBytes {
			t.Fatalf("source and blur charged %d bytes", c.bytes)
		}
		if c.Blurred("photo", 24, 24, 0, read) != c.Load("photo", 24, 24, read) {
			t.Fatal("nonpositive sigma did not use the normal load")
		}
		c.Forget("photo")
		if len(c.entries) != 0 || c.bytes != 0 || c.Blurred("photo", 24, 24, 1.5, read) != nil {
			t.Fatal("Forget did not remove the blur and source")
		}
		synctest.Wait()
		if reads.Load() != 2 || c.Blurred("photo", 24, 24, 1.5, read) == nil {
			t.Fatal("blur did not reload after Forget")
		}
	})
}

func TestImageCacheBlurredValidationAndForgetWhileWaiting(t *testing.T) {
	for _, tc := range []struct {
		name  string
		w, h  int
		sigma float32
	}{
		{"unbounded", 0, 0, 1}, {"negative size", -1, 8, 1},
		{"NaN", 8, 8, float32(math.NaN())}, {"infinity", 8, 8, float32(math.Inf(1))},
		{"kernel overflow", 8, 8, math.MaxFloat32},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewImageCache(1024, 1)
			if c.Blurred("x", tc.w, tc.h, tc.sigma, nil) != nil || c.BlurredErr("x", tc.w, tc.h, tc.sigma) == nil {
				t.Fatal("invalid blur request was not rejected")
			}
			if c.Err("x", 8, 8) != nil {
				t.Fatal("invalid blur contaminated its source error")
			}
		})
	}
	data := pngBytes(t, 8, 8)
	synctest.Test(t, func(t *testing.T) {
		c := NewImageCache(1024, 1)
		c.SetNativeDecoders(false)
		hold := make(chan struct{})
		c.Blurred("x", 8, 8, 1, func() ([]byte, error) { <-hold; return data, nil })
		synctest.Wait()
		c.Forget("x")
		synctest.Wait()
		close(hold)
		synctest.Wait()
		if len(c.loading) != 0 || len(c.entries) != 0 || c.work != 0 || c.decodes.used != 0 || c.reads.used != 0 {
			t.Fatal("invalidated blur retained work or published its result")
		}
	})
}

func TestBlurredBoundsWeakMemo(t *testing.T) {
	img := &Image{data: imaging.Bitmap{W: 2, H: 2, Pixels: make([]byte, 16)}}
	var held []*Image
	for n := range 32 {
		held = append(held, img.Blurred(float32(n+1)))
	}
	if len(img.blurs.m) > 16 || img.Blurred(32) != held[31] {
		t.Fatal("weak memo was not bounded or lost a live recent result")
	}
	runtime.KeepAlive(held)
}

func TestImageCacheBlurredPropagatesPriorityAndExpiry(t *testing.T) {
	for _, expire := range []bool{false, true} {
		name := "priority"
		if expire {
			name = "expiry"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c := NewImageCache(1024, 2)
				order := make(chan string, 3)
				hold := make(chan struct{})
				scheduleTestLoad(t, c, "A", order, hold)
				data := cacheTestImage(t, func() (image.Image, error) { order <- "B"; return image.NewRGBA(image.Rect(0, 0, 1, 1)), nil })
				read := func() ([]byte, error) { return data, nil }
				c.Blurred("B", 1, 1, 1, read)
				synctest.Wait()
				if expire {
					time.Sleep(imageLoadExpiry + time.Second)
				}
				scheduleTestLoad(t, c, "C", order, nil)
				if !expire {
					c.Blurred("B", 1, 1, 1, read)
				}
				close(hold)
				synctest.Wait()
				if got := <-order; got != "A" {
					t.Fatal(got)
				}
				want := "B"
				if expire {
					want = "C"
				}
				if got := <-order; got != want {
					t.Fatalf("next decode %s, want %s", got, want)
				}
				if expire {
					select {
					case got := <-order:
						t.Fatalf("expired blur decoded %s", got)
					default:
					}
				} else if got := <-order; got != "C" {
					t.Fatal(got)
				}
			})
		})
	}
}
