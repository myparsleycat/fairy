package imaging

import (
	"bytes"
	"encoding/hex"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"runtime"
	"testing"

	"github.com/myparsleycat/fairy/internal/win"
)

func TestWICDecodeMatchesGoDecoders(t *testing.T) {
	nrgba := image.NewNRGBA(image.Rect(0, 0, 97, 65))
	nrgba64 := image.NewNRGBA64(nrgba.Bounds())
	for y := range 65 {
		for x := range 97 {
			nrgba.SetNRGBA(x, y, color.NRGBA{uint8(x * 17), uint8(y * 13), 51, uint8(x*9 + y*5)})
			nrgba64.SetNRGBA64(x, y, color.NRGBA64{uint16(x * 617), uint16(y * 997), 12345, uint16(x*193 + y*311)})
		}
	}
	// Taller than one band of rows that decodeWIC copies at a time.
	tall := image.NewNRGBA(image.Rect(0, 0, 600, 1000))
	for i := range tall.Pix {
		tall.Pix[i] = uint8(i * 7 / 5)
		if i%4 == 3 {
			tall.Pix[i] = 255
		}
	}
	paletted := image.NewPaletted(image.Rect(0, 0, 40, 30), color.Palette{
		color.NRGBA{255, 0, 0, 255}, color.NRGBA{0, 0, 255, 96}, color.NRGBA{}})
	for i := range paletted.Pix {
		paletted.Pix[i] = uint8(i % 3)
	}
	encode := func(img image.Image, enc func(*bytes.Buffer, image.Image) error) []byte {
		var buf bytes.Buffer
		if err := enc(&buf, img); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	pngEnc := func(b *bytes.Buffer, img image.Image) error { return png.Encode(b, img) }
	jpegEnc := func(b *bytes.Buffer, img image.Image) error { return jpeg.Encode(b, img, nil) }
	progressive, err := os.ReadFile(imageFixturePath("progressive.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	gray := image.NewGray(nrgba.Rect)
	for i := range gray.Pix {
		gray.Pix[i] = uint8(i)
	}
	for _, tc := range []struct {
		name     string
		data     []byte
		lossless bool // 8 bits per channel and no resampling: Go's pixels exactly
	}{
		{"png alpha", encode(nrgba, pngEnc), true},
		{"png 16 bit", encode(nrgba64, pngEnc), false},
		{"png tall", encode(tall, pngEnc), true},
		{"png palette alpha", encode(paletted, pngEnc), true},
		{"jpeg", encode(nrgba, jpegEnc), false},
		{"jpeg gray", encode(gray, jpegEnc), false},
		{"progressive jpeg", progressive, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, err := Inspect(tc.data)
			if err != nil {
				t.Fatal(err)
			}
			for _, bound := range []Bound{{}, {W: 31, H: 31}, {W: 31, H: 31, Cover: true}} {
				got := DecodeNative(in, bound)
				if got == nil {
					t.Fatalf("bound %+v: WIC did not decode the image", bound)
				}
				want := referenceStill(t, tc.data, bound)
				if got.W != want.W || got.H != want.H {
					t.Fatalf("bound %+v: WIC decoded %dx%d, want %dx%d", bound, got.W, got.H, want.W, want.H)
				}
				if bound == (Bound{}) && tc.lossless {
					if !bytes.Equal(got.Pixels, want.Pixels) {
						t.Fatalf("natural size: pixels differ from Go's decoder")
					}
					continue
				}
				// Swapping red and blue scores about 11 dB here. The
				// sawtooth test patterns make chroma upsampling differ
				// more than in photographs, which score above 50 dB.
				if p := compositePSNR(got.Pixels, want.Pixels); p < 20 {
					t.Fatalf("bound %+v: PSNR against Go's decode is %.1f dB", bound, p)
				}
			}
		})
	}
}

func TestWICDecodeJPEGAppliesOrientation(t *testing.T) {
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
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, nil); err != nil {
		t.Fatal(err)
	}
	// A JPEG with EXIF Orientation=6 (rotate 90 degrees clockwise).
	exif := []byte{
		0xff, 0xe1, 0, 34, 'E', 'x', 'i', 'f', 0, 0,
		'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 1, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0,
	}
	data := append(append(append([]byte(nil), buf.Bytes()[:2]...), exif...), buf.Bytes()[2:]...)
	in, err := Inspect(data)
	if err != nil {
		t.Fatal(err)
	}
	bound := Bound{W: 96, H: 96} // small enough to decode at half scale first
	if _, _, ok := jpegDCTSize(517, 389, 96, 72); !ok {
		t.Fatal("the bound does not exercise the DCT-scaled decode")
	}
	img, usedNative, err := in.DecodePixels(bound, 0, 0, false, DecodeNative, nil)
	if err != nil || !usedNative {
		t.Fatalf("WIC did not decode the eligible JPEG: %v", err)
	}
	if w, h := bound.Size(389, 517); img.W != w || img.H != h {
		t.Fatalf("WIC decoded %dx%d, want %dx%d", img.W, img.H, w, h)
	}
	pixel := func(x, y int) []byte {
		i := (y*img.W + x) * 4
		return img.Pixels[i : i+4]
	}
	// The red top turns to the right.
	left, right := pixel(12, 12), pixel(img.W-12, 12)
	if right[0] < 220 || left[2] < 220 || right[3] != 255 || left[3] != 255 {
		t.Fatalf("left = %v, right = %v; want opaque blue left of opaque red", left, right)
	}

	t.Run("COM apartment fallback", func(t *testing.T) {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := win.OleInitialize(); err != nil {
			t.Fatal(err)
		}
		defer win.OleUninitialize()
		if DecodeNative(in, bound) != nil {
			t.Fatal("decoded in a single-threaded apartment instead of leaving it to Go")
		}
	})
}

func TestWICLeavesIncompleteAndOtherImagesToGo(t *testing.T) {
	var jb, pb bytes.Buffer
	src := image.NewNRGBA(image.Rect(0, 0, 40, 30))
	if err := jpeg.Encode(&jb, src, nil); err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(&pb, src); err != nil {
		t.Fatal(err)
	}
	webp, err := os.ReadFile(imageFixturePath("lossy.webp"))
	if err != nil {
		t.Fatal(err)
	}
	camera, cut := cameraJPEG(t)
	for name, data := range map[string][]byte{
		"truncated jpeg":        jb.Bytes()[:jb.Len()-4],
		"truncated camera jpeg": cut,
		"truncated png":         pb.Bytes()[:pb.Len()-20],
		"webp":                  webp,
	} {
		in, err := Inspect(data)
		if err != nil {
			t.Fatal(err)
		}
		if wicDecoderClass(in) != nil {
			t.Errorf("%s: WIC would decode it", name)
		}
	}
	if in, err := Inspect(camera); err != nil || wicDecoderClass(in) == nil {
		t.Errorf("WIC does not take a complete camera jpeg: %v", err)
	}
}

func TestWICDecodesAVIFWithItsMatrixCoefficients(t *testing.T) {
	data, err := hex.DecodeString(heifMatrixProbe)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Inspect(data)
	if err != nil || in.Format != "avif" || !in.avif.bt601 || wicDecoderClass(in) == nil {
		t.Fatalf("inspected %q %+v, %v", in.Format, in.avif, err)
	}
	// Without the HEIF Image Extensions the image is left to Go, which is
	// fine; what WIC returns must have the colors of BT.601 coefficients.
	img := DecodeNative(in, Bound{})
	if img == nil {
		return
	}
	for c, want := range [3]int{211, 68, 60} {
		if d := int(img.Pixels[c]) - want; d < -3 || d > 3 {
			t.Fatalf("decoded %v, want 211 68 60", img.Pixels[:4])
		}
	}
	if _, ok := inspectAVIF(data[:len(data)-1]); ok {
		t.Error("truncated AVIF inspected")
	}
}
