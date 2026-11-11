package imaging

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"io"
	"math"
	"time"

	"golang.org/x/image/webp"
)

type riffChunk struct {
	id      string
	data    []byte
	encoded []byte
}

func nextRIFFChunk(b []byte) (riffChunk, []byte, error) {
	if len(b) < 8 {
		return riffChunk{}, nil, io.ErrUnexpectedEOF
	}
	n := binary.LittleEndian.Uint32(b[4:8])
	if uint64(n) > uint64(len(b)-8) {
		return riffChunk{}, nil, errors.New("fairy: truncated WebP chunk")
	}
	end := 8 + int(n)
	padded := end
	if n&1 != 0 && padded < len(b) {
		padded++
	}
	return riffChunk{string(b[:4]), b[8:end], b[:padded]}, b[padded:], nil
}

func inspectWebP(data []byte) (animationInfo, error) {
	var a animationInfo
	for b := data[12:]; len(b) >= 8; {
		c, rest, err := nextRIFFChunk(b)
		if err != nil {
			return a, err
		}
		b = rest
		switch c.id {
		case "VP8X":
			if len(c.data) < 10 || a.W != 0 {
				return a, errors.New("fairy: invalid VP8X chunk")
			}
			a.W, a.H = u24(c.data[4:])+1, u24(c.data[7:])+1
			if imageTooLarge(a.W, a.H) {
				return a, fmt.Errorf("fairy: WebP canvas of %dx%d pixels exceeds the limit of %d megapixels",
					a.W, a.H, MaxImagePixels>>20)
			}
		case "ANIM":
			if len(c.data) >= 6 {
				a.plays = int(binary.LittleEndian.Uint16(c.data[4:]))
			}
		case "ANMF":
			r, err := webpFrameRect(c.data, a.W, a.H)
			if err != nil {
				return a, err
			}
			if a.Count == 0 {
				a.first = r
			}
			a.Count++
			a.delays = append(a.delays, time.Duration(u24(c.data[12:]))*time.Millisecond)
			a.maxFrame = max(a.maxFrame, int64(r.Dx())*int64(r.Dy()))
		}
	}
	if a.Count == 0 {
		return a, errors.New("fairy: animated WebP without frames")
	}
	return a, nil
}

func u24(b []byte) int { return int(b[0]) | int(b[1])<<8 | int(b[2])<<16 }

func webpFrameRect(f []byte, cw, ch int) (image.Rectangle, error) {
	if len(f) < 16 {
		return image.Rectangle{}, errors.New("fairy: short ANMF chunk")
	}
	x, y := u24(f)*2, u24(f[3:])*2
	w, h := u24(f[6:])+1, u24(f[9:])+1
	if x+w > cw || y+h > ch {
		return image.Rectangle{}, errors.New("fairy: WebP frame outside the canvas")
	}
	return image.Rect(x, y, x+w, y+h), nil
}

func decodeWebPFrames(data []byte, s *frameSink, firstCanvas bool) error {
	var canvas *image.NRGBA
	var dispose image.Rectangle
	cw, ch := 0, 0
	for b := data[12:]; len(b) >= 8; {
		c, rest, err := nextRIFFChunk(b)
		if err != nil {
			return err
		}
		b = rest
		switch c.id {
		case "VP8X":
			cw, ch = u24(c.data[4:])+1, u24(c.data[7:])+1
		case "ANMF":
			f := c.data
			r, err := webpFrameRect(f, cw, ch)
			if err != nil {
				return err
			}
			frame, err := decodeWebPFrame(f[16:], r.Dx(), r.Dy())
			if err != nil {
				return err
			}
			if !s.keepAll && !firstCanvas {
				r = frame.Bounds()
				cw, ch = r.Dx(), r.Dy()
				s.dw, s.dh = s.bound.Size(cw, ch)
			}
			if canvas == nil {
				canvas = image.NewNRGBA(image.Rect(0, 0, cw, ch))
			}
			if !dispose.Empty() {
				draw.Draw(canvas, dispose, image.Transparent, image.Point{}, draw.Src)
			}
			op := draw.Over
			if f[15]&2 != 0 {
				op = draw.Src
			}
			draw.Draw(canvas, r, frame, frame.Bounds().Min, op)
			if firstCanvas {
				s.frames = append(s.frames, canvas)
				return nil
			}
			if err := s.add(canvas, time.Duration(u24(f[12:]))*time.Millisecond); err != nil {
				return err
			}
			if !s.keepAll {
				return nil
			}
			dispose = image.Rectangle{}
			if f[15]&1 != 0 {
				dispose = r
			}
		}
	}
	return nil
}

func decodeWebPFrame(data []byte, w, h int) (image.Image, error) {
	var alpha, bits riffChunk
	for b := data; len(b) >= 8; {
		c, rest, err := nextRIFFChunk(b)
		if err != nil {
			return nil, err
		}
		b = rest
		switch c.id {
		case "ALPH":
			alpha = c
		case "VP8 ", "VP8L":
			bits = c
		}
	}
	if bits.encoded == nil {
		return nil, errors.New("fairy: WebP frame without image data")
	}
	if bits.id != "VP8 " {
		alpha = riffChunk{}
	}
	r, err := webpFrameReader(alpha, bits, w, h)
	if err != nil {
		return nil, err
	}
	// For extended WebP the config can come from VP8X, not the VP8
	// bitstream. Check the payload without ALPH/VP8X before decoding it.
	configReader, err := webpFrameReader(riffChunk{}, bits, w, h)
	if err != nil {
		return nil, err
	}
	c, err := webp.DecodeConfig(configReader)
	if err != nil {
		return nil, err
	}
	if c.Width != w || c.Height != h {
		return nil, errors.New("fairy: WebP frame size differs from its bitstream")
	}
	return webp.Decode(r)
}

func webpFrameReader(alpha, bits riffChunk, w, h int) (io.Reader, error) {
	n := uint64(4) + riffChunkSize(bits)
	var header [30]byte
	headerLen := 12
	if alpha.encoded != nil {
		n += 18 + riffChunkSize(alpha)
		copy(header[12:], "VP8X\x0a\x00\x00\x00\x10")
		put24(header[24:], w-1)
		put24(header[27:], h-1)
		headerLen = 30
	}
	if n > math.MaxUint32 {
		return nil, errors.New("fairy: WebP frame exceeds the RIFF size limit")
	}
	copy(header[:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(n))
	copy(header[8:12], "WEBP")
	readers := []io.Reader{bytes.NewReader(header[:headerLen])}
	for _, c := range []riffChunk{alpha, bits} {
		if c.encoded == nil {
			continue
		}
		readers = append(readers, bytes.NewReader(c.encoded))
		if uint64(len(c.encoded)) < riffChunkSize(c) {
			readers = append(readers, bytes.NewReader([]byte{0}))
		}
	}
	return io.MultiReader(readers...), nil
}

func riffChunkSize(c riffChunk) uint64 {
	n := uint64(len(c.data))
	return 8 + n + n&1
}

func put24(b []byte, v int) {
	b[0], b[1], b[2] = byte(v), byte(v>>8), byte(v>>16)
}
