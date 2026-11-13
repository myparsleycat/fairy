package imaging

import (
	"bytes"
	"encoding/binary"
)

// pngEnd is the IEND chunk, which a complete PNG ends with.
var pngEnd = []byte("\x00\x00\x00\x00IEND\xaeB`\x82")

// jpegComplete reports whether a JPEG's main image runs to its end marker
// after its first scan. It skips segments by their length, so the end
// marker of an EXIF thumbnail, which a truncated camera JPEG still carries
// in full, does not count. The platform decoders decode a truncated file
// partially, filling the rest with gray, and report success.
func jpegComplete(data []byte) bool {
	if !bytes.HasPrefix(data, []byte{0xff, 0xd8}) {
		return false
	}
	coded := false // past the first scan header
	for i := 2; i < len(data); {
		// Entropy-coded data, and junk between segments that decoders skip,
		// run to the next 0xff.
		j := bytes.IndexByte(data[i:], 0xff)
		if j < 0 {
			return false
		}
		i += j
		for i+1 < len(data) && data[i+1] == 0xff { // fill bytes
			i++
		}
		if i+1 >= len(data) {
			return false
		}
		m := data[i+1]
		i += 2
		switch {
		case m == 0xd9: // EOI
			return coded
		case m == 0x00 || m == 0x01 || m >= 0xd0 && m <= 0xd8: // stuffing, TEM, RSTn, SOI
			continue
		}
		if i+2 > len(data) {
			return false
		}
		n := int(binary.BigEndian.Uint16(data[i:]))
		if n < 2 || i+n > len(data) {
			return false
		}
		i += n
		coded = coded || m == 0xda // SOS
	}
	return false
}

// jpegDCTSize returns the size of a JPEG decoded at 1/8, 1/4 or 1/2 scale
// that still leaves at least twice the target dimensions, so the final
// resampling keeps its quality. The platform decoders decode reduced JPEGs
// at this size first.
func jpegDCTSize(sw, sh, dw, dh int) (int, int, bool) {
	w, h, _, ok := jpegDCTScale(sw, sh, dw, dh)
	return w, h, ok
}

// jpegDCTScale is jpegDCTSize that also returns the scale's denominator.
func jpegDCTScale(sw, sh, dw, dh int) (int, int, int, bool) {
	for _, f := range [...]int{8, 4, 2} {
		w, h := (sw+f-1)/f, (sh+f-1)/f
		if w >= 2*dw && h >= 2*dh {
			return w, h, f, true
		}
	}
	return 0, 0, 0, false
}

// heifMatrixProbe is a lossless 8 x 8 AVIF of Y 110, Cb 100, Cr 200 in full
// range with BT.601 matrix coefficients: RGB 211 68 60, or 223 82 58 when
// decoded with BT.709's.
const heifMatrixProbe = "00000020667479706176696600000000617669666d6966316d6961664d413141000000f9" +
	"6d657461000000000000002f68646c7200000000000000007069637400000000000000000000000050696374757265" +
	"48616e646c6572000000000e7069746d0000000000010000001e696c6f6300000000440000010001000000010000" +
	"01210000001e0000002869696e660000000000010000001a696e6665020000000001000061763031436f6c6f7200" +
	"0000006a697072700000004b6970636f0000001469737065000000000000000800000008000000107069786900000000" +
	"030808080000000c617631438120000000000013636f6c726e636c78000200020006800000001769706d6100000000" +
	"0000000100010401028304000000266d6461740a083808bf6d020206903212100000000ffa4aaf931ea7557f3f6bb6" +
	"4740"
