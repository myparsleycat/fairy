package diff

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// op is one run of an edit script: equal advances both sides, del the old
// side and ins the new side.
type op struct {
	kind     opKind
	old, new int
	n        int
}

type opKind uint8

const (
	opEqual opKind = iota
	opDel
	opIns
)

// maxD bounds the edits one subproblem searches before it splits where it
// got furthest, so a file rewritten wholesale costs bounded time.
const maxD = 1024

// myers returns the edit script turning a into b, in linear space (divide
// and conquer on the middle snake), with runs coalesced and deletions before
// the insertions they pair with.
func myers[T comparable](a, b []T) []op {
	ca, cb := make([]bool, len(a)), make([]bool, len(b))
	size := len(a) + len(b) + 3
	v := &vs{f: make([]int, size), b: make([]int, size), off: len(b) + 1}
	conquer(a, 0, len(a), b, 0, len(b), v, ca, cb)
	return script(ca, cb)
}

// vs holds the furthest-reaching x per diagonal (x - y), forward and
// backward, indexed from -off.
type vs struct {
	f, b []int
	off  int
}

func conquer[T comparable](a []T, a0, a1 int, b []T, b0, b1 int, v *vs, ca, cb []bool) {
	for a0 < a1 && b0 < b1 && a[a0] == b[b0] {
		a0, b0 = a0+1, b0+1
	}
	for a0 < a1 && b0 < b1 && a[a1-1] == b[b1-1] {
		a1, b1 = a1-1, b1-1
	}
	switch {
	case a0 == a1:
		fill(cb[b0:b1])
	case b0 == b1:
		fill(ca[a0:a1])
	default:
		if x, y, ok := middleSnake(a, a0, a1, b, b0, b1, v); ok {
			conquer(a, a0, x, b, b0, y, v, ca, cb)
			conquer(a, x, a1, b, y, b1, v, ca, cb)
		} else {
			fill(ca[a0:a1])
			fill(cb[b0:b1])
		}
	}
}

func fill(s []bool) {
	for i := range s {
		s[i] = true
	}
}

// middleSnake returns a point on an optimal path through a[a0:a1] × b[b0:b1]
// that splits it into two smaller halves; past maxD edits, the furthest
// point reached from the start. Both ranges are non-empty and differ at both
// ends.
func middleSnake[T comparable](a []T, xoff, xlim int, b []T, yoff, ylim int, v *vs) (int, int, bool) {
	dmin, dmax := xoff-ylim, xlim-yoff
	fmid, bmid := xoff-yoff, xlim-ylim
	fmin, fmax, bmin, bmax := fmid, fmid, bmid, bmid
	odd := (fmid-bmid)&1 != 0
	at := func(d int) int { return d + v.off }
	split := func(x, y int) (int, int, bool) {
		x, y = min(max(x, xoff), xlim), min(max(y, yoff), ylim)
		if x == xlim && y == ylim || x == xoff && y == yoff {
			return 0, 0, false
		}
		return x, y, true
	}
	v.f[at(fmid)] = xoff
	v.b[at(bmid)] = xlim
	const never = int(^uint(0) >> 1)
	for range maxD {
		if fmin > dmin {
			fmin--
			v.f[at(fmin-1)] = -1
		} else {
			fmin++
		}
		if fmax < dmax {
			fmax++
			v.f[at(fmax+1)] = -1
		} else {
			fmax--
		}
		for d := fmax; d >= fmin; d -= 2 {
			lo, hi := v.f[at(d-1)], v.f[at(d+1)]
			x := hi
			if lo >= hi {
				x = lo + 1
			}
			y := x - d
			for x < xlim && y < ylim && a[x] == b[y] {
				x, y = x+1, y+1
			}
			v.f[at(d)] = x
			if odd && bmin <= d && d <= bmax && v.b[at(d)] <= x {
				return split(x, y)
			}
		}
		if bmin > dmin {
			bmin--
			v.b[at(bmin-1)] = never
		} else {
			bmin++
		}
		if bmax < dmax {
			bmax++
			v.b[at(bmax+1)] = never
		} else {
			bmax--
		}
		for d := bmax; d >= bmin; d -= 2 {
			lo, hi := v.b[at(d-1)], v.b[at(d+1)]
			x := hi - 1
			if lo < hi {
				x = lo
			}
			y := x - d
			for xoff < x && yoff < y && a[x-1] == b[y-1] {
				x, y = x-1, y-1
			}
			v.b[at(d)] = x
			if !odd && fmin <= d && d <= fmax && x <= v.f[at(d)] {
				return split(x, y)
			}
		}
	}
	// Too costly to finish: split where the forward search got furthest, as
	// xdiff does, so a long file with many scattered edits still diffs line
	// by line in bounded time.
	bx, bd, best := 0, 0, -1<<62
	for d := fmin; d <= fmax; d += 2 {
		if x := v.f[at(d)]; 2*x-d > best {
			bx, bd, best = x, d, 2*x-d
		}
	}
	return split(bx, bx-bd)
}

func script(ca, cb []bool) []op {
	var out []op
	i, j := 0, 0
	for i < len(ca) || j < len(cb) {
		si, sj := i, j
		for i < len(ca) && j < len(cb) && !ca[i] && !cb[j] {
			i, j = i+1, j+1
		}
		if i > si {
			out = append(out, op{kind: opEqual, old: si, new: sj, n: i - si})
		}
		di := i
		for i < len(ca) && ca[i] {
			i++
		}
		if i > di {
			out = append(out, op{kind: opDel, old: di, n: i - di})
		}
		dj := j
		for j < len(cb) && cb[j] {
			j++
		}
		if j > dj {
			out = append(out, op{kind: opIns, new: dj, n: j - dj})
		}
		if i == si && j == sj {
			break
		}
	}
	return out
}

// Range is a byte range [Start, End) of a line.
type Range struct{ Start, End int }

// words returns the byte ranges of old and new that differ, by words: runs
// of letters, digits and _, runs of whitespace, and single other characters.
// Changes separated only by whitespace join into one range. Both are empty
// when the lines share too little for word changes to mean anything.
func words(old, new string) ([]Range, []Range) {
	if old == new || len(old) > 1000 || len(new) > 1000 {
		return nil, nil
	}
	ta, tb := tokens(old), tokens(new)
	sa, sb := make([]string, len(ta)), make([]string, len(tb))
	for i, r := range ta {
		sa[i] = old[r.Start:r.End]
	}
	for i, r := range tb {
		sb[i] = new[r.Start:r.End]
	}
	shared := 0
	var ra, rb []Range
	for _, o := range myers(sa, sb) {
		switch o.kind {
		case opEqual:
			for _, t := range sa[o.old : o.old+o.n] {
				if strings.TrimSpace(t) != "" {
					shared += utf8.RuneCountInString(t)
				}
			}
		case opDel:
			ra = append(ra, Range{ta[o.old].Start, ta[o.old+o.n-1].End})
		case opIns:
			rb = append(rb, Range{tb[o.new].Start, tb[o.new+o.n-1].End})
		}
	}
	if shared*4 < max(content(old), content(new)) {
		return nil, nil
	}
	return join(old, ra), join(new, rb)
}

func content(s string) int {
	n := 0
	for _, c := range s {
		if !unicode.IsSpace(c) {
			n++
		}
	}
	return max(n, 1)
}

func join(text string, rs []Range) []Range {
	var out []Range
	for _, r := range rs {
		if k := len(out); k > 0 && strings.TrimSpace(text[out[k-1].End:r.Start]) == "" {
			out[k-1].End = r.End
			continue
		}
		out = append(out, r)
	}
	return out
}

func tokens(s string) []Range {
	const (
		word = iota
		space
		other
	)
	kind := func(c rune) int {
		switch {
		case unicode.IsLetter(c) || unicode.IsNumber(c) || c == '_':
			return word
		case unicode.IsSpace(c):
			return space
		}
		return other
	}
	var out []Range
	prev := -1
	for i := 0; i < len(s); {
		c, w := utf8.DecodeRuneInString(s[i:])
		k := kind(c)
		end := i + w
		if n := len(out); n > 0 && prev == k && k != other {
			out[n-1].End = end
		} else {
			out = append(out, Range{i, end})
		}
		prev = k
		i = end
	}
	return out
}
