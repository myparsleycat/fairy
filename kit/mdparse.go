package kit

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type mdKind uint8

const (
	mdPara mdKind = iota
	mdHeading
	mdCode
	mdQuote
	mdList
	mdRule
	mdTable
	mdImage
	mdCallout
)

type mdBlock struct {
	kind  mdKind
	level int    // heading level
	text  string // paragraph and heading inline source, code body, image alt text
	lang  string // code fence info
	open  bool   // a code fence not closed yet
	// images
	src, title string
	// quotes and callouts
	kids    []mdBlock
	callout string // note, tip, important, warning or caution
	// lists
	ordered bool
	start   int
	items   []mdItem
	// tables
	head   []string
	aligns []CellAlign
	rows   [][]string
}

type mdItem struct {
	task, checked bool
	blocks        []mdBlock
}

var (
	reHeading = regexp.MustCompile(`^ {0,3}(#{1,6})(?:[ \t]+(.*?))?(?:[ \t]+#+)?[ \t]*$`)
	reFence   = regexp.MustCompile("^( {0,3})(`{3,}|~{3,})[ \t]*([^`]*)$")
	reRule    = regexp.MustCompile(`^ {0,3}(?:(?:-[ \t]*){3,}|(?:\*[ \t]*){3,}|(?:_[ \t]*){3,})$`)
	reQuote   = regexp.MustCompile(`^ {0,3}> ?`)
	reBullet  = regexp.MustCompile(`^( {0,3})([-*+])([ \t]+|$)`)
	reOrdered = regexp.MustCompile(`^( {0,3})(\d{1,9})([.)])([ \t]+|$)`)
	reDelim   = regexp.MustCompile(`^ *\|? *:?-+:? *(\| *:?-+:? *)*\|? *$`)
	reTask    = regexp.MustCompile(`^\[([ xX])\][ \t]+`)
	reCallout = regexp.MustCompile(`(?i)^ {0,3}\[!(note|tip|important|warning|caution)\][ \t]*$`)
)

// parseMarkdown splits source into blocks. It covers CommonMark's common
// constructs and GFM tables, task lists and strikethrough; an unclosed code
// fence runs to the end so partially streamed text renders sensibly.
func parseMarkdown(src string) []mdBlock {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.ReplaceAll(src, "\t", "    ")
	return parseBlocks(strings.Split(src, "\n"))
}

func isBlank(s string) bool { return strings.TrimSpace(s) == "" }

type listMarker struct {
	ordered bool
	char    byte // bullet char or ordered delimiter
	num     int
	indent  int // width of the marker including following spaces
	rest    string
}

func matchList(line string) (listMarker, bool) {
	if m := reBullet.FindStringSubmatch(line); m != nil && !reRule.MatchString(line) {
		w := len(m[0])
		if len(m[3]) > 4 { // a long gap means indented content; the marker takes one space
			w = len(m[1]) + 2
		}
		return listMarker{char: m[2][0], indent: w, rest: line[min(w, len(line)):]}, true
	}
	if m := reOrdered.FindStringSubmatch(line); m != nil {
		n, _ := strconv.Atoi(m[2])
		w := len(m[0])
		if len(m[4]) > 4 {
			w = len(m[1]) + len(m[2]) + 2
		}
		return listMarker{ordered: true, char: m[3][0], num: n, indent: w, rest: line[min(w, len(line)):]}, true
	}
	return listMarker{}, false
}

func isListStart(line string) bool { _, ok := matchList(line); return ok }

// interrupts reports whether line starts a block that ends a paragraph.
func interrupts(line string) bool {
	if reHeading.MatchString(line) || reFence.MatchString(line) || reRule.MatchString(line) || reQuote.MatchString(line) {
		return true
	}
	if m, ok := matchList(line); ok && !isBlank(m.rest) {
		return !m.ordered || m.num == 1
	}
	return false
}

func splitRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	if strings.HasSuffix(line, "|") && !strings.HasSuffix(line, `\|`) {
		line = line[:len(line)-1]
	}
	var cells []string
	var b strings.Builder
	code := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\\' && i+1 < len(line) && line[i+1] == '|':
			b.WriteByte('|')
			i++
		case c == '`':
			code = !code
			b.WriteByte(c)
		case c == '|' && !code:
			cells = append(cells, strings.TrimSpace(b.String()))
			b.Reset()
		default:
			b.WriteByte(c)
		}
	}
	return append(cells, strings.TrimSpace(b.String()))
}

// Sparse rows are padded to the header width. Bound that expansion before
// allocating cells; oversized tables retain their complete source as text.
const (
	mdMaxTableColumns = 256
	mdMaxTableCells   = 16384
)

func parseBlocks(lines []string) []mdBlock {
	out, _ := parseBlocksAt(lines)
	return out
}

// parseBlocksAt is parseBlocks that also returns the line each block starts
// on. Blocks made from one paragraph share its first line.
func parseBlocksAt(lines []string) (out []mdBlock, starts []int) {
	for i := 0; i < len(lines); {
		line := lines[i]
		from, n := i, len(out)
		switch {
		case isBlank(line):
			i++

		case reFence.MatchString(line):
			m := reFence.FindStringSubmatch(line)
			indent, fence := len(m[1]), m[2]
			var body []string
			closed := false
			i++
			for i < len(lines) {
				l := lines[i]
				t := strings.TrimLeft(l, " ")
				if len(l)-len(t) <= 3 && strings.HasPrefix(t, fence[:3]) &&
					strings.Trim(strings.TrimSpace(t), fence[:1]) == "" && len(strings.TrimSpace(t)) >= len(fence) {
					i++
					closed = true
					break
				}
				// strip up to the fence's indentation
				n := 0
				for n < indent && n < len(l) && l[n] == ' ' {
					n++
				}
				body = append(body, l[n:])
				i++
			}
			b := mdBlock{kind: mdCode, text: strings.Join(body, "\n"), open: !closed}
			if f := strings.Fields(m[3]); len(f) > 0 {
				b.lang = f[0]
			}
			out = append(out, b)

		case reHeading.MatchString(line):
			m := reHeading.FindStringSubmatch(line)
			out = append(out, mdBlock{kind: mdHeading, level: len(m[1]), text: strings.TrimSpace(m[2])})
			i++

		case reRule.MatchString(line):
			out = append(out, mdBlock{kind: mdRule})
			i++

		case reQuote.MatchString(line):
			var inner []string
			for i < len(lines) {
				l := lines[i]
				if loc := reQuote.FindStringIndex(l); loc != nil {
					inner = append(inner, l[loc[1]:])
				} else if !isBlank(l) && len(inner) > 0 && !isBlank(inner[len(inner)-1]) && !interrupts(l) {
					inner = append(inner, l) // lazy continuation
				} else {
					break
				}
				i++
			}
			if m := reCallout.FindStringSubmatch(inner[0]); m != nil {
				out = append(out, mdBlock{kind: mdCallout, callout: strings.ToLower(m[1]), kids: parseBlocks(inner[1:])})
			} else {
				out = append(out, mdBlock{kind: mdQuote, kids: parseBlocks(inner)})
			}

		case isListStart(line):
			var b mdBlock
			i, b = parseList(lines, i)
			out = append(out, b)

		case strings.Contains(line, "|") && i+1 < len(lines) && reDelim.MatchString(lines[i+1]) && strings.Contains(lines[i+1], "-"):
			b := mdBlock{kind: mdTable, head: splitRow(line)}
			fallback := len(b.head) > mdMaxTableColumns
			delims := splitRow(lines[i+1])
			for _, d := range delims[:min(len(b.head), len(delims))] {
				a := CellStart
				switch {
				case strings.HasPrefix(d, ":") && strings.HasSuffix(d, ":"):
					a = CellCenter
				case strings.HasSuffix(d, ":"):
					a = CellEnd
				}
				b.aligns = append(b.aligns, a)
			}
			i += 2
			cells := len(b.head)
			for i < len(lines) && !isBlank(lines[i]) && strings.Contains(lines[i], "|") && !interrupts(lines[i]) {
				fallback = fallback || len(b.head) > mdMaxTableCells-cells
				if !fallback {
					row := splitRow(lines[i])
					row = append(row, make([]string, max(len(b.head)-len(row), 0))...)
					b.rows = append(b.rows, row[:len(b.head)])
					cells += len(b.head)
				}
				i++
			}
			for len(b.aligns) < len(b.head) {
				b.aligns = append(b.aligns, CellStart)
			}
			if fallback {
				b = mdBlock{kind: mdPara, text: strings.Join(lines[from:i], "\n")}
			}
			out = append(out, b)

		default:
			var para []string
			for i < len(lines) && !isBlank(lines[i]) && (len(para) == 0 || !interrupts(lines[i])) {
				para = append(para, strings.TrimLeft(lines[i], " "))
				i++
			}
			out = append(out, liftImages(strings.Join(para, "\n"))...)
		}
		for range len(out) - n {
			starts = append(starts, from)
		}
	}
	return out, starts
}

func parseList(lines []string, i int) (int, mdBlock) {
	first, _ := matchList(lines[i])
	b := mdBlock{kind: mdList, ordered: first.ordered, start: first.num}
	for i < len(lines) {
		m, ok := matchList(lines[i])
		if !ok || m.ordered != first.ordered || m.char != first.char {
			break
		}
		body := []string{m.rest}
		i++
		for i < len(lines) {
			l := lines[i]
			lead := len(l) - len(strings.TrimLeft(l, " "))
			switch {
			case isBlank(l):
				// a blank line belongs to the item only if indented content follows
				j := i
				for j < len(lines) && isBlank(lines[j]) {
					j++
				}
				if j < len(lines) && len(lines[j])-len(strings.TrimLeft(lines[j], " ")) >= m.indent {
					for ; i < j; i++ {
						body = append(body, "")
					}
					continue
				}
				goto done
			case lead >= m.indent:
				body = append(body, l[m.indent:])
			case !isBlank(body[len(body)-1]) && !interrupts(l) && !isListStart(l):
				body = append(body, l) // lazy continuation
			default:
				goto done
			}
			i++
		}
	done:
		it := mdItem{}
		if t := reTask.FindStringSubmatch(body[0]); t != nil {
			it.task, it.checked = true, t[1] != " "
			body[0] = body[0][len(t[0]):]
		}
		it.blocks = parseBlocks(body)
		b.items = append(b.items, it)
		// blank lines between items keep the list going
		j := i
		for j < len(lines) && isBlank(lines[j]) {
			j++
		}
		if j < len(lines) && j > i {
			if n, ok := matchList(lines[j]); ok && n.ordered == first.ordered && n.char == first.char {
				i = j
			}
		}
	}
	return i, b
}

// Inline markup.

type mdStyle uint8

const (
	mdBold mdStyle = 1 << iota
	mdItalic
	mdStrike
	mdCodeSpan
)

type mdSpan struct {
	text  string
	style mdStyle
	link  string
}

func parseInline(s string) []mdSpan {
	var p inlineParser
	p.run(s, 0, "")
	return p.out
}

type inlineParser struct {
	out []mdSpan
	buf strings.Builder
	// collect records top-level images in images instead of writing them.
	collect bool
	images  []mdImageAt
	depth   int
}

func (p *inlineParser) flush(style mdStyle, link string) {
	if p.buf.Len() == 0 {
		return
	}
	t := p.buf.String()
	p.buf.Reset()
	if n := len(p.out); n > 0 && p.out[n-1].style == style && p.out[n-1].link == link {
		p.out[n-1].text += t
		return
	}
	p.out = append(p.out, mdSpan{t, style, link})
}

func isPunct(c byte) bool {
	return c < utf8.RuneSelf && unicode.IsPunct(rune(c)) || strings.IndexByte("$+<=>^`|~", c) >= 0
}

func isSpaceAt(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return unicode.IsSpace(r)
}

func isWordAt(s string, i int) bool {
	if i >= len(s) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func isWordBefore(s string, i int) bool {
	if i <= 0 {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// codeEnd returns the index after the code span starting at i, or -1.
func codeEnd(s string, i int) (n, end int) {
	for n = 0; i+n < len(s) && s[i+n] == '`'; n++ {
	}
	for j := i + n; j < len(s); {
		k := strings.IndexByte(s[j:], '`')
		if k < 0 {
			return n, -1
		}
		j += k
		m := 0
		for j+m < len(s) && s[j+m] == '`' {
			m++
		}
		if m == n {
			return n, j
		}
		j += m
	}
	return n, -1
}

// closeFinder finds closing delimiters in s. The search for a closer walks
// from position to position the same way whatever it starts from, so where
// each position's walk ends is remembered per delimiter: a text full of
// delimiters that never close ("__a __a ...") would otherwise be rescanned
// from every one of them.
type closeFinder struct {
	s string
	// found holds, per delimiter and position, 2 + the closer the walk
	// from there reaches (1 for none), or 0 while unknown.
	found map[string][]int32
	// runFrom and runTo are the delimiter run last measured: run tries
	// emphasis again at each byte of a run that did not open one.
	runFrom, runTo int
}

// runEnd returns the end of the run of s[i] that i is in.
func (f *closeFinder) runEnd(i int) int {
	if i < f.runFrom || i >= f.runTo {
		j := i
		for j < len(f.s) && f.s[j] == f.s[i] {
			j++
		}
		f.runFrom, f.runTo = i, j
	}
	return f.runTo
}

// find finds a closing delimiter d after from, skipping escapes and code
// spans. A closer may not follow whitespace.
func (f *closeFinder) find(from int, d string) int {
	if from >= len(f.s) {
		return -1
	}
	_, next := f.at(from, d)
	return f.walk(next, d)
}

// walk returns the first closer at or after j on the walk through j.
func (f *closeFinder) walk(j int, d string) int {
	if f.found == nil {
		f.found = map[string][]int32{}
	}
	found := f.found[d]
	if found == nil {
		found = make([]int32, len(f.s))
		f.found[d] = found
	}
	var path []int
	end := -1
	for j < len(f.s) {
		if v := found[j]; v != 0 {
			end = int(v) - 2
			break
		}
		path = append(path, j)
		closes, next := f.at(j, d)
		if closes {
			end = j
			break
		}
		j = next
	}
	for _, k := range path {
		found[k] = int32(end + 2)
	}
	return end
}

// at reports whether d closes at j, and otherwise where the walk goes next.
func (f *closeFinder) at(j int, d string) (closes bool, next int) {
	s := f.s
	switch {
	case s[j] == '\\':
		return false, j + 2
	case s[j] == '`':
		n, e := codeEnd(s, j)
		if e >= 0 {
			return false, e + n
		}
		return false, j + n
	case len(d) == 1 && s[j] == d[0] && j+1 < len(s) && s[j+1] == d[0] && !isSpaceAt(s, j+2):
		// a single delimiter skips a nested double one: "*a **b** c*"
		if e := f.find(j+2, d+d); e >= 0 {
			return false, e + 2
		}
	case strings.HasPrefix(s[j:], d) && !isSpaceAt(s, j-1):
		// "_" inside a word does not close
		return d[0] != '_' || !isWordAt(s, j+len(d)), j + 1
	}
	return false, j + 1
}

// linkEnd parses "[text](url)" at i and returns text, url and the end index.
func linkEnd(s string, i int) (text, url string, end int, ok bool) {
	depth := 0
	j := i
	for ; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case '[':
			depth++
		case ']':
			depth--
		}
		if depth == 0 {
			break
		}
	}
	if j >= len(s) || j+1 >= len(s) || s[j+1] != '(' {
		return "", "", 0, false
	}
	text = s[i+1 : j]
	k := j + 2
	depth = 1
	for ; k < len(s); k++ {
		if s[k] == '(' {
			depth++
		} else if s[k] == ')' {
			depth--
			if depth == 0 {
				break
			}
		}
	}
	if k >= len(s) {
		return "", "", 0, false
	}
	dest, _ := splitDest(s[j+2 : k])
	return text, dest, k + 1, true
}

// linkFinder indexes delimiter pairs once per inline source, so failed
// labels and destinations do not search overlapping suffixes repeatedly.
type linkFinder struct {
	s                string
	brackets, parens map[int]int
}

func delimiterPairs(s string, open, close byte, escapes bool) map[int]int {
	pairs := make(map[int]int)
	if strings.IndexByte(s, close) < 0 {
		return pairs
	}
	var stack []int
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			if escapes {
				i++
			}
		case open:
			stack = append(stack, i)
		case close:
			if n := len(stack); n > 0 {
				pairs[stack[n-1]] = i
				stack = stack[:n-1]
			}
		}
	}
	return pairs
}

func (f *linkFinder) find(i int) (text, url string, end int, ok bool) {
	if f.brackets == nil {
		f.brackets = delimiterPairs(f.s, '[', ']', true)
	}
	j, found := f.brackets[i]
	if !found || j+1 >= len(f.s) || f.s[j+1] != '(' {
		return "", "", 0, false
	}
	if f.parens == nil {
		// Destination parentheses follow linkEnd's existing raw balancing rules.
		f.parens = delimiterPairs(f.s, '(', ')', false)
	}
	k, found := f.parens[j+1]
	if !found {
		return "", "", 0, false
	}
	dest, _ := splitDest(f.s[j+2 : k])
	return f.s[i+1 : j], dest, k + 1, true
}

// splitDest splits a link destination and its optional quoted title.
func splitDest(raw string) (dest, title string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	if raw[0] == '<' {
		if k := strings.IndexByte(raw, '>'); k > 0 {
			dest, raw = raw[1:k], raw[k+1:]
		}
	} else if k := strings.IndexFunc(raw, unicode.IsSpace); k >= 0 {
		dest, raw = raw[:k], raw[k:]
	} else {
		dest, raw = raw, ""
	}
	raw = strings.TrimSpace(raw)
	if len(raw) >= 2 {
		switch q, e := raw[0], raw[len(raw)-1]; {
		case q == '"' && e == '"', q == '\'' && e == '\'', q == '(' && e == ')':
			title = raw[1 : len(raw)-1]
		}
	}
	return dest, title
}

type mdImageAt struct {
	start, end      int
	alt, src, title string
}

// liftImages turns a paragraph's images into image blocks of their own, the
// text before each ending a paragraph and the text after it starting one. An
// image inside a link or emphasis, or without a source, stays its alt text in
// the line.
func liftImages(src string) []mdBlock {
	p := inlineParser{collect: true}
	p.run(src, 0, "")
	if len(p.images) == 0 {
		return []mdBlock{{kind: mdPara, text: src}}
	}
	var out []mdBlock
	prev := 0
	for _, im := range p.images {
		if t := strings.TrimRight(src[prev:im.start], " \t\n"); strings.TrimSpace(t) != "" {
			out = append(out, mdBlock{kind: mdPara, text: t})
		}
		out = append(out, mdBlock{kind: mdImage, text: im.alt, src: im.src, title: im.title})
		prev = im.end
	}
	if t := strings.TrimLeft(src[prev:], " \t\n"); t != "" {
		out = append(out, mdBlock{kind: mdPara, text: t})
	}
	return out
}

func plainText(src string) string {
	var b strings.Builder
	for _, sp := range parseInline(src) {
		b.WriteString(sp.text)
	}
	return b.String()
}

// openImage reports whether s[i:] is an image whose destination is still
// open at the end of the text, as while it streams in, and returns its alt
// text.
func openImage(s string, i int) (alt string, ok bool) {
	depth := 0
	for j := i + 1; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case '[':
			depth++
		case ']':
			if depth--; depth == 0 {
				if j+1 < len(s) && s[j+1] == '(' && !strings.ContainsAny(s[j+2:], ")\n") {
					return s[i+2 : j], true
				}
				return "", false
			}
		}
	}
	return "", false
}

var reAutolink = regexp.MustCompile(`^(?:https?://|www\.)[^\s<]+`)

// reEmail is CommonMark's email autolink, with at least one dot in the domain.
var reEmail = regexp.MustCompile(`^[a-zA-Z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)+$`)

func trimAutolink(u string) string {
	opens, closes := strings.Count(u, "("), strings.Count(u, ")")
	for len(u) > 0 {
		c := u[len(u)-1]
		if strings.IndexByte(".,:;!?\"'*_~", c) >= 0 {
			u = u[:len(u)-1]
			continue
		}
		if c == ')' && opens < closes {
			u = u[:len(u)-1]
			closes--
			continue
		}
		break
	}
	return u
}

func (p *inlineParser) run(s string, style mdStyle, link string) {
	closes := closeFinder{s: s}
	links := linkFinder{s: s}
	angleEnd := -1
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s) && isPunct(s[i+1]):
			p.buf.WriteByte(s[i+1])
			i += 2
			continue
		case c == '\\' && i+1 < len(s) && s[i+1] == '\n':
			p.buf.WriteByte('\n')
			i += 2
			continue
		case c == '`':
			n, e := codeEnd(s, i)
			if e < 0 {
				p.buf.WriteString(s[i : i+n])
				i += n
				continue
			}
			code := strings.ReplaceAll(s[i+n:e], "\n", " ")
			if len(code) > 2 && code[0] == ' ' && code[len(code)-1] == ' ' && strings.TrimSpace(code) != "" {
				code = code[1 : len(code)-1]
			}
			p.flush(style, link)
			p.buf.WriteString(code)
			p.flush(style|mdCodeSpan, link)
			i = e + n
			continue
		case c == '*' || c == '_' || c == '~':
			if p.emphasis(&closes, &i, style, link) {
				continue
			}
		case c == '!' && i+1 < len(s) && s[i+1] == '[':
			if text, dest, end, ok := linkEnd(s, i+1); ok {
				if p.collect && p.depth == 0 && dest != "" {
					_, title := splitDest(s[i+len(text)+4 : end-1])
					p.images = append(p.images, mdImageAt{i, end, plainText(text), dest, title})
				} else {
					p.buf.WriteString(text)
				}
				i = end
				continue
			}
			if alt, ok := openImage(s, i); ok {
				p.buf.WriteString(alt)
				i = len(s)
				continue
			}
		case c == '[' && link == "":
			if text, url, end, ok := links.find(i); ok {
				p.flush(style, link)
				p.depth++
				p.run(text, style, url)
				p.depth--
				p.flush(style, url)
				i = end
				continue
			}
		case c == '<' && link == "":
			if angleEnd < i {
				angleEnd = len(s)
				if j := strings.IndexByte(s[i:], '>'); j >= 0 {
					angleEnd = i + j
				}
			}
			if angleEnd < len(s) {
				u, dest := s[i+1:angleEnd], ""
				switch {
				case reAutolink.MatchString(u) && !strings.ContainsAny(u, " <"):
					dest = u
				case reEmail.MatchString(u):
					dest = "mailto:" + u
				}
				if dest != "" {
					p.flush(style, link)
					p.buf.WriteString(u)
					p.flush(style, dest)
					i = angleEnd + 1
					continue
				}
			}
		case (c == 'h' || c == 'w') && link == "" && !isWordBefore(s, i):
			if m := reAutolink.FindString(s[i:]); m != "" {
				u := trimAutolink(m)
				p.flush(style, link)
				p.buf.WriteString(u)
				dest := u
				if strings.HasPrefix(u, "www.") {
					dest = "http://" + u
				}
				p.flush(style, dest)
				i += len(u)
				continue
			}
		}
		_, n := utf8.DecodeRuneInString(s[i:])
		p.buf.WriteString(s[i : i+n])
		i += n
	}
	p.flush(style, link)
}

func (p *inlineParser) emphasis(closes *closeFinder, ip *int, style mdStyle, link string) bool {
	s, i := closes.s, *ip
	c := s[i]
	run := closes.runEnd(i) - i
	if isSpaceAt(s, i+run) || c == '_' && isWordBefore(s, i) {
		return false
	}
	try := func(d string, add mdStyle) bool {
		if run < len(d) {
			return false
		}
		e := closes.find(i+len(d), d)
		if e < 0 || e == i+len(d) {
			return false
		}
		p.flush(style, link)
		p.depth++
		p.run(s[i+len(d):e], style|add, link)
		p.depth--
		*ip = e + len(d)
		return true
	}
	if c == '~' {
		return try("~~", mdStrike)
	}
	d := string(c)
	return run >= 3 && try(d+d+d, mdBold|mdItalic) || try(d+d, mdBold) || try(d, mdItalic)
}
