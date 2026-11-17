package imagecache

import (
	"bytes"
	"image"
	"image/color/palette"
	"testing"

	xdraw "golang.org/x/image/draw"

	"github.com/myparsleycat/fairy/internal/imaging"
)

// Fit is fairy.Fit, for tests of Sized.
type Fit uint8

const (
	FitCover Fit = iota
	FitContain
	FitFill
)

// sizedBound is the bound fairy.ImageCache.Sized asks for at a scale of 1.
func sizedBound(w, h float32, fit Fit) imaging.Bound {
	return imaging.Bound{W: SizeStep(w), H: SizeStep(h), Cover: fit != FitContain}
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
	return NewImage(imaging.New(src))
}

func solidPaletted(r image.Rectangle, idx uint8) *image.Paletted {
	p := image.NewPaletted(r, palette.Plan9)
	for i := range p.Pix {
		p.Pix[i] = idx
	}
	return p
}
