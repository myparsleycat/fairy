// Package diff is what a diff is: files of hunks of lines, parsed from a
// patch or computed from two texts, and the state a diff view keeps.
package diff

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Side is one side of a diff: the text before the change or after it.
type Side uint8

const (
	Old Side = iota
	New
)

// LineKind is what a line of a hunk is.
type LineKind uint8

const (
	// Context is unchanged, on both sides.
	Context LineKind = iota
	// Added is only on the new side.
	Added
	// Removed is only on the old side.
	Removed
)

// Line is one line of a hunk.
type Line struct {
	Kind LineKind
	// Old and New are its 1-based numbers on each side, 0 where it is absent.
	Old, New int
	// Text is the line without its line break.
	Text string
	// Changes are the byte ranges of Text that differ from the line it
	// replaced or was replaced by (word changes), when the two share enough.
	Changes []Range
	// NoNewline marks the file ending here without a line break.
	NoNewline bool
}

// Number is the line's number on side, or 0.
func (l *Line) Number(s Side) int {
	if s == Old {
		return l.Old
	}
	return l.New
}

// Hunk is a run of changed lines with the unchanged lines around them.
type Hunk struct {
	// OldStart is where the hunk starts on the old side, as its @@ header
	// says: the line after which lines are inserted when OldCount is 0.
	OldStart, OldCount int
	NewStart, NewCount int
	// Section is the text after the header's closing @@, often the
	// enclosing function.
	Section string
	Lines   []Line
}

// first is the first line number the hunk covers on side (its start, or the
// line after it when it covers none there).
func (h *Hunk) first(s Side) int {
	start, count := h.OldStart, h.OldCount
	if s == New {
		start, count = h.NewStart, h.NewCount
	}
	if count == 0 {
		return start + 1
	}
	return max(start, 1)
}

// end is one past the last line number the hunk covers on side.
func (h *Hunk) end(s Side) int {
	if s == Old {
		return h.first(s) + h.OldCount
	}
	return h.first(s) + h.NewCount
}

// Header is the header line as a patch writes it: @@ -1,4 +1,5 @@ section.
func (h *Hunk) Header() string {
	rng := func(start, count int) string {
		if count == 1 {
			return strconv.Itoa(start)
		}
		return fmt.Sprintf("%d,%d", start, count)
	}
	s := fmt.Sprintf("@@ -%s +%s @@", rng(h.OldStart, h.OldCount), rng(h.NewStart, h.NewCount))
	if h.Section != "" {
		s += " " + h.Section
	}
	return s
}

// FileStatus is what happened to a file.
type FileStatus uint8

const (
	Modified FileStatus = iota
	FileAdded
	FileDeleted
	Renamed
	Copied
)

// FileDiff is one file's changes.
type FileDiff struct {
	// OldPath is the path before the change, "" for an added file; NewPath
	// the path after it, "" for a deleted file.
	OldPath, NewPath string
	Status           FileStatus
	// Similarity is a rename's or copy's similarity in percent, or -1.
	Similarity int
	// OldMode and NewMode are file modes such as 100644, when the patch
	// gives them.
	OldMode, NewMode string
	// Binary marks a file the patch says is binary, so it has no lines.
	Binary bool
	Hunks  []Hunk

	oldText, newText *source
}

// source is a whole text split into lines.
type source struct {
	text  string
	lines [][2]int // each line's byte range, without its line break
}

func newSource(text string) *source {
	s := &source{text: text}
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] != '\n' {
			continue
		}
		end := i
		if end > start && text[end-1] == '\r' {
			end--
		}
		s.lines = append(s.lines, [2]int{start, end})
		start = i + 1
	}
	if start < len(text) {
		end := len(text)
		if text[end-1] == '\r' {
			end--
		}
		s.lines = append(s.lines, [2]int{start, end})
	}
	return s
}

// line is line n (1-based).
func (s *source) line(n int) (string, bool) {
	if n < 1 || n > len(s.lines) {
		return "", false
	}
	r := s.lines[n-1]
	return s.text[r[0]:r[1]], true
}

func (s *source) len() int { return len(s.lines) }

func (s *source) endsWithoutNewline() bool {
	return s.text != "" && !strings.HasSuffix(s.text, "\n")
}

// contextLines is how many unchanged lines a computed diff keeps around each
// change, as git diff does.
const contextLines = 3

func emptyFile(oldPath, newPath string) FileDiff {
	return FileDiff{OldPath: oldPath, NewPath: newPath, Similarity: -1}
}

// FromTexts is the diff of old to new, a file at path, with three lines of
// context around each change and word changes on paired lines. Both texts
// stay with it, so every unchanged range between hunks can be expanded.
func FromTexts(path, old, new string) FileDiff { return Between(path, path, old, new) }

// Between is FromTexts for a file whose path changed (a rename), was added
// (oldPath "", old empty) or deleted (newPath "", new empty).
func Between(oldPath, newPath, old, new string) FileDiff {
	f := emptyFile(oldPath, newPath)
	f.Status = statusOf(oldPath, newPath)
	if strings.IndexByte(old, 0) >= 0 || strings.IndexByte(new, 0) >= 0 {
		f.Binary = true
		return f
	}
	a, b := newSource(old), newSource(new)
	f.Hunks = hunks(a, b)
	f.oldText, f.newText = a, b
	f.pairWords()
	return f
}

func statusOf(oldPath, newPath string) FileStatus {
	switch {
	case oldPath == "":
		return FileAdded
	case newPath == "":
		return FileDeleted
	case oldPath != newPath:
		return Renamed
	}
	return Modified
}

// Path is the path to show: the new one, or the old one for a deleted file.
func (f *FileDiff) Path() string {
	if f.NewPath != "" {
		return f.NewPath
	}
	return f.OldPath
}

// Additions returns the number of added lines.
func (f *FileDiff) Additions() int { return f.count(Added) }

// Deletions returns the number of removed lines.
func (f *FileDiff) Deletions() int { return f.count(Removed) }

func (f *FileDiff) count(k LineKind) int {
	n := 0
	for i := range f.Hunks {
		for j := range f.Hunks[i].Lines {
			if f.Hunks[i].Lines[j].Kind == k {
				n++
			}
		}
	}
	return n
}

// Language is the highlighter language for the path's extension (or name,
// for a Makefile), as written: "go", "tsx". It is "" when there is none.
func (f *FileDiff) Language() string {
	p := f.Path()
	name := p[strings.LastIndexByte(p, '/')+1:]
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		return name[i+1:]
	}
	if name == "Makefile" || name == "Dockerfile" {
		return name
	}
	return ""
}

// Expandable reports whether the unchanged lines between hunks can be shown.
func (f *FileDiff) Expandable() bool { return f.oldText != nil || f.newText != nil }

// contextLine is unchanged line old of the old side (new on the new side),
// from whichever whole text the diff has.
func (f *FileDiff) contextLine(old, new int) (string, bool) {
	if f.oldText != nil {
		if s, ok := f.oldText.line(old); ok {
			return s, true
		}
	}
	if f.newText != nil {
		return f.newText.line(new)
	}
	return "", false
}

// oldLen is how many lines the old side has, when the diff knows: from the
// old text, or from the new text and the hunks' line counts.
func (f *FileDiff) oldLen() (int, bool) {
	if f.oldText != nil {
		return f.oldText.len(), true
	}
	if f.newText == nil {
		return 0, false
	}
	return max(f.newText.len()-f.Additions()+f.Deletions(), 0), true
}

// lastLine is the largest line number either side shows.
func (f *FileDiff) lastLine() int {
	n := 0
	if k := len(f.Hunks); k > 0 {
		h := &f.Hunks[k-1]
		n = max(h.end(Old), h.end(New)) - 1
	}
	for _, t := range []*source{f.oldText, f.newText} {
		if t != nil {
			n = max(n, t.len())
		}
	}
	return n
}

// pairWords sets word changes for each removed line followed by an added
// one, pairing them in order.
func (f *FileDiff) pairWords() {
	for hi := range f.Hunks {
		ls := f.Hunks[hi].Lines
		i := 0
		for i < len(ls) {
			del := i
			for i < len(ls) && ls[i].Kind == Removed {
				i++
			}
			add := i
			for i < len(ls) && ls[i].Kind == Added {
				i++
			}
			if add == del || i == add {
				i = max(i, del+1)
				continue
			}
			pairs := min(add-del, i-add)
			if pairs > 128 {
				continue
			}
			for k := range pairs {
				ls[del+k].Changes, ls[add+k].Changes = words(ls[del+k].Text, ls[add+k].Text)
			}
		}
	}
}

// Columns is how many columns line takes with tabs stopping every four.
func Columns(line string) int {
	col := 0
	for _, c := range line {
		if c == '\t' {
			col += 4 - col%4
		} else {
			col++
		}
	}
	return col
}

type change struct{ o0, o1, n0, n1 int } // 0-based old and new ranges

// hunks are the hunks of old to new with contextLines around each change,
// merged when their context would touch.
func hunks(old, new *source) []Hunk {
	type key struct {
		text string
		last bool // a last line without its break differs from one with it
	}
	ids := map[key]int{}
	intern := func(s *source) []int {
		out := make([]int, s.len())
		for n := 1; n <= s.len(); n++ {
			t, _ := s.line(n)
			k := key{t, n == s.len() && s.endsWithoutNewline()}
			id, ok := ids[k]
			if !ok {
				id = len(ids)
				ids[k] = id
			}
			out[n-1] = id
		}
		return out
	}
	a, b := intern(old), intern(new)
	var changes []change
	i, j := 0, 0
	for _, o := range myers(a, b) {
		last := len(changes) - 1
		adjoins := last >= 0 && changes[last].o1 == i && changes[last].n1 == j
		switch o.kind {
		case opEqual:
			i, j = i+o.n, j+o.n
		case opDel:
			if adjoins {
				changes[last].o1 += o.n
			} else {
				changes = append(changes, change{i, i + o.n, j, j})
			}
			i += o.n
		case opIns:
			if adjoins {
				changes[last].n1 += o.n
			} else {
				changes = append(changes, change{i, i, j, j + o.n})
			}
			j += o.n
		}
	}
	var groups [][]change
	for _, c := range changes {
		if g := len(groups) - 1; g >= 0 && c.o0-groups[g][len(groups[g])-1].o1 <= 2*contextLines {
			groups[g] = append(groups[g], c)
		} else {
			groups = append(groups, []change{c})
		}
	}
	line := func(s *source, n int) string { t, _ := s.line(n + 1); return t }
	out := make([]Hunk, 0, len(groups))
	for _, g := range groups {
		first, last := g[0], g[len(g)-1]
		start := max(first.o0-contextLines, 0)
		os, ns := start, first.n0-(first.o0-start)
		endOld := min(last.o1+contextLines, old.len())
		var lines []Line
		i, j := os, ns
		for _, c := range g {
			for ; i < c.o0; i, j = i+1, j+1 {
				lines = append(lines, Line{Kind: Context, Old: i + 1, New: j + 1, Text: line(old, i)})
			}
			for n := c.o0; n < c.o1; n++ {
				lines = append(lines, Line{Kind: Removed, Old: n + 1, Text: line(old, n)})
			}
			for n := c.n0; n < c.n1; n++ {
				lines = append(lines, Line{Kind: Added, New: n + 1, Text: line(new, n)})
			}
			i, j = c.o1, c.n1
		}
		for ; i < endOld; i, j = i+1, j+1 {
			lines = append(lines, Line{Kind: Context, Old: i + 1, New: j + 1, Text: line(old, i)})
		}
		for k := range lines {
			l := &lines[k]
			if l.Kind == Added {
				l.NoNewline = l.New == new.len() && new.endsWithoutNewline()
			} else {
				l.NoNewline = l.Old == old.len() && old.endsWithoutNewline()
			}
		}
		oc, nc := i-os, j-ns
		h := Hunk{OldStart: os, OldCount: oc, NewStart: ns, NewCount: nc, Section: section(old, os), Lines: lines}
		if oc > 0 {
			h.OldStart++
		}
		if nc > 0 {
			h.NewStart++
		}
		out = append(out, h)
	}
	return out
}

// section is the nearest line above 0-based line before that starts with a
// letter, _ or $ (a function or type at the left margin), as git diff names
// a hunk by default.
func section(s *source, before int) string {
	for n, seen := min(before, s.len()), 0; n >= 1 && seen < 2000; n, seen = n-1, seen+1 {
		l, _ := s.line(n)
		c, _ := utf8.DecodeRuneInString(l)
		if l != "" && (unicode.IsLetter(c) || c == '_' || c == '$') {
			l = strings.TrimRightFunc(l, unicode.IsSpace)
			if utf8.RuneCountInString(l) > 80 {
				l = string([]rune(l)[:80])
			}
			return l
		}
	}
	return ""
}
