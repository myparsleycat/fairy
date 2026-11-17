package spell

import (
	"strings"
	"unicode/utf8"
)

// Lines returns the misspellings of cur given those of old: it checks
// the lines of cur that differ from old and moves the ranges on the other
// lines. The checker also reports errors that depend on the next or previous
// line, such as a word repeated across a line break, so the ranges on the
// lines next to the changed ones are taken from a check that includes the
// lines next to those.
func Lines(old, cur string, errs []Range, check func(string) []Range) []Range {
	p, s := commonEnds(old, cur)
	lo, hi := linesBefore(cur, p, 1), linesAfter(cur, len(cur)-s, 1)
	from, to := linesBefore(cur, p, 2), linesAfter(cur, len(cur)-s, 2)
	d := len(cur) - len(old)
	var out []Range
	for _, e := range errs {
		if e.End <= lo {
			out = append(out, e)
		}
	}
	for _, e := range check(cur[from:to]) {
		if e.Start+from < hi && e.End+from > lo {
			out = append(out, Range{e.Start + from, e.End + from})
		}
	}
	for _, e := range errs {
		if e.Start >= hi-d {
			out = append(out, Range{e.Start + d, e.End + d})
		}
	}
	return out
}

// linesBefore returns the start of the line n lines before the one holding
// s[i], or 0.
func linesBefore(s string, i, n int) int {
	start := strings.LastIndexByte(s[:i], '\n') + 1
	for ; n > 0 && start > 0; n-- {
		start = strings.LastIndexByte(s[:start-1], '\n') + 1
	}
	return start
}

// linesAfter returns the end, before its line break, of the line n lines
// after the one holding s[i], or len(s).
func linesAfter(s string, i, n int) int {
	end := strings.IndexByte(s[i:], '\n')
	if end < 0 {
		return len(s)
	}
	end += i
	for ; n > 0; n-- {
		next := strings.IndexByte(s[end+1:], '\n')
		if next < 0 {
			return len(s)
		}
		end += 1 + next
	}
	return end
}

// commonEnds returns the lengths of the common prefix of a and b and of the
// common suffix of what follows it.
func commonEnds(a, b string) (p, s int) {
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	return p, s
}

// Shift moves errs, the misspellings of old, to cur: ranges before the
// first changed byte stay, ranges after the last one shift with it, and
// ranges the edit touched are dropped.
func Shift(old, cur string, errs []Range) []Range {
	if old == cur {
		return errs
	}
	p, s := commonEnds(old, cur)
	d := len(cur) - len(old)
	var out []Range
	for _, e := range errs {
		switch {
		case e.End < p:
			out = append(out, e)
		case e.Start > len(old)-s:
			out = append(out, Range{e.Start + d, e.End + d})
		}
	}
	return out
}

// Valid reports whether r lies on rune boundaries of s.
func Valid(s string, r Range) bool {
	return r.Start >= 0 && r.Start < r.End && r.End <= len(s) &&
		(r.Start == len(s) || utf8.RuneStart(s[r.Start])) && (r.End == len(s) || utf8.RuneStart(s[r.End]))
}
