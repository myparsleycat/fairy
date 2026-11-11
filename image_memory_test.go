package fairy

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/color/palette"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkImageCacheResize(b *testing.B) {
	for _, tc := range []struct{ name, fixture, ext string }{
		{"JPEG", "progressive.jpg", "jpg"},
		{"WebP", "lossy.webp", "webp"},
	} {
		b.Run(tc.name, func(b *testing.B) {
			path := filepath.Join("testdata", "images", tc.fixture)
			largeW, largeH, smallW, smallH := 64, 48, 16, 12
			// FAIRY_RESIZE_FIXTURES names a directory with resize.jpg and resize.webp
			// (1280 x 720 or larger) to measure with full-size images.
			if dir := os.Getenv("FAIRY_RESIZE_FIXTURES"); dir != "" {
				path = filepath.Join(dir, "resize."+tc.ext)
				largeW, largeH, smallW, smallH = 1280, 720, 256, 144
			}
			data, err := os.ReadFile(path)
			if err != nil {
				b.Fatal(err)
			}
			var reads atomic.Int64
			read := func() ([]byte, error) {
				reads.Add(1)
				return data, nil
			}
			await := func(c *ImageCache, w, h int) {
				deadline := time.Now().Add(10 * time.Second)
				for time.Now().Before(deadline) {
					if c.Load("resize", w, h, read) != nil {
						return
					}
					if err := c.Err("resize", w, h); err != nil {
						b.Fatal(err)
					}
					runtime.Gosched()
				}
				b.Fatal("cache load timed out")
			}
			b.ReportAllocs()
			var total int64
			for b.Loop() {
				b.StopTimer()
				c := NewImageCache(32<<20, 1)
				await(c, largeW, largeH)
				before := reads.Load()
				b.StartTimer()
				await(c, smallW, smallH)
				b.StopTimer()
				total += reads.Load() - before
				c.Forget("resize")
				b.StartTimer()
			}
			b.ReportMetric(float64(total)/float64(b.N), "reads/op")
		})
	}
}

func BenchmarkImageMemory(b *testing.B) {
	for _, name := range []string{"PNG", "SmallPNG", "GIF", "WebP"} {
		b.Run(name, func(b *testing.B) {
			data := memoryImageBytes(b, name)
			bound, count, frames := 128, 1, 48
			switch name {
			case "PNG":
				bound, count, frames = 384, 16, 1
			case "SmallPNG":
				bound, count, frames = 128, 128, 1
			}
			for range b.N {
				runtime.GC()
				var baseline runtime.MemStats
				runtime.ReadMemStats(&baseline)
				cache := NewImageCache(256<<20, 0)
				stop := make(chan struct{})
				var wg sync.WaitGroup
				var peak uint64
				wg.Go(func() {
					ticker := time.NewTicker(time.Millisecond)
					defer ticker.Stop()
					for {
						select {
						case <-stop:
							return
						case <-ticker.C:
							var m runtime.MemStats
							runtime.ReadMemStats(&m)
							peak = max(peak, m.HeapAlloc)
						}
					}
				})
				start := time.Now()
				err := loadMemoryImages(cache, data, bound, count, frames)
				elapsed := time.Since(start)
				close(stop)
				wg.Wait()
				if err != nil {
					b.Fatal(err)
				}
				var after runtime.MemStats
				runtime.ReadMemStats(&after)
				b.ReportMetric(float64(max(peak, after.HeapAlloc)-baseline.HeapAlloc)/(1<<20), "peak-MiB")
				b.ReportMetric(float64(after.TotalAlloc-baseline.TotalAlloc)/(1<<20), "allocated-MiB")
				b.ReportMetric(float64(elapsed.Microseconds())/1000, "load-ms")
				runtime.KeepAlive(cache)
				runtime.KeepAlive(data)
			}
		})
	}
}

func loadMemoryImages(c *ImageCache, data []byte, bound, count, frames int) error {
	read := func() ([]byte, error) { return data, nil }
	for i := range count {
		c.Load(fmt.Sprint(i), bound, bound, read)
	}
	deadline := time.Now().Add(30 * time.Second)
	for i := range count {
		key := fmt.Sprint(i)
		for {
			if img := c.Load(key, bound, bound, read); img != nil {
				if img.Frames() != frames {
					return fmt.Errorf("image %s has %d frames, want %d", key, img.Frames(), frames)
				}
				break
			}
			if err := c.Err(key, bound, bound); err != nil {
				return err
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("image %s timed out", key)
			}
			time.Sleep(time.Millisecond)
		}
	}
	return nil
}

func memoryImageBytes(b *testing.B, name string) []byte {
	b.Helper()
	switch name {
	case "PNG", "SmallPNG":
		w, h := 3000, 2000
		if name == "SmallPNG" {
			w, h = 512, 384
		}
		src := image.NewNRGBA(image.Rect(0, 0, w, h))
		src.Pix[3] = 128
		var buf bytes.Buffer
		if err := png.Encode(&buf, src); err != nil {
			b.Fatal(err)
		}
		return buf.Bytes()
	case "GIF":
		src := image.NewPaletted(image.Rect(0, 0, 2048, 2048), palette.Plan9)
		src.Pix[0] = 1
		g := gif.GIF{Image: make([]*image.Paletted, 48), Delay: make([]int, 48)}
		for i := range g.Image {
			g.Image[i], g.Delay[i] = src, 10
		}
		var buf bytes.Buffer
		if err := gif.EncodeAll(&buf, &g); err != nil {
			b.Fatal(err)
		}
		return buf.Bytes()
	case "WebP":
		var vp8x [10]byte
		vp8x[0] = 0x12
		vp8x[4], vp8x[5], vp8x[7], vp8x[8] = 255, 7, 255, 7
		body := append([]byte("WEBP"), webpChunk("VP8X", vp8x[:])...)
		bits := solidVP8L(2048, 2048, color.NRGBA{R: 255, A: 128})
		for range 48 {
			body = append(body, anmf(0, 0, 2048, 2048, 100, 2, bits)...)
		}
		data := append([]byte("RIFF"), 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(data[4:], uint32(len(body)))
		return append(data, body...)
	default:
		b.Fatalf("unknown memory fixture %q", name)
		return nil
	}
}
