package fairy

import (
	"bytes"
	"image"
	"testing"

	xdraw "golang.org/x/image/draw"

	"github.com/myparsleycat/fairy/internal/imaging"
)

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
