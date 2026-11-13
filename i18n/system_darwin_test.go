package i18n

import (
	"strings"
	"testing"
)

func TestSystemLanguagesDarwin(t *testing.T) {
	langs := SystemLanguages()
	if len(langs) == 0 {
		t.Fatal("no preferred languages")
	}
	for _, tag := range langs {
		if tag == "" || strings.ContainsAny(tag, "_.@ ") {
			t.Errorf("tag %q is not BCP 47", tag)
		}
	}
	t.Log(langs)
}
