//go:build windows

package text

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestClusters(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		want       []int
	}{
		{"ZWJ family", "a👨‍👩‍👧‍👦b", []int{0, 1, 26, 27}},
		{"flags", "🇰🇷🇯🇵", []int{0, 8, 16}},
		{"combining accent", "éx", []int{0, 3, 4}},
		{"skin tone", "👋🏽!", []int{0, 8, 9}},
		{"CRLF", "\r\nA", []int{0, 2, 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Clusters(tc.text)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("Clusters = %v, want %v", got, tc.want)
			}
			for i := len(tc.want) - 1; i > 0; i-- {
				if p := PrevBoundary(tc.text, tc.want[i]); p != tc.want[i-1] {
					t.Errorf("PrevBoundary(%d) = %d, want %d", tc.want[i], p, tc.want[i-1])
				}
			}
		})
	}
}

// TestClustersMatchesNextBoundary checks the single pass against stepping
// with NextBoundary, around the ends of ICU windows.
func TestClustersMatchesNextBoundary(t *testing.T) {
	long := strings.Repeat("ab e\u0301 \U0001F468\u200D\U0001F469\u200D\U0001F467 ", 400)
	for _, tc := range []struct{ name, text string }{
		{"empty", ""},
		{"combining marks", "e\u0301\u0302x\u0308\u0301 a\u0300"},
		{"emoji ZWJ", "\U0001F468\u200D\U0001F469\u200D\U0001F467\u200D\U0001F466\U0001F3F3\uFE0F\u200D\U0001F308\U0001F44B\U0001F3FD\U0001F1F0\U0001F1F7"},
		{"newlines", "a\nb\r\n\n\ne\u0301\n\U0001F468\u200D\U0001F469\n"},
		{"long line", long},
		{"long lines", long + "\n" + long + "\r\n" + "x" + long},
		{"long ascii", strings.Repeat("x", 5000)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := []int{0}
			for b := 0; b < len(tc.text); {
				b = NextBoundary(tc.text, b)
				want = append(want, b)
			}
			if got := Clusters(tc.text); !slices.Equal(got, want) {
				t.Fatalf("Clusters differs from NextBoundary: got %d bounds, want %d", len(got), len(want))
			}
		})
	}
}

func TestPrevDeleteBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		want       int
	}{
		{"accent alone", "é", 1},
		{"skin tone sequence", "👋🏽", 0},
		{"emoji presentation", "❤️", 0},
		{"flag", "🇰🇷", 0},
	} {
		if got := PrevDeleteBoundary(tc.text, len(tc.text)); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestWordBoundaries(t *testing.T) {
	code := "foo.bar(baz)"
	if got := NextWord(code, 3); got != 7 {
		t.Errorf("NextWord(code, 3) = %d, want 7", got)
	}
	if got := PrevWord(code, 12); got != 8 {
		t.Errorf("PrevWord(code, 12) = %d, want 8", got)
	}
	if got := PrevWord("hello world", 11); got != 6 {
		t.Errorf("PrevWord(hello world) = %d, want 6", got)
	}
	// Without spaces, Chinese words come from ICU's dictionary.
	zh := "你好世界。"
	if got := NextWord(zh, 0); got != 6 {
		t.Errorf("NextWord(zh, 0) = %d, want 6", got)
	}
	if got := PrevWord(zh, len(zh)); got != 6 {
		t.Errorf("PrevWord(zh, end) = %d, want 6", got)
	}
}

func BenchmarkClusters(b *testing.B) {
	s := strings.Repeat("text 한글 😀 e\u0301 👨‍👩‍👧 ", 2048/len("text 한글 😀 e\u0301 👨‍👩‍👧 ")+1)
	for len(s) > 2048 {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	b.ReportAllocs()
	for b.Loop() {
		Clusters(s)
	}
}
