package syntax

import "strings"

// Markup (HTML and XML) and CSS.

func markup(s string, i, end int, o *out, depth int) {
	for i < end && !o.full {
		b := s[i]
		switch {
		case starts(s, i, end, "<!--"):
			e := end
			if k := find(s, i+4, end, "-->"); k >= 0 {
				e = k + 3
			}
			o.push(i, e, Comment)
			i = e
		case starts(s, i, end, "<![CDATA["):
			e := end
			if k := find(s, i+9, end, "]]>"); k >= 0 {
				e = k + 3
			}
			o.push(i, e, String)
			i = e
		case b == '<' && (isIdentStart(at(s, i+1, end)) || strings.IndexByte("/!?", at(s, i+1, end)) >= 0 && at(s, i+1, end) != 0):
			e, ns, ne, closing, selfClosing := tag(s, i, end, o)
			i = e
			name := s[ns:ne]
			var lang Lang
			var cl string
			if eqFold(name, "script") {
				lang, cl = LangJS, "</script"
			} else if eqFold(name, "style") {
				lang, cl = LangCSS, "</style"
			}
			if lang != LangNone && !closing && !selfClosing {
				stop := find(s, i, end, cl)
				if stop < 0 {
					stop = end
				}
				if depth < maxDepth {
					scan(lang, s, i, stop, o, depth+1)
				}
				i = stop
			}
		case b == '&':
			limit := min(end, i+32)
			e := -1
			for k := i + 1; k < limit; k++ {
				if !(isAlnum(s[k]) || s[k] == '#') {
					e = k
					break
				}
			}
			if e > i+1 && s[e] == ';' {
				o.push(i, e+1, Number)
				i = e + 1
			} else {
				i++
			}
		default:
			i++
		}
	}
}

func eqFold(a, b string) bool { return len(a) == len(b) && eqFoldASCII(a, b) }

// tag colors a tag from the < at i and returns its end, its name's range,
// whether it closes, and whether it is self-closing or a declaration.
func tag(s string, i, end int, o *out) (e, nameStart, nameEnd int, closing, selfClosing bool) {
	j := i + 1
	closing = at(s, j, end) == '/'
	decl := at(s, j, end) == '!' || at(s, j, end) == '?'
	if closing || decl {
		j++
	}
	o.push(i, j, Punctuation)
	nameStart, nameEnd = j, identEnd(s, j, end, "-:.")
	o.push(nameStart, nameEnd, Keyword)
	j = nameEnd
	for j < end && !o.full {
		switch c := s[j]; {
		case c == '>':
			o.push(j, j+1, Punctuation)
			return j + 1, nameStart, nameEnd, closing, decl
		case (c == '/' || c == '?') && at(s, j+1, end) == '>':
			o.push(j, j+2, Punctuation)
			return j + 2, nameStart, nameEnd, closing, true
		case c == '<':
			return j, nameStart, nameEnd, closing, true
		case c == '"' || c == '\'':
			e := stringEnd(s, j, end, c, true)
			o.push(j, e, String)
			j = e
		case c == '=':
			o.push(j, j+1, Punctuation)
			j++
			v := j
			for j < end && strings.IndexByte(" \t\r\n>\"'<", s[j]) < 0 {
				j++
			}
			o.push(v, j, String)
		case isIdentStart(c):
			e := identEnd(s, j, end, "-:.")
			o.push(j, e, Property)
			j = e
		default:
			j++
		}
	}
	return end, nameStart, nameEnd, closing, true
}

func css(s string, i, end int, o *out) {
	blocks := 0
	// At a statement's start, before its first token; inside a declaration;
	// after its :.
	fresh, decl, value := true, false, false
	identStart := func(k int) bool {
		return isIdentStart(at(s, k, end)) || (at(s, k, end) == '-' && isIdentStart(at(s, k+1, end)))
	}
	for i < end && !o.full {
		b := s[i]
		start := i
		if isSpace(b) {
			i++
			continue
		}
		if b == '/' && at(s, i+1, end) == '*' {
			i = blockCommentEnd(s, i+2, end, false)
			o.push(start, i, Comment)
			continue
		}
		if b == '{' || b == '}' || b == ';' {
			switch b {
			case '{':
				blocks++
			case '}':
				blocks = max(blocks-1, 0)
			}
			i++
			o.push(start, i, Punctuation)
			fresh, decl, value = true, false, false
			continue
		}
		if fresh {
			fresh = false
			// Inside braces, a statement that reaches ; or } (or the end)
			// before { is a declaration; otherwise it is a nested rule's
			// selector.
			k := strings.IndexAny(s[i:end], "{;}")
			decl = blocks > 0 && !(k >= 0 && s[i+k] == '{')
		}
		switch {
		case b == '"' || b == '\'':
			i = stringEnd(s, i, end, b, false)
			o.push(start, i, String)
		case b == ':' && decl && !value:
			i++
			o.push(start, i, Punctuation)
			value = true
		case b == '@':
			i = identEnd(s, i+1, end, "-")
			o.push(start, i, Keyword)
		case b == '#' && value:
			i = identEnd(s, i+1, end, "")
			o.push(start, i, Number)
		case (b == '#' || b == '.') && !value && identStart(i+1):
			i = identEnd(s, i+1, end, "-")
			o.push(start, i, Type)
		case b == ':' && !value:
			for at(s, i, end) == ':' {
				i++
			}
			i = identEnd(s, i, end, "-")
			o.push(start, i, Keyword)
		case b == '!' && value:
			i = identEnd(s, i+1, end, "")
			o.push(start, i, Keyword)
		case isDigit(b) || ((b == '.' || b == '-' || b == '+') && value && isDigit(at(s, i+1, end))):
			from := i
			if b != '.' && !isDigit(b) {
				from++
			}
			i = numberEnd(s, from, end)
			if at(s, i, end) == '%' {
				i++
			}
			o.push(start, i, Number)
		case b == '-' && at(s, i+1, end) == '-':
			i = identEnd(s, i+2, end, "-")
			o.push(start, i, Property)
		case identStart(i):
			i = identEnd(s, i+1, end, "-")
			if decl && !value {
				o.push(start, i, Property)
			} else if at(s, i, end) == '(' {
				o.push(start, i, Function)
			}
		default:
			i++
			o.push(start, i, Punctuation)
		}
	}
}
