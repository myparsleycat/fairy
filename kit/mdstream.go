package kit

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// mdDoc is a parsed source with the line each top-level block starts on.
type mdDoc struct {
	src    string // with CRLF and tabs normalized
	blocks []mdBlock
	starts []int
}

func normalizeMarkdown(src string) string {
	if !strings.ContainsAny(src, "\r\t") {
		return src
	}
	src = strings.ReplaceAll(src, "\r\n", "\n")
	return strings.ReplaceAll(src, "\t", "    ")
}

// completeLen is the length of the part of src that ends in its last newline.
func completeLen(src string) int { return strings.LastIndexByte(src, '\n') + 1 }

// parseFrom parses src, reusing blocks of prev. The complete lines of prev
// (completeLen) must begin src. A block ends where a later line ends it, so
// the blocks before the one that holds prev's last complete line, or before
// the last line block that starts by then, end where they did; the parse
// starts again at that block.
// Reuse avoids parsing stable blocks, but counting and splitting source lines
// and copying the block index still cost O(N) per update for N source bytes.
func parseFrom(prev *mdDoc, src string) *mdDoc {
	lines := strings.Split(src, "\n")
	keep, from := 0, 0
	if prev != nil {
		last := strings.Count(prev.src, "\n") - 1
		for j := len(prev.starts) - 1; j >= 0; j-- {
			if prev.starts[j] <= last {
				keep = j
				break
			}
		}
		for keep > 0 && prev.starts[keep-1] == prev.starts[keep] {
			keep--
		}
		if keep > 0 {
			from = prev.starts[keep]
		}
	}
	bs, st := parseBlocksAt(lines[from:])
	d := &mdDoc{
		src:    src,
		blocks: make([]mdBlock, 0, keep+len(bs)),
		starts: make([]int, 0, keep+len(st)),
	}
	if keep > 0 {
		d.blocks = append(d.blocks, prev.blocks[:keep]...)
		d.starts = append(d.starts, prev.starts[:keep]...)
	}
	d.blocks = append(d.blocks, bs...)
	for _, s := range st {
		d.starts = append(d.starts, s+from)
	}
	return d
}

// genCache keeps recent entries in two generations: when the current one
// fills up it becomes the old one, so an entry survives at least size puts
// after its last use. It is only touched on the UI thread.
type genCache[V any] struct {
	size     int
	cur, old map[string]V
}

func (c *genCache[V]) get(k string) (V, bool) {
	if v, ok := c.cur[k]; ok {
		return v, true
	}
	if v, ok := c.old[k]; ok {
		delete(c.old, k)
		c.put(k, v)
		return v, true
	}
	var zero V
	return zero, false
}

func (c *genCache[V]) put(k string, v V) {
	if c.cur == nil {
		c.cur = map[string]V{}
	}
	if len(c.cur) >= c.size {
		c.old, c.cur = c.cur, map[string]V{}
	}
	c.cur[k] = v
}

func (c *genCache[V]) drop(k string) {
	delete(c.cur, k)
	delete(c.old, k)
}

func (c *genCache[V]) each(f func(V)) {
	for _, v := range c.cur {
		f(v)
	}
	for _, v := range c.old {
		f(v)
	}
}

var (
	mdDocs  = genCache[*mdDoc]{size: 64}
	mdSpans = genCache[[]mdSpan]{size: 1024}
)

// parseDoc returns the parse of src, from the cache or by extending the
// cached parse that shares the most complete lines with it. A source that
// grew (streamed text) replaces its earlier version in the cache.
func parseDoc(src string) *mdDoc { return parseDocCached(src, true) }

func parseDocCached(src string, replace bool) *mdDoc {
	src = normalizeMarkdown(src)
	if d, ok := mdDocs.get(src); ok {
		return d
	}
	var best *mdDoc
	bestLen := 0
	mdDocs.each(func(d *mdDoc) {
		if n := completeLen(d.src); n > bestLen && strings.HasPrefix(src, d.src[:n]) {
			best, bestLen = d, n
		}
	})
	d := parseFrom(best, src)
	if replace && best != nil && strings.HasPrefix(src, best.src) {
		mdDocs.drop(best.src)
	}
	mdDocs.put(src, d)
	return d
}

func inlineSpans(src string) []mdSpan {
	if s, ok := mdSpans.get(src); ok {
		return s
	}
	s := parseInline(src)
	mdSpans.put(src, s)
	return s
}

// streamView returns the source to parse while d's text is still arriving,
// so that markup at its end draws as what it is about to become: emphasis,
// code and links still open in the last paragraph are closed, markers that
// may yet open or close one are left out, and trailing "|" lines that may
// become a table's header are left out until they are a table or plainly
// not one. Every change is at the end, so the blocks before stay the same.
func streamView(d *mdDoc) string {
	src := d.src
	var last *mdBlock
	if n := len(d.blocks); n > 0 {
		last = &d.blocks[n-1]
	}
	if last != nil && (last.kind == mdCode && last.open || last.kind == mdTable) {
		return src
	}
	end := len(src)
	if cut := heldTable(src); cut >= 0 {
		end = cut
	}
	run := lastRun(src[:end])
	para := last != nil && last.kind == mdPara
	cut, closers := closeTail(src[run:end], para)
	if closers == "" {
		return src[:run+cut]
	}
	return strings.TrimRight(src[:run+cut], " \n") + closers
}

// lastRun returns where the text after the last blank line starts.
func lastRun(src string) int {
	for end := strings.LastIndexByte(src, '\n'); end >= 0; {
		start := strings.LastIndexByte(src[:end], '\n') + 1
		if isBlank(src[start:end]) {
			return end + 1
		}
		end = start - 1
	}
	return 0
}

// heldTable returns where trailing "|" lines start when they may still become
// a table's header and do not draw as a table yet, or -1.
func heldTable(src string) int {
	body := strings.TrimSuffix(src, "\n")
	open := len(body) == len(src)
	start := func(end int) int { return strings.LastIndexByte(body[:end], '\n') + 1 }
	piped := func(l string) bool { return strings.HasPrefix(strings.TrimLeft(l, " "), "|") }
	delim := func(l string) bool { return strings.Trim(strings.TrimSpace(l), "|:- ") == "" }
	line := start(len(body))
	if !piped(body[line:]) {
		return -1
	}
	if open {
		if delim(body[line:]) && line > 0 {
			s := start(line - 1)
			if h := body[s : line-1]; piped(h) && !delim(h) && (s == 0 || !piped(body[start(s-1):s-1])) {
				return s
			}
		}
		return line
	}
	if line == 0 || !piped(body[start(line-1):line-1]) {
		return line
	}
	return -1
}

type mdOpen struct {
	ch byte // '*', '_', '~', '[' for a link's text, '(' for its destination
	n  int
	// image marks a '[' that starts an image's alt text.
	image bool
}

// wordStart reports whether s[i] starts a word: nothing before it, or a space
// or punctuation.
func wordStart(s string, i int) bool {
	if i == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

// closeTail scans run, the inline text a stream ends in, and returns how much
// of it to keep and the closers to add after it. Only an opener at a word's
// start counts, so "2 * 3" and "snake_case" stay text, and "~" only as "~~".
// In a paragraph a span may continue over line breaks; elsewhere each line
// starts afresh.
func closeTail(run string, para bool) (cut int, closers string) {
	var stack []mdOpen
	cut = len(run)
	code := 0
scan:
	for i := 0; i < len(run); {
		c := run[i]
		switch {
		case c == '\\':
			i += 2
			continue
		case c == '\n' && !para:
			stack = stack[:0]
		case c == '`':
			n, e := codeEnd(run, i)
			if e < 0 {
				code = n
				break scan
			}
			i = e + n
			continue
		case c == '!' && i+1 == len(run) && wordStart(run, i):
			cut = i
			break scan
		case c == '[' && (wordStart(run, i) || run[i-1] == '!'):
			stack = append(stack, mdOpen{ch: '[', n: 1, image: i > 0 && run[i-1] == '!'})
		case c == ']':
			p := -1
			for j := len(stack) - 1; j >= 0; j-- {
				if stack[j].ch == '[' {
					p = j
					break
				}
			}
			if p < 0 {
				break
			}
			if i+1 == len(run) {
				cut = i
				break scan
			}
			if run[i+1] == '(' {
				if j := strings.IndexByte(run[i+2:], ')'); j >= 0 {
					stack = stack[:p]
					i += 2 + j + 1
					continue
				}
				if stack[p].image {
					// Left open, the image shows its alt text until the
					// destination is complete, rather than loading every
					// prefix of it.
					stack = stack[:p]
					break scan
				}
				stack = append(stack[:p], mdOpen{ch: '(', n: 1})
				break scan
			}
			stack = append(stack[:p], stack[p+1:]...)
		case (c == 'h' || c == 'w') && !isWordBefore(run, i):
			if m := reAutolink.FindString(run[i:]); m != "" {
				i += len(m)
				continue
			}
		case c == '*' || c == '_' || c == '~':
			n := 1
			for i+n < len(run) && run[i+n] == c {
				n++
			}
			spaceBefore := i == 0 || isSpaceAt(run, i-1)
			if i+n == len(run) {
				mayClose := !spaceBefore && hasOpen(stack, c)
				if wordStart(run, i) || mayClose {
					cut = i
				}
				break scan
			}
			if c == '~' && n < 2 {
				i += n
				continue
			}
			if !spaceBefore && hasOpen(stack, c) {
				for j := len(stack) - 1; j >= 0; j-- {
					if stack[j].ch == c {
						stack = stack[:j]
						break
					}
				}
			} else if wordStart(run, i) && !isSpaceAt(run, i+n) {
				if c == '~' {
					n = 2
				}
				stack = append(stack, mdOpen{ch: c, n: min(n, 3)})
			}
			i += n
			continue
		}
		_, n := utf8.DecodeRuneInString(run[i:])
		i += n
	}
	var b strings.Builder
	if code > 0 {
		b.WriteString(strings.Repeat("`", code))
	}
	for j := len(stack) - 1; j >= 0; j-- {
		switch o := stack[j]; o.ch {
		case '[':
			b.WriteString("]()")
		case '(':
			b.WriteByte(')')
		default:
			b.WriteString(strings.Repeat(string(o.ch), o.n))
		}
	}
	return cut, b.String()
}

func hasOpen(stack []mdOpen, c byte) bool {
	for _, o := range stack {
		if o.ch == c {
			return true
		}
	}
	return false
}
