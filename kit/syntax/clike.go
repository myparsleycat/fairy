package syntax

// C-like languages: JavaScript, TypeScript, JSX, JSON, Rust, Swift and Go.

var (
	jsKeywords = []string{
		"as", "async", "await", "break", "case", "catch", "class", "const", "continue", "debugger", "default", "delete", "do",
		"else", "export", "extends", "finally", "for", "from", "function", "if", "import", "in", "instanceof", "let", "new",
		"of", "return", "static", "super", "switch", "this", "throw", "try", "typeof", "var", "void", "while", "with", "yield",
	}
	tsKeywords = []string{
		"abstract", "asserts", "declare", "enum", "implements", "infer", "interface", "is", "keyof", "module", "namespace",
		"override", "private", "protected", "public", "readonly", "satisfies", "type", "unique",
	}
	tsTypes      = []string{"any", "bigint", "boolean", "never", "number", "object", "string", "symbol", "unknown"}
	jsConstants  = []string{"true", "false", "null", "undefined", "NaN", "Infinity"}
	rustKeywords = []string{
		"as", "async", "await", "break", "const", "continue", "crate", "dyn", "else", "enum", "extern", "fn", "for", "if",
		"impl", "in", "let", "loop", "match", "mod", "move", "mut", "pub", "ref", "return", "self", "Self", "static",
		"struct", "super", "trait", "type", "union", "unsafe", "use", "where", "while", "yield",
	}
	rustTypes = []string{
		"bool", "char", "f32", "f64", "i8", "i16", "i32", "i64", "i128", "isize", "str", "u8", "u16", "u32", "u64", "u128",
		"usize",
	}
	swiftKeywords = []string{
		"actor", "any", "as", "associatedtype", "async", "await", "break", "case", "catch", "class", "continue",
		"convenience", "default", "defer", "deinit", "didSet", "do", "else", "enum", "extension", "fallthrough", "final",
		"fileprivate", "for", "func", "get", "guard", "if", "import", "in", "indirect", "init", "inout", "internal", "is",
		"lazy", "let", "mutating", "nonisolated", "open", "operator", "override", "private", "protocol", "public", "repeat",
		"required", "rethrows", "return", "self", "Self", "set", "some", "static", "struct", "subscript", "super", "switch",
		"throw", "throws", "try", "typealias", "unowned", "var", "weak", "where", "while", "willSet",
	}
	goKeywords = []string{
		"break", "case", "chan", "const", "continue", "default", "defer", "else", "fallthrough", "for", "func", "go", "goto",
		"if", "import", "interface", "map", "package", "range", "return", "select", "struct", "switch", "type", "var",
	}
	goTypes = []string{
		"any", "bool", "byte", "comparable", "complex64", "complex128", "error", "float32", "float64", "int", "int8", "int16",
		"int32", "int64", "rune", "string", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
	}
	goConstants    = []string{"true", "false", "nil", "iota"}
	bools          = []string{"true", "false"}
	jsonConstants  = []string{"true", "false", "null"}
	swiftConstants = []string{"true", "false", "nil"}
	// Keywords after which a name is a function or a type being defined.
	definesFunction = []string{"fn", "function", "func"}
	definesType     = []string{"class", "struct", "enum", "trait", "interface", "type", "protocol", "extension", "typealias", "actor", "union"}
	// Keywords that end an expression, so a / after them divides and a < compares.
	valueKeywords = []string{"this", "super", "self", "Self"}
)

type nextName uint8

const (
	nextAny nextName = iota
	nextFunction
	nextType
)

func cLike(s string, i, end int, lang Lang, o *out, depth int) {
	js := lang == LangJS || lang == LangTS || lang == LangTSX
	jsx := lang == LangJS || lang == LangTSX
	var keywords, extraKeywords, types, constants []string
	switch lang {
	case LangJS:
		keywords, constants = jsKeywords, jsConstants
	case LangTS, LangTSX:
		keywords, extraKeywords, types, constants = jsKeywords, tsKeywords, tsTypes, jsConstants
	case LangRust:
		keywords, types, constants = rustKeywords, rustTypes, bools
	case LangSwift:
		keywords, constants = swiftKeywords, swiftConstants
	case LangGo:
		keywords, types, constants = goKeywords, goTypes, goConstants
	default:
		constants = jsonConstants
	}
	identExtra := ""
	if js {
		identExtra = "$"
	}
	// Whether the last token ends an expression (a name, literal or closing bracket).
	value := false
	next := nextAny
	afterDot := false
	// A defined function's parameter list: a ( comes next, then its bracket
	// depth while inside it (0 outside), and whether a name there starts a
	// parameter (after ( or a top-level ,).
	sigNext := false
	sig := 0
	wantParam := false
	for i < end && !o.full {
		b := s[i]
		start := i
		if b == ' ' || b == '\t' || b == '\r' || b == '\n' {
			i++
			continue
		}
		if b == '/' && at(s, i+1, end) == '/' {
			i = lineEnd(s, i, end)
			o.push(start, i, Comment)
			continue
		}
		if b == '/' && at(s, i+1, end) == '*' {
			i = blockCommentEnd(s, i+2, end, lang == LangRust || lang == LangSwift)
			o.push(start, i, Comment)
			continue
		}
		wasDot := afterDot
		afterDot = false
		wasNext := next
		next = nextAny
		lit, re := -1, -1
		if lang == LangRust && (b == 'r' || b == 'b') {
			lit = rustPrefixedLiteral(s, i, end)
		}
		if b == '/' && js && !value {
			re = regexEnd(s, i, end)
		}
		if lit >= 0 {
			o.push(start, lit, String)
			i, value = lit, true
		} else if lang == LangRust && b == '\'' {
			// 'x' and '\n' are characters; 'a with no quote after one
			// character is a lifetime.
			c := at(s, i+1, end)
			after := i + 1 + utf8Len(c)
			switch {
			case c == '\\':
				i = stringEnd(s, i, end, '\'', false)
				o.push(start, i, String)
			case c != 0 && at(s, after, end) == '\'':
				i = after + 1
				o.push(start, i, String)
			case isIdentStart(c):
				i = identEnd(s, i+1, end, "")
				o.push(start, i, Keyword)
			default:
				i++
				o.push(start, i, Punctuation)
			}
			value = true
		} else if lang == LangSwift && starts(s, i, end, `"""`) {
			if k := find(s, i+3, end, `"""`); k >= 0 {
				i = k + 3
			} else {
				i = end
			}
			o.push(start, i, String)
			value = true
		} else if b == '"' || (b == '\'' && lang != LangSwift) {
			i = stringEnd(s, i, end, b, lang == LangRust)
			k := i
			for k < end && isSpace(s[k]) {
				k++
			}
			if lang == LangJSON && at(s, k, end) == ':' {
				o.push(start, i, Property)
			} else {
				o.push(start, i, String)
			}
			value = true
		} else if b == '`' && js {
			i = template(s, i, end, lang, o, depth)
			value = true
		} else if b == '`' && lang == LangGo {
			// A raw string: no escapes, any lines, to the next backtick.
			i = end
			for k := start + 1; k < end; k++ {
				if s[k] == '`' {
					i = k + 1
					break
				}
			}
			o.push(start, i, String)
			value = true
		} else if re >= 0 {
			i = re
			o.push(start, i, String)
			value = true
		} else if b == '<' && jsx && !value && (isIdentStart(at(s, i+1, end)) || at(s, i+1, end) == '>') && !typeParameters(s, i, end) {
			i = jsxElement(s, i, end, lang, o, depth)
			value = true
		} else if isDigit(b) || (b == '.' && !value && isDigit(at(s, i+1, end))) {
			i = numberEnd(s, i, end)
			o.push(start, i, Number)
			value = true
		} else if (b == '@' && (lang == LangTS || lang == LangTSX || lang == LangJS || lang == LangSwift)) || (b == '#' && lang == LangSwift) {
			i = identEnd(s, i+1, end, "")
			o.push(start, i, Keyword)
			value = false
		} else if isIdentStart(b) || (b == '$' && js) {
			i = identEnd(s, i, end, identExtra)
			w := s[start:i]
			k := i
			for k < end && (s[k] == ' ' || s[k] == '\t') {
				k++
			}
			call := at(s, k, end) == '('
			param := sig == 1 && wantParam
			// Go capitalizes exported names: `Addr string` and `Addr: x` name a field.
			goField := lang == LangGo && (isIdentStart(at(s, k, end)) || (at(s, k, end) == ':' && at(s, k+1, end) != '='))
			value = true
			tok, ok := Kind(0), true
			switch {
			case lang == LangRust && at(s, i, end) == '!' && at(s, i+1, end) != '=':
				i++
				tok = Function
			case wasDot:
				tok, ok = Function, call
			case isWord(w, constants):
				tok = Number
			case isWord(w, keywords) || isWord(w, extraKeywords):
				value = isWord(w, valueKeywords)
				if isWord(w, definesFunction) {
					next = nextFunction
				} else if isWord(w, definesType) {
					next = nextType
				}
				tok = Keyword
			case isWord(w, types) || wasNext == nextType:
				tok = Type
			case param:
				wantParam = false
				tok = Parameter
			case wasNext == nextFunction || (lang == LangGo && call):
				// Go capitalizes exported functions, so a call is a call
				// before it is a type.
				sigNext = wasNext == nextFunction && call
				tok = Function
			case isAllCaps(w):
				tok = Number
			case isUpper(w[0]) && !goField:
				tok = Type
			default:
				tok, ok = Function, call
			}
			if ok {
				o.push(start, i, tok)
			}
		} else {
			i++
			afterDot = b == '.' && at(s, i, end) != '.'
			value = b == ')' || b == ']'
			switch {
			case b == '(' && sig == 0 && sigNext:
				sig, wantParam = 1, true
			case (b == '(' || b == '[' || b == '{' || b == '<') && sig > 0:
				sig++
			// => in a default value closes nothing.
			case b == '>' && sig > 0 && start > 0 && s[start-1] == '=':
			case (b == ')' || b == ']' || b == '}' || b == '>') && sig > 0:
				sig--
			case b == ',' && sig == 1:
				wantParam = true
			case (b == ':' || b == '=') && sig == 1:
				wantParam = false
			}
			sigNext = false
			o.push(start, i, Punctuation)
		}
	}
}

func utf8Len(lead byte) int {
	switch {
	case lead >= 0xf0:
		return 4
	case lead >= 0xe0:
		return 3
	case lead >= 0xc0:
		return 2
	}
	return 1
}

// rustPrefixedLiteral returns the index past a Rust r"…", r#"…"#, b"…",
// br"…" or b'…' literal starting at i, or -1.
func rustPrefixedLiteral(s string, i, end int) int {
	j := i
	if s[j] == 'b' {
		j++
		switch at(s, j, end) {
		case '"':
			return stringEnd(s, j, end, '"', true)
		case '\'':
			return stringEnd(s, j, end, '\'', false)
		case 'r':
		default:
			return -1
		}
	}
	// A raw string: r, up to 255 #, a quote, and the same quote and #s to close.
	j++
	hashes := 0
	for at(s, j, end) == '#' && hashes < 255 {
		hashes++
		j++
	}
	if at(s, j, end) != '"' {
		return -1
	}
	j++
	for j < end {
		if s[j] == '"' {
			k := j + 1
			n := 0
			for n < hashes && at(s, k, end) == '#' {
				n++
				k++
			}
			if n == hashes {
				return k
			}
			j = k
		} else {
			j++
		}
	}
	return end
}

// regexEnd returns the index past a JavaScript regex literal starting at the
// / at i, if one closes on this line within maxRegex bytes, or -1.
func regexEnd(s string, i, end int) int {
	limit := min(end, i+maxRegex)
	j := i + 1
	class := false
	for j < limit {
		switch s[j] {
		case '\\':
			j++
		case '[':
			class = true
		case ']':
			class = false
		case '\n':
			return -1
		case '/':
			if class {
				break
			}
			j++
			for j < end && isAlpha(s[j]) {
				j++
			}
			if j > i+2 {
				return j
			}
			return -1
		}
		j++
	}
	return -1
}

// template colors a template literal from the backtick at i, its ${}
// expressions as code.
func template(s string, i, end int, lang Lang, o *out, depth int) int {
	seg := i
	j := i + 1
	for j < end && !o.full {
		switch {
		case s[j] == '\\':
			j += 2
		case s[j] == '`':
			o.push(seg, j+1, String)
			return j + 1
		case s[j] == '$' && at(s, j+1, end) == '{':
			o.push(seg, j, String)
			cs := jsComments
			if lang == LangJS || lang == LangTSX {
				cs = jsxComments
			}
			cl := matching(s, j+1, end, '{', '}', cs)
			o.push(j, j+2, Punctuation)
			if depth < maxDepth {
				cLike(s, j+2, cl, lang, o, depth+1)
			}
			if cl >= end {
				return end
			}
			o.push(cl, cl+1, Punctuation)
			j = cl + 1
			seg = j
		default:
			j++
		}
	}
	j = min(j, end)
	o.push(seg, j, String)
	return j
}

// typeParameters reports whether the < at i opens TypeScript type parameters
// rather than a JSX tag: <T,>(…), <T extends U>(…), or <T>(…) followed by =>
// or :. Looks at most maxRegex bytes ahead, on this line.
func typeParameters(s string, i, end int) bool {
	limit := min(end, i+maxRegex)
	angles := 0
	j := i + 1
	for j < limit {
		c := s[j]
		switch {
		case c == '<':
			angles++
		case c == '>' && angles > 0:
			angles--
		case c == '>':
			if at(s, j+1, limit) != '(' {
				return false
			}
			// <T>(x: T) => T is a signature; <b>(note)</b> is an element.
			parens := 0
			k := j + 1
		scan:
			for k < limit && s[k] != '\n' {
				switch s[k] {
				case '(':
					parens++
				case ')':
					parens--
					if parens == 0 {
						break scan
					}
				}
				k++
			}
			if at(s, k, limit) != ')' {
				return true
			}
			k++
			for at(s, k, limit) == ' ' || at(s, k, limit) == '\t' {
				k++
			}
			return starts(s, k, limit, "=>") || at(s, k, limit) == ':'
		case c == ',' && angles == 0:
			return true
		case c == '=' || c == '{' || c == '"' || c == '\'' || c == '/' || c == '\n':
			return false
		case isIdentStart(c):
			e := identEnd(s, j, limit, "")
			if s[j:e] == "extends" {
				return true
			}
			j = e
			continue
		}
		j++
	}
	return false
}

// statementStarts are words that start a statement, not JSX text.
var statementStarts = []string{"export", "import", "const", "let", "var", "function", "class", "interface", "enum", "return"}

// codeLine reports whether the line from i starts like code rather than JSX
// text: with a statement keyword or a closing }, ) or ;. An element still
// open there never closed, so scanning goes back to code instead of taking
// the rest of the block as children.
func codeLine(s string, i, end int) bool {
	for c := at(s, i, end); c == ' ' || c == '\t' || c == '\r'; c = at(s, i, end) {
		i++
	}
	switch c := at(s, i, end); {
	case c == '}' || c == ')' || c == ';':
		return true
	case isIdentStart(c):
		return isWord(s[i:identEnd(s, i, end, "")], statementStarts)
	}
	return false
}

// jsxElement colors a JSX element (with its children and nested elements)
// from the < at i. One that never closes ends at a stray < in a tag or at the
// first line that reads as code.
func jsxElement(s string, i, end int, lang Lang, o *out, depth int) int {
	open := 0
	for {
		closing := at(s, i+1, end) == '/'
		j := i + 1
		if closing {
			j++
		}
		o.push(i, j, Punctuation)
		name := j
		j = identEnd(s, j, end, ".:-$")
		if j > name {
			if isUpper(s[name]) {
				o.push(name, j, Type)
			} else {
				o.push(name, j, Keyword)
			}
		}
		selfClosing, closed := false, false
	tag:
		for j < end && !o.full {
			switch c := s[j]; {
			case c == '>':
				o.push(j, j+1, Punctuation)
				j++
				closed = true
				break tag
			case c == '/' && at(s, j+1, end) == '>':
				o.push(j, j+2, Punctuation)
				j += 2
				selfClosing, closed = true, true
				break tag
			case c == '"' || c == '\'':
				e := stringEnd(s, j, end, c, true)
				o.push(j, e, String)
				j = e
			case c == '{':
				j = jsxExpression(s, j, end, lang, o, depth)
			case c == '=':
				o.push(j, j+1, Punctuation)
				j++
			case c == '<':
				break tag
			case c == '\n' && codeLine(s, j+1, end):
				break tag
			case isIdentStart(c):
				e := identEnd(s, j, end, "-:")
				o.push(j, e, Property)
				j = e
			default:
				j++
			}
		}
		if !closed {
			return min(j, end)
		}
		if closing {
			open = max(open-1, 0)
		} else if !selfClosing {
			open++
		}
		if open == 0 {
			return j
		}
		// Children: text stays plain, {…} is code, < before a name, / or >
		// starts the next tag (a < b is text).
	children:
		for {
			if j >= end || o.full {
				return end
			}
			switch c := s[j]; {
			case c == '<' && (at(s, j+1, end) == '/' || at(s, j+1, end) == '>' || isIdentStart(at(s, j+1, end))):
				break children
			case c == '{':
				j = jsxExpression(s, j, end, lang, o, depth)
			case c == '\n' && codeLine(s, j+1, end):
				return j
			default:
				j++
			}
		}
		i = j
	}
}

func jsxExpression(s string, j, end int, lang Lang, o *out, depth int) int {
	cl := matching(s, j, end, '{', '}', jsxComments)
	o.push(j, j+1, Punctuation)
	if depth < maxDepth {
		cLike(s, j+1, cl, lang, o, depth+1)
	}
	if cl >= end {
		return end
	}
	o.push(cl, cl+1, Punctuation)
	return cl + 1
}
