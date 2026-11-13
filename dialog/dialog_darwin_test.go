package dialog

import (
	"slices"
	"testing"
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
