package syntax

import "strings"

func yaml(s string, i, end int, o *out) {
	// A block scalar (| or >) runs over the lines indented past this column;
	// -1 outside one.
	block := -1
	for i < end && !o.full {
		le := lineEnd(s, i, end)
		indent := 0
		for i+indent < le && s[i+indent] == ' ' {
			indent++
		}
		content := i + indent
		blank := strings.TrimLeft(s[content:le], " \t\r\n\f\v") == ""
		if block >= 0 {
			if blank || indent > block {
				o.push(content, le, String)
				i = le + 1
				continue
			}
			block = -1
		}
		if !blank {
			yamlLine(s, i, content, le, o, &block)
		}
		i = le + 1
	}
}

func yamlLine(s string, line, j, le int, o *out, block *int) {
	content := j
	sep := func(k int) bool { return k >= le || s[k] == ' ' || s[k] == '\t' || s[k] == '\r' }
	if j == line && (starts(s, j, le, "---") || starts(s, j, le, "...")) && sep(j+3) {
		o.push(j, j+3, Punctuation)
		j += 3
	}
	parent := j - line
	for j < le && s[j] == '-' && sep(j+1) {
		o.push(j, j+1, Punctuation)
		parent = j - line
		j++
		for j < le && s[j] == ' ' {
			j++
		}
	}
	if j < le && s[j] != '#' {
		if keyEnd, colon, ok := yamlKey(s, j, le); ok {
			o.push(j, keyEnd, Property)
			o.push(colon, colon+1, Punctuation)
			parent = j - line
			j = colon + 1
		}
	}
	flow := 0
	for j < le && !o.full {
		b := s[j]
		start := j
		switch {
		case b == ' ' || b == '\t' || b == '\r':
			j++
		case b == '#' && (j == content || s[j-1] == ' ' || s[j-1] == '\t'):
			o.push(j, le, Comment)
			return
		case b == '&' || b == '*' || b == '!':
			for j < le && strings.IndexByte(" \t\r,]}", s[j]) < 0 {
				j++
			}
			o.push(start, j, Type)
		case b == '"' || b == '\'':
			j = stringEnd(s, j, le, b, false)
			o.push(start, j, String)
		case (b == '|' || b == '>') && flow == 0:
			j++
			for j < le && (s[j] == '+' || s[j] == '-' || isDigit(s[j])) {
				j++
			}
			o.push(start, j, Punctuation)
			k := j
			for k < le && (s[k] == ' ' || s[k] == '\t' || s[k] == '\r') {
				k++
			}
			if k >= le || s[k] == '#' {
				*block = parent
			}
		case b == '[' || b == '{':
			flow++
			j++
			o.push(start, j, Punctuation)
		case (b == ']' || b == '}' || b == ',' || b == ':') && (flow > 0 || b == ':'):
			if b == ']' || b == '}' {
				flow = max(flow-1, 0)
			}
			j++
			o.push(start, j, Punctuation)
		default:
			// A plain scalar: to a ` #` comment or the line end, or a flow separator.
			e := j
			for e < le && !((s[e] == ' ' || s[e] == '\t') && at(s, e+1, le) == '#') && !(flow > 0 && strings.IndexByte(",]}:", s[e]) >= 0) {
				e++
			}
			t := e
			for t > j && (s[t-1] == ' ' || s[t-1] == '\t' || s[t-1] == '\r') {
				t--
			}
			kind := scalarKind(s[j:t])
			if flow > 0 && at(s, e, le) == ':' {
				kind = Property
			}
			o.push(j, t, kind)
			j = max(e, j+1)
		}
	}
}

// yamlKey finds a mapping key at j: its end and its colon, when a : followed
// by a space or the line end comes before any comment.
func yamlKey(s string, j, le int) (keyEnd, colon int, ok bool) {
	isColon := func(k int) bool {
		return at(s, k, le) == ':' && (k+1 >= le || s[k+1] == ' ' || s[k+1] == '\t' || s[k+1] == '\r')
	}
	if s[j] == '"' || s[j] == '\'' {
		e := stringEnd(s, j, le, s[j], false)
		k := e
		for k < le && s[k] == ' ' {
			k++
		}
		return e, k, isColon(k)
	}
	if strings.IndexByte("[{&*!|>%@`?", s[j]) >= 0 {
		return 0, 0, false
	}
	for k := j; k < le; k++ {
		if isColon(k) {
			e := k
			for e > j && s[e-1] == ' ' {
				e--
			}
			return e, k, true
		}
		if s[k] == '#' && k > j && (s[k-1] == ' ' || s[k-1] == '\t') {
			return 0, 0, false
		}
	}
	return 0, 0, false
}

var yamlConstants = []string{"true", "false", "null", "~", "yes", "no", "on", "off", ".inf", "-.inf", ".nan"}

func scalarKind(w string) Kind {
	if isWordCI(w, yamlConstants) {
		return Number
	}
	digits := w
	if strings.HasPrefix(w, "-") || strings.HasPrefix(w, "+") {
		digits = w[1:]
	}
	if digits == "" || !isDigit(digits[0]) {
		return String
	}
	if strings.HasPrefix(digits, "0x") || strings.HasPrefix(digits, "0o") {
		return Number
	}
	for k := 0; k < len(digits); k++ {
		if c := digits[k]; !isDigit(c) && strings.IndexByte("._eE+-", c) < 0 {
			return String
		}
	}
	return Number
}
