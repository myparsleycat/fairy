package imaging

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/draw"
	"image/gif"
	"io"
	"time"
)

type gifFrame struct {
	header, blocks []byte
	rect           image.Rectangle
	disposal       byte
	delay          time.Duration
}

func walkGIF(data []byte, visit func(gifFrame) error) (animationInfo, error) {
	a := animationInfo{plays: 1}
	if len(data) < 13 {
		return a, io.ErrUnexpectedEOF
	}
	a.W = int(binary.LittleEndian.Uint16(data[6:8]))
	a.H = int(binary.LittleEndian.Uint16(data[8:10]))
	if imageTooLarge(a.W, a.H) {
		return a, errors.New("fairy: GIF canvas exceeds the pixel limit")
	}
	n := 13 + gifPaletteBytes(data[10])
	if n > len(data) {
		return a, io.ErrUnexpectedEOF
	}
	f := gifFrame{header: data[:n]}
	b := data[n:]
	pending := b
	for len(b) > 0 {
		switch b[0] {
		case 0x3b: // trailer
			return a, nil
		case 0x21: // extension
			var err error
			b, err = gifExtension(b[1:], &f, &a)
			if err != nil {
				return a, err
			}
		case 0x2c: // image descriptor
			if len(b) < 10 {
				return a, io.ErrUnexpectedEOF
			}
			x := int(binary.LittleEndian.Uint16(b[1:3]))
			y := int(binary.LittleEndian.Uint16(b[3:5]))
			w := int(binary.LittleEndian.Uint16(b[5:7]))
			h := int(binary.LittleEndian.Uint16(b[7:9]))
			if x+w > a.W || y+h > a.H {
				return a, errors.New("fairy: GIF frame outside the canvas")
			}
			n := 11 + gifPaletteBytes(b[9]) // includes the LZW code size
			if n > len(b) {
				return a, io.ErrUnexpectedEOF
			}
			var err error
			b, err = skipGIFSubBlocks(b[n:])
			if err != nil {
				return a, err
			}
			f.rect = image.Rect(x, y, x+w, y+h)
			if a.Count == 0 {
				a.first = f.rect
			}
			a.Count++
			a.delays = append(a.delays, f.delay)
			area := int64(w) * int64(h)
			a.maxFrame = max(a.maxFrame, area)
			if f.disposal == gif.DisposalPrevious {
				a.maxSaved = max(a.maxSaved, area)
			}
			if visit != nil {
				f.blocks = pending[:len(pending)-len(b)]
				if err := visit(f); err != nil {
					return a, err
				}
			}
			pending = b
			f.delay = 0
		default:
			return a, errors.New("fairy: unknown GIF block")
		}
	}
	return a, io.ErrUnexpectedEOF
}

func gifPaletteBytes(flags byte) int {
	if flags&0x80 == 0 {
		return 0
	}
	return 3 << (1 + flags&7)
}

func gifExtension(b []byte, f *gifFrame, a *animationInfo) ([]byte, error) {
	if len(b) == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	switch b[0] {
	case 0xf9: // graphic control
		if len(b) < 7 || b[1] != 4 || b[6] != 0 {
			return nil, errors.New("fairy: invalid GIF graphic control extension")
		}
		f.disposal = (b[2] >> 2) & 7
		f.delay = time.Duration(binary.LittleEndian.Uint16(b[3:5])) * 10 * time.Millisecond
		return b[7:], nil
	case 0xff: // application
		if len(b) < 2 || len(b) < 2+int(b[1]) {
			return nil, io.ErrUnexpectedEOF
		}
		n := 2 + int(b[1])
		if string(b[2:n]) == "NETSCAPE2.0" && len(b)-n >= 4 && b[n] == 3 && b[n+1] == 1 {
			a.plays = int(binary.LittleEndian.Uint16(b[n+2 : n+4]))
			if a.plays != 0 {
				a.plays++
			}
		}
		b = b[n:]
	case 0x01: // plain text: the fixed header includes its size byte
		if len(b) < 14 {
			return nil, io.ErrUnexpectedEOF
		}
		b = b[14:]
	case 0xfe: // comment
		b = b[1:]
	default:
		return nil, errors.New("fairy: unknown GIF extension")
	}
	return skipGIFSubBlocks(b)
}

func skipGIFSubBlocks(b []byte) ([]byte, error) {
	for len(b) != 0 {
		n := int(b[0])
		b = b[1:]
		if n == 0 {
			return b, nil
		}
		if n > len(b) {
			break
		}
		b = b[n:]
	}
	return nil, io.ErrUnexpectedEOF
}

func decodeGIFFrames(data []byte, a animationInfo, s *frameSink) error {
	canvas := image.NewNRGBA(image.Rect(0, 0, a.W, a.H))
	var saved []byte
	_, err := walkGIF(data, func(f gifFrame) error {
		// gif.Decode stops at the end of the first image data block. Keeping
		// that block and its extensions intact preserves the standard
		// decoder's palette, transparency and LZW handling.
		frame, err := gif.Decode(io.MultiReader(bytes.NewReader(f.header), bytes.NewReader(f.blocks)))
		if err != nil {
			return err
		}
		var prev image.NRGBA
		if f.disposal == gif.DisposalPrevious && !f.rect.Empty() {
			if saved == nil {
				saved = make([]byte, a.maxSaved*4)
			}
			prev = image.NRGBA{Pix: saved, Stride: f.rect.Dx() * 4, Rect: f.rect}
			draw.Draw(&prev, f.rect, canvas, f.rect.Min, draw.Src)
		}
		draw.Draw(canvas, f.rect, frame, f.rect.Min, draw.Over)
		if err := s.add(canvas, f.delay); err != nil {
			return err
		}
		switch f.disposal {
		case gif.DisposalBackground:
			draw.Draw(canvas, f.rect, image.Transparent, image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			draw.Draw(canvas, f.rect, &prev, f.rect.Min, draw.Src)
		}
		return nil
	})
	return err
}
