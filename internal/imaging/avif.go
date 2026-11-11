package imaging

import (
	"encoding/binary"
	"slices"
)

// avifInfo is what inspectAVIF reads from the boxes of a still AVIF image.
type avifInfo struct {
	w, h int
	// plain is set for one AV1-coded primary item with no auxiliary image,
	// such as alpha, no rotation, mirroring or cropping to apply, and SDR
	// color with BT.709 or BT.601 matrix coefficients.
	plain bool
	bt601 bool // the colors are coded with BT.601 matrix coefficients

	primaries int // nclx color primaries; 0 without an nclx property
	transfer  int
	icc       []byte // a prof or rICC property's ICC profile
}

// inspectAVIF reads the primary item of a still AVIF image. It reports false
// for other files, for image sequences, and for files whose top-level boxes
// do not fit in data, as a truncated file's do.
func inspectAVIF(data []byte) (avifInfo, bool) {
	var ftyp, meta []byte
	for rest := data; len(rest) > 0; {
		box, r, ok := nextISOBox(rest)
		if !ok {
			return avifInfo{}, false
		}
		switch box.typ {
		case "ftyp":
			if ftyp == nil {
				ftyp = box.body
			}
		case "meta":
			meta = box.body
		case "moov":
			return avifInfo{}, false
		}
		rest = r
	}
	if len(ftyp) < 8 || len(meta) < 4 || !avifStillBrand(ftyp) {
		return avifInfo{}, false
	}

	var (
		primary    uint32
		hasPrimary bool
		picture    bool
		itemType   = map[uint32]string{}
		auxFor     []uint32 // items that an auxiliary image refers to
		props      []isoBox
		assoc      = map[uint32][]int{}
	)
	for rest := meta[4:]; len(rest) > 0; {
		box, r, ok := nextISOBox(rest)
		if !ok {
			return avifInfo{}, false
		}
		rest = r
		b := box.body
		switch box.typ {
		case "hdlr":
			picture = len(b) >= 12 && string(b[8:12]) == "pict"
		case "pitm":
			primary, hasPrimary = readItemID(b, 4, len(b) > 0 && b[0] == 0)
		case "iinf":
			if !readItemTypes(b, itemType) {
				return avifInfo{}, false
			}
		case "iref":
			if len(b) < 4 {
				return avifInfo{}, false
			}
			short := b[0] == 0
			for refs := b[4:]; len(refs) > 0; {
				ref, r, ok := nextISOBox(refs)
				if !ok {
					return avifInfo{}, false
				}
				refs = r
				if ref.typ != "auxl" {
					continue
				}
				ids, ok := readReferences(ref.body, short)
				if !ok {
					return avifInfo{}, false
				}
				auxFor = append(auxFor, ids...)
			}
		case "iprp":
			for children := b; len(children) > 0; {
				child, r, ok := nextISOBox(children)
				if !ok {
					return avifInfo{}, false
				}
				children = r
				switch child.typ {
				case "ipco":
					for list := child.body; len(list) > 0; {
						prop, r, ok := nextISOBox(list)
						if !ok {
							return avifInfo{}, false
						}
						list = r
						props = append(props, prop)
					}
				case "ipma":
					if !readAssociations(child.body, assoc) {
						return avifInfo{}, false
					}
				}
			}
		}
	}
	if !picture || !hasPrimary {
		return avifInfo{}, false
	}

	var info avifInfo
	transformed := false
	matrix := -1
	for _, i := range assoc[primary] {
		if i == 0 { // no property
			continue
		}
		if i > len(props) {
			return avifInfo{}, false
		}
		p := props[i-1]
		switch p.typ {
		case "ispe":
			if len(p.body) < 12 {
				return avifInfo{}, false
			}
			info.w = int(binary.BigEndian.Uint32(p.body[4:]))
			info.h = int(binary.BigEndian.Uint32(p.body[8:]))
		case "irot", "imir", "clap":
			transformed = true
		case "colr":
			switch kind := string(p.body[:min(4, len(p.body))]); {
			case kind == "nclx" && len(p.body) >= 10:
				info.primaries = int(binary.BigEndian.Uint16(p.body[4:]))
				info.transfer = int(binary.BigEndian.Uint16(p.body[6:]))
				matrix = int(binary.BigEndian.Uint16(p.body[8:]))
			case (kind == "prof" || kind == "rICC") && len(p.body) > 4:
				info.icc = p.body[4:]
			}
		}
	}
	if info.w <= 0 || info.h <= 0 {
		return avifInfo{}, false
	}
	// Without matrix coefficients in the container, the AV1 sequence header
	// decides; PQ and HLG need tone mapping.
	known := matrix == 1 || matrix == 5 || matrix == 6
	info.plain = itemType[primary] == "av01" && !transformed && !slices.Contains(auxFor, primary) && known && nclxCurve(info.transfer) != nil
	info.bt601 = matrix == 5 || matrix == 6
	return info, true
}

func avifStillBrand(ftyp []byte) bool {
	still := false
	for i := 0; i+4 <= len(ftyp); i += 4 {
		if i == 4 {
			continue // minor version
		}
		switch string(ftyp[i : i+4]) {
		case "avis":
			return false
		case "avif":
			still = true
		}
	}
	return still
}

type isoBox struct {
	typ  string
	body []byte
}

// nextISOBox splits off the first box of b. It reports false when the box's
// size does not fit in b.
func nextISOBox(b []byte) (isoBox, []byte, bool) {
	if len(b) < 8 {
		return isoBox{}, nil, false
	}
	size, header := uint64(binary.BigEndian.Uint32(b)), uint64(8)
	switch size {
	case 0: // to the end of the file
		size = uint64(len(b))
	case 1:
		if len(b) < 16 {
			return isoBox{}, nil, false
		}
		size, header = binary.BigEndian.Uint64(b[8:]), 16
	}
	if size < header || size > uint64(len(b)) {
		return isoBox{}, nil, false
	}
	return isoBox{string(b[4:8]), b[header:size]}, b[size:], true
}

// readItemID reads a 16-bit item ID at b[at:] when short, else a 32-bit one.
func readItemID(b []byte, at int, short bool) (uint32, bool) {
	if short {
		if at < 0 || len(b) < at+2 {
			return 0, false
		}
		return uint32(binary.BigEndian.Uint16(b[at:])), true
	}
	if at < 0 || len(b) < at+4 {
		return 0, false
	}
	return binary.BigEndian.Uint32(b[at:]), true
}

func readItemTypes(b []byte, types map[uint32]string) bool {
	if len(b) < 4 {
		return false
	}
	at := 6
	if b[0] != 0 {
		at = 8
	}
	if len(b) < at {
		return false
	}
	for rest := b[at:]; len(rest) > 0; {
		box, r, ok := nextISOBox(rest)
		if !ok {
			return false
		}
		rest = r
		e := box.body
		// Item info entries before version 2 carry no item type.
		if box.typ != "infe" || len(e) < 4 || e[0] < 2 {
			continue
		}
		id, ok := readItemID(e, 4, e[0] == 2)
		at := 8
		if e[0] != 2 {
			at = 10
		}
		if !ok || len(e) < at+4 {
			return false
		}
		types[id] = string(e[at : at+4])
	}
	return true
}

// readReferences returns the items a single item reference box points to.
func readReferences(b []byte, short bool) ([]uint32, bool) {
	size := 4
	if short {
		size = 2
	}
	if len(b) < size+2 {
		return nil, false
	}
	n := int(binary.BigEndian.Uint16(b[size:]))
	b = b[size+2:]
	if len(b) < n*size {
		return nil, false
	}
	ids := make([]uint32, n)
	for i := range ids {
		ids[i], _ = readItemID(b, i*size, short)
	}
	return ids, true
}

func readAssociations(b []byte, assoc map[uint32][]int) bool {
	if len(b) < 8 {
		return false
	}
	short, wide := b[0] < 1, b[3]&1 != 0
	n := binary.BigEndian.Uint32(b[4:])
	p := 8
	for range n {
		id, ok := readItemID(b, p, short)
		if !ok {
			return false
		}
		if short {
			p += 2
		} else {
			p += 4
		}
		if len(b) < p+1 {
			return false
		}
		count := int(b[p])
		p++
		for range count {
			if wide {
				if len(b) < p+2 {
					return false
				}
				assoc[id] = append(assoc[id], int(binary.BigEndian.Uint16(b[p:])&0x7fff))
				p += 2
			} else {
				if len(b) < p+1 {
					return false
				}
				assoc[id] = append(assoc[id], int(b[p]&0x7f))
				p++
			}
		}
	}
	return true
}
