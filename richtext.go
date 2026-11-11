package fairy

import "strings"

// Span is a run of text with its own style inside RichText. Zero fields
// inherit the element's text style.
type Span struct {
	Text       string
	Weight     int
	Italic     bool
	Size       float32
	Font       string
	Color      Color
	Background Color // drawn behind the span's text, as for inline code
	Underline  bool
	Strike     bool
	// OnClick makes the span a link: clicking its text runs it.
	OnClick func()
	// URL is the link's address, written when the span is copied as HTML.
	URL string
}

// RichText is a paragraph of differently styled spans. It wraps like Text.
func RichText(spans ...Span) *El {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	e := Text(b.String())
	e.spans = spans
	for _, s := range spans {
		if s.OnClick != nil {
			e.links = true
		}
	}
	return e
}
