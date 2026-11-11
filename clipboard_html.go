package fairy

import (
	"html"
	"strings"
)

// spansHTML writes text[a:b] of a paragraph made of spans (or of plain text
// when spans is nil) as HTML.
func spansHTML(sb *strings.Builder, text string, spans []Span, a, b int) {
	if spans == nil {
		sb.WriteString(htmlText(text[a:b]))
		return
	}
	off := 0
	for _, s := range spans {
		end := off + len(s.Text)
		lo, hi := max(a, off), min(b, end)
		if lo < hi {
			t := htmlText(s.Text[lo-off : hi-off])
			code := s.Font != "" && s.Background.isSet()
			if code {
				t = "<code>" + t + "</code>"
			}
			if s.Strike {
				t = "<s>" + t + "</s>"
			}
			if s.Italic {
				t = "<i>" + t + "</i>"
			}
			if s.Weight >= 600 {
				t = "<b>" + t + "</b>"
			}
			if s.URL != "" {
				t = `<a href="` + html.EscapeString(s.URL) + `">` + t + "</a>"
			}
			sb.WriteString(t)
		}
		off = end
	}
}

func htmlText(s string) string {
	return strings.ReplaceAll(html.EscapeString(s), "\n", "<br>")
}
