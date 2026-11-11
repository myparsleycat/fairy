package diff

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func numbered(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return b.String()
}

func TestTextsMakeHunksWithThreeLinesOfContext(t *testing.T) {
	old := numbered(20)
	new := strings.Replace(old, "line 5\n", "line five\n", 1)
	new = strings.Replace(new, "line 17\n", "", 1)
	new = strings.Replace(new, "line 20\n", "line 20\nline 21\n", 1)
	f := FromTexts("a.txt", old, new)
	var headers []string
	for i := range f.Hunks {
		headers = append(headers, f.Hunks[i].Header())
	}
	if want := []string{"@@ -2,7 +2,7 @@ line 1", "@@ -14,7 +14,7 @@ line 13"}; !slices.Equal(headers, want) {
		t.Fatalf("headers %q, want %q", headers, want)
	}
	second := f.Hunks[1].Lines
	if l := second[3]; l.Kind != Removed || l.Old != 17 || l.New != 0 {
		t.Fatalf("second hunk line 3: %+v, want removed old 17", l)
	}
	if l := second[len(second)-1]; l.Kind != Added || l.New != 20 {
		t.Fatalf("last line: %+v, want added new 20", l)
	}
	if f.Additions() != 2 || f.Deletions() != 2 {
		t.Fatalf("+%d -%d, want +2 -2", f.Additions(), f.Deletions())
	}
	for _, l := range f.Hunks[0].Lines {
		if l.Kind == Added {
			if len(l.Changes) == 0 || l.Text[l.Changes[0].Start:l.Changes[0].End] != "five" {
				t.Fatalf("word change %v of %q, want \"five\"", l.Changes, l.Text)
			}
		}
	}
}

func TestMissingFinalNewlineIsAChange(t *testing.T) {
	type got struct {
		kind LineKind
		text string
		nl   bool
	}
	lines := func(f FileDiff) []got {
		var out []got
		for _, l := range f.Hunks[0].Lines {
			out = append(out, got{l.Kind, l.Text, l.NoNewline})
		}
		return out
	}
	if g := lines(FromTexts("a", "one\ntwo", "one\ntwo\n")); !slices.Equal(g, []got{{Context, "one", false}, {Removed, "two", true}, {Added, "two", false}}) {
		t.Fatalf("got %v", g)
	}
	if len(FromTexts("a", "same", "same").Hunks) != 0 || len(FromTexts("a", "one\r\ntwo\r\n", "one\ntwo\n").Hunks) != 0 {
		t.Fatal("equal texts, or texts differing only in CRLF, made hunks")
	}
	if bin := FromTexts("img.bin", "a\x00b", "c\x00d"); !bin.Binary || len(bin.Hunks) != 0 {
		t.Fatal("a NUL byte didn't mark the file binary")
	}
}

func TestParse(t *testing.T) {
	patch := "From 1234 Mon Sep 17 00:00:00 2001\nSubject: fix\n\n" +
		"diff --git a/old name.go b/new name.go\nsimilarity index 90%\nrename from old name.go\nrename to new name.go\n" +
		"index 111..222 100644\n--- a/old name.go\n+++ b/new name.go\n@@ -1,3 +1,3 @@ func f()\n a\n-b\n+B\n c\n" +
		"--- /dev/null\n+++ b/new.rs\n@@ -0,0 +1 @@\n+fn main() {}\n\\ No newline at end of file\n" +
		"diff --git a/bin.png b/bin.png\nBinary files a/bin.png and b/bin.png differ\n" +
		"diff --git a/x.sh b/x.sh\nold mode 100644\nnew mode 100755\n"
	fs := Parse(patch)
	if len(fs) != 4 {
		t.Fatalf("%d files, want 4", len(fs))
	}
	r := fs[0]
	if r.Status != Renamed || r.OldPath != "old name.go" || r.NewPath != "new name.go" || r.Similarity != 90 || r.NewMode != "100644" {
		t.Fatalf("rename: %+v", r)
	}
	if h := r.Hunks[0]; h.Section != "func f()" || len(h.Lines) != 4 || h.Lines[2].Text != "B" || h.Lines[2].New != 2 {
		t.Fatalf("rename hunk: %+v", h)
	}
	if !fs[2].Binary || fs[3].OldMode != "100644" || fs[3].NewMode != "100755" {
		t.Fatalf("binary %v, modes %q %q", fs[2].Binary, fs[3].OldMode, fs[3].NewMode)
	}
	added := fs[1]
	want := Between("", "new.rs", "", "fn main() {}")
	if added.Status != FileAdded || added.Hunks[0].Header() != "@@ -0,0 +1 @@" || !slices.EqualFunc(added.Hunks[0].Lines, want.Hunks[0].Lines, sameLine) {
		t.Fatalf("added file: %+v, want lines %+v", added, want.Hunks[0].Lines)
	}
	if got := Parse(`diff --git "a/caf\303\251" "b/caf\303\251"` + "\n")[0].NewPath; got != "café" {
		t.Fatalf("quoted path %q, want café", got)
	}
}

func sameLine(a, b Line) bool {
	return a.Kind == b.Kind && a.Old == b.Old && a.New == b.New && a.Text == b.Text && a.NoNewline == b.NoNewline
}

func TestHostilePatchesDoNotPanic(t *testing.T) {
	cases := []string{
		"",
		"diff --git\n--- \n+++ \n@@ -4294967295,4294967295 +4294967295,4294967295 @@\n-a\n+b\n\\ No newline",
		"diff --git \"a/unclosed b/unclosed\n--- \"a/\\777\\x\n+++ b/\n@@ -0,0 +0,0 @@\n",
		"--- a/f\n+++ b/f\n@@ -10,2 +10,2 @@\n ctx\n-old\n+new\n@@ -2,1 +2,1 @@\n-out_of_order\n+ok\n",
	}
	frags := []string{
		"diff --git a/f.rs b/f.rs\n", "--- a/f.rs\n", "+++ b/f.rs\n", "--- /dev/null\n", "+++ /dev/null\n",
		"@@ -1,3 +1,3 @@ fn f()\n", "@@ -0,0 +1,5 @@\n", "@@ -4294967295,10 +0,4294967295 @@\n", "@@ malformed @@\n",
		" context line\n", "-removed line\n", "+added line\n", "\\ No newline at end of file\n", "similarity index 999%\n", "\r\n",
	}
	seed := uint64(0x9e3779b97f4a7c15)
	next := func(n int) int {
		seed ^= seed << 13
		seed ^= seed >> 7
		seed ^= seed << 17
		return int(seed % uint64(n))
	}
	for range 300 {
		var b strings.Builder
		for range 1 + next(24) {
			b.WriteString(frags[next(len(frags))])
		}
		cases = append(cases, b.String())
	}
	for _, p := range cases {
		s := NewState(Parse(p))
		s.SetMode(Split)
		for f := range s.Files() {
			s.SideLine(f, Old, 1)
			s.SideLine(f, New, 1)
		}
	}
}

// Past maxD edits the search splits where it got furthest; it must still
// rebuild the new side and keep every unchanged line.
func TestMyersStaysLineByLinePastTheCostCap(t *testing.T) {
	a := make([]int, 30000)
	b := make([]int, 30000)
	for i := range a {
		a[i], b[i] = i, i
		if i%7 == 3 {
			b[i] = i + 1000000
		}
	}
	same := 0
	var rebuilt []int
	for _, o := range myers(a, b) {
		switch o.kind {
		case opEqual:
			rebuilt = append(rebuilt, a[o.old:o.old+o.n]...)
			same += o.n
		case opIns:
			rebuilt = append(rebuilt, b[o.new:o.new+o.n]...)
		}
	}
	if !slices.Equal(rebuilt, b) || same != 30000-4286 {
		t.Fatalf("rebuilt %v, kept %d unchanged lines, want %d", slices.Equal(rebuilt, b), same, 30000-4286)
	}
}

func TestMyersIsMinimal(t *testing.T) {
	seed := uint64(0x2545f491)
	next := func(n uint64) int {
		seed ^= seed << 13
		seed ^= seed >> 7
		seed ^= seed << 17
		return int(seed % n)
	}
	lcs := func(a, b []int) int {
		dp := make([][]int, len(a)+1)
		for i := range dp {
			dp[i] = make([]int, len(b)+1)
		}
		for i := len(a) - 1; i >= 0; i-- {
			for j := len(b) - 1; j >= 0; j-- {
				if a[i] == b[j] {
					dp[i][j] = dp[i+1][j+1] + 1
				} else {
					dp[i][j] = max(dp[i+1][j], dp[i][j+1])
				}
			}
		}
		return dp[0][0]
	}
	for round := range 1200 {
		n, alpha := uint64(14), uint64(4)
		if round%3 == 0 {
			n, alpha = 40, 3
		}
		a, b := make([]int, next(n)), make([]int, next(n))
		for i := range a {
			a[i] = next(alpha)
		}
		for i := range b {
			b[i] = next(alpha)
		}
		same := 0
		var rebuilt []int
		for _, o := range myers(a, b) {
			switch o.kind {
			case opEqual:
				if !slices.Equal(a[o.old:o.old+o.n], b[o.new:o.new+o.n]) {
					t.Fatalf("%v -> %v: unequal run %+v", a, b, o)
				}
				rebuilt = append(rebuilt, a[o.old:o.old+o.n]...)
				same += o.n
			case opIns:
				rebuilt = append(rebuilt, b[o.new:o.new+o.n]...)
			}
		}
		if !slices.Equal(rebuilt, b) || same != lcs(a, b) {
			t.Fatalf("%v -> %v: rebuilt %v, kept %d of %d", a, b, rebuilt, same, lcs(a, b))
		}
	}
}

func TestWordChanges(t *testing.T) {
	a, b := words("let total = price * count;", "let total = price * amount + tax;")
	if len(a) != 1 || a[0] != (Range{20, 25}) {
		t.Fatalf("old changes %v", a)
	}
	if n := "let total = price * amount + tax;"; n[b[0].Start:b[0].End] != "amount + tax" {
		t.Fatalf("new change %q", n[b[0].Start:b[0].End])
	}
	if a, b := words("fn main() {}", "SELECT * FROM users"); a != nil || b != nil {
		t.Fatal("lines with nothing in common got word changes")
	}
	if a, b := words("가 나다라마바사아자차카타", "가 하하하하하하하하하하하"); a != nil || b != nil {
		t.Fatal("multibyte lines sharing one character in twelve got word changes")
	}
	if a, b := words("title = \xb0\xa1", "title = \xb0\xa2"); len(a) != 1 || a[0] != (Range{9, 10}) || len(b) != 1 || b[0] != (Range{9, 10}) {
		t.Fatalf("invalid UTF-8 changes %v %v", a, b)
	}
}

func shown(s *State) []string {
	var out []string
	for _, r := range s.rows {
		switch r.kind {
		case rowFile:
			out = append(out, "file")
		case rowNote:
			out = append(out, "note")
		case rowGap:
			out = append(out, fmt.Sprintf("gap%d:%d", r.i, s.Hidden(r.f, r.i)))
		case rowCtx:
			out = append(out, fmt.Sprintf("%d=%d", r.old, r.new))
		default:
			num := func(side Side) string {
				if n, ok := s.rowLine(r, side); ok {
					return fmt.Sprint(n)
				}
				return "-"
			}
			out = append(out, num(Old)+"|"+num(New))
		}
	}
	return out
}

func TestRangesExpandByTwentyAndAll(t *testing.T) {
	old := numbered(100)
	s := NewState([]FileDiff{FromTexts("a.txt", old, strings.Replace(old, "line 50\n", "line fifty\n", 1))})
	if got := shown(s)[:3]; !slices.Equal(got, []string{"file", "gap0:46", "47|47"}) || s.Hidden(0, 1) != 47 {
		t.Fatalf("got %v, hidden after %d", got, s.Hidden(0, 1))
	}
	s.Expand(0, 0, ExpandUp)
	if got := shown(s)[1:4]; s.Hidden(0, 0) != 26 || !slices.Equal(got, []string{"gap0:26", "27=27", "28=28"}) {
		t.Fatalf("after Up: hidden %d, rows %v", s.Hidden(0, 0), got)
	}
	s.Expand(0, 1, ExpandDown)
	rows := shown(s)
	at := slices.Index(rows, "53|53")
	if !slices.Equal(rows[at+1:at+3], []string{"54=54", "55=55"}) || rows[len(rows)-1] != "gap1:27" {
		t.Fatalf("after Down: %v", rows[at:])
	}
	s.Expand(0, 1, ExpandAll)
	s.Expand(0, 0, ExpandAll)
	if len(s.rows) != 1+101 {
		t.Fatalf("%d rows after expanding all, want 102", len(s.rows))
	}
	if l, _ := s.SideLine(0, Old, 10); l != "line 10" {
		t.Fatalf("old line 10 is %q", l)
	}
	if l, _ := s.SideLine(0, New, 50); l != "line fifty" {
		t.Fatalf("new line 50 is %q", l)
	}
}

func TestSplitPairsChangesAndKeepsKeys(t *testing.T) {
	s := NewState([]FileDiff{FromTexts("x", "a\nb\nc\nd\n", "a\nB\nC\nX\nd\n")})
	if got := shown(s); !slices.Equal(got, []string{"file", "1|1", "2|-", "3|-", "-|2", "-|3", "-|4", "4|5"}) {
		t.Fatalf("unified %v", got)
	}
	keyB := s.keys[2]
	s.SetMode(Split)
	if got := shown(s); !slices.Equal(got, []string{"file", "1|1", "2|2", "3|3", "-|4", "4|5"}) || s.keys[2] != keyB {
		t.Fatalf("split %v, key %q want %q", got, s.keys[2], keyB)
	}
}

func TestLineSelectionCopiesOneSide(t *testing.T) {
	s := NewState([]FileDiff{FromTexts("x", "keep\nold one\nold two\nend\n", "keep\nnew one\nnew two\nend\n")})
	s.pressLine(0, LineRef{New, 1}, false)
	s.releaseLine()
	s.pressLine(0, LineRef{New, 4}, true)
	s.releaseLine()
	if got, _ := s.SelectedText(); got != "keep\nnew one\nnew two\nend" {
		t.Fatalf("copied %q", got)
	}
	s.SetSelection(&LineRange{File: 0, Start: LineRef{Old, 3}, End: LineRef{Old, 2}})
	if got, _ := s.SelectedText(); got != "old one\nold two" {
		t.Fatalf("copied %q", got)
	}
	one := LineRef{New, 1}
	s.pressLine(0, one, false)
	s.releaseLine()
	s.pressLine(0, one, false)
	if s.releaseLine() != nil {
		t.Fatal("pressing the one selected line didn't clear it")
	}
	s.pressLine(0, one, false)
	s.releaseLine()
	s.pressLine(0, one, false)
	s.dragTo(0, LineRef{New, 2})
	if sel := s.releaseLine(); sel == nil || sel.End != (LineRef{New, 2}) {
		t.Fatalf("a press that dragged left %v", sel)
	}
}

func TestCancelingAGutterDragRestoresTheSelection(t *testing.T) {
	old := numbered(100)
	s := NewState([]FileDiff{FromTexts("a.txt", old, strings.Replace(old, "line 50\n", "line fifty\n", 1))})
	for _, orig := range []*LineRange{nil, {File: 0, Start: LineRef{Old, 48}, End: LineRef{Old, 48}}} {
		s.SetSelection(orig)
		s.pressLine(0, LineRef{New, 47}, false)
		s.dragTo(0, LineRef{New, 49})
		s.cancelLine()
		if (s.selection == nil) != (orig == nil) || orig != nil && *s.selection != *orig || s.selecting || s.unselect {
			t.Fatalf("after cancel: %v, want %v", s.selection, orig)
		}
	}
}

func TestHunkNavigation(t *testing.T) {
	old := numbered(100)
	new := strings.Replace(old, "line 10\n", "line ten\n", 1)
	new = strings.Replace(new, "line 60\n", "line sixty\n", 1)
	s := NewState([]FileDiff{FromTexts("a", old, new), FromTexts("b", "x\n", "y\n")})
	// The view scrolls each revealed row to the top before the next key.
	scroll := func() {
		var y float32
		for r := range s.reveal {
			y += s.estimate(r)
		}
		s.scrollY = y
	}
	var tops []int
	for s.NextHunk(true) {
		tops = append(tops, s.reveal)
		scroll()
	}
	if !slices.Equal(tops, s.hunkRows) {
		t.Fatalf("j visited %v, want every hunk %v", tops, s.hunkRows)
	}
	if !s.NextHunk(false) || s.reveal != s.hunkRows[len(s.hunkRows)-2] {
		t.Fatalf("k went to %d", s.reveal)
	}
	s.nav, s.scrollY = -1, 0
	if !s.NextFile(true) || s.reveal != s.fileRows[1] {
		t.Fatalf("] went to %d", s.reveal)
	}
	scroll()
	if s.NextFile(true) {
		t.Fatalf("] went to %d", s.reveal)
	}
}
