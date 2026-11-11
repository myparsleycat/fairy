package i18n

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// finalConsonant returns the final consonant (jongseong) index of the last
// letter of word: 0 when it has none and 8 for ㄹ. A last letter that is not a
// precomposed Hangul syllable counts as having no final consonant.
func finalConsonant(word string) int {
	r, _ := utf8.DecodeLastRuneInString(word)
	if r < 0xAC00 || r > 0xD7A3 {
		return 0
	}
	return int(r-0xAC00) % 28
}

// Josa appends to word the Korean particle from pair that matches its last
// letter, such as "은/는", "이/가", "을/를", "와/과" or "으로/로". pair lists
// the form used after a final consonant first, except "와/과", which lists the
// form used after a vowel first. When the first form starts with "으로", the
// second form is also used after a final ㄹ.
func Josa(word, pair string) string {
	a, b, ok := strings.Cut(pair, "/")
	if !ok {
		return word + pair
	}
	if pair == "와/과" {
		a, b = b, a
	}
	fc := finalConsonant(word)
	if fc == 0 || fc == 8 && strings.HasPrefix(a, "으로") {
		return word + b
	}
	return word + a
}

func josaFormatter(v any, _ string, opts map[string]string) string {
	s := fmt.Sprint(v)
	if pair := opts["pair"]; pair != "" {
		return Josa(s, pair)
	}
	return s
}
