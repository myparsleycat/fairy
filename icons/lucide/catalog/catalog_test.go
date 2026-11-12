package catalog

import (
	"slices"
	"testing"

	"github.com/myparsleycat/fairy/icons/lucide"
)

func TestLookupResolvesAliasesToTheRenamedIcon(t *testing.T) {
	if got := Lookup("loader-circle"); got != lucide.LoaderCircle {
		t.Errorf("Lookup(loader-circle) = %p, want lucide.LoaderCircle", got)
	}
	if got := Lookup("loader-2"); got != lucide.LoaderCircle {
		t.Errorf("Lookup(loader-2) = %p, want lucide.LoaderCircle", got)
	}
	if got := Lookup("no-such-icon"); got != nil {
		t.Errorf("Lookup(no-such-icon) = %p, want nil", got)
	}
}

func TestNamesListsCurrentNamesOnly(t *testing.T) {
	names := slices.Collect(Names())
	if !slices.Contains(names, "loader-circle") || slices.Contains(names, "loader-2") {
		t.Errorf("Names: loader-circle present = %t, loader-2 present = %t; want true, false",
			slices.Contains(names, "loader-circle"), slices.Contains(names, "loader-2"))
	}
	if !slices.IsSorted(names) {
		t.Error("Names are not sorted")
	}
}
