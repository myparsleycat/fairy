// Package spell checks spelling with the system spell checker.
package spell

// Range identifies a misspelling by UTF-8 byte offsets, with End exclusive.
type Range struct{ Start, End int }

// utf16ToBytes turns UTF-16 offsets into byte offsets of s.
func utf16ToBytes(s string, rs []Range) []Range {
	if len(rs) == 0 {
		return nil
	}
	pos := map[int]int{}
	for _, r := range rs {
		pos[r.Start], pos[r.End] = -1, -1
	}
	u := 0
	for i, r := range s {
		if _, ok := pos[u]; ok {
			pos[u] = i
		}
		u++
		if r >= 0x10000 {
			u++
		}
	}
	if _, ok := pos[u]; ok {
		pos[u] = len(s)
	}
	out := rs[:0]
	for _, r := range rs {
		if a, b := pos[r.Start], pos[r.End]; a >= 0 && b > a {
			out = append(out, Range{a, b})
		}
	}
	return out
}
