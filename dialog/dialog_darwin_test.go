package dialog

import (
	"slices"
	"testing"

	"github.com/myparsleycat/fairy/internal/darwin"
)

func TestFilterExtensions(t *testing.T) {
	for _, tc := range []struct {
		filters []Filter
		exts    []string
		all     bool
	}{
		{nil, nil, false},
		{[]Filter{{"Images", []string{"*.png", "*.JPG", " *.jpeg "}}}, []string{"png", "jpg", "jpeg"}, false},
		{[]Filter{{"Text", []string{"*.txt"}}, {"All files", []string{"*.*"}}}, []string{"txt"}, true},
		{[]Filter{{"Any", []string{"*"}}}, nil, true},
		{[]Filter{{"Logs", []string{"log*.txt"}}}, nil, true},
		{[]Filter{{"Odd", []string{"*.t?t"}}}, nil, true},
	} {
		exts, all := filterExtensions(tc.filters)
		if !slices.Equal(exts, tc.exts) || all != tc.all {
			t.Errorf("filterExtensions(%v) = %v, %v; want %v, %v", tc.filters, exts, all, tc.exts, tc.all)
		}
	}
}

// An extension with dots, which UTType has no type for, is allowed by its
// last part; one with no type at all lifts the restriction.
func TestContentTypes(t *testing.T) {
	darwin.WithPool(func() {
		for _, tc := range []struct {
			patterns []string
			want     []string // the types' preferred extensions, or nil for no restriction
		}{
			{[]string{"*.png", "*.txt"}, []string{"png", "txt"}},
			{[]string{"*.tar.gz", "*.zip"}, []string{"gz", "zip"}},
			{[]string{"*.zip", "*.tar."}, nil},
		} {
			arr := contentTypes(FileOptions{Filters: []Filter{{"Files", tc.patterns}}}, false)
			var got []string
			if arr != 0 {
				got = []string{}
				for i := range darwin.Get[uint64](arr, "count") {
					got = append(got, darwin.GoString(darwin.Send(darwin.Send(arr, "objectAtIndex:", i), "preferredFilenameExtension")))
				}
			}
			if !slices.Equal(got, tc.want) || (got == nil) != (tc.want == nil) {
				t.Errorf("contentTypes(%v) = %v, want %v", tc.patterns, got, tc.want)
			}
		}
	})
}
