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
