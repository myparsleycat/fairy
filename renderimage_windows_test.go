package fairy

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/myparsleycat/fairy/internal/render"
)

func TestRenderImage(t *testing.T) {
	el := Row(
		Box().W(10).HFull().Fill(Hex(0xff0000)),
		Box().W(10).HFull().Fill(Hex(0x0000ff).Alpha(0.5)),
		Box().Grow(1).HFull(),
	).Size(40, 10)
	img, err := RenderImage(el, 40, 10, 2)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 80 || b.Dy() != 20 {
		t.Fatalf("bounds %v, want 80x20", b)
	}
	if c := img.NRGBAAt(5, 10); c.R != 255 || c.G != 0 || c.B != 0 || c.A != 255 {
		t.Errorf("opaque box %v", c)
	}
	if c := img.NRGBAAt(30, 10); c.B < 254 || c.R != 0 || c.A < 127 || c.A > 128 {
		t.Errorf("half-transparent box %v, want straight blue at half alpha", c)
	}
	if c := img.NRGBAAt(70, 10); c.A != 0 {
		t.Errorf("background %v, want transparent", c)
	}
}

func TestRenderImageText(t *testing.T) {
	img, err := RenderImage(Text("가A😀").FontSize(20).Color(Hex(0xffffff)), 80, 30, 1)
	if err != nil {
		t.Fatal(err)
	}
	if inked := inkedPixels(img); inked < 50 {
		t.Fatalf("only %d pixels have ink", inked)
	}
}

func TestRenderViewImageShowsPersistentScrollThumb(t *testing.T) {
	img, err := RenderViewImage(func(cx *Cx) *El {
		w, h := cx.Size()
		return Col(Box().H(160).Shrink(0)).Size(w, h).ScrollY().ScrollBarStyle(ScrollBarStyle{
			Thickness: 6, Inset: 2, TrackInset: 12, AlwaysVisible: true, Color: Hex(0x000000),
		}).Fill(Hex(0xffffff))
	}, 80, 80, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := img.NRGBAAt(75, 20); got != (color.NRGBA{A: 255}) {
		t.Errorf("visible thumb pixel = %v, want black", got)
	}
	if got := img.NRGBAAt(75, 7); got != (color.NRGBA{R: 255, G: 255, B: 255, A: 255}) {
		t.Errorf("track inset pixel = %v, want white", got)
	}
}

// Antialiased edges must blend toward transparent in premultiplied space;
// mixing straight color with transparent black darkens them.
func TestRenderImageTransparentEdgesKeepColor(t *testing.T) {
	// Compare premultiplied, so 8-bit rounding at faint alpha does not count.
	darkened := func(c, want, a uint8) bool { return int(c)*int(a)/255 < int(want)*int(a)/255-2 }
	t.Run("border inside rounded corner", func(t *testing.T) {
		gray := Hex(0xe5e5e5)
		img, err := RenderImage(Box().Size(40, 32).Radius(10).Border(1, gray), 40, 32, 1)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < len(img.Pix); i += 4 {
			if darkened(img.Pix[i], 0xe5, img.Pix[i+3]) {
				t.Fatalf("pixel %d = %v, want straight gray 0xe5", i/4, img.Pix[i:i+4])
			}
		}
	})
	t.Run("stretched image", func(t *testing.T) {
		src := image.NewNRGBA(image.Rect(0, 0, 2, 1))
		src.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
		img, err := RenderImage(ImageEl(NewImage(src)).Fit(FitFill).Size(40, 4), 40, 4, 1)
		if err != nil {
			t.Fatal(err)
		}
		for x := range 40 {
			if c := img.NRGBAAt(x, 2); darkened(c.R, 255, c.A) {
				t.Fatalf("x=%d: %v, want straight red", x, c)
			}
		}
	})
}

func TestRenderImageKeepsPixelsAcrossTextureUploads(t *testing.T) {
	colors := []color.NRGBA{{R: 255, A: 128}, {B: 255, A: 128}, {G: 255, A: 255}}
	row := Row()
	for _, c := range colors {
		src := image.NewNRGBA(image.Rect(0, 0, 8, 8))
		for y := range 8 {
			for x := range 8 {
				src.SetNRGBA(x, y, c)
			}
		}
		row.Child(ImageEl(NewImage(src)).Size(8, 8).Shrink(0))
	}
	got, err := RenderImage(row, 24, 8, 1)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range colors {
		if c := got.NRGBAAt(i*8+4, 4); c != want {
			t.Errorf("image %d = %v, want %v", i, c, want)
		}
	}
}

func TestRenderImageDashedBorder(t *testing.T) {
	red := Hex(0xff0000)
	gaps := func(el *El) int {
		img, err := RenderImage(el, 60, 20, 1)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for x := 10; x < 50; x++ {
			if img.NRGBAAt(x, 0).A < 128 {
				n++
			}
		}
		return n
	}

	if n := gaps(Box().Size(60, 20).Border(1, red)); n != 0 {
		t.Errorf("solid border has %d gaps along its top edge, want none", n)
	}
	// Dashes are three widths long and two apart: 40 pixels hold 16 of gap.
	if n := gaps(Box().Size(60, 20).Border(1, red).BorderDashed()); n < 14 || n > 18 {
		t.Errorf("dashed border has %d gap pixels along 40 pixels of its top edge, want about 16", n)
	}
}

func TestRenderImageEdges(t *testing.T) {
	black := color.NRGBA{A: 255}
	white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	red := color.NRGBA{R: 255, A: 255}
	blue := color.NRGBA{B: 255, A: 255}
	src := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	src.SetNRGBA(0, 0, blue)

	cases := []struct {
		name         string
		el           *El
		edge, center color.NRGBA
		outside      bool
	}{
		{
			name:    "sharp shadow",
			el:      Box().Radius(5).Fill(Hex(0xffffff)).Shadow(0, 0, 0, 1, Hex(0)),
			edge:    black,
			center:  white,
			outside: true,
		},
		{
			name:   "scroll clip",
			el:     Box(Box().Inset(0).Fill(Hex(0xff0000))).Fill(Hex(0x0000ff)).ScrollY(),
			edge:   red,
			center: red,
		},
		{
			name:   "inset shadow",
			el:     Box().Radius(5).Fill(Hex(0xffffff)).InsetShadow(0, 0, 0, 3, Hex(0)),
			edge:   black,
			center: white,
		},
		{
			name:   "image border",
			el:     ImageEl(NewImage(src)).Fit(FitFill).Radius(5).Border(1, Hex(0xff0000)),
			edge:   red,
			center: blue,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			el := Box(tc.el.Left(12.5).Top(14.5).Size(56, 32)).Fill(Hex(0xffffff))
			img, err := RenderImage(el, 84, 60, 1)
			if err != nil {
				t.Fatal(err)
			}
			left, right, top, bottom := 13, 68, 15, 46
			if tc.outside {
				left, right, top, bottom = left-1, right+1, top-1, bottom+1
			}
			for _, p := range []image.Point{{X: left, Y: 31}, {X: right, Y: 31}, {X: 41, Y: top}, {X: 41, Y: bottom}} {
				if got := img.NRGBAAt(p.X, p.Y); got != tc.edge {
					t.Errorf("edge %v = %v, want %v", p, got, tc.edge)
				}
			}
			if got := img.NRGBAAt(41, 31); got != tc.center {
				t.Errorf("center = %v, want %v", got, tc.center)
			}
		})
	}
}

// A child that fills a bordered Clip box must stay inside the border's inner
// edge, rounded corners included.
func TestRenderImageRoundedClip(t *testing.T) {
	blue := color.NRGBA{B: 255, A: 255}
	red := color.NRGBA{R: 255, A: 255}
	for _, radius := range []float32{10, 0} {
		el := Box(Box().Inset(0).Fill(Hex(0xff0000))).Size(40, 40).Radius(radius).Border(1, Hex(0x0000ff)).Clip()
		img, err := RenderImage(el, 40, 40, 1)
		if err != nil {
			t.Fatal(err)
		}
		if c := img.NRGBAAt(1, 1); radius > 0 && c.A != 0 {
			t.Errorf("radius %v: corner = %v, want transparent", radius, c)
		}
		for _, p := range []image.Point{{X: 0, Y: 20}, {X: 39, Y: 20}, {X: 20, Y: 0}, {X: 20, Y: 39}} {
			if c := img.NRGBAAt(p.X, p.Y); c != blue {
				t.Errorf("radius %v: border %v = %v, want %v", radius, p, c, blue)
			}
		}
		if c := img.NRGBAAt(20, 20); c != red {
			t.Errorf("radius %v: center = %v, want %v", radius, c, red)
		}
	}
}

// An isolated group fades as one picture: where its opaque children overlap
// it is no more opaque than where they do not.
func TestRenderImageIsolate(t *testing.T) {
	el := Box(
		Box().Absolute().Left(0).Top(0).Size(30, 20).Fill(Hex(0x0000ff)),
		Box().Absolute().Left(20).Top(0).Size(30, 20).Fill(Hex(0x0000ff)),
	).Size(50, 20).Isolate().Opacity(0.5)
	img, err := RenderImage(el, 50, 20, 1)
	if err != nil {
		t.Fatal(err)
	}
	single, overlap := img.NRGBAAt(10, 10), img.NRGBAAt(25, 10)
	if single.A < 126 || single.A > 129 || overlap != single {
		t.Fatalf("one child %v, overlap %v; want both blue at half alpha", single, overlap)
	}
}

func TestSweepTexturesReleasesUnusedImages(t *testing.T) {
	r, err := render.NewOffscreen(8, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	w := &Window{r: r, textures: map[uint64]*texEntry{}}
	newImg := func() *Image { return NewImage(image.NewNRGBA(image.Rect(0, 0, 64, 64))) }
	oldest, kept, retired, drawnRetired, collected := newImg(), newImg(), newImg(), newImg(), newImg()
	w.texture(oldest, 0)
	w.frame = 1
	w.texture(kept, 0)
	w.texture(retired, 0)
	w.texture(collected, 0)
	collectedID := collected.id
	collected = nil
	runtime.GC()
	w.frame = 2
	w.texture(drawnRetired, 0)
	retired.retired.Store(true)
	drawnRetired.retired.Store(true)

	w.sweepTextures(2, 64*64*4)
	for _, tc := range []struct {
		name string
		id   uint64
		want bool
	}{
		{"least recently drawn beyond the budget", oldest.id, false},
		{"recently drawn within the budget", kept.id, true},
		{"dropped from its cache", retired.id, false},
		{"garbage collected", collectedID, false},
		{"dropped but drawn this frame", drawnRetired.id, true},
	} {
		if _, ok := w.textures[tc.id]; ok != tc.want {
			t.Errorf("texture %s: kept = %v, want %v", tc.name, ok, tc.want)
		}
	}
	runtime.KeepAlive(oldest)
	w.releaseTextures()
}

func TestRenderImageDecodesLazyImageAtDrawnSize(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 400, 200))
	for i := range src.Pix {
		src.Pix[i] = []byte{0, 128, 255, 255}[i%4]
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	c := NewImageCache(1<<20, 1)
	img := waitImage(t, c, "photo", 0, 0, buf.Bytes())
	el := ImageEl(img).Size(50, 25).Fit(FitContain)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	got, err := RenderImageContext(ctx, el, 50, 25, 2)
	if err != nil {
		t.Fatal(err)
	}
	if px := got.NRGBAAt(50, 25); px != (color.NRGBA{0, 128, 255, 255}) {
		t.Fatalf("pixel %v, want the image's color", px)
	}
	// At scale 2 the 100x50 device-pixel box rounds up to a 128x64 decode.
	if v := c.Load("photo", 128, 64, nil); v == nil {
		t.Fatal("capture did not cache a decode for the drawn size")
	} else if w, h := v.Size(); w != 128 || h != 64 {
		t.Fatalf("drew a %dx%d decode, want 128x64", w, h)
	}
}

func TestRenderImageDecodesLazyImageAfterTransform(t *testing.T) {
	for _, tc := range []struct {
		name  string
		scale float32
		angle float32
		want  int
	}{
		{"enlarged", 4, 0, 256}, {"reduced", 0.5, 0, 32},
		{"rotated", 4, math.Pi / 4, 256}, {"zero", 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewImageCache(4<<20, 1)
			img := waitImage(t, c, "transformed", 0, 0, pngBytes(t, 400, 400))
			el := Box(Box(ImageEl(img).Size(50, 50).Fit(FitContain)).Size(50, 50).Scale(tc.scale).Rotate(tc.angle).Origin(0, 0))
			if _, err := RenderImageContext(t.Context(), el, 200, 200, 1); err != nil {
				t.Fatal(err)
			}
			if tc.want == 0 {
				c.mu.Lock()
				defer c.mu.Unlock()
				if len(c.entries) != 1 || len(c.loading) != 0 {
					t.Fatal("zero scale requested pixels")
				}
				return
			}
			if c.Load("transformed", tc.want, tc.want, nil) == nil {
				t.Fatalf("no %dx%d decode for transformed image", tc.want, tc.want)
			}
		})
	}
}

func gatedCaptureImage(t *testing.T) (*ImageCache, *Image, <-chan struct{}, func()) {
	t.Helper()
	started, unblock := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(unblock) })
	t.Cleanup(release)
	data := cacheTestImage(t, func() (image.Image, error) {
		close(started)
		<-unblock
		pix := image.NewNRGBA(image.Rect(0, 0, 1, 1))
		pix.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
		return pix, nil
	})
	c := NewImageCache(1024, 1)
	return c, waitImage(t, c, "capture", 0, 0, data), started, release
}

func TestRenderImageReportsPendingLazyImage(t *testing.T) {
	_, img, _, _ := gatedCaptureImage(t)
	pix, err := RenderImage(ImageEl(img).Size(8, 8), 8, 8, 1)
	if pix != nil || !errors.Is(err, ErrImageNotReady) {
		t.Fatalf("cold capture = %v, %v; want ErrImageNotReady", pix, err)
	}
}

func TestRenderViewImageContextWaitsWithoutRepainting(t *testing.T) {
	_, img, started, release := gatedCaptureImage(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	go func() {
		select {
		case <-started:
			release()
		case <-ctx.Done():
		}
	}()
	var builds, surfaces int
	pix, err := RenderViewImageContext(ctx, func(*Cx) *El {
		builds++
		return Row(
			Box(ImageEl(img).Size(8, 8)).Size(8, 8).Isolate(),
			Surface(func(SurfaceFrame) bool {
				surfaces++
				return false
			}).Size(8, 8),
		)
	}, 16, 8, 1)
	if err != nil {
		t.Fatal(err)
	}
	if p := pix.NRGBAAt(4, 4); p != (color.NRGBA{R: 255, A: 255}) {
		t.Fatalf("captured pixel = %v, want opaque red", p)
	}
	if builds != 1 || surfaces != 1 {
		t.Fatalf("callbacks: view=%d, surface=%d; want one each", builds, surfaces)
	}
}

func TestRenderImageContextInterruptedWhileDecoding(t *testing.T) {
	for _, name := range []string{"cancel", "forget"} {
		t.Run(name, func(t *testing.T) {
			cache, img, started, release := gatedCaptureImage(t)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			want := errImageLoadInvalidated
			interrupt := func() { cache.Forget("capture") }
			if name == "cancel" {
				want, interrupt = context.Canceled, cancel
			}
			go func() {
				select {
				case <-started:
					interrupt()
				case <-ctx.Done():
				}
			}()
			pix, err := RenderImageContext(ctx, ImageEl(img).Size(8, 8), 8, 8, 1)
			if pix != nil || !errors.Is(err, want) {
				t.Fatalf("interrupted capture = %v, %v; want %v", pix, err, want)
			}
			if name == "cancel" {
				release()
				shared, stop := context.WithTimeout(t.Context(), 5*time.Second)
				defer stop()
				pix, err := RenderImageContext(shared, ImageEl(img).Size(8, 8), 8, 8, 1)
				if err != nil || pix.NRGBAAt(4, 4).R != 255 {
					t.Fatalf("shared decode did not survive cancellation: %v", err)
				}
			}
		})
	}
}

func TestRenderImageContextDoesNotWaitOnUIThread(t *testing.T) {
	_, img, _, _ := gatedCaptureImage(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err := Main(func() error {
		_, err := RenderImageContext(ctx, ImageEl(img).Size(8, 8), 8, 8, 1)
		return err
	})
	if !errors.Is(err, ErrImageNotReady) {
		t.Fatalf("UI capture = %v, want ErrImageNotReady", err)
	}
}

func TestRenderImageReturnsFailedVariantError(t *testing.T) {
	want := errors.New("image decoder failed")
	data := cacheTestImage(t, func() (image.Image, error) { return nil, want })
	img := waitImage(t, NewImageCache(1024, 1), "broken", 0, 0, data)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	el := ImageEl(img).Size(8, 8)
	if pix, err := RenderImageContext(ctx, el, 8, 8, 1); pix != nil || !errors.Is(err, want) {
		t.Fatalf("waiting capture = %v, %v; want decoder error", pix, err)
	}
	if pix, err := RenderImage(el, 8, 8, 1); pix != nil || !errors.Is(err, want) {
		t.Fatalf("capture of failed variant = %v, %v; want decoder error", pix, err)
	}
}

func TestRenderImageContextStopsOnLaterInvalidatedImage(t *testing.T) {
	_, first, firstStarted, _ := gatedCaptureImage(t)
	c, second, secondStarted, _ := gatedCaptureImage(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	go func() {
		select {
		case <-firstStarted:
		case <-ctx.Done():
			return
		}
		select {
		case <-secondStarted:
			c.Forget("capture")
		case <-ctx.Done():
		}
	}()
	el := Row(ImageEl(first).Size(8, 8), ImageEl(second).Size(8, 8))
	if pix, err := RenderImageContext(ctx, el, 16, 8, 1); pix != nil || !errors.Is(err, errImageLoadInvalidated) {
		t.Fatalf("capture with later invalidated image = %v, %v; want invalidation", pix, err)
	}
}

func TestRenderImageContextStopsWhenMainExits(t *testing.T) {
	_, img, started, _ := gatedCaptureImage(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result := make(chan error, 1)
	if err := Main(func() error {
		if _, err := NewWindow(Options{StartHidden: true, View: func(*Cx) *El { return Box() }}); err != nil {
			return err
		}
		go func() {
			_, err := RenderImageContext(ctx, ImageEl(img).Size(8, 8), 8, 8, 1)
			result <- err
		}()
		go func() {
			select {
			case <-started:
			case <-ctx.Done():
			}
			Quit()
		}()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, errUIUnavailable) {
			t.Fatalf("capture after Main exit = %v, want UI unavailable", err)
		}
	case <-ctx.Done():
		t.Fatal("capture did not return after Main exited")
	}
}

// Span colors are numbered past 255 when a paragraph has that many colored
// spans, as a highlighted code block does; uncolored text keeps the base color.
func TestRenderImageManySpanColors(t *testing.T) {
	spans := []Span{{Text: "W"}}
	for range 299 {
		spans = append(spans, Span{Text: ".", Color: Hex(0xff0000)})
	}
	spans = append(spans, Span{Text: ".", Color: Hex(0x0000ff)})
	img, err := RenderImage(RichText(spans...).FontSize(16).Color(Hex(0x00ff00)).NoWrap(), 2400, 30, 1)
	if err != nil {
		t.Fatal(err)
	}
	green, blue := 0, 0
	for i := 0; i < len(img.Pix); i += 4 {
		r, g, b, a := img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]
		switch {
		case a < 128:
		case g > r && g > b:
			green++
		case b > r && b > g:
			blue++
		}
	}
	if green == 0 || blue == 0 {
		t.Fatalf("%d base-colored and %d span #300 pixels, want both", green, blue)
	}
}

// Glyphs that need the atlas to grow twice must still be drawn: each growth
// discards the tiles the scene was painted with.
func TestRenderImageGrowsAtlasTwice(t *testing.T) {
	const glyphs = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	var spans []Span
	for size := float32(300); size < 360; size += 6 {
		spans = append(spans, Span{Text: glyphs, Size: size})
	}
	img, err := RenderImage(RichText(spans...).Color(Hex(0xffffff)).NoWrap(), 200, 400, 1)
	if err != nil {
		t.Fatal(err)
	}
	if inked := inkedPixels(img); inked < 1000 {
		t.Fatalf("only %d pixels have ink", inked)
	}
}

// inkedPixels counts the pixels of img with any alpha.
func inkedPixels(img *image.NRGBA) int {
	n := 0
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] > 0 {
			n++
		}
	}
	return n
}
