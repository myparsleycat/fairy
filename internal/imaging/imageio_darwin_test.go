package imaging

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/hex"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"runtime"
	"sync"
	"testing"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/darwin"
)

// goDecode decodes in with the Go decoders only, finished as shown.
func goDecode(t testing.TB, in Input, bound Bound) *Bitmap {
	t.Helper()
	img, native, err := in.DecodePixels(bound, 0, 0, false, nil, nil)
	if err != nil || native || img == nil {
		t.Fatalf("Go decode: %v", err)
	}
	return img
}

// nativeDecode decodes in through ImageIO, finished as shown.
func nativeDecode(t testing.TB, in Input, bound Bound) *Bitmap {
	t.Helper()
	img, native, err := in.DecodePixels(bound, 0, 0, false, DecodeNative, nil)
	if err != nil || !native || img == nil {
		t.Fatalf("ImageIO did not decode the image: %v", err)
	}
	return img
}

// pixelDiff returns the largest and the mean difference of two images'
// channels.
func pixelDiff(a, b []byte) (int, float64) {
	most, sum := 0, 0
	for i := range a {
		d := int(a[i]) - int(b[i])
		if d < 0 {
			d = -d
		}
		most, sum = max(most, d), sum+d
	}
	return most, float64(sum) / float64(len(a))
}

// grayAlphaPNG encodes 8-bit gray with alpha, which image/png does not
// write.
func grayAlphaPNG(t *testing.T, w, h int, at func(x, y int) (uint8, uint8)) []byte {
	var raw bytes.Buffer
	for y := range h {
		raw.WriteByte(0)
		for x := range w {
			g, a := at(x, y)
			raw.Write([]byte{g, a})
		}
	}
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	zw.Write(raw.Bytes())
	zw.Close()
	buf := []byte("\x89PNG\r\n\x1a\n")
	add := func(typ string, body []byte) {
		c := binary.BigEndian.AppendUint32(nil, uint32(len(body)))
		c = append(append(c, typ...), body...)
		buf = append(buf, binary.BigEndian.AppendUint32(c, crc32.ChecksumIEEE(c[4:]))...)
	}
	ihdr := binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(nil, uint32(w)), uint32(h))
	add("IHDR", append(ihdr, 8, 4, 0, 0, 0))
	add("IDAT", z.Bytes())
	add("IEND", nil)
	data := buf
	if c, err := png.DecodeConfig(bytes.NewReader(data)); err != nil || c.ColorModel != color.NRGBAModel {
		t.Fatalf("gray alpha PNG: %v", err)
	}
	return data
}

func encodeJPEG(t testing.TB, img image.Image, quality int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func encodePNG(t testing.TB, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestImageIODecodeMatchesGoDecoders(t *testing.T) {
	// Smooth images, which decode within a few steps of the Go decoders,
	// and the sawtooth patterns of the other platforms' tests, on which
	// ImageIO's smoothing of subsampled chroma differs from Go's
	// replication by far more.
	smooth := image.NewNRGBA(image.Rect(0, 0, 400, 300))
	smoothAlpha := image.NewNRGBA(smooth.Rect)
	smooth64 := image.NewNRGBA64(smooth.Rect)
	smoothGray := image.NewGray(smooth.Rect)
	for y := range 300 {
		for x := range 400 {
			smooth.SetNRGBA(x, y, color.NRGBA{uint8(x / 2), uint8(y * 2 / 3), uint8(128 + x/4 - y/4), 255})
			smoothAlpha.SetNRGBA(x, y, color.NRGBA{uint8(x / 2), uint8(y * 2 / 3), 51, uint8(x * 255 / 400)})
			smooth64.SetNRGBA64(x, y, color.NRGBA64{uint16(x * 163), uint16(y * 218), 12345, uint16(65535 - x*100)})
			smoothGray.SetGray(x, y, color.Gray{uint8((x + y) * 255 / 700)})
		}
	}
	sawtooth := image.NewNRGBA(image.Rect(0, 0, 97, 65))
	for y := range 65 {
		for x := range 97 {
			sawtooth.SetNRGBA(x, y, color.NRGBA{uint8(x * 17), uint8(y * 13), 51, uint8(x*9 + y*5)})
		}
	}
	paletted := image.NewPaletted(image.Rect(0, 0, 40, 30), color.Palette{
		color.NRGBA{255, 0, 0, 255}, color.NRGBA{0, 0, 255, 96}, color.NRGBA{}})
	for i := range paletted.Pix {
		paletted.Pix[i] = uint8(i % 3)
	}
	progressive, err := os.ReadFile(imageFixturePath("progressive.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		data []byte
		// The most a channel of the unscaled image may differ from Go's,
		// or -1 to compare only the PSNR; and the least PSNR.
		most int
		psnr float64
	}{
		{"jpeg", encodeJPEG(t, smooth, 95), 4, 40},
		{"jpeg gray", encodeJPEG(t, smoothGray, 95), 3, 40},
		{"progressive jpeg", progressive, -1, 20},
		{"png", encodePNG(t, smooth), 0, 40},
		{"png alpha", encodePNG(t, smoothAlpha), 1, 40},
		{"png alpha sawtooth", encodePNG(t, sawtooth), 1, 20},
		{"png 16 bit", encodePNG(t, smooth64), 2, 40}, // rounded rather than truncated to 8 bits
		{"png gray", encodePNG(t, smoothGray), 0, 40},
		{"png gray alpha", grayAlphaPNG(t, 400, 300, func(x, y int) (uint8, uint8) {
			return uint8(x * 255 / 400), uint8(255 - y*255/300)
		}), 1, 40},
		{"png palette alpha", encodePNG(t, paletted), 1, 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, err := Inspect(tc.data)
			if err != nil {
				t.Fatal(err)
			}
			if !nativeAccepts(in) {
				t.Fatal("ImageIO does not take the image")
			}
			for _, bound := range []Bound{{}, {W: 31, H: 31}, {W: 31, H: 31, Cover: true}, {W: 150, H: 150}} {
				got, want := nativeDecode(t, in, bound), goDecode(t, in, bound)
				if got.W != want.W || got.H != want.H {
					t.Fatalf("bound %+v: decoded %dx%d, want %dx%d", bound, got.W, got.H, want.W, want.H)
				}
				most, mean := pixelDiff(got.Pixels, want.Pixels)
				if bound == (Bound{}) && tc.most >= 0 && (most > tc.most || mean > 1) {
					t.Fatalf("natural size: channels differ from Go's by up to %d, %.2f on average", most, mean)
				}
				if p := compositePSNR(got.Pixels, want.Pixels); p < tc.psnr {
					t.Fatalf("bound %+v: PSNR against Go's decode is %.1f dB", bound, p)
				}
				for i := 0; i < len(got.Pixels); i += 4 {
					if a := got.Pixels[i+3]; got.Pixels[i] > a || got.Pixels[i+1] > a || got.Pixels[i+2] > a {
						t.Fatalf("bound %+v: pixel %v is not premultiplied", bound, got.Pixels[i:i+4])
					}
				}
			}
		})
	}
}

func TestImageIODecodeJPEGLeavesOrientationToFinish(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 517, 389))
	for y := range 389 {
		for x := range 517 {
			c := color.NRGBA{R: 240, A: 255}
			if y >= 194 {
				c = color.NRGBA{B: 240, A: 255}
			}
			src.SetNRGBA(x, y, c)
		}
	}
	// EXIF Orientation 6: rotate 90 degrees clockwise.
	data := withJPEGSegment(encodeJPEG(t, src, 90), 0xe1, append([]byte("Exif\x00\x00"), exifTIFF(6)...))
	in, err := Inspect(data)
	if err != nil {
		t.Fatal(err)
	}
	bound := Bound{W: 96, H: 96} // small enough to decode at a DCT scale first
	if _, _, ok := jpegDCTSize(517, 389, 96, 72); !ok {
		t.Fatal("the bound does not exercise the DCT-scaled decode")
	}
	// ImageIO's pixels keep the coded orientation: red on top.
	raw := DecodeNative(in, bound)
	if raw == nil || raw.W != 96 || raw.H != 72 {
		t.Fatalf("ImageIO decoded %+v, want 96x72 before rotation", raw)
	}
	if top, bottom := raw.Pixels[(4*raw.W+48)*4:], raw.Pixels[((raw.H-4)*raw.W+48)*4:]; top[0] < 220 || bottom[2] < 220 {
		t.Fatalf("top = %v, bottom = %v; want red over blue", top[:4], bottom[:4])
	}
	img := nativeDecode(t, in, bound)
	if w, h := bound.Size(389, 517); img.W != w || img.H != h {
		t.Fatalf("decoded %dx%d, want %dx%d", img.W, img.H, w, h)
	}
	pixel := func(x, y int) []byte {
		i := (y*img.W + x) * 4
		return img.Pixels[i : i+4]
	}
	// The red top turns to the right.
	if left, right := pixel(12, 12), pixel(img.W-12, 12); right[0] < 220 || left[2] < 220 || right[3] != 255 || left[3] != 255 {
		t.Fatalf("left = %v, right = %v; want opaque blue left of opaque red", left, right)
	}
	want := goDecode(t, in, bound)
	if p := compositePSNR(img.Pixels, want.Pixels); p < 30 {
		t.Fatalf("PSNR against Go's decode is %.1f dB", p)
	}
}

func TestImageIOLeavesColorConversionToFinish(t *testing.T) {
	// A Display P3 JPEG: ImageIO's pixels keep the P3 values, and finish
	// converts them to sRGB as it does Go's.
	src := image.NewNRGBA(image.Rect(0, 0, 64, 48))
	for i := 0; i < len(src.Pix); i += 4 {
		copy(src.Pix[i:], []byte{200, 100, 50, 255})
	}
	data := withJPEGSegment(encodeJPEG(t, src, 100), 0xe2,
		append([]byte("ICC_PROFILE\x00\x01\x01"), iccProfile(displayP3D50)...))
	in, err := Inspect(data)
	if err != nil || in.meta.icc == nil || in.meta.colorTransform() == nil {
		t.Fatalf("inspected %+v, %v", in.meta, err)
	}
	raw := DecodeNative(in, Bound{})
	if raw == nil {
		t.Fatal("ImageIO did not decode the image")
	}
	goRaw, err := in.decode(Bound{}, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if most, _ := pixelDiff(raw.Pixels, goRaw.Pixels); most > 2 {
		t.Fatalf("ImageIO decoded %v, Go %v: converted colors", raw.Pixels[:4], goRaw.Pixels[:4])
	}
	img, want := nativeDecode(t, in, Bound{}), goDecode(t, in, Bound{})
	if most, _ := pixelDiff(img.Pixels, want.Pixels); most > 2 {
		t.Fatalf("finished %v, want %v", img.Pixels[:4], want.Pixels[:4])
	}
	if d, _ := pixelDiff(img.Pixels[:3], raw.Pixels[:3]); d < 10 {
		t.Fatalf("finish left %v as %v: no conversion to test", raw.Pixels[:4], img.Pixels[:4])
	}
	// So with a reduced decode.
	if got := DecodeNative(in, Bound{W: 8, H: 8}); got == nil || got.W != 8 {
		t.Fatal("ImageIO did not decode the reduced image")
	} else if most, _ := pixelDiff(got.Pixels[:4], raw.Pixels[:4]); most > 2 {
		t.Fatalf("reduced decode %v, full %v", got.Pixels[:4], raw.Pixels[:4])
	}
}

func TestImageIOLeavesIncompleteAndOtherImagesToGo(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 40, 30))
	jb, pb := encodeJPEG(t, src, 75), encodePNG(t, src)
	webp, err := os.ReadFile(imageFixturePath("lossy.webp"))
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"truncated jpeg": jb[:len(jb)-4],
		"truncated png":  pb[:len(pb)-20],
		"webp":           webp,
	} {
		in, err := Inspect(data)
		if err != nil {
			t.Fatal(err)
		}
		if nativeAccepts(in) || DecodeNative(in, Bound{}) != nil {
			t.Errorf("%s: ImageIO would decode it", name)
		}
	}
	// A file whose content ImageIO reads as another format than Go does.
	in, err := Inspect(pb)
	if err != nil {
		t.Fatal(err)
	}
	in.Format = "jpeg"
	in.Config.ColorModel = color.YCbCrModel
	in.data = append([]byte{0xff, 0xd8}, append(pb, 0xff, 0xd9)...)
	if DecodeNative(in, Bound{}) != nil {
		t.Error("decoded an image ImageIO does not read as the format Go does")
	}
}

func TestImageIODecodesAVIFWithItsMatrixCoefficients(t *testing.T) {
	data, err := hex.DecodeString(heifMatrixProbe)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Inspect(data)
	if err != nil || in.Format != "avif" || !in.avif.bt601 || !nativeAccepts(in) {
		t.Fatalf("inspected %q %+v, %v", in.Format, in.avif, err)
	}
	// ImageIO converts with the BT.601 coefficients the file asks for,
	// unlike the HEIF decoder on Windows: 211 68 60, not 223 82 58.
	for _, bound := range []Bound{{}, {W: 4, H: 4}} {
		img := nativeDecode(t, in, bound)
		if w, h := bound.Size(8, 8); img.W != w || img.H != h {
			t.Fatalf("bound %+v: decoded %dx%d", bound, img.W, img.H)
		}
		for i := 0; i < len(img.Pixels); i += 4 {
			for c, want := range [4]int{211, 68, 60, 255} {
				if d := int(img.Pixels[i+c]) - want; d < -3 || d > 3 {
					t.Fatalf("bound %+v: decoded %v, want 211 68 60 255", bound, img.Pixels[i:i+4])
				}
			}
		}
	}
}

func TestImageIOJPEGDecodesAtDCTScale(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 801, 599))
	data := encodeJPEG(t, src, 75)
	for _, tc := range []struct{ dw, dh, w, h int }{
		{100, 74, 200, 149}, // a quarter, rounded down
		{50, 37, 100, 74},   // an eighth
		{200, 149, 400, 299},
		{300, 224, 0, 0}, // not reduced enough
	} {
		var pin runtime.Pinner
		pin.Pin(&data[0])
		cfData := cfDataCreateWithBytesNoCopy(nil, unsafe.Pointer(&data[0]), len(data), kCFAllocatorNull)
		src := cgImageSourceCreateWithData(cfData, 0)
		img := cgImageSourceCreateImageAtIndex(src, 0, 0)
		r := jpegReduced(src, cgImageGetColorSpace(img), 801, 599, tc.dw, tc.dh)
		w, h := 0, 0
		if r != 0 {
			w, h = cgImageGetWidth(r), cgImageGetHeight(r)
			cgImageRelease(r)
		}
		cgImageRelease(img)
		darwin.CFRelease(src)
		darwin.CFRelease(cfData)
		pin.Unpin()
		if w != tc.w || h != tc.h {
			t.Errorf("for %dx%d decoded at %dx%d, want %dx%d", tc.dw, tc.dh, w, h, tc.w, tc.h)
		}
	}
}

func TestImageIODecodesConcurrently(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 640, 480))
	for i := range src.Pix {
		src.Pix[i] = uint8(i * 7 / 5)
	}
	inputs := []Input{}
	for _, data := range [][]byte{encodeJPEG(t, src, 90), encodePNG(t, src)} {
		in, err := Inspect(data)
		if err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, in)
	}
	bounds := []Bound{{}, {W: 100, H: 100}, {W: 333, H: 333}}
	want := map[[2]int][]byte{}
	for i, in := range inputs {
		for j, b := range bounds {
			img := DecodeNative(in, b)
			if img == nil {
				t.Fatal("ImageIO did not decode the image")
			}
			want[[2]int{i, j}] = img.Pixels
		}
	}
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for k := range 12 {
				i, j := (g+k)%len(inputs), (g*k)%len(bounds)
				img := DecodeNative(inputs[i], bounds[j])
				if img == nil || !bytes.Equal(img.Pixels, want[[2]int{i, j}]) {
					t.Errorf("decode %d of input %d at bound %d differs", k, i, j)
					return
				}
			}
		})
	}
	wg.Wait()
}

func TestImageIOWorkCoversReducedDecode(t *testing.T) {
	in, err := Inspect(encodeJPEG(t, image.NewGray(image.Rect(0, 0, 4000, 3000)), 75))
	if err != nil {
		t.Fatal(err)
	}
	small, full := nativeWork(in, Bound{W: 400, H: 400}), nativeWork(in, Bound{})
	if want := int64(400*300*4 + 1000*750*4); small < want || small >= full {
		t.Fatalf("work for 400 px is %d, for full size %d", small, full)
	}
}

// benchJPEG is a 4000 x 3000 photo-like JPEG.
var benchJPEG = sync.OnceValue(func() []byte {
	src := image.NewRGBA(image.Rect(0, 0, 4000, 3000))
	for y := range 3000 {
		for x := range 4000 {
			i := src.PixOffset(x, y)
			src.Pix[i], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3] =
				uint8(x/16+y/24), uint8((x^y)>>4), uint8(y/12), 255
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 85}); err != nil {
		panic(err)
	}
	return buf.Bytes()
})

func benchmarkDecode(b *testing.B, native func(Input, Bound) *Bitmap) {
	in, err := Inspect(benchJPEG())
	if err != nil {
		b.Fatal(err)
	}
	bound := Bound{W: 400, H: 400}
	b.ResetTimer()
	for b.Loop() {
		img, used, err := in.DecodePixels(bound, 0, 0, false, native, nil)
		if err != nil || used != (native != nil) || img.W != 400 || img.H != 300 {
			b.Fatalf("decoded %v, native %v: %v", img, used, err)
		}
	}
}

func BenchmarkDecodeJPEGTo400ImageIO(b *testing.B) { benchmarkDecode(b, DecodeNative) }
func BenchmarkDecodeJPEGTo400Go(b *testing.B)      { benchmarkDecode(b, nil) }
