//go:build windows || darwin

package text

import (
	"slices"
	"testing"
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
