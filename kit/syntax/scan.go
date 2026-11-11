// Package syntax colors source code: one small hand-written scanner per
// language family (C-like, Python, shell, markup, CSS, SQL, YAML, diff) walks
// the bytes once and returns kinds of token over byte ranges. Anything a
// scanner doesn't recognize stays uncolored.
//
// Input is untrusted. Every scanner only moves forward, lookaheads are either
// bounded or skip bytes the scanner would visit anyway, and embedded code
// (template ${}, JSX {}, f-string {}, <script>, $()) nests at most three deep,
// so time is linear in the source. Coloring stops after MaxSource bytes (cut
// at a line end) or MaxTokens ranges; the rest stays plain. Ranges start and
// end only on ASCII bytes or the ends of the source, so they fall on character
// boundaries.
//
// A code block without a language is as often program output or prose as
// code, so nothing guesses one: unknown and empty languages stay plain.
package syntax

import (
	"strings"
	"unicode/utf8"
)

// Kind is a kind of token, each with its own color in a theme.
type Kind uint8

const (
	Keyword     Kind = iota // keywords, and markup tag names
	String                  // string, character and regex literals, and YAML's unquoted values
	Number                  // numbers, and constants such as true, null and ALL_CAPS names
	Comment                 // comments, and a diff's header lines
	Type                    // type names, CSS classes and ids, YAML tags
	Function                // functions where called or defined, macros, shell commands
	Property                // JSON and YAML keys, CSS properties, markup attributes, shell and SQL variables
	Punctuation             // brackets, operators and separators
	Inserted                // a diff's added lines
	Deleted                 // a diff's removed lines
	Parameter               // a function's parameters where it is defined
	KindCount               // number of kinds
)

// Lang is a language the scanners know.
type Lang uint8

const (
	LangNone Lang = iota
	LangJS        // JavaScript, with JSX
	LangTS
	LangTSX
	LangJSON
	LangRust
	LangSwift
	LangGo
	LangPython
	LangCSS
	LangShell
	LangHTML // HTML and XML
	LangSQL
	LangYAML
	LangDiff // unified diffs
)

// LangFromName returns the language a code fence's info word names
// (case-insensitive), if the scanners color it.
func LangFromName(name string) (Lang, bool) {
	var l Lang
	switch strings.ToLower(name) {
	case "js", "javascript", "jsx", "mjs", "cjs":
		l = LangJS
	case "ts", "typescript", "mts", "cts":
		l = LangTS
	case "tsx":
		l = LangTSX
	case "json", "jsonc", "json5":
		l = LangJSON
	case "rust", "rs":
		l = LangRust
	case "swift":
		l = LangSwift
	case "go", "golang":
		l = LangGo
	case "python", "py", "python3", "py3", "pyi":
		l = LangPython
	case "css":
		l = LangCSS
	case "bash", "sh", "shell", "zsh", "shellscript":
		l = LangShell
	case "html", "htm", "xhtml", "xml", "svg":
		l = LangHTML
	case "sql", "postgres", "postgresql", "mysql", "sqlite":
		l = LangSQL
	case "yaml", "yml":
		l = LangYAML
	case "diff", "patch":
		l = LangDiff
	default:
		return LangNone, false
	}
	return l, true
}

// Known reports whether the scanners color the language name.
func Known(name string) bool {
	_, ok := LangFromName(name)
	return ok
}

const (
	// MaxSource is how many bytes of a source get color; past it (cut at a
	// line end) the code draws plain.
	MaxSource = 32 * 1024
	// MaxTokens is how many colored ranges a source gets.
	MaxTokens = 8192
	// maxDepth is how deep embedded code nests.
	maxDepth = 3
	// maxRegex is the longest a JavaScript regex literal can be, which bounds
	// the lookahead after a /.
	maxRegex = 512
)

// Token is a colored byte range of the source.
type Token struct {
	Start, End int
	Kind       Kind
}

// ColoredLen returns how much of src gets color: all of it, or up to the
// last line end within MaxSource.
func ColoredLen(src string) int {
	if len(src) <= MaxSource {
		return len(src)
	}
	return max(strings.LastIndexByte(src[:MaxSource], '\n'), 0)
}

// Scan returns the colored ranges of src, in order and not overlapping. It
// returns nil for LangNone.
func Scan(lang Lang, src string) []Token {
	if lang == LangNone {
		return nil
	}
	end := ColoredLen(src)
	var o out
	scan(lang, src, 0, end, &o, 0)
	// The scanners only cut at ASCII bytes; this keeps a slip from ever
	// splitting a character.
	toks := o.tokens[:0]
	for _, t := range o.tokens {
		if boundary(src, t.Start) && boundary(src, t.End) {
			toks = append(toks, t)
		}
	}
	return toks
}

// ScanName scans src in the language a fence names; nil for one the scanners
// don't know or none.
func ScanName(name, src string) []Token {
	l, ok := LangFromName(name)
	if !ok {
		return nil
	}
	return Scan(l, src)
}

func boundary(s string, i int) bool { return i >= len(s) || utf8.RuneStart(s[i]) }

type out struct {
	tokens []Token
	full   bool
}

func (o *out) push(a, b int, k Kind) {
	if a >= b || o.full {
		return
	}
	if n := len(o.tokens); n > 0 && o.tokens[n-1].End == a && o.tokens[n-1].Kind == k {
		o.tokens[n-1].End = b
		return
	}
	if len(o.tokens) >= MaxTokens {
		o.full = true
		return
	}
	o.tokens = append(o.tokens, Token{a, b, k})
}

func scan(lang Lang, s string, i, end int, o *out, depth int) {
	switch lang {
	case LangJS, LangTS, LangTSX, LangJSON, LangRust, LangSwift, LangGo:
		cLike(s, i, end, lang, o, depth)
	case LangPython:
		python(s, i, end, o, depth)
	case LangDiff:
		diff(s, i, end, o)
	case LangCSS:
		css(s, i, end, o)
	case LangShell:
		shell(s, i, end, o, depth)
	case LangHTML:
		markup(s, i, end, o, depth)
	case LangSQL:
		sql(s, i, end, o)
	case LangYAML:
		yaml(s, i, end, o)
	}
}

// Shared helpers.

func at(s string, i, end int) byte {
	if i < end {
		return s[i]
	}
	return 0
}

func isAlpha(b byte) bool { return b|0x20 >= 'a' && b|0x20 <= 'z' }
func isDigit(b byte) bool { return b >= '0' && b <= '9' }
func isAlnum(b byte) bool { return isAlpha(b) || isDigit(b) }
func isUpper(b byte) bool { return b >= 'A' && b <= 'Z' }
func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' || b == '\v'
}

func isIdentStart(b byte) bool { return isAlpha(b) || b == '_' || b >= 0x80 }
func isIdent(b byte) bool      { return isAlnum(b) || b == '_' || b >= 0x80 }

func identEnd(s string, i, end int, extra string) int {
	for i < end && (isIdent(s[i]) || strings.IndexByte(extra, s[i]) >= 0) {
		i++
	}
	return i
}

func lineEnd(s string, i, end int) int {
	if k := strings.IndexByte(s[i:end], '\n'); k >= 0 {
		return i + k
	}
	return end
}

func starts(s string, i, end int, pat string) bool {
	return end-min(i, end) >= len(pat) && s[i:i+len(pat)] == pat
}

func eqFoldASCII(a, b string) bool {
	for k := 0; k < len(a); k++ {
		x, y := a[k], b[k]
		if isUpper(x) {
			x |= 0x20
		}
		if isUpper(y) {
			y |= 0x20
		}
		if x != y {
			return false
		}
	}
	return true
}

// find returns where pat (a few bytes long) next starts at or after i,
// ignoring ASCII case, or -1.
func find(s string, i, end int, pat string) int {
	for k := i; k+len(pat) <= end; k++ {
		if eqFoldASCII(s[k:k+len(pat)], pat) {
			return k
		}
	}
	return -1
}

// stringEnd returns the index past the string whose quote q is at i: after
// the closing quote, at the line end for a single-line string, or end.
func stringEnd(s string, i, end int, q byte, multiline bool) int {
	j := i + 1
	for j < end {
		switch c := s[j]; {
		case c == '\\':
			j += 2
		case c == '\n' && !multiline:
			return j
		case c == q:
			return j + 1
		default:
			j++
		}
	}
	return end
}

// commentStyle is the comments matching skips: JavaScript's // and /* */, or
// the shell's #. jsxComments is JavaScript that also steps over JSX elements,
// whose text is not code (<p>don't</p>).
type commentStyle uint8

const (
	jsComments commentStyle = iota
	jsxComments
	shellComments
)

// frame is where matching is inside JSX: code between brackets (with its
// bracket depth), or an element (with how many of its tags are open, and
// whether it is in a tag rather than text).
type frame struct {
	element bool
	depth   int
	open    int
	inTag   bool
	closing bool
}

// maxFrames is how deep matching follows elements and the code inside them
// before treating < as an operator.
const maxFrames = 32

// jsxStarts reports whether the < at j (code from i) starts a JSX element: it
// follows an operator, an opening bracket or return, and opens a tag rather
// than type parameters.
func jsxStarts(s string, i, j, end int) bool {
	if !(isIdentStart(at(s, j+1, end)) || at(s, j+1, end) == '>') || typeParameters(s, j, end) {
		return false
	}
	k := j
	for k > i && j-k < 64 && isSpace(s[k-1]) {
		k--
	}
	prev := byte('{')
	if k > i {
		prev = s[k-1]
	}
	switch prev {
	case '(', ',', '=', ':', '?', '&', '|', '{', '[', '!', ';', '>':
		return true
	}
	return k >= i+6 && s[k-6:k] == "return" && (k == i+6 || !isIdent(s[k-7]))
}

// matching returns the index of the bracket closing the one at i, skipping
// quoted strings and comments, or end.
func matching(s string, i, end int, open, close byte, comments commentStyle) int {
	stack := []frame{{}}
	j := i
	for j < end {
		top := len(stack) - 1
		if f := &stack[top]; f.element {
			c := s[j]
			if f.inTag {
				switch {
				case c == '"' || c == '\'':
					j = stringEnd(s, j, end, c, true)
					continue
				case c == '{':
					stack = append(stack, frame{})
					continue
				case c == '>' || c == '/' && at(s, j+1, end) == '>':
					if c == '>' {
						j++
					} else {
						j += 2
					}
					if f.closing {
						f.open = max(f.open-1, 0)
					} else if c == '>' {
						f.open++
					}
					f.inTag = false
					if f.open == 0 {
						stack = stack[:top]
					}
					continue
				case c == '<':
					// A tag cut short; read on as code.
					stack = stack[:top]
					continue
				}
			} else {
				switch {
				case c == '{':
					stack = append(stack, frame{})
					continue
				case c == '<' && at(s, j+1, end) == '/':
					f.inTag, f.closing = true, true
					j += 2
					continue
				case c == '<' && (isIdentStart(at(s, j+1, end)) || at(s, j+1, end) == '>'):
					f.inTag, f.closing = true, false
				case c == '\n' && codeLine(s, j+1, end):
					stack = stack[:top]
				}
			}
			j++
			continue
		}
		f := &stack[top]
		switch c := s[j]; {
		case c == '<' && comments == jsxComments && top+3 <= maxFrames && jsxStarts(s, i, j, end):
			stack = append(stack, frame{element: true, inTag: true})
		case c == '\\':
			j++
		case c == '"' || c == '\'' || c == '`':
			j = stringEnd(s, j, end, c, c == '`')
			continue
		case c == '/' && comments != shellComments && at(s, j+1, end) == '/':
			j = lineEnd(s, j, end)
			continue
		case c == '/' && comments != shellComments && at(s, j+1, end) == '*':
			j = blockCommentEnd(s, j+2, end, false)
			continue
		// A # starting a word; $# and a#b are not comments.
		case c == '#' && comments == shellComments && j > i && strings.IndexByte(" \t\n(;|&", s[j-1]) >= 0:
			j = lineEnd(s, j, end)
			continue
		case c == open:
			f.depth++
		case c == close:
			f.depth--
			if f.depth == 0 {
				if top == 0 {
					return j
				}
				stack = stack[:top]
			}
		}
		j++
	}
	return end
}

func numberEnd(s string, i, end int) int {
	hex := s[i] == '0' && (at(s, i+1, end) == 'x' || at(s, i+1, end) == 'X')
	i++
	for i < end {
		c := s[i]
		if isAlnum(c) || c == '_' {
			if !hex && (c == 'e' || c == 'E') && (at(s, i+1, end) == '+' || at(s, i+1, end) == '-') {
				i += 2
			} else {
				i++
			}
		} else if c == '.' && isDigit(at(s, i+1, end)) {
			i++
		} else {
			break
		}
	}
	return min(i, end)
}

func blockCommentEnd(s string, i, end int, nested bool) int {
	depth := 1
	for i < end {
		if s[i] == '*' && at(s, i+1, end) == '/' {
			depth--
			i += 2
			if depth == 0 {
				return i
			}
		} else if nested && s[i] == '/' && at(s, i+1, end) == '*' {
			depth++
			i += 2
		} else {
			i++
		}
	}
	return end
}

func isWord(w string, list []string) bool {
	for _, k := range list {
		if k == w {
			return true
		}
	}
	return false
}

func isWordCI(w string, list []string) bool {
	for _, k := range list {
		if len(k) == len(w) && eqFoldASCII(k, w) {
			return true
		}
	}
	return false
}

func isAllCaps(w string) bool {
	if len(w) <= 1 || !isUpper(w[0]) {
		return false
	}
	for k := 0; k < len(w); k++ {
		if c := w[k]; !isUpper(c) && !isDigit(c) && c != '_' {
			return false
		}
	}
	return true
}

// diff colors whole lines: added, removed, or a header (@@, diff, index,
// ---, +++, \).
func diff(s string, i, end int, o *out) {
	headers := [...]string{"@@", "diff ", "index ", "--- ", "+++ ", "\\"}
	for i < end && !o.full {
		le := lineEnd(s, i, end)
		line := s[i:le]
		kind, ok := Kind(0), false
		for _, h := range headers {
			if strings.HasPrefix(line, h) {
				kind, ok = Comment, true
				break
			}
		}
		if !ok && line != "" {
			switch line[0] {
			case '+':
				kind, ok = Inserted, true
			case '-':
				kind, ok = Deleted, true
			}
		}
		if ok {
			textEnd := le
			if strings.HasSuffix(line, "\r") {
				textEnd--
			}
			o.push(i, textEnd, kind)
		}
		i = le + 1
	}
}
