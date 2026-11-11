package imaging

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/color/palette"
	"image/draw"
	"image/gif"
	"os"
	"testing"
	"time"
)

func solidPaletted(r image.Rectangle, idx uint8) *image.Paletted {
	p := image.NewPaletted(r, palette.Plan9)
	for i := range p.Pix {
		p.Pix[i] = idx
	}
	return p
}

func TestGIFFramesAndTiming(t *testing.T) {
	red := uint8(color.Palette(palette.Plan9).Index(color.RGBA{255, 0, 0, 255}))
	blue := uint8(color.Palette(palette.Plan9).Index(color.RGBA{0, 0, 255, 255}))
	g := &gif.GIF{
		Image: []*image.Paletted{
			solidPaletted(image.Rect(0, 0, 4, 4), red),
			solidPaletted(image.Rect(2, 2, 4, 4), blue),
			solidPaletted(image.Rect(0, 0, 1, 1), blue),
		},
		Delay:     []int{0, 20, 5}, // 0 plays as 100 ms, as in browsers
		Disposal:  []byte{gif.DisposalNone, gif.DisposalBackground, gif.DisposalNone},
		LoopCount: -1, // play once
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	img, err := DecodeImage(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if img.FrameCount() != 3 || img.Total != 350*time.Millisecond {
		t.Fatalf("frames %d duration %v, want 3 and 350ms", img.FrameCount(), img.Total)
	}
	at := func(frame, x, y int) color.NRGBA { return straightAt(img.FrameData[frame].Pixels, (y*4+x)*4) }
	if c := at(1, 3, 3); c.B != 255 || c.R != 0 {
		t.Errorf("frame 1 corner %v, want blue over red", c)
	}
	if c := at(2, 3, 3); c.A != 0 {
		t.Errorf("frame 2 corner %v, want cleared by frame 1's disposal", c)
	}
	if c := at(2, 1, 1); c.R != 255 {
		t.Errorf("frame 2 keeps frame 0 outside the disposed rect: %v", c)
	}
	for _, c := range []struct {
		at    time.Duration
		frame int
		wait  time.Duration
	}{
		{0, 0, 100 * time.Millisecond},
		{150 * time.Millisecond, 1, 150 * time.Millisecond},
		{330 * time.Millisecond, 2, 20 * time.Millisecond},
		{400 * time.Millisecond, 2, -1}, // played once, rests on the last frame
	} {
		n, wait := img.FrameAt(c.at)
		if n != c.frame || wait != c.wait {
			t.Errorf("frameAt(%v) = %d, %v; want %d, %v", c.at, n, wait, c.frame, c.wait)
		}
	}
	img.Plays = 0
	if n, _ := img.FrameAt(400 * time.Millisecond); n != 0 {
		t.Errorf("looping frameAt(400ms) = %d, want 0", n)
	}
}

func TestAnimationOverBudgetIsStill(t *testing.T) {
	g := &gif.GIF{
		Image: []*image.Paletted{solidPaletted(image.Rect(0, 0, 8, 8), 1), solidPaletted(image.Rect(0, 0, 8, 8), 2)},
		Delay: []int{10, 10},
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	img, err := Decode(buf.Bytes(), Bound{}, 8*8*4) // room for one frame only
	if err != nil {
		t.Fatal(err)
	}
	if img.FrameCount() != 0 {
		t.Fatalf("frames %d, want the still first frame", img.FrameCount())
	}
	img, err = Decode(buf.Bytes(), Bound{W: 4, H: 4}, 4*4*4*2) // scaled frames fit
	if err != nil {
		t.Fatal(err)
	}
	if w, h := img.Size(); img.FrameCount() != 2 || w != 4 || h != 4 {
		t.Fatalf("scaled animation %dx%d with %d frames, want 4x4 with 2", w, h, img.FrameCount())
	}
}

func TestGIFRestoresPreviousRegion(t *testing.T) {
	g := &gif.GIF{
		Image: []*image.Paletted{
			solidPaletted(image.Rect(0, 0, 6, 6), 1),
			solidPaletted(image.Rect(2, 3, 5, 5), 2),
			solidPaletted(image.Rect(0, 0, 2, 2), 3),
		},
		Delay:     []int{10, 10, 10},
		Disposal:  []byte{gif.DisposalNone, gif.DisposalPrevious, gif.DisposalNone},
		LoopCount: 3,
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	img, err := DecodeImage(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if img.FrameCount() != 3 || img.Plays != 4 {
		t.Fatalf("frames = %d, plays = %d, want 3 and 4", img.FrameCount(), img.Plays)
	}
	p := (4*6 + 3) * 4
	if bytes.Equal(img.FrameData[0].Pixels[p:p+4], img.FrameData[1].Pixels[p:p+4]) ||
		!bytes.Equal(img.FrameData[0].Pixels[p:p+4], img.FrameData[2].Pixels[p:p+4]) {
		t.Fatal("temporary frame was not painted and then restored")
	}
	t.Run("scaled pixels", func(t *testing.T) {
		all, err := gif.DecodeAll(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		bound := Bound{W: 3, H: 3}
		got, err := Decode(buf.Bytes(), bound, maxAnimBytes)
		if err != nil {
			t.Fatal(err)
		}
		canvas := image.NewNRGBA(image.Rect(0, 0, 6, 6))
		for i, frame := range all.Image {
			saved := append([]byte(nil), canvas.Pix...)
			draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)
			want := Resize(canvas, bound)
			if !bytes.Equal(got.FrameData[i].Pixels, want.Pixels) ||
				got.FrameData[i].Delay != frameDelay(time.Duration(all.Delay[i])*10*time.Millisecond) {
				t.Fatalf("frame %d pixels or timing differ", i)
			}
			if all.Disposal[i] == gif.DisposalPrevious {
				copy(canvas.Pix, saved)
			}
		}
	})

	truncated, err := DecodeImage(buf.Bytes()[:buf.Len()-2])
	if err != nil || truncated.FrameCount() != 0 {
		t.Fatalf("truncated GIF = %v, want the first frame", err)
	}
}

func TestWebPAnimatedLossyAlpha(t *testing.T) {
	data, err := os.ReadFile(imageFixturePath("alpha.webp"))
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var payload []byte
	for b := data[12:]; len(b) >= 8; {
		chunk, rest, err := nextRIFFChunk(b)
		if err != nil {
			t.Fatal(err)
		}
		b = rest
		if chunk.id == "ALPH" || chunk.id == "VP8 " {
			payload = append(payload, chunk.encoded...)
		}
	}
	vp8x := make([]byte, 10)
	vp8x[0] = 0x12
	put24(vp8x[4:], c.Width-1)
	put24(vp8x[7:], c.Height-1)
	body := append([]byte("WEBP"), webpChunk("VP8X", vp8x)...)
	for range 2 {
		frame := make([]byte, 16)
		put24(frame[6:], c.Width-1)
		put24(frame[9:], c.Height-1)
		put24(frame[12:], 100)
		frame[15] = 2
		body = append(body, webpChunk("ANMF", append(frame, payload...))...)
	}
	anim := append([]byte("RIFF"), 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(anim[4:], uint32(len(body)))
	anim = append(anim, body...)
	bound := Bound{W: 31, H: 31}
	got, err := Decode(anim, bound, maxAnimBytes)
	if err != nil {
		t.Fatal(err)
	}
	frame, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, c.Width, c.Height))
	draw.Draw(canvas, canvas.Bounds(), frame, frame.Bounds().Min, draw.Src)
	want := Resize(canvas, bound)
	if got.FrameCount() != 2 {
		t.Fatalf("frames = %d, want 2", got.FrameCount())
	}
	for i, f := range got.FrameData {
		if !bytes.Equal(f.Pixels, want.Pixels) {
			t.Fatalf("frame %d pixels differ from the still WebP decoder", i)
		}
	}
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

func TestWebPFrames(t *testing.T) {
	vp8x := make([]byte, 10)
	vp8x[0] = 0x12 // animation and alpha
	vp8x[4], vp8x[7] = 3, 3
	anim := make([]byte, 6)
	binary.LittleEndian.PutUint16(anim[4:], 2) // two plays
	body := []byte("WEBP")
	body = append(body, webpChunk("VP8X", vp8x)...)
	body = append(body, webpChunk("ANIM", anim)...)
	body = append(body, anmf(0, 0, 4, 4, 80, 0x01, solidVP8L(4, 4, color.NRGBA{255, 0, 0, 255}))...) // disposes to background
	body = append(body, anmf(2, 2, 2, 2, 5, 0x02, solidVP8L(2, 2, color.NRGBA{0, 255, 0, 128}))...)  // replaces, no blend
	data := append([]byte("RIFF"), 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(data[4:], uint32(len(body)))
	data = append(data, body...)

	img, err := DecodeImage(data)
	if err != nil {
		t.Fatal(err)
	}
	if img.FrameCount() != 2 || img.Plays != 2 {
		t.Fatalf("frames %d plays %d, want 2 and 2", img.FrameCount(), img.Plays)
	}
	if img.FrameData[0].Delay != 80*time.Millisecond || img.FrameData[1].Delay != minFrameDelay {
		t.Fatalf("delays %v, %v", img.FrameData[0].Delay, img.FrameData[1].Delay)
	}
	px := func(n, x, y int) color.NRGBA { return straightAt(img.FrameData[n].Pixels, (y*4+x)*4) }
	if c := px(0, 1, 1); c != (color.NRGBA{255, 0, 0, 255}) {
		t.Errorf("frame 0 %v, want red", c)
	}
	if c := px(1, 0, 0); c.A != 0 {
		t.Errorf("frame 1 outside the new rect %v, want disposed to transparent", c)
	}
	if c := px(1, 3, 3); c != (color.NRGBA{0, 255, 0, 128}) {
		t.Errorf("frame 1 rect %v, want unblended half-transparent green", c)
	}

	// Over budget, x/image/webp cannot decode it, so the first frame is
	// decoded here instead.
	img, err = Decode(data, Bound{}, 4*4*4)
	if err != nil {
		t.Fatal(err)
	}
	if w, h := img.Size(); img.FrameCount() != 0 || w != 4 || h != 4 {
		t.Fatalf("over budget %dx%d with %d frames, want the 4x4 first frame", w, h, img.FrameCount())
	}
	if _, err := DecodeImage(data[:len(data)-2]); err == nil {
		t.Fatal("decoded a truncated WebP frame")
	}
}

func TestAnimationCanvasBounds(t *testing.T) {
	// A few bytes can declare a huge logical screen; scaling the frames
	// down must not let the full-size canvas through.
	g := &gif.GIF{
		Image:  []*image.Paletted{solidPaletted(image.Rect(0, 0, 1, 1), 1), solidPaletted(image.Rect(0, 0, 1, 1), 2)},
		Delay:  []int{10, 10},
		Config: image.Config{Width: 8193, Height: 8193},
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	if img, err := Decode(buf.Bytes(), Bound{W: 64, H: 64}, maxAnimBytes); err == nil {
		t.Fatalf("decoded %d frames for a canvas over maxImagePixels", img.FrameCount())
	}

	// The lone first frame of a WebP is decoded without the canvas; its
	// header must not size the image past the canvas or its own bitstream.
	for _, canvas := range []int{1, 3000} {
		vp8x := make([]byte, 10)
		vp8x[0] = 0x12
		vp8x[4], vp8x[5] = byte(canvas-1), byte((canvas-1)>>8)
		vp8x[7], vp8x[8] = byte(canvas-1), byte((canvas-1)>>8)
		body := []byte("WEBP")
		body = append(body, webpChunk("VP8X", vp8x)...)
		body = append(body, anmf(0, 0, 3000, 2000, 100, 0, solidVP8L(1, 1, color.NRGBA{A: 255}))...)
		data := append([]byte("RIFF"), 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(data[4:], uint32(len(body)))
		data = append(data, body...)
		if img, err := DecodeImage(data); err == nil {
			w, h := img.Size()
			t.Fatalf("canvas %d: decoded a %dx%d image from a 1x1 frame, want an error", canvas, w, h)
		}
	}

	// An odd-sized last chunk without its pad byte.
	if _, _, err := nextRIFFChunk([]byte("ABCD\x01\x00\x00\x00x")); err != nil {
		t.Fatal(err)
	}
}
