package spell

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestShift(t *testing.T) {
	old := "teh cat adn dog"
	errs := []Range{{0, 3}, {8, 11}}
	for _, tc := range []struct {
		cur  string
		want []Range
	}{
		{old, errs},
		{"teh black cat adn dog", []Range{{0, 3}, {14, 17}}},
		{"tehh cat adn dog", []Range{{9, 12}}},
		{"teh cat and dog", []Range{{0, 3}}},
		{"teh cat adn dogs", []Range{{0, 3}, {8, 11}}},
	} {
		if got := Shift(old, tc.cur, errs); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Shift(%q) = %v, want %v", tc.cur, got, tc.want)
		}
	}
}

// TestLines checks that rechecking only the changed lines gives the
// ranges a check of the whole text gives.
func TestLines(t *testing.T) {
	// The fake checker marks words holding "xx" and words that repeat the
	// word before them, across line breaks too.
	check := func(s string) []Range {
		var errs []Range
		prev := ""
		for i := 0; i < len(s); {
			if s[i] == ' ' || s[i] == '\n' {
				i++
				continue
			}
			j := i + strings.IndexAny(s[i:]+" ", " \n")
			if w := s[i:j]; strings.Contains(w, "xx") || w == prev {
				errs = append(errs, Range{i, j})
			}
			prev, i = s[i:j], j
		}
		return errs
	}
	old := "a xx b\nxx c\nd xx\nxx"
	for _, cur := range []string{
		old,
		"a xx b\nxx cxx\nd xx\nxx",
		"xx a xx b\nxx c\nd xx\nxx",
		"a xx b\nd xx\nxx",
		"a xx bxx c\nd xx\nxx",
		"a xx b\nxx c\nnew xx\nd xx\nxx",
		"a xx b\nxx c\nd xx\nxxxx",
		"",
	} {
		got := Lines(old, cur, check(old), check)
		if want := check(cur); !slices.Equal(got, want) {
			t.Errorf("Lines(%q) = %v, want %v", cur, got, want)
		}
	}
	for _, c := range [][2]string{
		{"a the\nb\nc", "a the\nthe\nc"},
		{"the\nthe x\ny", "they\nthe x\ny"},
		{"p\nq\nthe\nthe\nr", "pp\nq\nthe\nthe\nr"},
		{"p\nthe\nthe\nq", "p\nthe\nthe\nqq"},
		{"a\nb b\nc", "a\nb bb\nc"},
	} {
		old, cur := c[0], c[1]
		got := Lines(old, cur, check(old), check)
		if want := check(cur); !slices.Equal(got, want) {
			t.Errorf("Lines(%q, %q) = %v, want %v", old, cur, got, want)
		}
	}
}
