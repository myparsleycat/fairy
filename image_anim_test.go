package fairy

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/color/palette"
	"image/gif"
	"testing"
)

func solidPaletted(r image.Rectangle, idx uint8) *image.Paletted {
	p := image.NewPaletted(r, palette.Plan9)
	for i := range p.Pix {
		p.Pix[i] = idx
	}
	return p
}

type bitWriter struct {
	buf  []byte
	acc  uint64
	bits uint
}

func (b *bitWriter) put(v uint64, n uint) {
	b.acc |= v << b.bits
	b.bits += n
	for b.bits >= 8 {
		b.buf = append(b.buf, byte(b.acc))
		b.acc >>= 8
		b.bits -= 8
	}
}

func (b *bitWriter) bytes() []byte {
	if b.bits > 0 {
		b.buf = append(b.buf, byte(b.acc))
	}
	return b.buf
}

// solidVP8L encodes a w x h lossless bitstream of one color: five simple
// one-symbol prefix codes, so the pixels themselves take no bits.
func solidVP8L(w, h int, c color.NRGBA) []byte {
	var b bitWriter
	b.put(0x2f, 8)
	b.put(uint64(w-1), 14)
	b.put(uint64(h-1), 14)
	b.put(1, 1) // alpha is used
	b.put(0, 3) // version
	b.put(0, 1) // no transform
	b.put(0, 1) // no color cache
	b.put(0, 1) // no meta prefix codes
	for _, sym := range []uint8{c.G, c.R, c.B, c.A} {
		b.put(1, 1) // simple code
		b.put(0, 1) // one symbol
		b.put(1, 1) // eight-bit symbol
		b.put(uint64(sym), 8)
	}
	b.put(1, 1) // distance: simple code
	b.put(0, 1)
	b.put(0, 1) // one-bit symbol
	b.put(0, 1)
	return b.bytes()
}

func webpChunk(id string, data []byte) []byte {
	chunk := append([]byte(id), 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(chunk[4:], uint32(len(data)))
	chunk = append(chunk, data...)
	if len(data)&1 != 0 {
		chunk = append(chunk, 0)
	}
	return chunk
}

func anmf(x, y, w, h int, ms int, flags byte, bits []byte) []byte {
	p := make([]byte, 16)
	put24 := func(o, v int) { p[o], p[o+1], p[o+2] = byte(v), byte(v>>8), byte(v>>16) }
	put24(0, x/2)
	put24(3, y/2)
	put24(6, w-1)
	put24(9, h-1)
	put24(12, ms)
	p[15] = flags
	return webpChunk("ANMF", append(p, webpChunk("VP8L", bits)...))
}

func TestLazyImagePixelReadCompositesFirstFrame(t *testing.T) {
	first := image.Rect(2, 2, 4, 4)
	canvas := image.Rect(0, 0, 6, 6)
	var gifData bytes.Buffer
	err := gif.EncodeAll(&gifData, &gif.GIF{
		Image:  []*image.Paletted{solidPaletted(first, 1), solidPaletted(canvas, 2)},
		Delay:  []int{10, 10},
		Config: image.Config{Width: 6, Height: 6},
	})
	if err != nil {
		t.Fatal(err)
	}
	vp8x := make([]byte, 10)
	vp8x[0], vp8x[4], vp8x[7] = 0x12, 5, 5
	body := append([]byte("WEBP"), webpChunk("VP8X", vp8x)...)
	body = append(body, anmf(2, 2, 2, 2, 100, 2, solidVP8L(2, 2, color.NRGBA{R: 255, A: 255}))...)
	// The second frame has a valid container header but an invalid bitstream.
	body = append(body, anmf(0, 0, 6, 6, 100, 2, []byte{0xff})...)
	webp := append(binary.LittleEndian.AppendUint32([]byte("RIFF"), uint32(len(body))), body...)
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"GIF", gifData.Bytes()},
		{"WebP", webp},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := waitImage(t, NewImageCache(1<<20, 1), tc.name, 0, 0, tc.data)
			pix, err := img.NRGBA()
			if err != nil {
				t.Fatal(err)
			}
			if pix.Bounds() != canvas || pix.NRGBAAt(0, 0).A != 0 || pix.NRGBAAt(2, 2).A != 255 {
				t.Fatalf("first frame: bounds=%v, outside=%v, inside=%v", pix.Bounds(), pix.NRGBAAt(0, 0), pix.NRGBAAt(2, 2))
			}
		})
	}
}
