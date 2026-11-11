package i18n

import (
	"slices"
	"strings"
)

// posixLanguages returns the languages POSIX locale variables name, read
// through lookup: LANGUAGE's colon list, then LC_ALL, LC_MESSAGES and LANG,
// each turned from de_AT.UTF-8@euro into de-AT, without C, POSIX and repeats.
func posixLanguages(lookup func(string) (string, bool)) []string {
	get := func(name string) string {
		v, _ := lookup(name)
		return v
	}
	values := strings.Split(get("LANGUAGE"), ":")
	for _, name := range [...]string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		values = append(values, get(name))
	}
	var out []string
	for _, v := range values {
		if i := strings.IndexAny(v, ".@"); i >= 0 {
			v = v[:i]
		}
		tag := strings.ReplaceAll(v, "_", "-")
		if tag == "" || tag == "C" || tag == "POSIX" || slices.Contains(out, tag) {
			continue
		}
		out = append(out, tag)
	}
	return out
}
