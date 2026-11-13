package spell

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

// A long text is checked in chunks that keep every misspelling, including
// those next to a chunk's end.
func TestSpellLongText(t *testing.T) {
	text := strings.Repeat("😀 This is a test of speling. ", 1000) + strings.Repeat("x", 3*chunkUnits) + " speling"
	errs := Check(context.Background(), "en-US", text)
	n := 0
	for _, r := range errs {
		if text[r.Start:r.End] == "speling" {
			n++
		}
	}
	if n != 1001 {
		t.Errorf("found %d of 1001 misspellings in %d ranges", n, len(errs))
	}
}

// The right-click menu must not wait for a background check of a long text
// to finish.
func TestSpellSuggestDuringCheck(t *testing.T) {
	text := strings.Repeat("This is a tset of speling in a long documnet. ", 4000)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		Check(ctx, "en-US", text)
	}()
	defer func() {
		cancel()
		<-done
	}()
	time.Sleep(50 * time.Millisecond)
	select {
	case <-done:
		t.Skip("the check finished before Suggest ran")
	default:
	}
	start := time.Now()
	s := Suggest("en-US", "speling")
	took := time.Since(start)
	if !slices.Contains(s, "spelling") {
		t.Errorf("suggestions for speling = %q", s)
	}
	if took > 300*time.Millisecond {
		t.Errorf("Suggest waited %v for the running check", took)
	}
}

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
