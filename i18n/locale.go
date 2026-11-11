package i18n

import (
	"strings"

	"golang.org/x/text/language"
)

// Negotiate returns the first of supported that one of preferred (BCP 47
// tags, most wanted first) names by language and likely script: de-AT picks
// de, zh-TW picks zh-Hant over zh-Hans, pa-PK picks pa-Arab and pa-IN does
// not. Norwegian no, nb and nn match one another. Regions are ignored, so the
// earlier of en-US and en-GB in supported wins for any English. When nothing
// matches it returns supported[0], or "" when supported is empty.
func Negotiate(preferred, supported []string) string {
	type id struct {
		base   language.Base
		script language.Script
		ok     bool
	}
	maximize := func(tag string) id {
		t, err := language.Parse(strings.ReplaceAll(tag, "_", "-"))
		if err != nil {
			return id{}
		}
		b, _ := t.Base()
		s, _ := t.Script()
		return id{b, s, true}
	}
	targets := make([]id, len(supported))
	for i, s := range supported {
		targets[i] = maximize(s)
	}
	for _, p := range preferred {
		src := maximize(p)
		if !src.ok {
			continue
		}
		norwegian := isNorwegian(src.base)
		for i, t := range targets {
			if !t.ok {
				continue
			}
			if norwegian && isNorwegian(t.base) || !norwegian && t.base == src.base && t.script == src.script {
				return supported[i]
			}
		}
	}
	if len(supported) == 0 {
		return ""
	}
	return supported[0]
}

func isNorwegian(b language.Base) bool {
	switch b.String() {
	case "no", "nb", "nn":
		return true
	}
	return false
}
