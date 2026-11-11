package fairy

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/gif"
	"image/png"
	"io"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/myparsleycat/fairy/internal/imaging"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// oversizedPNG is a valid all-black PNG just over maxImagePixels, built
// without holding its pixels.
func oversizedPNG(t *testing.T) []byte {
	t.Helper()
	const w, h = 8193, 8193
	var idat bytes.Buffer
	z := zlib.NewWriter(&idat)
	row := make([]byte, 1+(w+7)/8) // filter byte, then one bit per pixel
	for range h {
		z.Write(row)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	ihdr := binary.BigEndian.AppendUint32(nil, w)
	ihdr = binary.BigEndian.AppendUint32(ihdr, h)
	ihdr = append(ihdr, 1, 0, 0, 0, 0) // 1-bit grayscale
	out := []byte("\x89PNG\r\n\x1a\n")
	for _, c := range []struct {
		typ  string
		data []byte
	}{{"IHDR", ihdr}, {"IDAT", idat.Bytes()}, {"IEND", nil}} {
		out = binary.BigEndian.AppendUint32(out, uint32(len(c.data)))
		start := len(out)
		out = append(append(out, c.typ...), c.data...)
		out = binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(out[start:]))
	}
	return out
}

func TestDecodeImageRejectsOversizedImage(t *testing.T) {
	if img, err := DecodeImage(oversizedPNG(t)); err == nil {
		w, h := img.Size()
		t.Fatalf("decoded a %dx%d image, want an error over %d pixels", w, h, imaging.MaxImagePixels)
	}
}

func waitImage(t *testing.T, c *ImageCache, src string, maxW, maxH int, data []byte) *Image {
	t.Helper()
	read := func() ([]byte, error) { return data, nil }
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if img := c.Load(src, maxW, maxH, read); img != nil {
			return img
		}
		if err := c.Err(src, maxW, maxH); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("%s did not load", src)
	return nil
}

func TestImageCacheEvictsOverBudget(t *testing.T) {
	data := pngBytes(t, 10, 10) // 400 bytes decoded
	c := NewImageCache(1000, 1)
	waitImage(t, c, "a", 100, 100, data)
	waitImage(t, c, "b", 100, 100, data)
	waitImage(t, c, "a", 100, 100, data) // a is now the most recently used
	waitImage(t, c, "c", 100, 100, data)

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.bytes > c.maxBytes {
		t.Fatalf("cache holds %d bytes, budget %d", c.bytes, c.maxBytes)
	}
	for _, src := range []string{"a", "c"} {
		if _, ok := c.entries[imageKey{src: src, Bound: imaging.Bound{W: 100, H: 100}}]; !ok {
			t.Errorf("%s was evicted", src)
		}
	}
	if _, ok := c.entries[imageKey{src: "b", Bound: imaging.Bound{W: 100, H: 100}}]; ok {
		t.Error("least recently used image b was kept")
	}
}

func TestImageCacheRejectsImageLargerThanBudget(t *testing.T) {
	small, large := pngBytes(t, 4, 4), pngBytes(t, 10, 10)
	synctest.Test(t, func(t *testing.T) {
		c := NewImageCache(100, 1)
		readSmall := func() ([]byte, error) { return small, nil }
		readLarge := func() ([]byte, error) { return large, nil }
		c.Load("small", 100, 100, readSmall)
		synctest.Wait()
		c.Load("large", 100, 100, readLarge)
		synctest.Wait()

		if err := c.Err("large", 100, 100); err == nil {
			t.Error("oversized image load did not report an error")
		}
		if c.Load("large", 100, 100, readLarge) != nil {
			t.Error("oversized image was cached")
		}
		if c.Load("small", 100, 100, readSmall) == nil {
			t.Error("oversized image evicted a cached image")
		}
	})
}

func TestImageCacheOversizedVariantKeepsLazySource(t *testing.T) {
	data := pngBytes(t, 32, 32)
	read := func() ([]byte, error) { return data, nil }
	synctest.Test(t, func(t *testing.T) {
		c := NewImageCache(1024, 1)
		c.Load("photo", 0, 0, read)
		synctest.Wait()
		img := c.Load("photo", 0, 0, read)
		if img == nil {
			t.Fatal("metadata did not load")
		}
		small := imaging.Bound{W: 8, H: 8}
		img.lazy.variant(32, 32, small)
		synctest.Wait()
		prior := img.lazy.variant(32, 32, small)
		if prior == nil {
			t.Fatal("small variant did not load")
		}
		large := imaging.Bound{W: 32, H: 32}
		img.lazy.variant(32, 32, large)
		synctest.Wait()
		if c.Err("photo", 32, 32) == nil {
			t.Fatal("oversized variant did not report its error")
		}
		if c.Load("photo", 0, 0, read) != img || c.Err("photo", 0, 0) != nil {
			t.Fatalf("oversized variant invalidated the lazy source: %v", c.Err("photo", 0, 0))
		}
		if got := img.lazy.variant(32, 32, large); got != prior {
			t.Fatal("oversized variant did not preserve the last drawn result")
		}
	})
}

func TestImageCacheReusesDirectStillWithoutRereading(t *testing.T) {
	data := pngBytes(t, 80, 40)
	synctest.Test(t, func(t *testing.T) {
		c := NewImageCache(1<<20, 1)
		var reads atomic.Int32
		read := func() ([]byte, error) {
			reads.Add(1)
			return data, nil
		}
		load := func(w, h int) *Image {
			t.Helper()
			c.Load("still", w, h, read)
			synctest.Wait()
			img := c.Load("still", w, h, read)
			if img == nil {
				t.Fatalf("did not load %dx%d: %v", w, h, c.Err("still", w, h))
			}
			return img
		}
		load(40, 40)
		load(80, 80)
		img := load(20, 20)
		if w, h := img.Size(); w != 20 || h != 10 || reads.Load() != 2 {
			t.Fatalf("thumbnail = %dx%d, reads = %d; want 20x10 with no new read", w, h, reads.Load())
		}
		// Leave only a derived thumbnail: it cannot supply either a smaller
		// decode (which would resample twice) or a larger one.
		c.mu.Lock()
		c.remove(c.entries[imageKey{src: "still", Bound: imaging.Bound{W: 80, H: 80}}])
		c.remove(c.entries[imageKey{src: "still", Bound: imaging.Bound{W: 40, H: 40}}])
		c.mu.Unlock()
		load(10, 10)
		load(30, 30)
		if n := reads.Load(); n != 4 {
			t.Fatalf("reads without a sufficient direct result = %d, want 4", n)
		}
		c.Forget("still")
		load(6, 6)
		if n := reads.Load(); n != 5 {
			t.Fatalf("reads after Forget = %d, want 5", n)
		}
	})
}

func TestImageCacheKeepsAnimationWithinBudget(t *testing.T) {
	const side = 8
	bounds := image.Rect(0, 0, 2*side, 2*side)
	var buf bytes.Buffer
	err := gif.EncodeAll(&buf, &gif.GIF{
		Image: []*image.Paletted{solidPaletted(bounds, 1), solidPaletted(bounds, 2)},
		Delay: []int{10, 10},
	})
	if err != nil {
		t.Fatal(err)
	}

	synctest.Test(t, func(t *testing.T) {
		c := NewImageCache(4*side*side*4, 1)
		read := func() ([]byte, error) { return buf.Bytes(), nil }
		c.Sized("gif", 2*side, 2*side, FitContain, read)
		synctest.Wait()
		if img := c.Sized("gif", 2*side, 2*side, FitContain, read); img == nil || img.Frames() != 1 {
			t.Fatal("large GIF did not fall back to its first frame")
		}
		c.Sized("gif", side, side, FitContain, read)
		synctest.Wait()
		img := c.Sized("gif", side, side, FitContain, read)
		if img == nil {
			t.Fatalf("GIF did not load: %v", c.SizedErr("gif", side, side, FitContain))
		}
		if got := img.Frames(); got != 2 {
			t.Fatalf("GIF has %d frames, want 2 within the cache budget", got)
		}
	})
}

func TestImageCacheScalesDownToFit(t *testing.T) {
	c := NewImageCache(1<<20, 1)
	img := waitImage(t, c, "wide", 50, 50, pngBytes(t, 200, 100))
	if w, h := img.Size(); w != 50 || h != 25 {
		t.Fatalf("size %dx%d, want 50x25", w, h)
	}
	img = waitImage(t, c, "small", 50, 50, pngBytes(t, 20, 10))
	if w, h := img.Size(); w != 20 || h != 10 {
		t.Fatalf("small image resized to %dx%d", w, h)
	}
}

func TestImageCacheSizedCoversBox(t *testing.T) {
	c := NewImageCache(1<<20, 1)
	for _, tc := range []struct {
		name         string
		w, h         int
		fit          Fit
		wantW, wantH int
	}{
		// A 50x50 box rounds up to 64x64 device pixels.
		{"cover", 400, 100, FitCover, 256, 64},
		{"contain", 400, 100, FitContain, 64, 16},
		{"panorama", 1000, 10, FitCover, 256, 3},
	} {
		data := pngBytes(t, tc.w, tc.h)
		read := func() ([]byte, error) { return data, nil }
		var img *Image
		for deadline := time.Now().Add(5 * time.Second); img == nil && time.Now().Before(deadline); {
			if img = c.Sized(tc.name, 50, 50, tc.fit, read); img == nil {
				if err := c.SizedErr(tc.name, 50, 50, tc.fit); err != nil {
					t.Fatal(err)
				}
				time.Sleep(time.Millisecond)
			}
		}
		if img == nil {
			t.Fatalf("%s did not load", tc.name)
		}
		if w, h := img.Size(); w != tc.wantW || h != tc.wantH {
			t.Errorf("%s: size %dx%d, want %dx%d", tc.name, w, h, tc.wantW, tc.wantH)
		}
	}
}

func TestImageCacheForgetDuringLoad(t *testing.T) {
	data := pngBytes(t, 4, 3)
	synctest.Test(t, func(t *testing.T) {
		c := NewImageCache(1<<20, 2)
		old, next, started := make(chan struct{}), make(chan struct{}), make(chan struct{})
		c.Load("image", 0, 0, func() ([]byte, error) {
			<-old
			return nil, errors.New("old source failed")
		})
		synctest.Wait()

		c.Forget("image")
		read := func() ([]byte, error) {
			close(started)
			<-next
			return data, nil
		}
		c.Load("image", 0, 0, read)
		synctest.Wait()
		select {
		case <-started:
		default:
			t.Error("Load after Forget did not start a new read")
		}
		close(old)
		synctest.Wait()
		if err := c.Err("image", 0, 0); err != nil {
			t.Errorf("Forgotten read populated the cache: %v", err)
		}
		close(next)
		synctest.Wait()
		img := c.Load("image", 0, 0, read)
		if img == nil {
			t.Fatal("New read did not populate the cache")
		}
		if w, h := img.Size(); w != 4 || h != 3 {
			t.Errorf("size %dx%d, want 4x3", w, h)
		}
	})
}

var (
	cacheTestFormat  sync.Once
	cacheTestDecodes sync.Map
	cacheTestSeq     atomic.Uint64
)

func cacheTestImage(t *testing.T, decode func() (image.Image, error)) []byte {
	const magic = "fairy-cache-test:"
	cacheTestFormat.Do(func() {
		image.RegisterFormat("fairy-cache-test", magic, func(r io.Reader) (image.Image, error) {
			data, err := io.ReadAll(r)
			if err != nil {
				return nil, err
			}
			f, ok := cacheTestDecodes.Load(string(data))
			if !ok {
				return nil, errors.New("missing test decoder")
			}
			return f.(func() (image.Image, error))()
		}, func(io.Reader) (image.Config, error) {
			return image.Config{Width: 1, Height: 1}, nil
		})
	})
	data := binary.LittleEndian.AppendUint64([]byte(magic), cacheTestSeq.Add(1))
	key := string(data)
	cacheTestDecodes.Store(key, decode)
	t.Cleanup(func() { cacheTestDecodes.Delete(key) })
	return data
}

func TestImageCacheSerializesUnknownDecodersAndReleasesOnFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := NewImageCache(1024, 5)
		hold := make(chan struct{})
		started := make(chan string, 5)
		start := func(key string, run func() error) {
			t.Helper()
			data := cacheTestImage(t, func() (image.Image, error) {
				started <- key
				if err := run(); err != nil {
					return nil, err
				}
				return image.NewNRGBA(image.Rect(0, 0, 1, 1)), nil
			})
			c.Load(key, 1, 1, func() ([]byte, error) { return data, nil })
			synctest.Wait()
		}
		start("hold", func() error { <-hold; return nil })
		if got := <-started; got != "hold" {
			t.Fatalf("first decode = %q", got)
		}
		start("forgotten", func() error { return nil })
		start("error", func() error { return errors.New("decode failed") })
		start("panic", func() error { panic("decoder panicked") })
		start("last", func() error { return nil })
		c.Forget("forgotten")
		synctest.Wait()
		select {
		case got := <-started:
			close(hold)
			t.Fatalf("%q overlapped an exclusive decode", got)
		default:
		}
		close(hold)
		synctest.Wait()
		for _, want := range []string{"last", "panic", "error"} {
			select {
			case got := <-started:
				if got != want {
					t.Fatalf("decode = %q, want %q", got, want)
				}
			default:
				t.Fatalf("%q did not start", want)
			}
		}
		if c.Err("error", 1, 1) == nil || c.Err("panic", 1, 1) == nil {
			t.Fatal("decoder failure was not recorded")
		}
		if c.Load("last", 1, 1, nil) == nil {
			t.Fatal("last image was not cached")
		}
	})
}

func TestImageCacheLazyImageDecodesRequestedSizes(t *testing.T) {
	data := pngBytes(t, 400, 200)
	synctest.Test(t, func(t *testing.T) {
		c := NewImageCache(1<<20, 1)
		read := func() ([]byte, error) { return data, nil }
		c.Load("a", 0, 0, read)
		synctest.Wait()
		img := c.Load("a", 0, 0, read)
		if img == nil {
			t.Fatalf("lazy image did not load: %v", c.Err("a", 0, 0))
		}
		if w, h := img.Size(); w != 400 || h != 200 {
			t.Fatalf("size %dx%d, want the natural 400x200", w, h)
		}
		cached := func() int64 {
			c.mu.Lock()
			defer c.mu.Unlock()
			return c.bytes
		}
		if n := cached(); n != 0 {
			t.Fatalf("lazy image holds %d bytes before it is drawn", n)
		}

		small := imaging.Bound{W: 100, H: 100}
		if v := img.lazy.variant(400, 200, small); v != nil {
			t.Fatal("variant returned pixels before any decode finished")
		}
		synctest.Wait()
		v := img.lazy.variant(400, 200, small)
		if v == nil {
			t.Fatal("requested size did not load")
		}
		if w, h := v.Size(); w != 100 || h != 50 {
			t.Fatalf("decoded %dx%d, want 100x50", w, h)
		}
		if n := cached(); n != 100*50*4 {
			t.Fatalf("cache holds %d bytes, want only the decoded size", n)
		}

		large := imaging.Bound{W: 200, H: 200}
		if got := img.lazy.variant(400, 200, large); got != v {
			t.Fatal("a size that is still loading did not fall back to the size drawn last")
		}
		synctest.Wait()
		if w, _ := img.lazy.variant(400, 200, large).Size(); w != 200 {
			t.Fatalf("larger size decoded %d wide, want 200", w)
		}
	})
}

func TestImageCacheLazyImageReportsDecodeFailure(t *testing.T) {
	data := pngBytes(t, 40, 20)
	broken := slices.Clone(data[:len(data)-20])
	synctest.Test(t, func(t *testing.T) {
		c := NewImageCache(1<<20, 1)
		read := func() ([]byte, error) { return broken, nil }
		c.Load("a", 0, 0, read)
		synctest.Wait()
		img := c.Load("a", 0, 0, read)
		if img == nil {
			t.Fatalf("header did not load: %v", c.Err("a", 0, 0))
		}
		img.lazy.variant(40, 20, imaging.Bound{W: 10, H: 10})
		synctest.Wait()
		if c.Err("a", 0, 0) == nil || c.Load("a", 0, 0, read) != nil {
			t.Fatal("a lazy image whose pixels fail to decode is still offered")
		}
	})
}

func TestImageCachePrunesSizesNotRequestedSinceANewLoad(t *testing.T) {
	old := imagePruneDelay
	imagePruneDelay = time.Hour // pruneSizes is called directly
	t.Cleanup(func() { imagePruneDelay = old })
	data := pngBytes(t, 400, 200)
	c := NewImageCache(1<<20, 1)
	has := func(src string, side int) bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		_, ok := c.entries[imageKey{src: src, Bound: imaging.Bound{W: side, H: side}}]
		return ok
	}

	waitImage(t, c, "stale", 100, 100, data)
	waitImage(t, c, "stale", 50, 50, data)
	c.pruneSizes(imageKey{src: "stale", Bound: imaging.Bound{W: 50, H: 50}})
	if has("stale", 100) || !has("stale", 50) {
		t.Error("an older size nobody requested was not dropped, or the new size was")
	}

	waitImage(t, c, "shown", 100, 100, data)
	release := make(chan struct{})
	c.Load("shown", 50, 50, func() ([]byte, error) { <-release; return data, nil })
	c.Load("shown", 100, 100, nil) // still drawn while the new size loads
	close(release)
	waitImage(t, c, "shown", 50, 50, data)
	c.pruneSizes(imageKey{src: "shown", Bound: imaging.Bound{W: 50, H: 50}})
	if !has("shown", 100) {
		t.Error("a size requested after the new size started loading was dropped")
	}
}

func TestAutoImageBudget(t *testing.T) {
	for _, tc := range []struct {
		pixels, want int64
	}{
		{0, maxAutoImageBudget},
		{1920 * 1080, minAutoImageBudget},
		{2 * 3840 * 2160, 2 * 2 * 3840 * 2160 * 4},
		{4 * 7680 * 4320, maxAutoImageBudget},
	} {
		if got := autoImageBudget(tc.pixels); got != tc.want {
			t.Errorf("autoImageBudget(%d) = %d, want %d", tc.pixels, got, tc.want)
		}
	}
}

func TestImageCacheBoundsMetadata(t *testing.T) {
	data := pngBytes(t, 1, 1)
	synctest.Test(t, func(t *testing.T) {
		c := NewImageCache(4, 1)
		var reads atomic.Int32
		read := func() ([]byte, error) { reads.Add(1); return data, nil }
		old := waitImage(t, c, "held", 0, 0, data)
		for n := range 1025 {
			c.Load(fmt.Sprint(n), 0, 0, read)
			synctest.Wait()
		}
		before := reads.Load()
		c.Load("0", 0, 0, read)
		synctest.Wait()
		if reads.Load() != before+1 {
			t.Fatal("old lazy metadata was retained beyond the entry limit")
		}
		if got := old.lazy.variant(1, 1, imaging.Bound{W: 1, H: 1}); got != nil {
			t.Fatal("evicted lazy handle unexpectedly had a pixel decode")
		}
		synctest.Wait()
		if got := old.lazy.variant(1, 1, imaging.Bound{W: 1, H: 1}); got == nil {
			t.Fatal("evicted lazy handle could not load its pixels")
		}
		for n := range 257 {
			c.Load(fmt.Sprintf("bad%d", n), 1, 1, func() ([]byte, error) { return nil, errors.New("missing") })
			synctest.Wait()
		}
		if c.Err("bad0", 1, 1) != nil {
			t.Fatal("old failure was retained beyond the failure limit")
		}
	})
}
