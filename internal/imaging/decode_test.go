package imaging

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"testing"

	xdraw "golang.org/x/image/draw"
)

func TestImageDecodeKeepsStillPixels(t *testing.T) {
	nrgba := image.NewNRGBA(image.Rect(0, 0, 97, 65))
	nrgba64 := image.NewNRGBA64(nrgba.Bounds())
	for y := range 65 {
		for x := range 97 {
			nrgba.SetNRGBA(x, y, color.NRGBA{uint8(x * 17), uint8(y * 13), 51, uint8(x*9 + y*5)})
			nrgba64.SetNRGBA64(x, y, color.NRGBA64{uint16(x * 617), uint16(y * 997), 12345, uint16(x*193 + y*311)})
		}
	}
	images := map[string][]byte{}
	for _, tc := range []struct {
		name string
		img  image.Image
	}{
		{"png alpha", nrgba},
		{"png 16 bit", nrgba64},
	} {
		var buf bytes.Buffer
		if err := png.Encode(&buf, tc.img); err != nil {
			t.Fatal(err)
		}
		images[tc.name] = buf.Bytes()
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, nrgba, nil); err != nil {
		t.Fatal(err)
	}
	images["jpeg"] = buf.Bytes()
	for _, name := range []string{"progressive.jpg", "lossy.webp", "lossless.webp", "alpha.webp"} {
		data, err := os.ReadFile(imageFixturePath(name))
		if err != nil {
			t.Fatal(err)
		}
		images[name] = data
	}
	images["registered"] = cacheTestImage(t, func() (image.Image, error) { return nrgba64, nil })
	for name, data := range images {
		t.Run(name, func(t *testing.T) {
			for _, bound := range []Bound{{}, {W: 31, H: 31}, {W: 31, H: 31, Cover: true}} {
				in, err := Inspect(data)
				if err != nil {
					t.Fatal(err)
				}
				got, err := in.decode(bound, maxAnimBytes, maxAnimBytes)
				if err != nil {
					t.Fatal(err)
				}
				want := referenceStill(t, data, bound)
				if got.W != want.W || got.H != want.H {
					t.Fatalf("bound %+v: decoded %dx%d, want %dx%d", bound, got.W, got.H, want.W, want.H)
				}
				if bound == (Bound{}) {
					if !bytes.Equal(got.Pixels, want.Pixels) {
						t.Fatal("natural size: pixels differ from Go's decoder")
					}
				} else if p := compositePSNR(got.Pixels, want.Pixels); p < 40 {
					t.Fatalf("bound %+v: PSNR against x/image/draw is %.1f dB", bound, p)
				}
			}
		})
	}
}

func referenceStill(t *testing.T, data []byte, bound Bound) *Bitmap {
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
	return New(src)
}

// compositePSNR compares premultiplied pixels composited over mid gray.
func compositePSNR(a, b []byte) float64 {
	var se float64
	for i := 0; i < len(a); i += 4 {
		for c := range 3 {
			ca := float64(a[i+c]) + 128*(255-float64(a[i+3]))/255
			cb := float64(b[i+c]) + 128*(255-float64(b[i+3]))/255
			se += (ca - cb) * (ca - cb)
		}
	}
	if se == 0 {
		return math.Inf(1)
	}
	return 10 * math.Log10(255*255/(se/float64(len(a)/4*3)))
}

// straightAt returns the premultiplied pixel at byte offset i with straight alpha.
func straightAt(pix []byte, i int) color.NRGBA {
	var p [4]byte
	UnpremultiplyRGBA(p[:], pix[i:i+4])
	return color.NRGBA{p[0], p[1], p[2], p[3]}
}

func TestImageDecodeReservationIncludesOrientation(t *testing.T) {
	in, err := Inspect(pngBytes(t, 2, 1))
	if err != nil {
		t.Fatal(err)
	}
	if !wicAccepts(in) {
		t.Skip("the WIC decode path needs WIC")
	}
	in.meta.orient = 6
	in.Config.Width, in.Config.Height = 1, 2
	raw := New(image.NewRGBA(image.Rect(0, 0, 2, 1)))
	_, _, err = in.DecodePixels(Bound{}, 0, 0, false,
		func(Input, Bound) *Bitmap { return raw },
		func(_ int64, f func()) bool {
			f()
			if raw.W != 1 || raw.H != 2 {
				t.Error("decode reservation ended before orientation completed")
			}
			return true
		})
	if err != nil {
		t.Fatal(err)
	}
}
