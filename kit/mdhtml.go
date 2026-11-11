package kit

import (
	"html"
	"strconv"
	"strings"
)

// MarkdownHTML converts Markdown as Markdown renders it to an HTML fragment,
// for copying a message with its formatting (fairy.Window.SetClipboardRich).
func MarkdownHTML(src string) string {
	var b strings.Builder
	mdHTMLBlocks(&b, parseDoc(src).blocks)
	return b.String()
}

func mdHTMLBlocks(b *strings.Builder, bs []mdBlock) {
	for _, x := range bs {
		switch x.kind {
		case mdPara:
			b.WriteString("<p>")
			mdHTMLInline(b, x.text)
			b.WriteString("</p>")
		case mdHeading:
			tag := "h" + strconv.Itoa(x.level)
			b.WriteString("<" + tag + ">")
			mdHTMLInline(b, x.text)
			b.WriteString("</" + tag + ">")
		case mdCode:
			b.WriteString("<pre><code")
			if x.lang != "" {
				b.WriteString(` class="language-` + html.EscapeString(x.lang) + `"`)
			}
			b.WriteString(">" + html.EscapeString(x.text) + "</code></pre>")
		case mdQuote:
			b.WriteString("<blockquote>")
			mdHTMLBlocks(b, x.kids)
			b.WriteString("</blockquote>")
		case mdCallout:
			b.WriteString("<blockquote><p><strong>" + html.EscapeString(calloutTitle(x.callout)) + "</strong></p>")
			mdHTMLBlocks(b, x.kids)
			b.WriteString("</blockquote>")
		case mdList:
			tag := "ul"
			if x.ordered {
				tag = "ol"
				if x.start != 1 {
					tag += ` start="` + strconv.Itoa(x.start) + `"`
				}
			}
			b.WriteString("<" + tag + ">")
			for _, it := range x.items {
				b.WriteString("<li>")
				if it.task {
					if it.checked {
						b.WriteString("☑ ")
					} else {
						b.WriteString("☐ ")
					}
				}
				mdHTMLBlocks(b, it.blocks)
				b.WriteString("</li>")
			}
			b.WriteString("</" + tag[:2] + ">")
		case mdRule:
			b.WriteString("<hr>")
		case mdTable:
			b.WriteString("<table><thead><tr>")
			for _, c := range x.head {
				b.WriteString("<th>")
				mdHTMLInline(b, c)
				b.WriteString("</th>")
			}
			b.WriteString("</tr></thead><tbody>")
			for _, row := range x.rows {
				b.WriteString("<tr>")
				for _, c := range row {
					b.WriteString("<td>")
					mdHTMLInline(b, c)
					b.WriteString("</td>")
				}
				b.WriteString("</tr>")
			}
			b.WriteString("</tbody></table>")
		case mdImage:
			b.WriteString("<p><img ")
			if safeURL(x.src, true) {
				b.WriteString(`src="` + html.EscapeString(x.src) + `" `)
			}
			b.WriteString(`alt="` + html.EscapeString(x.text) + `"></p>`)
		}
	}
}

func mdHTMLInline(b *strings.Builder, src string) {
	for _, s := range inlineSpans(src) {
		t := strings.ReplaceAll(html.EscapeString(s.text), "\n", "<br>")
		if s.style&mdCodeSpan != 0 {
			t = "<code>" + t + "</code>"
		}
		if s.style&mdStrike != 0 {
			t = "<s>" + t + "</s>"
		}
		if s.style&mdItalic != 0 {
			t = "<em>" + t + "</em>"
		}
		if s.style&mdBold != 0 {
			t = "<strong>" + t + "</strong>"
		}
		if s.link != "" && safeURL(s.link, false) {
			t = `<a href="` + html.EscapeString(s.link) + `">` + t + "</a>"
		}
		b.WriteString(t)
	}
}

// safeURL reports whether u may be an href, or with image a src, in HTML
// pasted elsewhere: a relative or fragment URL, http, https or mailto, and
// for images data:image/. The scheme is read as browsers read it, after
// dropping surrounding controls and spaces and any tab or newline.
func safeURL(u string, image bool) bool {
	u = strings.TrimFunc(u, func(r rune) bool { return r <= ' ' })
	u = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, u)
	i := strings.IndexAny(u, ":/?#")
	if i < 0 || u[i] != ':' {
		return true
	}
	switch strings.ToLower(u[:i]) {
	case "http", "https", "mailto":
		return true
	case "data":
		return image && strings.HasPrefix(strings.ToLower(u), "data:image/")
	}
	return false
}

func calloutTitle(kind string) string {
	switch kind {
	case "tip":
		return Messages.CalloutTip
	case "important":
		return Messages.CalloutImportant
	case "warning":
		return Messages.CalloutWarning
	case "caution":
		return Messages.CalloutCaution
	}
	return Messages.CalloutNote
}
