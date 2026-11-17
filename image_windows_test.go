package fairy

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/color/palette"
	"image/gif"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	xdraw "golang.org/x/image/draw"

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

var (
	cacheTestFormat  sync.Once
	cacheTestDecodes sync.Map
	cacheTestSeq     atomic.Uint64
)

// oversizedPNG is a valid all-black PNG just over imaging.MaxImagePixels, built
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

func solidPaletted(r image.Rectangle, idx uint8) *image.Paletted {
	p := image.NewPaletted(r, palette.Plan9)
	for i := range p.Pix {
		p.Pix[i] = idx
	}
	return p
}

type bitWriter struct {
	buf  []byte
	acc  uint64
	bits uint
}

func (b *bitWriter) put(v uint64, n uint) {
	b.acc |= v << b.bits
	b.bits += n
	for b.bits >= 8 {
		b.buf = append(b.buf, byte(b.acc))
		b.acc >>= 8
		b.bits -= 8
	}
}

func (b *bitWriter) bytes() []byte {
	if b.bits > 0 {
		b.buf = append(b.buf, byte(b.acc))
	}
	return b.buf
}

// solidVP8L encodes a w x h lossless bitstream of one color: five simple
// one-symbol prefix codes, so the pixels themselves take no bits.
func solidVP8L(w, h int, c color.NRGBA) []byte {
	var b bitWriter
	b.put(0x2f, 8)
	b.put(uint64(w-1), 14)
	b.put(uint64(h-1), 14)
	b.put(1, 1) // alpha is used
	b.put(0, 3) // version
	b.put(0, 1) // no transform
	b.put(0, 1) // no color cache
	b.put(0, 1) // no meta prefix codes
	for _, sym := range []uint8{c.G, c.R, c.B, c.A} {
		b.put(1, 1) // simple code
		b.put(0, 1) // one symbol
		b.put(1, 1) // eight-bit symbol
		b.put(uint64(sym), 8)
	}
	b.put(1, 1) // distance: simple code
	b.put(0, 1)
	b.put(0, 1) // one-bit symbol
	b.put(0, 1)
	return b.bytes()
}

func webpChunk(id string, data []byte) []byte {
	chunk := append([]byte(id), 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(chunk[4:], uint32(len(data)))
	chunk = append(chunk, data...)
	if len(data)&1 != 0 {
		chunk = append(chunk, 0)
	}
	return chunk
}

func anmf(x, y, w, h int, ms int, flags byte, bits []byte) []byte {
	p := make([]byte, 16)
	put24 := func(o, v int) { p[o], p[o+1], p[o+2] = byte(v), byte(v>>8), byte(v>>16) }
	put24(0, x/2)
	put24(3, y/2)
	put24(6, w-1)
	put24(9, h-1)
	put24(12, ms)
	p[15] = flags
	return webpChunk("ANMF", append(p, webpChunk("VP8L", bits)...))
}

func TestLazyImagePixelReadCompositesFirstFrame(t *testing.T) {
	first := image.Rect(2, 2, 4, 4)
	canvas := image.Rect(0, 0, 6, 6)
	var gifData bytes.Buffer
	err := gif.EncodeAll(&gifData, &gif.GIF{
		Image:  []*image.Paletted{solidPaletted(first, 1), solidPaletted(canvas, 2)},
		Delay:  []int{10, 10},
		Config: image.Config{Width: 6, Height: 6},
	})
	if err != nil {
		t.Fatal(err)
	}
	vp8x := make([]byte, 10)
	vp8x[0], vp8x[4], vp8x[7] = 0x12, 5, 5
	body := append([]byte("WEBP"), webpChunk("VP8X", vp8x)...)
	body = append(body, anmf(2, 2, 2, 2, 100, 2, solidVP8L(2, 2, color.NRGBA{R: 255, A: 255}))...)
	// The second frame has a valid container header but an invalid bitstream.
	body = append(body, anmf(0, 0, 6, 6, 100, 2, []byte{0xff})...)
	webp := append(binary.LittleEndian.AppendUint32([]byte("RIFF"), uint32(len(body))), body...)
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"GIF", gifData.Bytes()},
		{"WebP", webp},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := waitImage(t, NewImageCache(1<<20, 1), tc.name, 0, 0, tc.data)
			pix, err := img.NRGBA()
			if err != nil {
				t.Fatal(err)
			}
			if pix.Bounds() != canvas || pix.NRGBAAt(0, 0).A != 0 || pix.NRGBAAt(2, 2).A != 255 {
				t.Fatalf("first frame: bounds=%v, outside=%v, inside=%v", pix.Bounds(), pix.NRGBAAt(0, 0), pix.NRGBAAt(2, 2))
			}
		})
	}
}

func referenceStill(t *testing.T, data []byte, bound imaging.Bound) *Image {
	t.Helper()
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	b := src.Bounds()
	if dw, dh := bound.Size(b.Dx(), b.Dy()); dw != b.Dx() || dh != b.Dy() {
		dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Src, nil)
		src = dst
	}
	return NewImage(src)
}

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

func TestTrimImageMemoryReleasesTexturesBeforeReturning(t *testing.T) {
	err := Main(func() error {
		w := &Window{
			hwnd:  app.hwnd,
			frame: 2,
			textures: map[uint64]*texEntry{
				1: {frame: 0},
				2: {frame: 1},
			},
		}
		liveMu.Lock()
		live[w] = struct{}{}
		liveMu.Unlock()
		defer func() {
			liveMu.Lock()
			delete(live, w)
			liveMu.Unlock()
		}()
		trimImageMemory()
		if _, ok := w.textures[1]; ok {
			t.Error("trimImageMemory returned before releasing an idle texture")
		}
		if _, ok := w.textures[2]; !ok {
			t.Error("trimImageMemory released a texture drawn in the last frame")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
