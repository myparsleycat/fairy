// Package catalog finds Lucide icons by their kebab-case names (folder-open,
// loader-circle), for icons chosen at run time such as names kept in user data.
// Importing it links every icon; refer to icons named in code through package
// lucide instead.
package catalog

import (
	"iter"
	"slices"
	"strings"
	"sync"

	"github.com/myparsleycat/fairy/svg"
)

type entry struct {
	name  string
	vec   *svg.Vector
	alias bool
}

var sorted = sync.OnceValue(entries)

// Lookup returns the icon with the given name, or nil. Former names that
// lucide-react keeps as aliases resolve to the renamed icon.
func Lookup(name string) *svg.Vector {
	es := sorted()
	i, ok := slices.BinarySearchFunc(es, name, func(e entry, name string) int { return strings.Compare(e.name, name) })
	if !ok {
		return nil
	}
	return es[i].vec
}

// Names yields the current name of every icon in sorted order, without the
// aliases Lookup also accepts.
func Names() iter.Seq[string] {
	return func(yield func(string) bool) {
		for _, e := range sorted() {
			if !e.alias && !yield(e.name) {
				return
			}
		}
	}
}
