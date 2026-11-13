package i18n

import (
	"slices"

	"github.com/myparsleycat/fairy/internal/darwin"
)

// SystemLanguages returns the user's preferred languages as BCP 47 tags, most
// wanted first, from the Language & Region settings
// ([NSLocale preferredLanguages]).
func SystemLanguages() []string {
	var out []string
	darwin.WithPool(func() {
		list := darwin.SendClass("NSLocale", "preferredLanguages")
		for i := range darwin.Get[uint64](list, "count") {
			tag := darwin.GoString(darwin.Send(list, "objectAtIndex:", i))
			if tag != "" && !slices.Contains(out, tag) {
				out = append(out, tag)
			}
		}
	})
	return out
}
