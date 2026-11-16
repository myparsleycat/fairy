package kit

import (
	"strings"
	"testing"
)

// The search filter matches as lowering the label would, with or without
// letters outside ASCII.
func TestContainsLower(t *testing.T) {
	for _, c := range []struct {
		s, q string
	}{
		{"Hello World", "o w"}, {"Hello World", "world"}, {"Hello", "hello!"}, {"abc", "abd"},
		{"ÄPFEL Kuchen", "äpfel"}, {"Straße", "STRASSE"}, {"İstanbul", "i̇st"}, {"x", "x"}, {"", "a"},
	} {
		q := strings.ToLower(c.q)
		if got, want := containsLower(c.s, q), strings.Contains(strings.ToLower(c.s), q); got != want {
			t.Errorf("containsLower(%q, %q) = %v, want %v", c.s, q, got, want)
		}
	}
}
