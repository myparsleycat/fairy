package syntax

import "strings"

var (
	pyKeywords = []string{
		"and", "as", "assert", "async", "await", "break", "class", "cls", "continue", "def", "del", "elif", "else", "except",
		"finally", "for", "from", "global", "if", "import", "in", "is", "lambda", "nonlocal", "not", "or", "pass", "raise",
		"return", "self", "try", "while", "with", "yield",
	}
	// Keywords only at the start of a statement that ends in : (match x:, case [a, b]:).
	pySoftKeywords = []string{"match", "case"}
	pyConstants    = []string{"True", "False", "None", "Ellipsis", "NotImplemented"}
	pyTypes        = []string{
		"bool", "bytearray", "bytes", "complex", "dict", "float", "frozenset", "int", "list", "object", "set", "str", "tuple",
		"type",
	}
)

func python(s string, i, end int, o *out, depth int) {
	next := nextAny
	afterDot := false
	// Before the line's first token (decorators and soft keywords only count there).
	lineStart := depth == 0
	for i < end && !o.full {
		b := s[i]
		start := i
		if b == '\n' {
			i++
			lineStart = true
			continue
		}
		if b == ' ' || b == '\t' || b == '\r' {
			i++
			continue
		}
		if b == '#' {
			i = lineEnd(s, i, end)
			o.push(start, i, Comment)
			continue
		}
		first := lineStart
		lineStart = false
		wasDot := afterDot
		afterDot = false
		wasNext := next
		next = nextAny
		if q, f, ok := pyStringStart(s, i, end); ok {
			i = pyString(s, i, q, f, end, o, depth)
		} else if isDigit(b) || (b == '.' && isDigit(at(s, i+1, end))) {
			i = numberEnd(s, i, end)
			o.push(start, i, Number)
		} else if b == '@' && first && isIdentStart(at(s, i+1, end)) {
			i = identEnd(s, i+1, end, ".")
			o.push(start, i, Keyword)
		} else if isIdentStart(b) {
			i = identEnd(s, i, end, "")
			w := s[start:i]
			k := i
			for k < end && (s[k] == ' ' || s[k] == '\t') {
				k++
			}
			call := at(s, k, end) == '('
			tok, ok := Kind(0), true
			switch {
			case wasDot:
				tok, ok = Function, call
			case isWord(w, pyConstants):
				tok = Number
			case isWord(w, pyKeywords):
				switch w {
				case "def":
					next = nextFunction
				case "class":
					next = nextType
				}
				tok = Keyword
			case first && isWord(w, pySoftKeywords) && softKeyword(s, i, end):
				tok = Keyword
			case wasNext == nextType:
				tok = Type
			case wasNext == nextFunction:
				tok = Function
			case isWord(w, pyTypes):
				tok = Type
			case isAllCaps(w):
				tok = Number
			case isUpper(w[0]):
				tok = Type
			default:
				tok, ok = Function, call
			}
			if ok {
				o.push(start, i, tok)
			}
		} else {
			i++
			afterDot = b == '.'
			o.push(start, i, Punctuation)
		}
	}
}

// pyStringStart reports whether a string literal starts at i: its quote's
// index, and whether it is an f-string. Up to two prefix letters (r, b, u, f,
// rb, fr, in either case) come before the quote.
func pyStringStart(s string, i, end int) (q int, f, ok bool) {
	for k := i; k < min(i+3, end); k++ {
		switch s[k] {
		case '"', '\'':
			return k, f, true
		case 'f', 'F':
			f = true
		case 'r', 'R', 'b', 'B', 'u', 'U':
		default:
			return 0, false, false
		}
	}
	return 0, false, false
}

// pyString colors a string from start (its prefix) whose quote is at q:
// single or triple quoted, an f-string's {} fields as code.
func pyString(s string, start, q int, f bool, end int, o *out, depth int) int {
	qc := s[q]
	triple := at(s, q+1, end) == qc && at(s, q+2, end) == qc
	var e int
	if triple {
		j := q + 3
		for {
			if j >= end {
				e = end
				break
			}
			if s[j] == '\\' {
				j += 2
			} else if s[j] == qc && at(s, j+1, end) == qc && at(s, j+2, end) == qc {
				e = j + 3
				break
			} else {
				j++
			}
		}
	} else {
		e = stringEnd(s, q, end, qc, false)
	}
	e = min(e, end)
	if !f || depth >= maxDepth {
		o.push(start, e, String)
		return e
	}
	seg := start
	j := q + 1
	if triple {
		j = q + 3
	}
	for j < e && !o.full {
		switch {
		case s[j] == '\\':
			j += 2
		case s[j] == '{' && at(s, j+1, e) == '{':
			j += 2
		case s[j] == '{':
			// The field runs to its matching }, stepping over strings in other quotes.
			k := j + 1
			braces := 1
		field:
			for k < e {
				switch c := s[k]; {
				case c == '{':
					braces++
				case c == '}':
					braces--
					if braces == 0 {
						break field
					}
				case (c == '"' || c == '\'') && c != qc:
					k = stringEnd(s, k, e, c, false)
					continue
				}
				k++
			}
			if k >= e {
				o.push(seg, e, String)
				return e
			}
			o.push(seg, j, String)
			o.push(j, j+1, Punctuation)
			python(s, j+1, k, o, depth+1)
			o.push(k, k+1, Punctuation)
			j = k + 1
			seg = j
		default:
			j++
		}
	}
	o.push(seg, e, String)
	return e
}

// softKeyword reports whether the soft keyword ending at i starts a
// statement: a space follows it and the line ends with : (before any comment).
func softKeyword(s string, i, end int) bool {
	if at(s, i, end) != ' ' {
		return false
	}
	line := s[i:lineEnd(s, i, end)]
	if p := strings.IndexByte(line, '#'); p >= 0 {
		line = line[:p]
	}
	return strings.HasSuffix(strings.TrimRight(line, " \t\r\n\f\v"), ":")
}
