package clipfmt

import (
	"encoding/binary"
	"errors"
	"image"
	"image/draw"
)

// DecodeDIB decodes a packed DIB (BITMAPINFOHEADER or a later header followed
// by the pixels), as found in CF_DIB and CF_DIBV5 clipboard data. It handles
// uncompressed and bit-field 24 and 32 bits per pixel, bottom-up or top-down.
func DecodeDIB(b []byte) (*image.NRGBA, error) {
	if len(b) < 40 {
		return nil, errors.New("fairy: DIB too short")
	}
	le := binary.LittleEndian
	hdr := int(le.Uint32(b[0:]))
	w := int(int32(le.Uint32(b[4:])))
	h := int(int32(le.Uint32(b[8:])))
	bpp := int(le.Uint16(b[14:]))
	compression := le.Uint32(b[16:])
	if hdr < 40 || hdr > len(b) || w <= 0 || h == 0 || w > 1<<15 || h > 1<<15 || h < -(1<<15) {
		return nil, errors.New("fairy: bad DIB header")
	}
	topDown := h < 0
	if topDown {
		h = -h
	}
	const biRGB, biBitfields = 0, 3
	masks := [4]uint32{0x00ff0000, 0x0000ff00, 0x000000ff, 0}
	if bpp == 32 {
		masks[3] = 0xff000000
	}
	offset := hdr
	switch {
	case compression == biBitfields && bpp == 32:
		if hdr >= 52 { // V3 and later headers carry the masks
			masks = [4]uint32{le.Uint32(b[40:]), le.Uint32(b[44:]), le.Uint32(b[48:]), 0}
			if hdr >= 56 {
				masks[3] = le.Uint32(b[52:])
			}
		} else {
			if len(b) < hdr+12 {
				return nil, errors.New("fairy: DIB masks missing")
			}
			masks = [4]uint32{le.Uint32(b[hdr:]), le.Uint32(b[hdr+4:]), le.Uint32(b[hdr+8:]), 0}
			offset += 12
		}
	case compression == biRGB && (bpp == 24 || bpp == 32):
	default:
		return nil, errors.New("fairy: unsupported DIB format")
	}
	stride := (w*bpp/8 + 3) &^ 3
	if len(b) < offset+stride*h {
		return nil, errors.New("fairy: DIB pixels truncated")
	}
	channel := func(px, mask uint32) uint8 {
		if mask == 0 {
			return 0
		}
		shift := 0
		for mask&1 == 0 {
			mask >>= 1
			shift++
		}
		return uint8((px >> shift & mask) * 255 / mask)
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	anyAlpha := false
	for y := 0; y < h; y++ {
		src := b[offset+y*stride:]
		row := y
		if !topDown {
			row = h - 1 - y
		}
		dst := img.Pix[row*img.Stride:]
		for x := 0; x < w; x++ {
			var px uint32
			if bpp == 24 {
				px = uint32(src[x*3]) | uint32(src[x*3+1])<<8 | uint32(src[x*3+2])<<16
			} else {
				px = le.Uint32(src[x*4:])
			}
			a := uint8(255)
			if masks[3] != 0 {
				a = channel(px, masks[3])
				anyAlpha = anyAlpha || a != 0
			}
			dst[x*4], dst[x*4+1], dst[x*4+2], dst[x*4+3] = channel(px, masks[0]), channel(px, masks[1]), channel(px, masks[2]), a
		}
	}
	// Many programs leave the alpha byte of 32-bit DIBs at zero: treat that as opaque.
	if masks[3] != 0 && !anyAlpha {
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 255
		}
	}
	return img, nil
}

// EncodeDIB encodes img as a packed 32-bit top-down DIB with BITMAPINFOHEADER.
// The color bytes are not premultiplied, as Windows programs expect for CF_DIB.
func EncodeDIB(img image.Image) []byte {
	src := toNRGBA(img)
	w, h := src.Rect.Dx(), src.Rect.Dy()
	out := make([]byte, 40+w*h*4)
	le := binary.LittleEndian
	le.PutUint32(out[0:], 40)
	le.PutUint32(out[4:], uint32(w))
	le.PutUint32(out[8:], uint32(-int32(h)))
	le.PutUint16(out[12:], 1)
	le.PutUint16(out[14:], 32)
	le.PutUint32(out[20:], uint32(w*h*4))
	for y := 0; y < h; y++ {
		s := src.Pix[y*src.Stride:]
		d := out[40+y*w*4:]
		for x := 0; x < w; x++ {
			d[x*4], d[x*4+1], d[x*4+2], d[x*4+3] = s[x*4+2], s[x*4+1], s[x*4], s[x*4+3]
		}
	}
	return out
}

func toNRGBA(src image.Image) *image.NRGBA {
	b := src.Bounds()
	nrgba, ok := src.(*image.NRGBA)
	if !ok || nrgba.Stride != b.Dx()*4 || b.Min != (image.Point{}) {
		r := image.Rect(0, 0, b.Dx(), b.Dy())
		var dst draw.Image
		// Opaque RGBA has the same bytes as NRGBA and retains image/draw's
		// optimized conversions for YCbCr, Gray and CMYK sources.
		if o, ok := src.(interface{ Opaque() bool }); ok && o.Opaque() {
			rgba := image.NewRGBA(r)
			dst = rgba
			nrgba = &image.NRGBA{Pix: rgba.Pix, Stride: rgba.Stride, Rect: r}
		} else {
			nrgba = image.NewNRGBA(r)
			dst = nrgba
		}
		draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	}
	return nrgba
}
