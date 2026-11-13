package spell

import (
	"context"
	"slices"
	"testing"
)

// Add is not tested: it would change the user's dictionary.
func TestSpellDarwin(t *testing.T) {
	text := "😀 This is a tset of speling."
	errs := Check(context.Background(), "en-US", text)
	var words []string
	for _, r := range errs {
		words = append(words, text[r.Start:r.End])
	}
	if !slices.Equal(words, []string{"tset", "speling"}) {
		t.Fatalf("misspelled %q, want tset and speling", words)
	}
	if s := Suggest("en-US", "speling"); !slices.Contains(s, "spelling") || len(s) > suggestions {
		t.Errorf("suggestions for speling = %q", s)
	}
	Ignore("en", "tset")
	if errs := Check(context.Background(), "en", "a tset"); len(errs) != 0 {
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
