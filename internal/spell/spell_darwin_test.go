package spell

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// runs counts the runs of TestSpellDarwin.
var runs int

// Add is not tested: it would change the user's dictionary.
func TestSpellDarwin(t *testing.T) {
	runs++
	// Ignore lasts for the process, so each run ignores a word of its own.
	ignored := "tset" + strings.Repeat("q", runs)
	text := "😀 This is a " + ignored + " of speling."
	errs := Check(context.Background(), "en-US", text)
	var words []string
	for _, r := range errs {
		words = append(words, text[r.Start:r.End])
	}
	if !slices.Equal(words, []string{ignored, "speling"}) {
		t.Fatalf("misspelled %q, want %s and speling", words, ignored)
	}
	if s := Suggest("en-US", "speling"); !slices.Contains(s, "spelling") || len(s) > suggestions {
		t.Errorf("suggestions for speling = %q", s)
	}
	Ignore("en", ignored)
	if errs := Check(context.Background(), "en", "a "+ignored); len(errs) != 0 {
		t.Errorf("ignored word still reported: %v", errs)
	}
	if errs := Check(context.Background(), "xx-YY", "qqqq zzzz"); errs != nil {
		t.Errorf("language without a dictionary reported %v", errs)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if errs := Check(ctx, "en", "tset speling"); len(errs) != 0 {
		t.Errorf("canceled check reported %v", errs)
	}
}
