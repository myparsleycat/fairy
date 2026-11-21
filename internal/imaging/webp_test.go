package imaging

import (
	"bytes"
	"image"
	"os"
	"testing"

	"golang.org/x/image/webp"
)

// libwebpRGB is libwebp's fixed-point VP8YUVToR/G/B, which reads VP8's YCbCr as BT.601 limited range.
func libwebpRGB(y, cb, cr uint8) [3]int {
	clip := func(v int) int { return min(max(v>>6, 0), 255) }
	luma := int(y) * 19077 >> 8
	return [3]int{
		clip(luma + int(cr)*26149>>8 - 14234),
		clip(luma - int(cb)*6419>>8 - int(cr)*13320>>8 + 8708),
		clip(luma + int(cb)*33050>>8 - 17685),
	}
}

// Read as full-range JFIF, lossy WebP's colours shrink toward grey, white turning 235 and black 16; decoded images
// match libwebp's conversion of the same planes instead.
func TestDecodeImageMatchesLibwebpColours(t *testing.T) {
	for _, name := range []string{"lossy.webp", "alpha.webp"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(imageFixturePath(name))
			if err != nil {
				t.Fatal(err)
			}
			raw, err := webp.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			var ycc *image.YCbCr
			switch src := raw.(type) {
			case *image.YCbCr:
				ycc = src
			case *image.NYCbCrA:
				ycc = &src.YCbCr
			default:
				t.Fatalf("fixture decodes to %T, want YCbCr", raw)
			}

			img, err := DecodeImage(data)
			if err != nil {
				t.Fatal(err)
			}
			straight := make([]byte, len(img.Pixels))
			UnpremultiplyRGBA(straight, img.Pixels)
			worst := 0
			for y := range img.H {
				for x := range img.W {
					i := (y*img.W + x) * 4
					if straight[i+3] < 128 {
						continue
					}
					yi, ci := ycc.YOffset(x, y), ycc.COffset(x, y)
					for c, want := range libwebpRGB(ycc.Y[yi], ycc.Cb[ci], ycc.Cr[ci]) {
						worst = max(worst, abs(int(straight[i+c])-want))
					}
				}
			}
			if worst > 3 {
				t.Errorf("largest difference from libwebp = %d, want at most 3", worst)
			}
		})
	}
}
