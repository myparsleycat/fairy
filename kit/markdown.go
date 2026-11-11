package kit

import (
	"fmt"
	"hash/fnv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
)

// MarkdownOptions configures Markdown.
type MarkdownOptions struct {
	// OnLink runs when a link is clicked. Links are plain text without it.
	OnLink func(url string)
	// OnCopy adds a copy button to code blocks; it receives the code, for
	// example to pass to Window.SetClipboardText.
	OnCopy func(code string)
	// Highlighter colors code blocks by their fence language. Nil uses the
	// built-in scanners.
	Highlighter *Highlighter
	// Images returns the picture for an image's source as written
	// (![alt](src)), for example from fairy.Images, or nil while it loads or
	// when there is none. Until then, and without Images, an image shows a
	// broken-image icon and its alt text.
	Images func(src string) *fairy.Image
	// ImageFrame wraps each image that shows its picture, for example to
	// open it larger on a click.
	ImageFrame func(el *fairy.El, src string) *fairy.El
	// Selectable lets the user select the text by dragging and copy it with
	// Ctrl+C (fairy.El.Selectable). A selection stays within one Markdown.
	Selectable bool
	// Streaming marks text that is still arriving. Emphasis, code and links
	// open at its end draw as already closed, markers that may still open or
	// close one are left out, and trailing "|" lines are left out until they
	// form a table. Turn it off once the text is complete.
	Streaming bool
	// Typography applies only to this Markdown. Nil retains the kit defaults.
	Typography *MarkdownTypography
	// Find highlights each case-insensitive occurrence of Find in the text,
	// and FindCurrent (counted from 0 in document order, as MarkdownMatches
	// counts) more strongly. FindKey, when set, is given as Key to the
	// element holding the current match, for Window.ScrollIntoView.
	Find        string
	FindCurrent int
	FindKey     string
}

// MarkdownTypography overrides positive metrics; zero values retain the defaults.
// BlockMargin collapses neighboring block margins and trims the outer margins.
type MarkdownTypography struct {
	FontSize, LineHeight         float32
	Headings                     [6]MarkdownHeading
	BlockMargin                  float32
	ListIndent, ListBlockMargin  float32
	QuoteBorder, QuotePadding    float32
	InlineCodeScale              float32
	InlineCodeBackground         fairy.Color
	CodeFontSize, CodeLineHeight float32
	CodePadding, CodeRadius      float32
	CodeBackground               fairy.Color
	// PlainCode omits syntax coloring and the language/copy toolbar.
	PlainCode bool
	WrapCode  bool
	// TableCellPadding is horizontal then vertical; RulesOnlyTable removes
	// the outer frame, vertical rules, header fill and minimum column width.
	TableCellPadding [2]float32
	RulesOnlyTable   bool
	ImageRadius      float32
}

// MarkdownHeading overrides one level when FontSize is positive.
type MarkdownHeading struct {
	FontSize, LineHeight    float32
	MarginTop, MarginBottom float32
	Muted                   bool
}

// Markdown renders CommonMark with GFM tables, task lists, strikethrough,
// autolinks and alerts ("> [!NOTE]"). A newline inside a paragraph is kept as
// a line break, as chat messages expect. Code blocks are highlighted by their
// fence language, and an image that is not inside a link or emphasis becomes
// a block of its own, drawn at its natural size up to the text width. Raw
// HTML is shown as text.
// Tables exceeding 256 columns or 16,384 cells including the header are
// rendered as paragraphs, preserving their source without cell expansion.
//
// Parses are cached, and a source that extends an earlier one reuses stable
// blocks and reparses its tail. Each update still does O(N) source scanning
// and block copying for N source bytes, and rebuilds the rendered elements;
// its cost is not limited to the newly arrived bytes.
func Markdown(src string, opts MarkdownOptions) *fairy.El {
	theme := current
	if p := opts.Typography; p != nil && p.FontSize > 0 {
		theme.FontSize = p.FontSize
	}
	r := mdRenderer{opts: opts, t: &theme}
	d := parseDoc(src)
	if opts.Streaming {
		if v := streamView(d); v != d.src {
			d = parseDocCached(v, false)
		}
	}
	el := r.blocks(d.blocks, 0).LineHeight(r.t.FontSize * 1.6)
	if p := opts.Typography; p != nil {
		el.FontSize(theme.FontSize)
		if p.LineHeight > 0 {
			el.LineHeight(p.LineHeight)
		}
	}
	if opts.Selectable {
		el.Selectable()
	}
	return el
}

type mdRenderer struct {
	opts  MarkdownOptions
	t     *Theme
	match int // matches of opts.Find drawn so far
}

func (r *mdRenderer) blocks(bs []mdBlock, depth int) *fairy.El {
	if p := r.opts.Typography; p != nil && p.BlockMargin > 0 {
		return r.spacedBlocks(bs, depth, false)
	}
	col := fairy.Col().Gap(10).MinW(0)
	for i, b := range bs {
		e := r.block(b, depth)
		if b.kind == mdHeading && i > 0 {
			e.MarginEdges(6, 0, 0, 0)
		}
		col.Child(e)
	}
	return col
}

func (r *mdRenderer) spacedBlocks(bs []mdBlock, depth int, inList bool) *fairy.El {
	p := r.opts.Typography
	col := fairy.Col().MinW(0)
	var previousBottom float32
	for i, b := range bs {
		top, bottom := p.BlockMargin, p.BlockMargin
		if inList && p.ListBlockMargin > 0 && (b.kind == mdPara || b.kind == mdList) {
			top, bottom = p.ListBlockMargin, p.ListBlockMargin
		}
		if b.kind == mdHeading && p.Headings[b.level-1].FontSize > 0 {
			h := p.Headings[b.level-1]
			top, bottom = h.MarginTop, h.MarginBottom
		}
		if b.kind == mdRule {
			top, bottom = p.BlockMargin*2, p.BlockMargin*2
		}
		if i == 0 && depth == 0 {
			top = 0
		}
		lastBottom := float32(0)
		if i == len(bs)-1 && depth > 0 {
			lastBottom = bottom
		}
		col.Child(r.block(b, depth).MarginEdges(max(previousBottom, top), 0, lastBottom, 0))
		previousBottom = bottom
	}
	return col
}

func (r *mdRenderer) inline(src string, size float32) *fairy.El {
	spans := inlineSpans(src)
	out := make([]fairy.Span, 0, len(spans))
	for _, s := range spans {
		g := fairy.Span{Text: s.text, Size: size}
		if s.style&mdBold != 0 {
			g.Weight = 600
		}
		g.Italic = s.style&mdItalic != 0
		g.Strike = s.style&mdStrike != 0
		if s.style&mdCodeSpan != 0 {
			g.Font = r.t.MonoFont
			g.Background = r.t.CodeBackground
			if size == 0 {
				size = r.t.FontSize
			}
			g.Size = size * 0.9
			if p := r.opts.Typography; p != nil {
				if p.InlineCodeScale > 0 {
					g.Size = size * p.InlineCodeScale
				}
				if p.InlineCodeBackground.A > 0 {
					g.Background = p.InlineCodeBackground
				}
			}
		}
		if s.link != "" {
			g.Color = r.t.Primary
			g.Underline = true
			g.URL = s.link
			if f := r.opts.OnLink; f != nil {
				url := s.link
				g.OnClick = func() { f(url) }
			}
		}
		out = append(out, g)
	}
	out, _, current := r.mark(out)
	e := fairy.RichText(out...).MinW(0)
	if current && r.opts.FindKey != "" {
		e.Key(r.opts.FindKey)
	}
	return e
}

// mark splits spans at the matches of opts.Find and gives the matches a
// background. found reports any match, current the current one.
func (r *mdRenderer) mark(spans []fairy.Span) (out []fairy.Span, found, current bool) {
	if r.opts.Find == "" {
		return spans, false, false
	}
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	ms := findAllFold(b.String(), r.opts.Find)
	if len(ms) == 0 {
		return spans, false, false
	}
	bg := make([]fairy.Color, len(ms))
	for i := range ms {
		bg[i] = alpha(r.t.Warning, 0.3)
		if r.match == r.opts.FindCurrent {
			bg[i] = alpha(r.t.Warning, 0.7)
			current = true
		}
		r.match++
	}
	off, m := 0, 0
	for _, s := range spans {
		end := off + len(s.Text)
		at := off
		for at < end {
			for m < len(ms) && ms[m][1] <= at {
				m++
			}
			piece := s
			switch {
			case m < len(ms) && ms[m][0] <= at:
				stop := min(ms[m][1], end)
				piece.Text = s.Text[at-off : stop-off]
				piece.Background = bg[m]
				at = stop
			default:
				stop := end
				if m < len(ms) {
					stop = min(ms[m][0], end)
				}
				piece.Text = s.Text[at-off : stop-off]
				at = stop
			}
			out = append(out, piece)
		}
		off = end
	}
	return out, true, current
}

// findAllFold returns the byte ranges of the case-insensitive, non-overlapping
// occurrences of q in s.
func findAllFold(s, q string) [][2]int {
	if q == "" {
		return nil
	}
	var out [][2]int
	for i := 0; i+len(q) <= len(s); {
		if strings.EqualFold(s[i:i+len(q)], q) {
			out = append(out, [2]int{i, i + len(q)})
			i += len(q)
			continue
		}
		_, n := utf8.DecodeRuneInString(s[i:])
		i += n
	}
	return out
}

// MarkdownMatches returns how many case-insensitive occurrences of q the text
// of Markdown(src, ...) shows, for stepping MarkdownOptions.FindCurrent.
func MarkdownMatches(src, q string) int {
	if q == "" {
		return 0
	}
	n := 0
	var walk func(bs []mdBlock)
	inline := func(s string) {
		var b strings.Builder
		for _, sp := range inlineSpans(s) {
			b.WriteString(sp.text)
		}
		n += len(findAllFold(b.String(), q))
	}
	walk = func(bs []mdBlock) {
		for _, b := range bs {
			switch b.kind {
			case mdHeading, mdPara:
				inline(b.text)
			case mdCode:
				n += len(findAllFold(b.text, q))
			case mdQuote, mdCallout:
				walk(b.kids)
			case mdList:
				for _, it := range b.items {
					walk(it.blocks)
				}
			case mdTable:
				for _, c := range b.head {
					inline(c)
				}
				for _, row := range b.rows {
					for _, c := range row {
						inline(c)
					}
				}
			}
		}
	}
	walk(parseDoc(src).blocks)
	return n
}

func (r *mdRenderer) block(b mdBlock, depth int) *fairy.El {
	t := r.t
	switch b.kind {
	case mdHeading:
		if p := r.opts.Typography; p != nil && p.Headings[b.level-1].FontSize > 0 {
			h := p.Headings[b.level-1]
			el := r.inline(b.text, h.FontSize).SemiBold().Heading(b.level)
			if h.LineHeight > 0 {
				el.LineHeight(h.LineHeight)
			}
			if h.Muted {
				el.Color(t.MutedForeground)
			}
			return el
		}
		scale := [...]float32{1.6, 1.35, 1.15, 1, 1, 1}[b.level-1]
		return r.inline(b.text, t.FontSize*scale).SemiBold().LineHeight(t.FontSize * scale * 1.35).Heading(b.level)
	case mdCode:
		return r.code(b)
	case mdQuote:
		if p := r.opts.Typography; p != nil && p.QuoteBorder > 0 {
			padding := float32(12)
			if p.QuotePadding > 0 {
				padding = p.QuotePadding
			}
			return r.blocks(b.kids, depth).PadEdges(0, 0, 0, padding).
				BorderEdges(0, 0, 0, p.QuoteBorder, t.Border).Color(t.MutedForeground)
		}
		return r.blocks(b.kids, depth).PadEdges(2, 0, 2, 12).BorderEdges(0, 0, 0, 3, t.Input).Color(t.MutedForeground)
	case mdCallout:
		return r.callout(b, depth)
	case mdList:
		return r.list(b, depth)
	case mdRule:
		return Separator().MarginEdges(4, 0, 4, 0)
	case mdTable:
		return r.table(b)
	case mdImage:
		return r.image(b)
	}
	return r.inline(b.text, 0)
}

// callout draws a GitHub alert: a quote with a colored bar, an icon and a
// title naming its kind.
func (r *mdRenderer) callout(b mdBlock, depth int) *fairy.El {
	t := r.t
	c, icon, title := t.Primary, lucide.Info, Messages.CalloutNote
	switch b.callout {
	case "note":
		c = fairy.Hex(0x0969da)
		if t.Dark {
			c = fairy.Hex(0x4493f8)
		}
	case "tip":
		c, icon, title = t.Success, lucide.CircleCheck, Messages.CalloutTip
	case "important":
		c, icon, title = fairy.Hex(0x8250df), lucide.MessageSquareWarning, Messages.CalloutImportant
		if t.Dark {
			c = fairy.Hex(0xab7df8)
		}
	case "warning":
		c, icon, title = t.Warning, lucide.TriangleAlert, Messages.CalloutWarning
	case "caution":
		c, icon, title = t.Destructive, lucide.OctagonX, Messages.CalloutCaution
	}
	head := fairy.Row(fairy.Icon(icon).Size(16, 16).Color(c).Role(fairy.RoleHidden), fairy.Text(title).SemiBold().Color(c)).
		Gap(8).Align(fairy.Center)
	return fairy.Col(head, r.blocks(b.kids, depth)).Gap(6).PadEdges(2, 0, 2, 12).BorderEdges(0, 0, 0, 3, c).
		Role(fairy.RoleGroup).Label(title)
}

type copyState struct{ at time.Time }

func (r *mdRenderer) code(b mdBlock) *fairy.El {
	t := r.t
	spans := []fairy.Span{{Text: b.text}}
	hs := r.opts.Highlighter.Highlight(b.lang, b.text)
	p := r.opts.Typography
	plain := p != nil && p.PlainCode
	if plain {
		hs = nil
	}
	if len(hs) > 0 {
		spans = t.Syntax.Spans(b.text, hs)
	}
	spans, found, current := r.mark(spans)
	text := fairy.Text(b.text)
	if len(hs) > 0 || found {
		text = fairy.RichText(spans...)
	}
	text.Font(t.MonoFont).FontSize(t.FontSize - 1).LineHeight(t.FontSize * 1.45)
	body := fairy.Box(text).PadXY(14, 12)
	box := fairy.Col().Radius(t.Radius+1).Fill(t.CodeBackground).Border(1, t.Border).MinW(0)
	if p != nil {
		if p.CodeFontSize > 0 {
			text.FontSize(p.CodeFontSize)
		}
		if p.CodeLineHeight > 0 {
			text.LineHeight(p.CodeLineHeight)
		}
		if p.CodePadding > 0 {
			body.Pad(p.CodePadding)
		}
		if p.CodeRadius > 0 {
			box.Radius(p.CodeRadius)
		}
		if p.CodeBackground.A > 0 {
			box.Fill(p.CodeBackground)
		}
	}
	if p != nil && p.WrapCode {
		text.MinW(0).WFull()
		body.MinW(0).WFull()
		box.WFull()
	} else {
		text.NoWrap()
		body.ScrollX()
	}
	if !plain && (b.lang != "" || r.opts.OnCopy != nil) {
		head := fairy.Row(fairy.Text(b.lang).FontSize(t.SmallSize).Color(t.MutedForeground).Grow(1)).
			Align(fairy.Center).H(30).PadEdges(0, 4, 0, 14).BorderEdges(0, 0, 1, 0, t.Border)
		if f := r.opts.OnCopy; f != nil {
			h := fnv.New64a()
			h.Write([]byte(b.text))
			st := fairy.Local[copyState](fmt.Sprint("kit.md.copy:", h.Sum64()))
			label, icon := Messages.Copy, lucide.Copy
			if time.Since(st.at) < 1500*time.Millisecond {
				label, icon = Messages.Copied, lucide.Check
			}
			code := b.text
			head.Child(Button(label, func() {
				f(code)
				st.at = time.Now()
				time.AfterFunc(1600*time.Millisecond, func() { fairy.Go(func() {}) })
			}, Ghost, Small, Icon(icon)).H(24).FontSize(t.SmallSize))
		}
		box.Child(head)
	}
	if current && r.opts.FindKey != "" {
		box.Key(r.opts.FindKey)
	}
	return box.Child(body)
}

var bullets = [...]string{"•", "◦", "▪"}

func (r *mdRenderer) list(b mdBlock, depth int) *fairy.El {
	t := r.t
	col := fairy.Col().Gap(4).MinW(0).Role(fairy.RoleList)
	for i, it := range b.items {
		var mark *fairy.El
		switch {
		case it.task:
			mark = fairy.Box().Size(14, 14).Radius(3).Center().MarginEdges(4, 4, 0, 0).Role(fairy.RoleCheckBox).Checked(it.checked)
			if it.checked {
				mark.Fill(t.Primary).Child(fairy.Icon(lucide.Check).Size(10, 10).Color(t.PrimaryForeground))
			} else {
				mark.Border(1, t.Input)
			}
		case b.ordered:
			mark = fairy.Text(fmt.Sprintf("%d.", b.start+i)).MinW(20).Color(t.MutedForeground).Role(fairy.RoleHidden)
		default:
			mark = fairy.Text(bullets[depth%len(bullets)]).W(14).TextAlign(fairy.TextCenter).Color(t.MutedForeground).Role(fairy.RoleHidden)
		}
		var body *fairy.El
		if p := r.opts.Typography; p != nil && p.ListIndent > 0 {
			if !b.ordered && !it.task {
				mark = fairy.Text(bullets[0]).TextAlign(fairy.TextCenter).Color(t.MutedForeground).Role(fairy.RoleHidden)
			}
			mark.W(max(p.ListIndent-6, 0)).MinW(0).Shrink(0)
			if p.BlockMargin > 0 {
				if len(it.blocks) == 1 && it.blocks[0].kind == mdPara {
					body = fairy.Col(r.block(it.blocks[0], depth+1)).Grow(1).MinW(0)
				} else {
					body = r.spacedBlocks(it.blocks, depth+1, true).Grow(1).MinW(0)
				}
			}
		}
		if body == nil {
			body = r.blocks(it.blocks, depth+1).Gap(6).Grow(1)
		}
		col.Child(fairy.Row(mark, body).Gap(6).Align(fairy.Start).Role(fairy.RoleListItem))
	}
	if p := r.opts.Typography; p != nil && p.ListIndent > 0 {
		return col
	}
	return col.PadEdges(0, 0, 0, 4)
}

func (r *mdRenderer) table(b mdBlock) *fairy.El {
	t := r.t
	p := r.opts.Typography
	flat := p != nil && p.RulesOnlyTable
	row := func(cells []string, head bool) *fairy.El {
		e := fairy.Row().Align(fairy.Stretch)
		for i, c := range cells {
			cell := fairy.Row(r.inline(c, 0)).PadXY(10, 6).Grow(1).Basis(0).MinW(80)
			if p != nil && p.TableCellPadding[0] > 0 && p.TableCellPadding[1] > 0 {
				cell.PadXY(p.TableCellPadding[0], p.TableCellPadding[1])
			}
			if flat {
				cell.MinW(0)
			}
			if i < len(b.aligns) {
				alignCell(cell, b.aligns[i])
			}
			if i > 0 && !flat {
				cell.BorderEdges(0, 0, 0, 1, t.Border)
			}
			if head {
				cell.SemiBold()
			}
			if flat {
				border := t.Border
				if head {
					border = t.MutedForeground
				}
				cell.BorderEdges(0, 0, 1, 0, border)
			}
			e.Child(cell)
		}
		return e
	}
	header := row(b.head, true)
	if !flat {
		header.Fill(t.Muted)
	}
	tbl := fairy.Col(header).MinW(0)
	for _, cells := range b.rows {
		e := row(cells, false)
		if !flat {
			e.BorderEdges(1, 0, 0, 0, t.Border)
		}
		tbl.Child(e)
	}
	if flat {
		return tbl.WFull()
	}
	return fairy.Box(tbl.Radius(t.Radius).Border(1, t.Border).Clip()).ScrollX()
}

func (r *mdRenderer) image(b mdBlock) *fairy.El {
	var img *fairy.Image
	if f := r.opts.Images; f != nil {
		img = f(b.src)
	}
	w := 0
	if img != nil {
		w, _ = img.Size()
	}
	if w == 0 {
		return r.brokenImage(b.text)
	}
	e := fairy.ImageEl(img).MaxW(float32(w)).Radius(4).Role(fairy.RoleImage).MarginEdges(6, 0, 6, 0)
	if p := r.opts.Typography; p != nil && p.ImageRadius > 0 {
		e.Radius(p.ImageRadius)
	}
	if b.text != "" {
		e.Label(b.text)
	}
	if b.title != "" {
		e.Tooltip(b.title)
	}
	if f := r.opts.ImageFrame; f != nil {
		e = f(e, b.src)
	}
	return e
}

// brokenImage stands in for an image without its picture: the broken-image
// glyph and the alt text, one line tall, or nothing without alt text, as a
// browser leaves it out.
func (r *mdRenderer) brokenImage(alt string) *fairy.El {
	if alt == "" {
		return fairy.Box().H(0)
	}
	glyph := fairy.Icon(lucide.ImageOff).Size(16, 16).Color(current.MutedForeground).Shrink(0).Role(fairy.RoleHidden)
	return fairy.Row(glyph, fairy.Text(alt).MinW(0).Shrink(1)).Gap(4).Align(fairy.Center).Role(fairy.RoleImage).Label(alt)
}
