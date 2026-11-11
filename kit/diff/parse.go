package diff

import (
	"strconv"
	"strings"
)

// Parse reads every file in a unified diff, as git diff, git show, git
// format-patch or diff -u write them: renames, copies, mode changes, added
// and deleted files, binary files and "\ No newline at end of file"
// included. Text around the diffs (a commit message, a mail header) is
// skipped. Word changes are paired as FromTexts pairs them.
func Parse(patch string) []FileDiff {
	var files []FileDiff
	lines := strings.Split(patch, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	var cur *FileDiff
	flush := func() {
		if cur != nil {
			files = append(files, *cur)
			cur = nil
		}
	}
	inHeader := false // in a diff --git section, before its first hunk
	for i := 0; i < len(lines); {
		l := lines[i]
		if rest, ok := strings.CutPrefix(l, "diff --git "); ok {
			flush()
			a, b := gitPaths(rest)
			f := emptyFile(a, b)
			cur, inHeader = &f, true
			i++
			continue
		}
		if strings.HasPrefix(l, "--- ") && i+2 < len(lines) && strings.HasPrefix(lines[i+1], "+++ ") && strings.HasPrefix(lines[i+2], "@@ ") {
			a, b := pathOf(l[4:]), pathOf(lines[i+1][4:])
			if cur != nil && inHeader {
				// These name the paths unambiguously, and /dev/null for none.
				cur.OldPath, cur.NewPath = a, b
			} else {
				flush()
				f := emptyFile(a, b)
				cur = &f
			}
			inHeader = false
			i += 2
			continue
		}
		if cur != nil {
			if strings.HasPrefix(l, "@@ ") {
				inHeader = false
				if h, ok := hunkHeader(l); ok {
					h, next := hunkBody(h, lines, i+1)
					cur.Hunks = append(cur.Hunks, h)
					i = next
					continue
				}
			} else if inHeader {
				headerLine(cur, l)
			}
		}
		i++
	}
	flush()
	for i := range files {
		f := &files[i]
		if f.Status == Modified {
			f.Status = statusOf(f.OldPath, f.NewPath)
			if f.OldPath == "" && f.NewPath == "" {
				f.Status = Modified
			}
		}
		f.pairWords()
	}
	return files
}

func headerLine(f *FileDiff, l string) {
	word := func(p string) (string, bool) {
		s, ok := strings.CutPrefix(l, p)
		return strings.TrimSpace(s), ok
	}
	if m, ok := word("old mode "); ok {
		f.OldMode = m
	} else if m, ok := word("new mode "); ok {
		f.NewMode = m
	} else if m, ok := word("new file mode "); ok {
		f.NewMode, f.Status, f.OldPath = m, FileAdded, ""
	} else if m, ok := word("deleted file mode "); ok {
		f.OldMode, f.Status, f.NewPath = m, FileDeleted, ""
	} else if p, ok := word("rename from "); ok {
		f.OldPath, f.Status = unquote(p), Renamed
	} else if p, ok := word("rename to "); ok {
		f.NewPath, f.Status = unquote(p), Renamed
	} else if p, ok := word("copy from "); ok {
		f.OldPath, f.Status = unquote(p), Copied
	} else if p, ok := word("copy to "); ok {
		f.NewPath, f.Status = unquote(p), Copied
	} else if s, ok := word("similarity index "); ok {
		if n, err := strconv.ParseUint(strings.TrimRight(s, "%"), 10, 8); err == nil {
			f.Similarity = int(n)
		}
	} else if rest, ok := word("index "); ok {
		if _, mode, ok := strings.Cut(rest, " "); ok {
			if f.OldMode == "" {
				f.OldMode = mode
			}
			if f.NewMode == "" {
				f.NewMode = mode
			}
		}
	} else if strings.HasPrefix(l, "Binary files ") || l == "GIT binary patch" {
		f.Binary = true
	}
}

// gitPaths are the two paths of a diff --git a/x b/y line. With unquoted
// paths containing spaces the split is ambiguous; the ---/+++ or rename lines
// that follow correct it.
func gitPaths(rest string) (string, string) {
	if strings.HasPrefix(rest, `"`) {
		if a, b, ok := splitQuoted(rest); ok {
			return stripPrefix(a), stripPrefix(b)
		}
	}
	// Equal paths split in the middle; otherwise at the first " b/".
	half := len(rest) / 2
	if len(rest)%2 == 1 && rest[half] == ' ' {
		a, okA := strings.CutPrefix(rest[:half], "a/")
		b, okB := strings.CutPrefix(rest[half+1:], "b/")
		if okA && okB && a == b {
			return stripPrefix(rest[:half]), stripPrefix(rest[half+1:])
		}
	}
	if at := strings.Index(rest, " b/"); at >= 0 {
		return stripPrefix(rest[:at]), stripPrefix(rest[at+1:])
	}
	return "", ""
}

func splitQuoted(rest string) (string, string, bool) {
	end := quotedEnd(rest)
	if end < 0 {
		return "", "", false
	}
	return unquote(rest[:end]), unquote(strings.TrimLeft(rest[end:], " \t")), true
}

// quotedEnd is the byte after the closing quote of the quoted string s
// starts with, or -1.
func quotedEnd(s string) int {
	escaped := false
	for i := 1; i < len(s); i++ {
		switch c := s[i]; {
		case escaped:
			escaped = false
		case c == '\\':
			escaped = true
		case c == '"':
			return i + 1
		}
	}
	return -1
}

// unquote reads a path as git writes it: bare, or in double quotes with C
// escapes (octal bytes for non-ASCII).
func unquote(p string) string {
	if len(p) < 2 || p[0] != '"' || p[len(p)-1] != '"' {
		return p
	}
	in := p[1 : len(p)-1]
	var b []byte
	for i := 0; i < len(in); i++ {
		c := in[i]
		if c != '\\' {
			b = append(b, c)
			continue
		}
		i++
		if i >= len(in) {
			b = append(b, '\\')
			break
		}
		switch d := in[i]; {
		case d == 'n':
			b = append(b, '\n')
		case d == 't':
			b = append(b, '\t')
		case d == 'r':
			b = append(b, '\r')
		case d >= '0' && d <= '7':
			v := int(d - '0')
			for k := 0; k < 2 && i+1 < len(in) && in[i+1] >= '0' && in[i+1] <= '7'; k++ {
				i++
				v = v*8 + int(in[i]-'0')
			}
			b = append(b, byte(v))
		default:
			b = append(b, d)
		}
	}
	return strings.ToValidUTF8(string(b), "�")
}

func stripPrefix(p string) string {
	p = unquote(p)
	if strings.HasPrefix(p, "a/") || strings.HasPrefix(p, "b/") {
		return p[2:]
	}
	return p
}

// pathOf is the path of a --- or +++ line: "" for /dev/null, without a
// trailing timestamp.
func pathOf(s string) string {
	if !strings.HasPrefix(s, `"`) {
		s, _, _ = strings.Cut(s, "\t")
	}
	s = strings.TrimRight(s, " \t")
	if s == "/dev/null" {
		return ""
	}
	return stripPrefix(s)
}

func hunkHeader(l string) (Hunk, bool) {
	rest, ok := strings.CutPrefix(l, "@@ -")
	if !ok {
		return Hunk{}, false
	}
	ranges, sect, ok := strings.Cut(rest, " @@")
	if !ok {
		return Hunk{}, false
	}
	old, new, ok := strings.Cut(ranges, " +")
	if !ok {
		return Hunk{}, false
	}
	rng := func(r string) (int, int, bool) {
		s, c, has := strings.Cut(r, ",")
		start, err := strconv.ParseUint(s, 10, 32)
		if err != nil {
			return 0, 0, false
		}
		count := uint64(1)
		if has {
			if count, err = strconv.ParseUint(c, 10, 32); err != nil {
				return 0, 0, false
			}
		}
		return int(start), int(count), true
	}
	os, oc, ok1 := rng(old)
	ns, nc, ok2 := rng(new)
	if !ok1 || !ok2 {
		return Hunk{}, false
	}
	return Hunk{OldStart: os, OldCount: oc, NewStart: ns, NewCount: nc, Section: strings.TrimSpace(sect)}, true
}

// hunkBody reads the lines of h from lines[i:] until its counts are used up,
// and returns the index of the line after it.
func hunkBody(h Hunk, lines []string, i int) (Hunk, int) {
	old, new := h.first(Old), h.first(New)
	oldLeft, newLeft := h.OldCount, h.NewCount
	for ; i < len(lines); i++ {
		l := lines[i]
		if strings.HasPrefix(l, `\`) {
			if n := len(h.Lines); n > 0 {
				h.Lines[n-1].NoNewline = true
			}
			continue
		}
		if oldLeft == 0 && newLeft == 0 {
			break
		}
		var kind LineKind
		text := ""
		switch {
		case l == "" && i+1 < len(lines):
			kind = Context // a context line whose space an editor trimmed
		case l == "":
			return finish(h, oldLeft, newLeft), i
		case l[0] == ' ':
			kind, text = Context, l[1:]
		case l[0] == '-' && oldLeft > 0:
			kind, text = Removed, l[1:]
		case l[0] == '+' && newLeft > 0:
			kind, text = Added, l[1:]
		default:
			return finish(h, oldLeft, newLeft), i
		}
		switch kind {
		case Context:
			if oldLeft == 0 || newLeft == 0 {
				return finish(h, oldLeft, newLeft), i
			}
			oldLeft, newLeft = oldLeft-1, newLeft-1
			h.Lines = append(h.Lines, Line{Kind: kind, Old: old, New: new, Text: text})
			old, new = old+1, new+1
		case Removed:
			oldLeft--
			h.Lines = append(h.Lines, Line{Kind: kind, Old: old, Text: text})
			old++
		case Added:
			newLeft--
			h.Lines = append(h.Lines, Line{Kind: kind, New: new, Text: text})
			new++
		}
	}
	return finish(h, oldLeft, newLeft), i
}

func finish(h Hunk, oldLeft, newLeft int) Hunk {
	h.OldCount -= oldLeft
	h.NewCount -= newLeft
	return h
}
