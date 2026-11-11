package kit

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func dumpInline(spans []mdSpan) string {
	var b strings.Builder
	for _, s := range spans {
		var tags []string
		if s.style&mdBold != 0 {
			tags = append(tags, "b")
		}
		if s.style&mdItalic != 0 {
			tags = append(tags, "i")
		}
		if s.style&mdStrike != 0 {
			tags = append(tags, "s")
		}
		if s.style&mdCodeSpan != 0 {
			tags = append(tags, "code")
		}
		if s.link != "" {
			tags = append(tags, "a="+s.link)
		}
		if len(tags) == 0 {
			b.WriteString(s.text)
		} else {
			fmt.Fprintf(&b, "<%s>%s</>", strings.Join(tags, ","), s.text)
		}
	}
	return b.String()
}

func TestParseInline(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"plain text", "plain text"},
		{"a **bold** b", "a <b>bold</> b"},
		{"*it* and _it_", "<i>it</> and <i>it</>"},
		{"***both***", "<b,i>both</>"},
		{"*a **b** c*", "<i>a </><b,i>b</><i> c</>"},
		{"snake_case_name", "snake_case_name"},
		{"2 * 3 * 4", "2 * 3 * 4"},
		{"~~gone~~", "<s>gone</>"},
		{"use `a*b*c` here", "use <code>a*b*c</> here"},
		{"`` x ` y ``", "<code>x ` y</>"},
		{"unclosed `tick", "unclosed `tick"},
		{`\*not\*`, "*not*"},
		{"[docs](https://x.dev/a_(b)) end", "<a=https://x.dev/a_(b)>docs</> end"},
		{"[**bold** link](u)", "<b,a=u>bold</><a=u> link</>"},
		{"see https://go.dev/doc.", "see <a=https://go.dev/doc>https://go.dev/doc</>."},
		{"(www.example.com)", "(<a=http://www.example.com>www.example.com</>)"},
		{"<https://a.b/c>", "<a=https://a.b/c>https://a.b/c</>"},
		{"<a.b@c.de>", "<a=mailto:a.b@c.de>a.b@c.de</>"},
		{"<javascript://x@y.z%0aalert(1)>", "<javascript://x@y.z%0aalert(1)>"},
		{"![alt](img.png)", "alt"},
		{"see ![a cat](https://ex", "see a cat"},
		{"[a](b) ![c] d", "<a=b>a</> ![c] d"},
		{"**한글 굵게**도", "<b>한글 굵게</>도"},
		{"** not bold**", "** not bold**"},
	} {
		if got := dumpInline(parseInline(c.in)); got != c.want {
			t.Errorf("parseInline(%q)\n got %s\nwant %s", c.in, got, c.want)
		}
	}
}

// Delimiters that never close, as in identifiers, must not make the closer
// search start over from each of them.
func TestUnclosedEmphasisParsesInLinearTime(t *testing.T) {
	s := strings.Repeat("filter(author__name__startswith=x, _db=y) __a ", 400)
	start := time.Now()
	parseInline(s)
	if d := time.Since(start); d > time.Second {
		t.Fatalf("parsing %d bytes took %v", len(s), d)
	}
}

func TestMarkdownHTMLKeepsOnlySafeURLs(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"[a](https://x.y) [b](#top) [c](rel/p) [d](mailto:m@x.y)",
			`<p><a href="https://x.y">a</a> <a href="#top">b</a> <a href="rel/p">c</a> <a href="mailto:m@x.y">d</a></p>`},
		{"[a](javascript:alert(1)) [b](JavaScript:x) [c](vbscript:x) [d](data:text/html,x)", "<p>a b c d</p>"},
		{"![x](data:image/png;base64,AA)", `<p><img src="data:image/png;base64,AA" alt="x"></p>`},
		{"![x](javascript:x)", `<p><img alt="x"></p>`},
		{"![x](data:text/html,x)", `<p><img alt="x"></p>`},
	} {
		if got := MarkdownHTML(c.in); got != c.want {
			t.Errorf("MarkdownHTML(%q)\n got %s\nwant %s", c.in, got, c.want)
		}
	}
}

// dumpBlocks prints one block per line, nesting by indentation. Newlines
// inside text show as "/".
func dumpBlocks(bs []mdBlock, indent string) string {
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "\n", "/") + "'" }
	var b strings.Builder
	for _, x := range bs {
		switch x.kind {
		case mdPara:
			fmt.Fprintf(&b, "%sp %s;", indent, q(x.text))
		case mdHeading:
			fmt.Fprintf(&b, "%sh%d %s;", indent, x.level, q(x.text))
		case mdCode:
			fmt.Fprintf(&b, "%scode[%s] %s;", indent, x.lang, q(x.text))
		case mdRule:
			fmt.Fprintf(&b, "%shr;", indent)
		case mdQuote:
			fmt.Fprintf(&b, "%squote;%s", indent, dumpBlocks(x.kids, indent+">"))
		case mdCallout:
			fmt.Fprintf(&b, "%scallout %s;%s", indent, x.callout, dumpBlocks(x.kids, indent+">"))
		case mdList:
			fmt.Fprintf(&b, "%slist ordered=%v start=%d;", indent, x.ordered, x.start)
			for _, it := range x.items {
				mark := "-"
				if it.task {
					mark = "[ ]"
					if it.checked {
						mark = "[x]"
					}
				}
				fmt.Fprintf(&b, "%s%s;%s", indent, mark, dumpBlocks(it.blocks, indent+"."))
			}
		case mdTable:
			fmt.Fprintf(&b, "%stable %v %v %v;", indent, x.head, x.aligns, x.rows)
		case mdImage:
			fmt.Fprintf(&b, "%simg %s %s %s;", indent, q(x.text), x.src, q(x.title))
		}
	}
	return b.String()
}

func TestParseBlocks(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"heading and paragraph", "# Title #\nline one\nline two\n\nnext",
			"h1 'Title';p 'line one/line two';p 'next';"},
		{"fence keeps markup", "```go\nfunc main() {\n\n  *x* # no\n}\n```\nafter",
			"code[go] 'func main() {//  *x* # no/}';p 'after';"},
		{"unclosed fence runs to end", "text\n```\npartial",
			"p 'text';code[] 'partial';"},
		{"rule vs list", "a\n\n---\n\n- b",
			"p 'a';hr;list ordered=false start=0;-;.p 'b';"},
		{"nested list", "1. one\n   - inner\n2. two\n\n3. three",
			"list ordered=true start=1;-;.p 'one';.list ordered=false start=0;.-;..p 'inner';-;.p 'two';-;.p 'three';"},
		{"list lazy continuation", "- item\ncontinued\n\npara",
			"list ordered=false start=0;-;.p 'item/continued';p 'para';"},
		{"task list", "- [ ] todo\n- [x] done",
			"list ordered=false start=0;[ ];.p 'todo';[x];.p 'done';"},
		{"different bullet starts new list", "- a\n* b",
			"list ordered=false start=0;-;.p 'a';list ordered=false start=0;-;.p 'b';"},
		{"quote", "> quoted\nlazy\n> > deeper",
			"quote;>p 'quoted/lazy';>quote;>>p 'deeper';"},
		{"table", "| a | b |\n|:--|--:|\n| 1 | `x|y` |\n| 2 \\| 3 |\n\nend",
			"table [a b] [0 2] [[1 `x|y`] [2 | 3 ]];p 'end';"},
		{"heading interrupts paragraph", "para\n## sub",
			"p 'para';h2 'sub';"},
		{"ordered list not starting at 1 does not interrupt", "costs\n2. two",
			"p 'costs/2. two';"},
		{"image block", "![A *cat*](<cat 1.png> \"Napping\")",
			"img 'A cat' cat 1.png 'Napping';"},
		{"image splits its paragraph", "before\n![x](a.png) after\nmore",
			"p 'before';img 'x' a.png '';p 'after/more';"},
		{"image in a list item", "- ![x](a.png)",
			"list ordered=false start=0;-;.img 'x' a.png '';"},
		{"image in a link or emphasis stays in the line", "[![x](a.png)](u) *![y](b.png)*",
			"p '[![x](a.png)](u) *![y](b.png)*';"},
		{"image without a source stays in the line", "![x]()",
			"p '![x]()';"},
		{"alert", "> [!Warning]\n> Back up first.\n\n> [!NOTE] not alone",
			"callout warning;>p 'Back up first.';quote;>p '[!NOTE] not alone';"},
	} {
		if got := dumpBlocks(parseMarkdown(c.in), ""); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
	}
}

func TestTableResourceLimits(t *testing.T) {
	const columns, cells = 256, 16384
	for _, c := range []struct {
		name     string
		cols     int
		rows     int
		fallback bool
	}{
		{"column limit", columns, 1, false},
		{"too many columns", columns + 1, 1, true},
		{"cell limit", columns, cells/columns - 1, false},
		{"too many cells", columns, cells / columns, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			src := strings.Repeat("| h", c.cols) + "|\n" + strings.Repeat("| -", c.cols) + "|\n"
			src += strings.TrimSuffix(strings.Repeat("| x |\n", c.rows), "\n")
			blocks := parseMarkdown(src + "\n# after")
			if len(blocks) != 2 || blocks[1].kind != mdHeading || blocks[1].text != "after" {
				t.Fatalf("table consumed following heading: %d blocks", len(blocks))
			}
			b := blocks[0]
			prefix := src[:strings.LastIndexByte(src, '\n')+1]
			d := parseFrom(parseFrom(nil, prefix), src+"\n# after")
			if dumpBlocks(d.blocks, "") != dumpBlocks(blocks, "") || !slices.Equal(d.starts, []int{0, c.rows + 2}) {
				t.Fatal("incremental table fallback differs from a full parse")
			}
			if c.fallback {
				if b.kind != mdPara || b.text != src {
					t.Fatalf("oversized table must fall back to its complete source: kind=%d, text bytes=%d, want %d", b.kind, len(b.text), len(src))
				}
				return
			}
			if b.kind != mdTable || len(b.head) != c.cols || len(b.aligns) != c.cols || len(b.rows) != c.rows {
				t.Fatalf("table at limit changed: kind=%d, columns=%d, aligns=%d, rows=%d", b.kind, len(b.head), len(b.aligns), len(b.rows))
			}
			for _, row := range b.rows {
				if len(row) != c.cols || row[0] != "x" || strings.Join(row[1:], "") != "" {
					t.Fatalf("sparse row padding changed: %v", row)
				}
			}
		})
	}
}

func TestInlineLinkDelimiters(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"unmatched brackets", strings.Repeat("[", 4096), strings.Repeat("[", 4096)},
		{"unmatched angles", strings.Repeat("<", 4096), strings.Repeat("<", 4096)},
		{"nested label", "[a [b] c](u)", "<a=u>a [b] c</>"},
		{"escaped closer", `[a\]b](u)`, "<a=u>a]b</>"},
		{"escaped opener", `[a\[b](u)`, "<a=u>a[b</>"},
		{"failed outer label", "[[b](u)", "[<a=u>b</>"},
		{"failed destination", "[a](broken [b](u)", "[a](broken <a=u>b</>"},
		{"escaped destination paren", `[a](u\(v))`, `<a=u\(v)>a</>`},
		{"angle before URL", "<<<https://a.b>", "<<<a=https://a.b>https://a.b</>"},
		{"invalid angle then email", "<bad> <a@b.co>", "<bad> <a=mailto:a@b.co>a@b.co</>"},
		{"unbalanced URL", "https://a.b/(x)" + strings.Repeat(")", 4096), "<a=https://a.b/(x)>https://a.b/(x)</>" + strings.Repeat(")", 4096)},
		{"punctuation between parens", "https://a.b/(x)).)!", "<a=https://a.b/(x)>https://a.b/(x)</>).)!"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := dumpInline(parseInline(c.in)); got != c.want {
				t.Fatalf("parseInline(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// mdCorpus exercises how blocks end: paragraphs interrupted or continued,
// lists with blank lines, lazy continuations, tables, fences, quotes and
// images that split a paragraph.
var mdCorpus = []string{
	"# Title\npara one\nstill one\n\n- a\n- b\n\n  more b\n- c\n\npara\n#tag\n## real\n",
	"> quote\nlazy\n> > deep\n\n> [!TIP]\n> tip *text*\n\n1. one\n2. two\nlazy\n\n---\ntext",
	"intro\n| a | b |\n|---|--:|\n| 1 | 2 |\n| 3 |\n\nafter ![img](x.png) tail\nline\n",
	"```go\nfunc f() {\n\n}\n```\n\n~~~\nopen fence\n- not a list",
	"para\n- item\n  - nested\n\n    code-ish\n\n* other\n+ plus\n\n2. two\n3) three",
	"text **bold\nacross** lines\n\n[link](u) and <https://x.y> and www.z.com.\n\n- [ ] task\n- [x] done",
}

// Parsing a stream from the parse of an earlier part of it gives the same
// blocks as parsing it whole, at every length and from parses some way back.
func TestParseIncremental(t *testing.T) {
	for _, doc := range mdCorpus {
		var prev *mdDoc
		var older []*mdDoc
		for n := 0; n <= len(doc); n++ {
			if !utf8.RuneStart(doc[n%max(len(doc), 1)]) && n < len(doc) {
				continue
			}
			src := doc[:n]
			wantBlocks, wantStarts := parseBlocksAt(strings.Split(src, "\n"))
			want := dumpBlocks(wantBlocks, "")
			for _, p := range []*mdDoc{prev, pick(older, 7)} {
				if p != nil && !strings.HasPrefix(src, p.src[:completeLen(p.src)]) {
					t.Fatalf("test bug: %q does not start %q", p.src, src)
				}
				d := parseFrom(p, src)
				if got := dumpBlocks(d.blocks, ""); got != want {
					from := ""
					if p != nil {
						from = p.src
					}
					t.Fatalf("parse of %q from %q:\n got %s\nwant %s", src, from, got, want)
				}
				if !slices.Equal(d.starts, wantStarts) {
					t.Fatalf("starts of %q = %v, want %v", src, d.starts, wantStarts)
				}
			}
			prev = parseFrom(prev, src)
			older = append(older, prev)
		}
	}
}

func pick(ds []*mdDoc, back int) *mdDoc {
	if len(ds) < back {
		return nil
	}
	return ds[len(ds)-back]
}

func TestStreamView(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Hello **wor", "Hello **wor**"},
		{"a *b **c", "a *b **c***"},
		{"2 * 3", "2 * 3"},
		{"snake_case", "snake_case"},
		{"a **", "a "},
		{"x ~~gone", "x ~~gone~~"},
		{"run `code", "run `code`"},
		{"see [docs", "see [docs]()"},
		{"see [docs](https://ex", "see [docs](https://ex)"},
		{"see ![alt](https://ex", "see ![alt](https://ex"},
		{"**see ![alt](ht", "**see ![alt](ht**"},
		{"done [x]", "done [x]()"},
		{"done [x] ok", "done [x] ok"},
		{"- item **bo", "- item **bo**"},
		{"# head *it", "# head *it*"},
		{"para *a\n\nnext", "para *a\n\nnext"},
		{"old **open\n \t \nnew *tail", "old **open\n \t \nnew *tail*"},
		{"old *open\n\n", "old *open\n\n"},
		{"```go\nx := `*", "```go\nx := `*"},
		{"intro\n\n| a | b |", "intro\n\n"},
		{"intro\n\n| a | b |\n|", "intro\n\n"},
		{"intro\n\n| a | b |\n|--", "intro\n\n| a | b |\n|--"},
		{"intro\n\n| a | b |\n", "intro\n\n"},
		{"| a | b |\n|---|---|\n| 1", "| a | b |\n|---|---|\n| 1"},
	} {
		if got := streamView(parseFrom(nil, c.in)); got != c.want {
			t.Errorf("streamView(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
