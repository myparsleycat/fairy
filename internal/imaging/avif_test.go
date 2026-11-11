package imaging

import (
	"encoding/binary"
	"slices"
	"testing"
)

// isoBoxBytes returns a box of typ holding the concatenated parts.
func isoBoxBytes(typ string, parts ...[]byte) []byte {
	body := slices.Concat(parts...)
	b := binary.BigEndian.AppendUint32(nil, uint32(8+len(body)))
	return append(append(b, typ...), body...)
}

type avifFixture struct {
	brand            string
	matrix, transfer uint16
	primaries        uint16   // 0 writes BT.709's
	extraProps       [][]byte // associated with the primary item after ispe and colr
	alpha            bool
}

func (f avifFixture) bytes() []byte {
	u16 := func(v uint16) []byte { return binary.BigEndian.AppendUint16(nil, v) }
	u32 := func(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }
	full := []byte{0, 0, 0, 0} // version 0, no flags
	infe := func(id uint16) []byte {
		return isoBoxBytes("infe", []byte{2, 0, 0, 0}, u16(id), u16(0), []byte("av01"))
	}
	primaries := max(f.primaries, 1)
	props := [][]byte{
		isoBoxBytes("ispe", full, u32(640), u32(480)),
		isoBoxBytes("colr", []byte("nclx"), u16(primaries), u16(f.transfer), u16(f.matrix), []byte{0x80}),
	}
	props = append(props, f.extraProps...)
	assoc := []byte{}
	for i := range props {
		assoc = append(assoc, byte(i+1))
	}
	items := [][]byte{infe(1)}
	ipma := slices.Concat(full, u32(1), u16(1), []byte{byte(len(assoc))}, assoc)
	var iref []byte
	if f.alpha {
		items = append(items, infe(2))
		ipma = slices.Concat(full, u32(2), u16(1), []byte{byte(len(assoc))}, assoc, u16(2), []byte{1, 1})
		iref = isoBoxBytes("iref", full, isoBoxBytes("auxl", u16(2), u16(1), u16(1)))
	}
	meta := isoBoxBytes("meta", full,
		isoBoxBytes("hdlr", full, u32(0), []byte("pict"), make([]byte, 13)),
		isoBoxBytes("pitm", full, u16(1)),
		isoBoxBytes("iinf", full, u16(uint16(len(items))), slices.Concat(items...)),
		iref,
		isoBoxBytes("iprp", isoBoxBytes("ipco", props...), isoBoxBytes("ipma", ipma)),
	)
	return slices.Concat(
		isoBoxBytes("ftyp", []byte(f.brand), u32(0), []byte("mif1"), []byte(f.brand)),
		meta,
		isoBoxBytes("mdat", make([]byte, 32)),
	)
}

func TestInspectAVIFRoutesOnlyPlainStillImages(t *testing.T) {
	plain := avifFixture{brand: "avif", matrix: 6, transfer: 13}
	data := plain.bytes()
	info, ok := inspectAVIF(data)
	if !ok || info.w != 640 || info.h != 480 || !info.plain || !info.bt601 {
		t.Fatalf("plain BT.601 image: %+v, %v", info, ok)
	}
	if info, ok := inspectAVIF((avifFixture{brand: "avif", matrix: 1, transfer: 13}).bytes()); !ok || !info.plain || info.bt601 {
		t.Fatalf("plain BT.709 image: %+v, %v", info, ok)
	}
	if _, ok := inspectAVIF(data[:len(data)-1]); ok {
		t.Error("truncated file inspected")
	}
	if _, ok := inspectAVIF((avifFixture{brand: "avis", matrix: 6, transfer: 13}).bytes()); ok {
		t.Error("image sequence inspected as a still image")
	}
	for name, f := range map[string]avifFixture{
		"alpha":           {brand: "avif", matrix: 6, transfer: 13, alpha: true},
		"rotation":        {brand: "avif", matrix: 6, transfer: 13, extraProps: [][]byte{isoBoxBytes("irot", []byte{1})}},
		"mirror":          {brand: "avif", matrix: 6, transfer: 13, extraProps: [][]byte{isoBoxBytes("imir", []byte{0})}},
		"BT.2020":         {brand: "avif", matrix: 9, transfer: 13},
		"PQ":              {brand: "avif", matrix: 1, transfer: 16},
		"HLG":             {brand: "avif", matrix: 1, transfer: 18},
		"unsupported SDR": {brand: "avif", matrix: 1, transfer: 4},
	} {
		info, ok := inspectAVIF(f.bytes())
		if !ok || info.w != 640 || info.h != 480 {
			t.Errorf("%s: %+v, %v; want the size", name, info, ok)
		}
		if info.plain {
			t.Errorf("%s: plain", name)
		}
	}
}
