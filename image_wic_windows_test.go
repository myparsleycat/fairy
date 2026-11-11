package fairy

import (
	"bytes"
	"encoding/hex"
	"image"
	"image/jpeg"
	"testing"

	"github.com/myparsleycat/fairy/internal/imaging"
)

const testAVIF = "00000020667479706176696600000000617669666d6966316d6961664d413141000000f9" +
	"6d657461000000000000002f68646c7200000000000000007069637400000000000000000000000050696374757265" +
	"48616e646c6572000000000e7069746d0000000000010000001e696c6f6300000000440000010001000000010000" +
	"01210000001e0000002869696e660000000000010000001a696e6665020000000001000061763031436f6c6f7200" +
	"0000006a697072700000004b6970636f0000001469737065000000000000000800000008000000107069786900000000" +
	"030808080000000c617631438120000000000013636f6c726e636c78000200020006800000001769706d6100000000" +
	"0000000100010401028304000000266d6461740a083808bf6d020206903212100000000ffa4aaf931ea7557f3f6bb6" +
	"4740"

func TestImageCacheWithoutNativeDecodersUsesGo(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 61, 47))
	for i := range src.Pix {
		src.Pix[i] = uint8(i * 37 / 7)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, nil); err != nil {
		t.Fatal(err)
	}
	want := referenceStill(t, buf.Bytes(), imaging.Bound{})
	c := NewImageCache(1<<20, 1)
	c.wic = func(in imaging.Input, bound imaging.Bound) *imaging.Bitmap {
		w, h := bound.Size(in.Config.Width, in.Config.Height)
		return imaging.New(image.NewRGBA(image.Rect(0, 0, w, h)))
	}
	c.SetNativeDecoders(false)
	img := waitImage(t, c, "jpeg", 61, 47, buf.Bytes())
	if !bytes.Equal(img.data.Pixels, want.data.Pixels) {
		t.Fatal("the cache decoded with WIC")
	}
	lazy := waitImage(t, c, "lazy", 0, 0, buf.Bytes())
	if pix, err := lazy.NRGBA(); err != nil || !bytes.Equal(NewImage(pix).data.Pixels, want.data.Pixels) {
		t.Fatalf("NRGBA of a lazy image decoded with WIC: %v", err)
	}
}

func TestLazyImagePixelReadUsesCacheDecoder(t *testing.T) {
	const want = 211
	avif, err := hex.DecodeString(testAVIF)
	if err != nil {
		t.Fatal(err)
	}
	c := NewImageCache(1<<20, 1)
	c.wic = func(in imaging.Input, bound imaging.Bound) *imaging.Bitmap {
		if in.Format != "avif" {
			return nil
		}
		w, h := bound.Size(in.Config.Width, in.Config.Height)
		n := image.NewNRGBA(image.Rect(0, 0, w, h))
		for i := 0; i < len(n.Pix); i += 4 {
			n.Pix[i], n.Pix[i+3] = want, 255
		}
		return imaging.New(n)
	}
	img := waitImage(t, c, "avif", 0, 0, avif)
	pix, err := img.NRGBA()
	if err != nil {
		t.Fatal(err)
	}
	if pix.Bounds() != image.Rect(0, 0, 8, 8) || pix.Pix[0] != want {
		t.Fatalf("AVIF pixels = %v, red=%d", pix.Bounds(), pix.Pix[0])
	}
	blur := img.Blurred(1)
	if blur == nil || blur.data.Pixels[0] != want {
		t.Fatal("Blurred did not use the native AVIF pixels")
	}
	png := waitImage(t, c, "png", 0, 0, pngBytes(t, 4, 3))
	if pix, err := png.NRGBA(); err != nil || pix.Bounds() != image.Rect(0, 0, 4, 3) {
		t.Fatalf("Go fallback: pixels=%v, error=%v", pix, err)
	}
}
