package kit

import (
	"strings"
	"testing"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/kit/syntax"
)

func TestHighlighterCachesPerKeyLanguageAndCode(t *testing.T) {
	clear(hlCache)
	hlCacheSize = 0
	runs := 0
	make := func(key string) *Highlighter {
		return &Highlighter{Key: key, Func: func(lang, code string) []Highlight {
			runs++
			return []Highlight{{Start: 0, End: min(3, len(code)), Kind: syntax.Comment}}
		}}
	}
	for range 3 {
		// A fresh highlighter each time, as a view rebuilt every frame makes.
		make("app").Highlight("x", "abcdef")
	}
	if runs != 1 {
		t.Fatalf("runs = %d, want 1", runs)
	}
	make("app").Highlight("", "abcdef")
	make("app").Highlight("x", "abcdeg")
	make("dark").Highlight("x", "abcdef")
	if runs != 4 {
		t.Fatalf("runs = %d, want 4", runs)
	}
	var builtin *Highlighter
	if got := builtin.Highlight("x", "abcdef"); got != nil {
		t.Errorf("built-in colors an unknown language: %v", got)
	}
	if got := builtin.Highlight("go", "func f() {}"); len(got) == 0 || got[0].Kind != syntax.Keyword {
		t.Errorf("built-in Go: %v", got)
	}
}

func TestAppHighlightsAreTidied(t *testing.T) {
	h := &Highlighter{Key: "tidy", Func: func(lang, code string) []Highlight {
		k := func(a, b int, kind syntax.Kind) Highlight { return Highlight{Start: a, End: b, Kind: kind} }
		return []Highlight{k(6, 9, syntax.Number), k(0, 3, syntax.Keyword), k(2, 5, syntax.String), k(4, 4, syntax.Type),
			k(9, 40, syntax.Comment), k(11, 12, syntax.Comment), k(13, 15, syntax.Type), k(-1, 1, syntax.Type)}
	}}
	// "é" is bytes 10..12, so 11..12 starts inside it.
	var got [][2]int
	for _, x := range h.Highlight("", "let x = 1 é ok") {
		got = append(got, [2]int{x.Start, x.End})
	}
	if want := [][2]int{{0, 3}, {6, 9}, {13, 15}}; !equalRanges(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func equalRanges(a, b [][2]int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSyntaxSpansColorOnly(t *testing.T) {
	th := Dark()
	white := fairy.Hex(0xffffff)
	code := "let x = 1"
	spans := th.Syntax.Spans(code, []Highlight{{Start: 0, End: 3, Kind: syntax.Keyword}, {Start: 8, End: 9, Color: white}})
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
		if s.Weight != 0 || s.Italic || s.Size != 0 || s.Font != "" || s.Background.A != 0 || s.Underline || s.Strike {
			t.Errorf("span %q changes more than color", s.Text)
		}
	}
	if b.String() != code {
		t.Errorf("spans spell %q", b.String())
	}
	if spans[0].Color != th.Syntax[syntax.Keyword] || spans[1].Color.A != 0 || spans[2].Color != white {
		t.Errorf("colors: %+v", spans)
	}
}

func TestSyntaxColorsAreDistinctInBothThemes(t *testing.T) {
	for _, th := range []Theme{Light(), Dark()} {
		for i := range th.Syntax {
			for j := range i {
				if th.Syntax[i] == th.Syntax[j] {
					t.Errorf("dark %v: kinds %d and %d share %v", th.Dark, i, j, th.Syntax[i])
				}
			}
		}
	}
}
