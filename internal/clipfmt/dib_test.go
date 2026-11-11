package clipfmt

import (
	"encoding/binary"
	"image"
	"image/color"
	"testing"
)

func dibHeader(w, h int32, bpp uint16, compression uint32) []byte {
	b := make([]byte, 40)
	le := binary.LittleEndian
	le.PutUint32(b[0:], 40)
	le.PutUint32(b[4:], uint32(w))
	le.PutUint32(b[8:], uint32(h))
	le.PutUint16(b[12:], 1)
	le.PutUint16(b[14:], bpp)
	le.PutUint32(b[16:], compression)
	return b
}

func TestDecodeDIB(t *testing.T) {
	red, blue := color.NRGBA{255, 0, 0, 255}, color.NRGBA{0, 0, 255, 255}

	// 2x2, 24 bpp, bottom-up: rows are padded to 4 bytes and stored last row first.
	bottomUp := dibHeader(2, 2, 24, 0)
	bottomUp = append(bottomUp, 255, 0, 0, 255, 0, 0, 0, 0) // bottom row: blue, blue
	bottomUp = append(bottomUp, 0, 0, 255, 0, 0, 255, 0, 0) // top row: red, red

	// 1x1, 32 bpp with every alpha byte zero, as many programs write it.
	zeroAlpha := append(dibHeader(1, 1, 32, 0), 0, 0, 255, 0)

	// 1x1, 32 bpp bit fields after a BITMAPINFOHEADER, with a half-transparent pixel.
	fields := append(dibHeader(1, -1, 32, 3), 0, 0, 0xff, 0, 0, 0xff, 0, 0, 0xff, 0, 0, 0)
	fields = append(fields, 0, 0, 255, 128)

	tests := []struct {
		name string
		data []byte
		want map[image.Point]color.NRGBA
	}{
		{"bottom-up 24 bpp", bottomUp, map[image.Point]color.NRGBA{{0, 0}: red, {1, 0}: red, {0, 1}: blue, {1, 1}: blue}},
		{"zero alpha is opaque", zeroAlpha, map[image.Point]color.NRGBA{{0, 0}: red}},
		{"bit fields", fields, map[image.Point]color.NRGBA{{0, 0}: {255, 0, 0, 255}}},
	}
	for _, tt := range tests {
		img, err := DecodeDIB(tt.data)
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		for p, want := range tt.want {
			if got := img.NRGBAAt(p.X, p.Y); got != want {
				t.Errorf("%s: pixel %v = %v, want %v", tt.name, p, got, want)
			}
		}
	}

	if _, err := DecodeDIB(dibHeader(4, 4, 24, 0)); err == nil {
		t.Error("truncated pixels were accepted")
	}
}

func TestEncodeDIBRoundTrip(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	src.SetNRGBA(0, 0, color.NRGBA{10, 20, 30, 255})
	src.SetNRGBA(2, 1, color.NRGBA{200, 100, 50, 128})
	img, err := DecodeDIB(EncodeDIB(src))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []image.Point{{0, 0}, {2, 1}, {1, 1}} {
		if got, want := img.NRGBAAt(p.X, p.Y), src.NRGBAAt(p.X, p.Y); got != want {
			t.Errorf("pixel %v = %v, want %v", p, got, want)
		}
	}
}
