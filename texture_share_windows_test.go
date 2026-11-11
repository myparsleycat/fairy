package fairy

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"runtime"
	"testing"
	"testing/synctest"
	"time"

	"github.com/myparsleycat/fairy/internal/imaging"
	"github.com/myparsleycat/fairy/internal/render"
)

func TestWindowBoundsAnimationUpdates(t *testing.T) {
	r, err := render.NewOffscreen(8, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	w := &Window{r: r, textures: map[uint64]*texEntry{}}
	defer w.releaseTextures()
	img, err := NewImageFrames([]image.Image{image.NewRGBA(image.Rect(0, 0, 16, 16)), image.NewRGBA(image.Rect(0, 0, 16, 16))}, []time.Duration{time.Second, time.Second}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if w.texture(img, 0) == nil {
		t.Fatal("initial animation upload failed")
	}
	w.uploaded = maxUploadBytesPerFrame
	w.texture(img, 1)
	if w.textures[img.id].shown != 0 {
		t.Fatal("animation update exceeded the frame budget")
	}
	w.uploaded = 0
	w.texture(img, 1)
	if w.textures[img.id].shown != 1 {
		t.Fatal("deferred animation update did not resume")
	}
}

func TestAnimationInitialUploadUsesRequestedFrame(t *testing.T) {
	const side = 16
	for _, mode := range []string{"kept", "stream ready", "stream pending", "stream failed"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r, err := render.NewOffscreen(8, 8)
				if err != nil {
					t.Fatal(err)
				}
				defer r.Release()
				w := &Window{r: r, textures: map[uint64]*texEntry{}}
				defer w.releaseTextures()
				red, green := image.NewRGBA(image.Rect(0, 0, side, side)), image.NewRGBA(image.Rect(0, 0, side, side))
				for y := range side {
					for x := range side {
						red.SetRGBA(x, y, color.RGBA{R: 255, A: 255})
						green.SetRGBA(x, y, color.RGBA{G: 255, A: 255})
					}
				}
				img, err := NewImageFrames([]image.Image{red, green}, []time.Duration{time.Second, time.Second}, 0)
				if err != nil {
					t.Fatal(err)
				}
				if mode != "kept" {
					img = textureTestStream(t, side, mode == "stream failed")
					defer func() {
						// Let the idle decoder exit before its synctest bubble.
						time.Sleep(2 * time.Second)
						synctest.Wait()
					}()
					if mode != "stream pending" {
						if img.data.Stream.WithFrame(1, func([]byte) {}) {
							t.Fatal("stream was ready before its first request")
						}
						synctest.Wait()
						var pixels []byte
						if !img.data.Stream.WithFrame(1, func(p []byte) { pixels = bytes.Clone(p) }) {
							t.Fatal("requested stream frame was not ready")
						}
						want := green.Pix
						if mode == "stream failed" {
							want = red.Pix
						}
						if !bytes.Equal(pixels, want) {
							t.Fatal("stream did not return the requested frame or its failure fallback")
						}
					}
				}
				tex, err := w.uploadImage(img, 1)
				if err != nil || tex == nil {
					t.Fatalf("upload: %v", err)
				}
				if w.uploaded != side*side*4 {
					t.Fatalf("initial upload charged %d bytes, want one %d-byte frame", w.uploaded, side*side*4)
				}
				want := color.NRGBA{G: 255, A: 255}
				if mode == "stream pending" || mode == "stream failed" {
					want = color.NRGBA{R: 255, A: 255}
				}
				checkPixel := func() {
					t.Helper()
					runtime.LockOSThread()
					d := r.Device()
					d.Lock()
					pixels, err := d.Ctx.ReadImage(tex.Resource(), true)
					d.Unlock()
					runtime.UnlockOSThread()
					if err != nil {
						t.Fatal(err)
					}
					if got := pixels.NRGBAAt(0, 0); got != want {
						t.Fatalf("pixel %v, want %v", got, want)
					}
				}
				checkPixel()
				if mode == "stream pending" {
					if w.textures[img.id].shown != 0 || w.wakeAt.IsZero() {
						t.Fatal("pending frame did not retain frame zero and schedule a retry")
					}
					synctest.Wait()
					var pixels []byte
					if !img.data.Stream.WithFrame(1, func(p []byte) { pixels = bytes.Clone(p) }) || !bytes.Equal(pixels, green.Pix) {
						t.Fatal("pending upload did not request and decode frame one")
					}
					w.uploaded = 0
					w.texture(img, 1)
					want = color.NRGBA{G: 255, A: 255}
					checkPixel()
				}
				if w.textures[img.id].shown != 1 {
					t.Fatal("requested frame was not recorded")
				}
			})
		})
	}
}

func textureTestStream(t *testing.T, side int, failed bool) *Image {
	t.Helper()
	const count = 16
	colors := color.Palette{color.RGBA{R: 255, A: 255}, color.RGBA{G: 255, A: 255}}
	g := &gif.GIF{Config: image.Config{ColorModel: colors, Width: side, Height: side}}
	for n := range count {
		frame := image.NewPaletted(image.Rect(0, 0, side, side), colors)
		for i := range frame.Pix {
			frame.Pix[i] = uint8(n % 2)
		}
		g.Image = append(g.Image, frame)
		g.Delay = append(g.Delay, 100)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	if failed {
		// Corrupt the second frame's LZW payload without changing its block
		// lengths: inspection and the first decode still succeed.
		descriptor := []byte{0x2c, 0, 0, 0, 0, byte(side), 0, byte(side), 0, 0}
		first := bytes.Index(data, descriptor)
		if first < 0 {
			t.Fatal("GIF fixture has no first frame descriptor")
		}
		next := bytes.Index(data[first+len(descriptor):], descriptor)
		if next < 0 {
			t.Fatal("GIF fixture has no second frame descriptor")
		}
		second := first + len(descriptor) + next
		data[second+12] = 0xff
		if _, err := gif.DecodeAll(bytes.NewReader(data)); err == nil {
			t.Fatal("corrupt GIF fixture unexpectedly decoded all frames")
		}
	}
	decoded, err := imaging.Decode(data, imaging.Bound{}, count*int64(side*side*4)-1)
	if err != nil || decoded.Stream == nil {
		t.Fatalf("GIF fixture did not produce a streamed animation: %v", err)
	}
	return wrapImage(decoded)
}

func TestIdleTextureBudgetIncludesMips(t *testing.T) {
	r, err := render.NewOffscreen(8, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	w := &Window{r: r, textures: map[uint64]*texEntry{}}
	defer w.releaseTextures()
	img := NewImage(image.NewRGBA(image.Rect(0, 0, 4, 4)))
	tex := w.texture(img, 0)
	if tex == nil {
		t.Fatal("upload failed")
	}
	if err := tex.EnsureMips(r.Device()); err != nil {
		t.Fatal(err)
	}
	w.sweepTextures(1, 4*4*4)
	if w.textures[img.id] != nil {
		t.Fatal("idle mip chain was charged only for its base level")
	}
	runtime.KeepAlive(img)
}

func TestImageWorkPrioritizesDeferredJobs(t *testing.T) {
	w := &Window{needPaint: true}
	a, b, c := imageWorkKey{1, imageUpdate}, imageWorkKey{2, imageMips}, imageWorkKey{3, imageUpload}
	w.imageWorkEpoch = 1
	if !w.allowImageWork(a, 40<<20) || w.allowImageWork(b, 16<<20) {
		t.Fatal("large first job or deferred mip budget failed")
	}
	w.uploaded = 0
	w.imageWorkEpoch++
	if w.allowImageWork(a, 16<<20) || !w.allowImageWork(b, 16<<20) || w.allowImageWork(c, 16<<20) {
		t.Fatal("older deferred work lost priority")
	}
	w.finishImageWork()
	w.uploaded = 0
	w.imageWorkEpoch++
	if !w.allowImageWork(a, 16<<20) || !w.allowImageWork(c, 16<<20) {
		t.Fatal("deferred jobs did not resume in order")
	}
	w.finishImageWork()
	if len(w.imageWork) != 0 {
		t.Fatal("completed work stayed queued")
	}
	w.uploaded = maxUploadBytesPerFrame
	if w.allowImageWork(b, 1) {
		t.Fatal("work exceeded full budget")
	}
	w.imageWorkEpoch++
	w.finishImageWork()
	if len(w.imageWork) != 0 {
		t.Fatal("offscreen job blocked future work")
	}
	w.capture = &offscreenCapture{}
	if !w.allowImageWork(a, 64<<20) {
		t.Fatal("capture was delayed by frame work budget")
	}
	repaint := &Window{needPaint: true, uploaded: maxUploadBytesPerFrame}
	repaint.paintScene()
	if repaint.allowImageWork(a, 1) {
		t.Fatal("repainting reset the current frame's image budget")
	}
}

func TestWindowBoundsAndSharesMipWork(t *testing.T) {
	r, err := render.NewOffscreen(8, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	w1 := &Window{r: r, textures: map[uint64]*texEntry{}}
	w2 := &Window{r: r, textures: map[uint64]*texEntry{}}
	defer w1.releaseTextures()
	defer w2.releaseTextures()
	img := NewImage(image.NewRGBA(image.Rect(0, 0, 128, 128)))
	tex := w1.texture(img, 0)
	if tex == nil || w2.texture(img, 0) != tex {
		t.Fatal("texture was not shared")
	}
	w1.uploaded = maxUploadBytesPerFrame
	if err := w1.promoteImageMips(img.id, tex); err != nil {
		t.Fatal(err)
	}
	if !tex.CanMip() {
		t.Fatal("mip promotion exceeded frame budget")
	}
	w1.uploaded = 0
	if err := w1.promoteImageMips(img.id, tex); err != nil {
		t.Fatal(err)
	}
	if tex.CanMip() || w1.uploaded != tex.ByteSize() {
		t.Fatal("mip work did not resume or was mischarged")
	}
	w2.uploaded = 0
	if err := w2.promoteImageMips(img.id, tex); err != nil {
		t.Fatal(err)
	}
	if w2.uploaded != 0 {
		t.Fatal("another window was charged for existing mips")
	}
}

func TestWindowsShareStillTexturesAndBoundUploads(t *testing.T) {
	r, err := render.NewOffscreen(8, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	w1 := &Window{r: r, textures: map[uint64]*texEntry{}}
	w2 := &Window{r: r, textures: map[uint64]*texEntry{}}
	shared := NewImage(image.NewNRGBA(image.Rect(0, 0, 64, 64)))
	if a, b := w1.texture(shared, 0), w2.texture(shared, 0); a == nil || a != b {
		t.Fatal("two windows uploaded the same still image twice")
	}
	w1.releaseTextures()
	if w2.textures[shared.id].tex.Resource().P == nil {
		t.Fatal("one window released a texture another still draws")
	}
	w2.releaseTextures()
	if stillTextureHeld(shared) {
		t.Fatal("the shared texture was kept after every window released it")
	}

	// 16 MiB each: two fit in a frame's upload budget, the third waits.
	w1.uploaded = 0
	big := func() *Image { return NewImage(image.NewRGBA(image.Rect(0, 0, 2048, 2048))) }
	a, b, c := big(), big(), big()
	if w1.texture(a, 0) == nil || w1.texture(b, 0) == nil {
		t.Fatal("uploads within the frame budget were deferred")
	}
	if w1.texture(c, 0) != nil || w1.wakeAt.IsZero() {
		t.Fatal("an upload past the frame budget did not wait for another frame")
	}
	w1.uploaded = 0 // the next frame
	if w1.texture(c, 0) == nil {
		t.Fatal("a deferred upload did not happen in the next frame")
	}
	w1.releaseTextures()
}

func TestRenderImageMipmapsReducedImages(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 512, 512))
	for y := range 512 {
		for x := range 512 {
			v := uint8(255 * ((x + y) % 2))
			src.SetNRGBA(x, y, color.NRGBA{v, v, v, 255})
		}
	}
	checker := NewImage(src)
	img, err := RenderImage(ImageEl(checker).Fit(FitFill).Size(60, 60), 60, 60, 1)
	if err != nil {
		t.Fatal(err)
	}
	for y := range 60 {
		for x := range 60 {
			if c := img.NRGBAAt(x, y); c.R < 100 || c.R > 156 {
				t.Fatalf("pixel (%d, %d) = %v; a 1-pixel checkerboard reduced 8.5 times should be gray", x, y, c)
			}
		}
	}
	if stillTextureHeld(checker) {
		t.Fatal("the capture kept its shared texture")
	}
}

func stillTextureHeld(img *Image) bool {
	stillTextures.Lock()
	defer stillTextures.Unlock()
	for k := range stillTextures.m {
		if k.id == img.id {
			return true
		}
	}
	return false
}
