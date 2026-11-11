package syntax

var (
	sqlKeywords = []string{
		"add", "all", "alter", "analyze", "and", "any", "as", "asc", "begin", "between", "by", "cascade", "case", "check",
		"column", "commit", "conflict", "constraint", "create", "cross", "database", "declare", "default", "delete", "desc",
		"distinct", "do", "drop", "else", "end", "except", "exists", "explain", "fetch", "filter", "first", "for", "foreign",
		"from", "full", "function", "grant", "group", "having", "if", "ilike", "in", "index", "inner", "insert", "intersect",
		"into", "is", "join", "key", "language", "lateral", "left", "like", "limit", "materialized", "next", "not", "nothing",
		"offset", "on", "only", "or", "order", "outer", "over", "partition", "primary", "procedure", "recursive",
		"references", "rename", "replace", "return", "returning", "returns", "revoke", "right", "rollback", "rows", "schema",
		"select", "set", "some", "table", "temp", "temporary", "then", "to", "transaction", "trigger", "truncate", "union",
		"unique", "update", "using", "vacuum", "values", "view", "when", "where", "window", "with",
	}
	sqlTypes = []string{
		"bigint", "bigserial", "blob", "bool", "boolean", "bytea", "char", "character", "date", "decimal", "double", "float",
		"int", "integer", "interval", "json", "jsonb", "money", "numeric", "precision", "real", "serial", "smallint", "text",
		"time", "timestamp", "timestamptz", "tinyint", "uuid", "varchar",
	}
	sqlConstants = []string{"true", "false", "null"}
)

func sql(s string, i, end int, o *out) {
	for i < end && !o.full {
		b := s[i]
		start := i
		switch {
		case isSpace(b):
			i++
		case b == '-' && at(s, i+1, end) == '-':
			i = lineEnd(s, i, end)
			o.push(start, i, Comment)
		case b == '/' && at(s, i+1, end) == '*':
			i = blockCommentEnd(s, i+2, end, false)
			o.push(start, i, Comment)
		case b == '\'':
			// '' inside a string is a quote.
			i++
			for i < end {
				if s[i] == '\'' {
					if at(s, i+1, end) == '\'' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			o.push(start, min(i, end), String)
		case b == '"' || b == '`':
			i = stringEnd(s, i, end, b, true)
			o.push(start, i, Property)
		case isDigit(b):
			i = numberEnd(s, i, end)
			o.push(start, i, Number)
		case (b == '$' || b == ':' || b == '@') && isIdent(at(s, i+1, end)):
			i = identEnd(s, i+1, end, "")
			o.push(start, i, Property)
		case b == '?':
			i++
			o.push(start, i, Property)
		case isIdentStart(b):
			i = identEnd(s, i, end, "")
			w := s[start:i]
			k := i
			for k < end && (s[k] == ' ' || s[k] == '\t') {
				k++
			}
			switch {
			case isWordCI(w, sqlConstants):
				o.push(start, i, Number)
			case isWordCI(w, sqlKeywords):
				o.push(start, i, Keyword)
			case isWordCI(w, sqlTypes):
				o.push(start, i, Type)
			case at(s, k, end) == '(':
				o.push(start, i, Function)
			}
		default:
			i++
			o.push(start, i, Punctuation)
		}
	}
}
