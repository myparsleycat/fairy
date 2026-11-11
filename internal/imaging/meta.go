package imaging

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
)

// imageMeta is what a still image's metadata says about how to show its
// pixels: an EXIF orientation and the color space they are coded in.
type imageMeta struct {
	orient    uint8  // EXIF orientation, 1 to 8; 0 when absent or invalid
	icc       []byte // embedded ICC profile
	srgb      bool   // a PNG sRGB chunk, which overrides any profile
	primaries int    // AVIF nclx color primaries; 0 when absent
	transfer  int
}

// maxICCSize bounds an embedded profile; matrix/TRC profiles are a few KiB.
const maxICCSize = 4 << 20

func readImageMeta(format string, data []byte) imageMeta {
	switch format {
	case "jpeg":
		return jpegMeta(data)
	case "png":
		return pngMeta(data)
	case "webp":
		return webpMeta(data)
	}
	return imageMeta{}
}

// jpegMeta reads the APP1 Exif and APP2 ICC_PROFILE segments before the
// first scan.
func jpegMeta(data []byte) imageMeta {
	var m imageMeta
	var icc [][]byte
	for i := 2; i+4 <= len(data) && data[i] == 0xff; {
		mk := data[i+1]
		switch {
		case mk == 0xda:
			i = len(data)
			continue
		case mk == 0xff:
			i++
			continue
		case mk == 0x01 || mk >= 0xd0 && mk <= 0xd8:
			i += 2
			continue
		}
		n := int(data[i+2])<<8 | int(data[i+3])
		if n < 2 || i+2+n > len(data) {
			break
		}
		seg := data[i+4 : i+2+n]
		switch {
		case mk == 0xe1 && bytes.HasPrefix(seg, []byte("Exif\x00\x00")) && m.orient == 0:
			m.orient = exifOrientation(seg[6:])
		case mk == 0xe2 && bytes.HasPrefix(seg, []byte("ICC_PROFILE\x00")) && len(seg) > 14:
			seq, count := int(seg[12]), int(seg[13])
			if count == 0 || seq == 0 || seq > count {
				break
			}
			if icc == nil {
				icc = make([][]byte, count)
			}
			if count == len(icc) {
				icc[seq-1] = seg[14:]
			}
		}
		i += 2 + n
	}
	if icc != nil {
		var full []byte
		for _, part := range icc {
			if part == nil {
				return m
			}
			full = append(full, part...)
		}
		m.icc = full
	}
	return m
}

// pngMeta reads the eXIf, iCCP and sRGB chunks before the image data.
func pngMeta(data []byte) imageMeta {
	var m imageMeta
	for rest := data[min(8, len(data)):]; len(rest) >= 12; {
		n := binary.BigEndian.Uint32(rest)
		if uint64(n)+12 > uint64(len(rest)) {
			break
		}
		typ, body := string(rest[4:8]), rest[8:8+n]
		rest = rest[12+n:]
		switch typ {
		case "IDAT":
			return m
		case "eXIf":
			m.orient = exifOrientation(body)
		case "sRGB":
			m.srgb = true
		case "iCCP":
			name := bytes.IndexByte(body, 0)
			if name < 1 || name+2 > len(body) || body[name+1] != 0 {
				break
			}
			z, err := zlib.NewReader(bytes.NewReader(body[name+2:]))
			if err != nil {
				break
			}
			icc, err := io.ReadAll(io.LimitReader(z, maxICCSize+1))
			if err == nil && len(icc) <= maxICCSize {
				m.icc = icc
			}
		}
	}
	return m
}

// webpMeta reads the ICCP and EXIF chunks of an extended WebP file.
func webpMeta(data []byte) imageMeta {
	var m imageMeta
	if len(data) < 16 || string(data[8:16]) != "WEBPVP8X" {
		return m
	}
	for rest := data[12:]; len(rest) > 0; {
		c, r, err := nextRIFFChunk(rest)
		if err != nil {
			break
		}
		rest = r
		switch c.id {
		case "ICCP":
			m.icc = c.data
		case "EXIF":
			m.orient = exifOrientation(bytes.TrimPrefix(c.data, []byte("Exif\x00\x00")))
		}
	}
	return m
}

// exifOrientation returns the Orientation tag of the first IFD of a TIFF
// structure, or 0.
func exifOrientation(tiff []byte) uint8 {
	if len(tiff) < 8 {
		return 0
	}
	var bo binary.ByteOrder
	switch string(tiff[:4]) {
	case "II*\x00":
		bo = binary.LittleEndian
	case "MM\x00*":
		bo = binary.BigEndian
	default:
		return 0
	}
	ifd := uint64(bo.Uint32(tiff[4:]))
	if ifd+2 > uint64(len(tiff)) {
		return 0
	}
	n := int(bo.Uint16(tiff[ifd:]))
	for i := range n {
		e := ifd + 2 + uint64(i)*12
		if e+12 > uint64(len(tiff)) {
			return 0
		}
		entry := tiff[e : e+12]
		if bo.Uint16(entry) != 0x0112 {
			continue
		}
		if bo.Uint16(entry[2:]) != 3 || bo.Uint32(entry[4:]) != 1 { // one SHORT
			return 0
		}
		if o := bo.Uint16(entry[8:]); o >= 1 && o <= 8 {
			return uint8(o)
		}
		return 0
	}
	return 0
}
