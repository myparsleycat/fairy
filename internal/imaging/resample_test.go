package imaging

import (
	"image"
	"image/color"
	"math/rand/v2"
	"testing"

	xdraw "golang.org/x/image/draw"
)

func TestResamplerMatchesCatmullRom(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	full := image.NewNRGBA(image.Rect(0, 0, 230, 170))
	for i := 0; i < len(full.Pix); i += 4 {
		x, y := i/4%230, i/4/230
		// Smooth color with noise, and fully transparent pixels whose color
		// must not bleed into their neighbors.
		c := color.NRGBA{uint8(x + r.IntN(24)), uint8(y + r.IntN(24)), uint8(x*y/80 + r.IntN(24)), uint8(64 + r.IntN(192))}
		if (x/7+y/5)%4 == 0 {
			c = color.NRGBA{255, 0, 255, 0}
		}
		full.SetNRGBA(x, y, c)
	}
	src := full.SubImage(image.Rect(9, 7, 220, 164)) // 211 x 157, off the origin
	for _, size := range []image.Point{{211, 40}, {50, 157}, {97, 73}, {13, 9}, {1, 1}, {3, 150}} {
		got := newResampler(211, 157, size.X, size.Y).scale(src)
		want := image.NewRGBA(image.Rect(0, 0, size.X, size.Y))
		xdraw.CatmullRom.Scale(want, want.Rect, src, src.Bounds(), xdraw.Src, nil)
		if p := compositePSNR(got.Pix, want.Pix); p < 45 {
			t.Errorf("%v: PSNR against x/image/draw is %.1f dB", size, p)
		}
	}

	flat := image.NewNRGBA(image.Rect(0, 0, 64, 48))
	for i := 0; i < len(flat.Pix); i += 4 {
		copy(flat.Pix[i:], []byte{200, 120, 40, 90})
	}
	got := newResampler(64, 48, 23, 17).scale(flat)
	want := make([]byte, 4)
	premultiplyRGBA(want, flat.Pix[:4])
	for i := 0; i < len(got.Pix); i += 4 {
		if p := got.Pix[i : i+4]; string(p) != string(want) {
			t.Fatalf("flat color became %v, want %v", p, want)
		}
	}
}
