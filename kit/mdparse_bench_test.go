package kit

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkMarkdownSparseTable(b *testing.B) {
	for _, n := range []int{128, 256, 512} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			src := strings.Repeat("|h", n) + "|\n" + strings.Repeat("|-", n) + "|\n" + strings.Repeat("|x|\n", n)
			b.SetBytes(int64(len(src)))
			b.ReportAllocs()
			for b.Loop() {
				parseMarkdown(src)
			}
		})
	}
}

func BenchmarkMarkdownLinkScans(b *testing.B) {
	for _, c := range []struct{ name, unit, tail string }{
		{"brackets", "[", ""},
		{"angles", "<", ""},
		{"angles with closer", "<", ">"},
		{"open destinations", "[x](", ""},
	} {
		b.Run(c.name, func(b *testing.B) {
			for _, n := range []int{4096, 8192, 16384} {
				b.Run(fmt.Sprint(n), func(b *testing.B) {
					src := strings.Repeat(c.unit, n) + c.tail
					b.SetBytes(int64(len(src)))
					b.ReportAllocs()
					for b.Loop() {
						parseInline(src)
					}
				})
			}
		})
	}
}

func BenchmarkMarkdownTrimAutolink(b *testing.B) {
	for _, n := range []int{4096, 8192, 16384} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			src := "https://a.b/(x)" + strings.Repeat(")", n)
			b.SetBytes(int64(len(src)))
			for b.Loop() {
				trimAutolink(src)
			}
		})
	}
}

func BenchmarkMarkdownStreamUpdate(b *testing.B) {
	for _, n := range []int{256, 512, 1024} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			src := strings.Repeat("paragraph\n\n", n) + "tail"
			prev := parseFrom(nil, src)
			b.ReportAllocs()
			for b.Loop() {
				parseFrom(prev, src+"x")
			}
		})
	}
}

// BenchmarkMarkdownStream streams about 30 KB in 24-byte chunks through the
// parse a streaming Markdown does on each update.
func BenchmarkMarkdownStream(b *testing.B) {
	docs := map[string]string{
		"paragraphs": strings.Repeat("Some text with *emphasis* and `code` in a paragraph.\n\n", 560),
		"list":       strings.Repeat("- an item with *emphasis* and `code` in it\n", 700),
		"table":      "| a | b | c |\n|---|---|---|\n" + strings.Repeat("| one | *two* | `three` |\n", 1200),
		"code":       "```go\n" + strings.Repeat("func f(x int) int { return x * 2 } // note\n", 700),
	}
	for name, doc := range docs {
		b.Run(name, func(b *testing.B) {
			saved := mdDocs
			b.Cleanup(func() { mdDocs = saved })
			for b.Loop() {
				mdDocs = genCache[*mdDoc]{size: 64}
				for n := 24; n < len(doc)+24; n += 24 {
					d := parseDoc(doc[:min(n, len(doc))])
					if v := streamView(d); v != d.src {
						viewDoc(d, v)
					}
				}
			}
		})
	}
}
