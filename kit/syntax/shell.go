package syntax

import "strings"

var (
	shellKeywords = []string{
		"if", "then", "else", "elif", "fi", "for", "while", "until", "do", "done", "case", "esac", "in", "function", "select",
		"time", "return", "exit", "export", "local", "readonly", "declare", "unset", "shift", "break", "continue",
	}
	// Words after which the next word is a command too.
	shellLeads    = []string{"if", "then", "else", "elif", "do", "while", "until", "time", "function", "!"}
	shellWrappers = []string{"sudo", "exec", "nohup", "xargs", "env", "command", "builtin"}
)

const shellWordStops = " \t\r\n|&;()<>'\"$`\\"

func shell(s string, i, end int, o *out, depth int) {
	command := true
	// A heredoc's delimiter (its byte range) and whether <<- strips leading tabs.
	hdA, hdZ, hdDash, heredoc := 0, 0, false, false
	for i < end && !o.full {
		b := s[i]
		start := i
		switch {
		case b == '\n':
			i++
			command = true
			if heredoc {
				heredoc = false
				i = heredocBody(s, i, end, s[hdA:hdZ], hdDash, o)
			}
		case b == ' ' || b == '\t' || b == '\r':
			i++
		case b == '#':
			i = lineEnd(s, i, end)
			o.push(start, i, Comment)
		case b == '\'':
			i = end
			if k := strings.IndexByte(s[start+1:end], '\''); k >= 0 {
				i = start + k + 2
			}
			o.push(start, i, String)
			command = false
		case b == '"':
			i = shellString(s, i, end, o, depth)
			command = false
		case b == '$':
			i = dollar(s, i, end, o, depth)
			command = false
		case b == '`':
			cl := end
			if k := strings.IndexByte(s[i+1:end], '`'); k >= 0 {
				cl = i + 1 + k
			}
			o.push(i, i+1, Punctuation)
			if depth < maxDepth {
				shell(s, i+1, cl, o, depth+1)
			}
			o.push(cl, min(cl+1, end), Punctuation)
			i = min(cl+1, end)
			command = false
		case b == '\\':
			// A line continuation keeps whether the next word is a command (after &&).
			crlf := at(s, i+1, end) == '\r' && at(s, i+2, end) == '\n'
			continues := crlf || at(s, i+1, end) == '\n'
			step := 1 + utf8Len(at(s, i+1, end))
			if crlf {
				step = 3
			}
			i = min(i+step, end)
			command = command && continues
		case b == '<' && at(s, i+1, end) == '<' && at(s, i+2, end) != '<':
			i += 2
			dash := at(s, i, end) == '-'
			if dash {
				i++
			}
			o.push(start, i, Punctuation)
			for at(s, i, end) == ' ' || at(s, i, end) == '\t' {
				i++
			}
			q := at(s, i, end)
			quoted := q == '\'' || q == '"'
			name := i
			if quoted {
				name++
			}
			nameEnd := identEnd(s, name, end, "-")
			if nameEnd > name {
				i = nameEnd
				if quoted && at(s, nameEnd, end) == q {
					i++
				}
				from := start + 2
				if dash {
					from++
				}
				o.push(from, i, String)
				hdA, hdZ, hdDash, heredoc = name, nameEnd, dash, true
			}
			command = false
		case strings.IndexByte("|&;(){}<>", b) >= 0:
			i++
			o.push(start, i, Punctuation)
			command = b != ')' && b != '<' && b != '>'
		default:
			for i < end && strings.IndexByte(shellWordStops, s[i]) < 0 {
				i++
			}
			w := s[start:i]
			eq := strings.IndexByte(w, '=')
			assign := command && eq > 0
			for k := 0; assign && k < eq; k++ {
				assign = isAlnum(w[k]) || w[k] == '_'
			}
			switch {
			case w == "[" || w == "[[":
				o.push(start, i, Punctuation)
				command = false
			case w == "]" || w == "]]":
				o.push(start, i, Punctuation)
			case assign:
				// FOO=bar before a command.
				o.push(start, start+eq, Property)
				o.push(start+eq, start+eq+1, Punctuation)
			case command:
				if isWord(w, shellKeywords) {
					o.push(start, i, Keyword)
					command = isWord(w, shellLeads)
				} else {
					o.push(start, i, Function)
					command = isWord(w, shellWrappers)
				}
			case w == "in":
				o.push(start, i, Keyword)
			case w != "" && strings.Trim(w, "0123456789") == "":
				o.push(start, i, Number)
			}
			if w == "" {
				i++
			}
		}
	}
}

// shellString colors a double-quoted shell string from i, its $ expansions
// as variables.
func shellString(s string, i, end int, o *out, depth int) int {
	seg := i
	j := i + 1
	for j < end && !o.full {
		switch c := s[j]; {
		case c == '\\':
			j += 2
		case c == '"':
			o.push(seg, j+1, String)
			return j + 1
		case c == '$' && (at(s, j+1, end) == '{' || at(s, j+1, end) == '(' || isIdentStart(at(s, j+1, end))):
			o.push(seg, j, String)
			j = dollar(s, j, end, o, depth)
			seg = j
		default:
			j++
		}
	}
	j = min(j, end)
	o.push(seg, j, String)
	return j
}

// dollar colors a $ expansion from i: $name, ${…}, $1, $?, or a $(…)
// command substitution.
func dollar(s string, i, end int, o *out, depth int) int {
	c := at(s, i+1, end)
	switch {
	case c == '{':
		e := end
		if k := strings.IndexAny(s[i:end], "}\n"); k >= 0 {
			e = i + k
			if s[e] == '}' {
				e++
			}
		}
		o.push(i, e, Property)
		return e
	case c == '(':
		cl := matching(s, i+1, end, '(', ')', shellComments)
		o.push(i, i+2, Punctuation)
		if depth < maxDepth {
			shell(s, i+2, cl, o, depth+1)
		}
		if cl >= end {
			return end
		}
		o.push(cl, cl+1, Punctuation)
		return cl + 1
	case isAlpha(c) || c == '_':
		e := i + 1
		for e < end && (isAlnum(s[e]) || s[e] == '_') {
			e++
		}
		o.push(i, e, Property)
		return e
	case c != 0 && strings.IndexByte("?@#*$!-0123456789", c) >= 0:
		o.push(i, i+2, Property)
		return i + 2
	}
	return i + 1
}

// heredocBody colors a heredoc's lines from i up to and including the line
// that is just delim.
func heredocBody(s string, i, end int, delim string, dash bool, o *out) int {
	j := i
	for j < end {
		le := lineEnd(s, j, end)
		line := s[j:le]
		if dash {
			line = strings.TrimLeft(line, "\t")
		}
		if strings.TrimSuffix(line, "\r") == delim {
			o.push(i, le, String)
			return le
		}
		j = le + 1
	}
	o.push(i, end, String)
	return end
}
