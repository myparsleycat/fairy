package fairy

import (
	"bytes"
	"encoding/hex"
	"image"
	"image/jpeg"
	"sync/atomic"
	"testing"
	"time"

	"github.com/myparsleycat/fairy/internal/imaging"
)

// heifProbe is an 8 x 8 AVIF of RGB 211 68 60, which only ImageIO decodes
// here: no Go AVIF decoder is registered.
const heifProbe = "00000020667479706176696600000000617669666d6966316d6961664d413141000000f9" +
	"6d657461000000000000002f68646c7200000000000000007069637400000000000000000000000050696374757265" +
	"48616e646c6572000000000e7069746d0000000000010000001e696c6f6300000000440000010001000000010000" +
	"01210000001e0000002869696e660000000000010000001a696e6665020000000001000061763031436f6c6f7200" +
	"0000006a697072700000004b6970636f0000001469737065000000000000000800000008000000107069786900000000" +
	"030808080000000c617631438120000000000013636f6c726e636c78000200020006800000001769706d6100000000" +
	"0000000100010401028304000000266d6461740a083808bf6d020206903212100000000ffa4aaf931ea7557f3f6bb6" +
	"4740"

func TestImageCacheDecodesWithImageIO(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 61, 47))
	for i := range src.Pix {
		src.Pix[i] = uint8(i * 37 / 7)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, nil); err != nil {
		t.Fatal(err)
	}
	c := NewImageCache(1<<20, 1)
	var calls, decoded atomic.Int32
	c.native = func(in imaging.Input, bound imaging.Bound) *imaging.Bitmap {
		calls.Add(1)
		img := imaging.DecodeNative(in, bound)
		if img != nil {
			decoded.Add(1)
		}
		return img
	}
	img := waitImage(t, c, "jpeg", 31, 31, buf.Bytes())
	if calls.Load() != 1 || decoded.Load() != 1 {
		t.Fatalf("ImageIO ran %d times and decoded %d images, want 1", calls.Load(), decoded.Load())
	}
	in, err := imaging.Inspect(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	want, used, err := in.DecodePixels(imaging.Bound{W: 31, H: 31}, 0, 0, false, imaging.DecodeNative, nil)
	if err != nil || !used || !bytes.Equal(img.data.Pixels, want.Pixels) {
		t.Fatalf("the cache's pixels differ from ImageIO's (used %v, %v)", used, err)
	}

	// By default the cache decodes AVIF, which only ImageIO decodes here,
	// and the lazy image's pixels come from it too.
	avif, err := hex.DecodeString(heifProbe)
	if err != nil {
		t.Fatal(err)
	}
	c = NewImageCache(1<<20, 1)
	for _, name := range []string{"avif", "lazy"} {
		w, h := 8, 8
		if name == "lazy" {
			w, h = 0, 0
		}
		img := waitImage(t, c, name, w, h, avif)
		pix, err := img.NRGBA()
		if err != nil {
			t.Fatal(err)
		}
		if pix.Bounds() != image.Rect(0, 0, 8, 8) || pix.Pix[0] < 208 || pix.Pix[0] > 214 {
			t.Fatalf("%s: AVIF pixels %v, red %d; want 211", name, pix.Bounds(), pix.Pix[0])
		}
	}
	// Without native decoders the AVIF has no decoder.
	c.SetNativeDecoders(false)
	deadline := time.Now().Add(5 * time.Second)
	for c.Err("off", 8, 8) == nil {
		if c.Load("off", 8, 8, func() ([]byte, error) { return avif, nil }) != nil {
			t.Fatal("decoded the AVIF without ImageIO")
		}
		if time.Now().After(deadline) {
			t.Fatal("the AVIF did not fail to load")
		}
		time.Sleep(time.Millisecond)
	}
}
