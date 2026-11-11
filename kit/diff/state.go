package diff

import (
	"fmt"
	"sort"
	"strings"
)

// Mode is how a diff lays out its two sides.
type Mode uint8

const (
	// Unified is one column, removed lines above the added lines that
	// replace them.
	Unified Mode = iota
	// Split puts the old side on the left and the new on the right, changed
	// lines side by side.
	Split
)

// LineRef is a line of one side of a file.
type LineRef struct {
	Side Side
	Line int // 1-based
}

// LineRange is selected lines of one file, from where the selection started
// to where it ends (either way round), as a click, Shift+click or drag down
// the line numbers makes them.
type LineRange struct {
	File       int
	Start, End LineRef
}

// Expand is which way an unchanged range between hunks opens.
type Expand uint8

const (
	// ExpandUp reveals twenty lines up from the hunk below the range.
	ExpandUp Expand = iota
	// ExpandDown reveals twenty lines down from the hunk above it.
	ExpandDown
	// ExpandAll reveals all of it.
	ExpandAll
)

// ExpandStep is how many lines one expand step reveals.
const ExpandStep = 20

// Row heights in pixels: estimates for the virtual list, exact for lines
// that don't wrap.
const (
	lineH   = 20
	headerH = 44
	gapH    = 32
	noteH   = 40
)

type rowKind uint8

const (
	rowFile rowKind = iota // a file's header
	rowNote                // a file with no lines to show
	rowGap                 // the unchanged range before hunk gap, with its expand buttons
	rowCtx                 // an unchanged line revealed from a range
	rowLine                // line i of hunk h; in split mode one without a partner
	rowPair                // split mode: removed line del beside added line add
)

type row struct {
	kind     rowKind
	f        int
	h, i     int // rowLine; h and gap index for rowGap in i
	del, add int // rowPair
	old, new int // rowCtx
}

type fileUI struct {
	collapsed bool
	// revealed holds the lines shown at the top and bottom of each
	// unchanged range (len(hunks)+1 of them).
	revealed [][2]int
}

// gap is an unchanged range before a hunk: its first old and new line
// numbers and its length, -1 when unknown (after the last hunk without the
// whole text).
type gap struct{ old, new, n int }

type lineKey struct {
	f    int
	side Side
	n    int
}

// State is a diff and everything about how it is shown: the app keeps one
// and View draws it. Its methods run on the UI thread.
type State struct {
	files []FileDiff
	ui    []fileUI
	mode  Mode

	selection        *LineRange
	selecting        bool
	selectionAtPress *LineRange
	unselect         bool // the press was on the one selected line
	pressedSide      Side
	sideLines        [][][2][]int // per file, hunk and side: the hunk index of each of its lines on that side
	gaps             [][]gap
	digits           []int
	rows             []row
	keys             []string
	rowOf            map[lineKey]int
	fileRows         []int
	hunkRows         []int
	rev              uint64
	scrollY          float32
	nav              int // the row navigation last put at the top, or -1
	reveal           int // a row to scroll to the top, or -1
	hlCache          map[chunkKey]*chunk
	heights          map[string]float32 // measured row heights by row key
}

// NewState returns the state of a diff of files.
func NewState(files []FileDiff) *State {
	s := &State{nav: -1, reveal: -1}
	s.SetFiles(files)
	return s
}

// SetFiles shows files instead, every range collapsed again and the
// selection cleared.
func (s *State) SetFiles(files []FileDiff) {
	s.ui = make([]fileUI, len(files))
	s.sideLines = make([][][2][]int, len(files))
	s.gaps = make([][]gap, len(files))
	s.digits = make([]int, len(files))
	for f := range files {
		file := &files[f]
		s.ui[f].revealed = make([][2]int, len(file.Hunks)+1)
		s.sideLines[f] = make([][2][]int, len(file.Hunks))
		for h := range file.Hunks {
			for i, l := range file.Hunks[h].Lines {
				for _, side := range []Side{Old, New} {
					if l.Number(side) > 0 {
						s.sideLines[f][h][side] = append(s.sideLines[f][h][side], i)
					}
				}
			}
		}
		s.gaps[f] = gaps(file)
		s.digits[f] = max(len(fmt.Sprint(max(file.lastLine(), 1))), 3)
	}
	s.files = files
	s.selection = nil
	s.hlCache = nil
	s.heights = map[string]float32{}
	s.rebuild()
}

// Files returns the files of the diff, in order.
func (s *State) Files() []FileDiff { return s.files }

// Mode returns whether the view is unified or split.
func (s *State) Mode() Mode { return s.mode }

// SetMode switches between unified and split. Unchanged and removed lines
// keep their row keys, so they stay in place where they can.
func (s *State) SetMode(m Mode) {
	if s.mode != m {
		s.mode = m
		s.rebuild()
	}
}

// Collapsed reports whether only file's header shows.
func (s *State) Collapsed(file int) bool { return file < len(s.ui) && s.ui[file].collapsed }

// SetCollapsed shows only file's header, or its lines again.
func (s *State) SetCollapsed(file int, collapsed bool) {
	if file < len(s.ui) && s.ui[file].collapsed != collapsed {
		s.ui[file].collapsed = collapsed
		s.rebuild()
	}
}

// Hidden is how many lines of file's unchanged range before hunk gap are
// still hidden, or -1 when unknown.
func (s *State) Hidden(file, gap int) int {
	if file >= len(s.gaps) || gap >= len(s.gaps[file]) {
		return -1
	}
	g := s.gaps[file][gap]
	if g.n < 0 {
		return -1
	}
	r := s.ui[file].revealed[gap]
	return max(g.n-r[0]-r[1], 0)
}

// Expand reveals more of file's unchanged range before hunk gap
// (len(Hunks): after the last). It does nothing without the file's whole
// text.
func (s *State) Expand(file, gap int, how Expand) {
	hidden := s.Hidden(file, gap)
	if hidden <= 0 || !s.files[file].Expandable() {
		return
	}
	last := gap == len(s.files[file].Hunks)
	r := &s.ui[file].revealed[gap]
	step := min(hidden, ExpandStep)
	switch {
	case how == ExpandAll:
		r[0] += hidden
	case how == ExpandUp && last, how == ExpandDown:
		// Nothing above the last range to grow from: it opens from the top.
		r[0] += step
	default:
		r[1] += step
	}
	s.rebuild()
}

// Selection is the selected lines, or nil.
func (s *State) Selection() *LineRange { return s.selection }

// SetSelection selects lines, or nothing with nil.
func (s *State) SetSelection(sel *LineRange) {
	if sel != nil {
		c := *sel
		sel = &c
	}
	s.selection = sel
}

// SelectedText is the selected lines' text on the side the selection
// started on, one per line, without numbers or markers: what copying the
// selection gives. It is "" without a selection.
func (s *State) SelectedText() (string, bool) {
	if s.selection == nil {
		return "", false
	}
	sel := *s.selection
	a, b, ok := s.selectedRows(sel)
	if !ok {
		return "", false
	}
	var out []string
	for r := a; r <= b; r++ {
		if n, ok := s.rowLine(s.rows[r], sel.Start.Side); ok {
			t, _ := s.SideLine(sel.File, sel.Start.Side, n)
			out = append(out, t)
		}
	}
	return strings.Join(out, "\n"), true
}

func (s *State) selectedRows(sel LineRange) (int, int, bool) {
	a, ok1 := s.rowOf[lineKey{sel.File, sel.Start.Side, sel.Start.Line}]
	b, ok2 := s.rowOf[lineKey{sel.File, sel.End.Side, sel.End.Line}]
	if !ok1 || !ok2 {
		return 0, 0, false
	}
	return min(a, b), max(a, b), true
}

// rowSelected reports whether row r's line on side is selected; any side
// when both is set.
func (s *State) rowSelected(r int, side Side, both bool) bool {
	if s.selection == nil {
		return false
	}
	sel := *s.selection
	a, b, ok := s.selectedRows(sel)
	if !ok || s.rows[r].f != sel.File || r < a || r > b {
		return false
	}
	return both || side == sel.Start.Side || side == sel.End.Side
}

// pressLine is a press on line at's number: a new selection, or with Shift
// one extended to it. Pressing the one selected line clears it on release,
// unless the press drags.
func (s *State) pressLine(file int, at LineRef, shift bool) {
	s.selectionAtPress = s.selection
	sel := LineRange{File: file, Start: at, End: at}
	if s.selection != nil && shift && s.selection.File == file {
		sel = LineRange{File: file, Start: s.selection.Start, End: at}
	}
	s.unselect = !shift && s.selection != nil && *s.selection == sel
	s.selecting = true
	s.pressedSide = at.Side
	s.selection = &sel
}

// dragTo moves the end of a selection being made by dragging to line at.
func (s *State) dragTo(file int, at LineRef) {
	if s.selection != nil && s.selecting && s.selection.File == file && s.selection.End != at {
		s.unselect = false
		sel := *s.selection
		sel.End = at
		s.selection = &sel
	}
}

// releaseLine ends a press on the line numbers and returns the selection
// it leaves.
func (s *State) releaseLine() *LineRange {
	s.selectionAtPress = nil
	if s.unselect {
		s.selection = nil
	}
	s.unselect, s.selecting = false, false
	return s.selection
}

// cancelLine ends a press on the line numbers, restoring the selection from
// before it.
func (s *State) cancelLine() {
	if s.selecting {
		s.selection = s.selectionAtPress
	}
	s.selectionAtPress = nil
	s.selecting, s.unselect = false, false
}

// VisibleFile is the file whose rows are at the top of the view.
func (s *State) VisibleFile() int {
	if r := s.position(); r < len(s.rows) {
		return s.rows[r].f
	}
	return 0
}

// ScrollToFile scrolls file's header to the top.
func (s *State) ScrollToFile(file int) {
	if file < len(s.fileRows) {
		s.reveal, s.nav = s.fileRows[file], s.fileRows[file]
	}
}

// ScrollToLine scrolls file's line at to the top, opening the range it is
// in when it is hidden.
func (s *State) ScrollToLine(file int, at LineRef) {
	if file >= len(s.files) {
		return
	}
	s.SetCollapsed(file, false)
	k := lineKey{file, at.Side, at.Line}
	if _, ok := s.rowOf[k]; !ok {
		for i, g := range s.gaps[file] {
			first := g.old
			if at.Side == New {
				first = g.new
			}
			if at.Line >= first && (g.n < 0 || at.Line < first+g.n) {
				s.Expand(file, i, ExpandAll)
				break
			}
		}
	}
	if r, ok := s.rowOf[k]; ok {
		s.reveal, s.nav = r, r
	}
}

// NextHunk scrolls the next (or previous) hunk to the top. It reports
// whether there was one.
func (s *State) NextHunk(forward bool) bool {
	from := s.position()
	target := -1
	if forward {
		for _, r := range s.hunkRows {
			if r > from {
				target = r
				break
			}
		}
	} else {
		for i := len(s.hunkRows) - 1; i >= 0; i-- {
			if s.hunkRows[i] < from {
				target = s.hunkRows[i]
				break
			}
		}
	}
	if target < 0 {
		return false
	}
	s.reveal, s.nav = target, target
	return true
}

// NextFile scrolls the next (or previous) file's header to the top. It
// reports whether there was one.
func (s *State) NextFile(forward bool) bool {
	from := s.position()
	if from >= len(s.rows) {
		return false
	}
	f := s.rows[from].f + 1
	if !forward {
		f = s.rows[from].f - 1
	}
	if f < 0 || f >= len(s.files) {
		return false
	}
	s.ScrollToFile(f)
	return true
}

// position is the row at the top of the view: where navigation last put
// it, unless the list has scrolled away since.
func (s *State) position() int {
	top := s.rowFrom(0, s.scrollY)
	if s.nav >= 0 && abs(top-s.nav) <= 3 {
		return s.nav
	}
	return top
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// rowFrom is the row reached by moving y pixels down from the top of row
// from, by the rows' estimated heights.
func (s *State) rowFrom(from int, y float32) int {
	if len(s.rows) == 0 {
		return 0
	}
	r := min(from, len(s.rows)-1)
	for y < 0 && r > 0 {
		r--
		y += s.estimate(r)
	}
	for r+1 < len(s.rows) {
		h := s.estimate(r)
		if y < h {
			break
		}
		y -= h
		r++
	}
	return r
}

// estimate is row r's height as the virtual list counts it: as measured
// when it was laid out, else the list's estimate.
func (s *State) estimate(r int) float32 {
	if h, ok := s.heights[s.keys[r]]; ok {
		return h
	}
	return lineH
}

// SideLine is line n of side of file f, from its whole text, its hunks or
// an unchanged range.
func (s *State) SideLine(f int, side Side, n int) (string, bool) {
	if f < 0 || f >= len(s.files) || n < 1 {
		return "", false
	}
	file := &s.files[f]
	h := sort.Search(len(file.Hunks), func(h int) bool { return file.Hunks[h].end(side) > n })
	if h < len(file.Hunks) && file.Hunks[h].first(side) <= n {
		hk := &file.Hunks[h]
		idx := s.sideLines[f][h][side]
		k := n - hk.first(side)
		if k >= len(idx) {
			return "", false
		}
		return hk.Lines[idx[k]].Text, true
	}
	g := s.gaps[f][h]
	off := n - g.old
	if side == New {
		off = n - g.new
	}
	if off < 0 {
		return "", false
	}
	return file.contextLine(g.old+off, g.new+off)
}

// rowLine is row r's line number on side.
func (s *State) rowLine(r row, side Side) (int, bool) {
	var n int
	switch r.kind {
	case rowCtx:
		n = r.old
		if side == New {
			n = r.new
		}
	case rowLine:
		n = s.files[r.f].Hunks[r.h].Lines[r.i].Number(side)
	case rowPair:
		i := r.del
		if side == New {
			i = r.add
		}
		n = s.files[r.f].Hunks[r.h].Lines[i].Number(side)
	}
	return n, n > 0
}

// rebuild lays the rows out again from the files, mode and revealed ranges.
func (s *State) rebuild() {
	s.rev++
	s.nav = -1
	var rows []row
	s.fileRows, s.hunkRows = s.fileRows[:0], s.hunkRows[:0]
	for f := range s.files {
		file := &s.files[f]
		s.fileRows = append(s.fileRows, len(rows))
		rows = append(rows, row{kind: rowFile, f: f})
		if s.ui[f].collapsed {
			continue
		}
		if len(file.Hunks) == 0 {
			rows = append(rows, row{kind: rowNote, f: f})
			continue
		}
		for gi := 0; gi <= len(file.Hunks); gi++ {
			g := s.gaps[f][gi]
			rev := s.ui[f].revealed[gi]
			ctx := func(k int) row { return row{kind: rowCtx, f: f, old: g.old + k, new: g.new + k} }
			for k := range rev[0] {
				rows = append(rows, ctx(k))
			}
			firstRow := len(rows)
			if h := s.Hidden(f, gi); h > 0 {
				rows = append(rows, row{kind: rowGap, f: f, i: gi})
			}
			if g.n >= 0 {
				for k := g.n - min(rev[1], g.n); k < g.n; k++ {
					rows = append(rows, ctx(k))
				}
			}
			if gi == len(file.Hunks) {
				break
			}
			s.hunkRows = append(s.hunkRows, firstRow)
			lines := file.Hunks[gi].Lines
			for i := 0; i < len(lines); {
				if s.mode == Split && lines[i].Kind == Removed {
					del := i
					for i < len(lines) && lines[i].Kind == Removed {
						i++
					}
					add := i
					for i < len(lines) && lines[i].Kind == Added {
						i++
					}
					nd, na := add-del, i-add
					for k := range max(nd, na) {
						switch {
						case k < nd && k < na:
							rows = append(rows, row{kind: rowPair, f: f, h: gi, del: del + k, add: add + k})
						case k < nd:
							rows = append(rows, row{kind: rowLine, f: f, h: gi, i: del + k})
						default:
							rows = append(rows, row{kind: rowLine, f: f, h: gi, i: add + k})
						}
					}
					continue
				}
				rows = append(rows, row{kind: rowLine, f: f, h: gi, i: i})
				i++
			}
		}
	}
	s.rows = rows
	s.keys = make([]string, len(rows))
	s.rowOf = make(map[lineKey]int, len(rows))
	for i, r := range rows {
		s.keys[i] = s.keyOf(r)
		for _, side := range []Side{Old, New} {
			if n, ok := s.rowLine(r, side); ok {
				s.rowOf[lineKey{r.f, side, n}] = i
			}
		}
	}
}

// keyOf is a row's key: the same for an unchanged or removed line in both
// modes, so switching keeps it.
func (s *State) keyOf(r row) string {
	switch r.kind {
	case rowFile:
		return fmt.Sprintf("df%d", r.f)
	case rowNote:
		return fmt.Sprintf("df%d.note", r.f)
	case rowGap:
		return fmt.Sprintf("df%d.g%d", r.f, r.i)
	case rowCtx:
		return fmt.Sprintf("df%d.c%d", r.f, r.old)
	case rowPair:
		return fmt.Sprintf("df%d.o%d", r.f, s.files[r.f].Hunks[r.h].Lines[r.del].Old)
	}
	l := &s.files[r.f].Hunks[r.h].Lines[r.i]
	switch l.Kind {
	case Removed:
		return fmt.Sprintf("df%d.o%d", r.f, l.Old)
	case Added:
		return fmt.Sprintf("df%d.n%d", r.f, l.New)
	}
	return fmt.Sprintf("df%d.c%d", r.f, l.Old)
}

// gaps are the unchanged ranges before each hunk of f and after its last.
func gaps(f *FileDiff) []gap {
	out := make([]gap, 0, len(f.Hunks)+1)
	old, new := 1, 1
	for i := range f.Hunks {
		h := &f.Hunks[i]
		out = append(out, gap{old, new, max(h.first(Old)-old, 0)})
		old, new = h.end(Old), h.end(New)
	}
	n := -1
	if len(f.Hunks) == 0 {
		n = 0
	} else if total, ok := f.oldLen(); ok {
		n = max(total+1-old, 0)
	}
	return append(out, gap{old, new, n})
}
