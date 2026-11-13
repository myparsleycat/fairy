package spell

import (
	"context"
	"strings"
	"sync"
	"unicode/utf16"

	"github.com/myparsleycat/fairy/internal/darwin"
)

const suggestions = 5

// The macOS spell checker (NSSpellChecker) serves every call from the
// goroutine that makes it; mu keeps one call at a time.
var (
	mu       sync.Mutex
	checker  darwin.ID
	docTag   int64
	dicts    map[string]darwin.ID // spell checker language per tag, 0 for none
	notFound = uint64(darwin.NSNotFound)
)

// spellChecker returns the shared spell checker. Run under mu in a pool.
func spellChecker() darwin.ID {
	if checker == 0 {
		checker = darwin.SendClass("NSSpellChecker", "sharedSpellChecker")
		docTag = darwin.Get[int64](darwin.ID(darwin.Cls("NSSpellChecker")), "uniqueSpellDocumentTag")
		dicts = map[string]darwin.ID{}
	}
	return checker
}

// language returns the spell checker's name for the dictionary of lang, a
// BCP 47 tag, or for its language without the region, or 0 when macOS has
// no dictionary for either. Run under mu in a pool.
func language(lang string) darwin.ID {
	if l, ok := dicts[lang]; ok {
		return l
	}
	c := spellChecker()
	avail := map[string]bool{}
	list := darwin.Send(c, "availableLanguages")
	for i := range darwin.Get[uint64](list, "count") {
		avail[darwin.GoString(darwin.Send(list, "objectAtIndex:", i))] = true
	}
	var l darwin.ID
	name := strings.ReplaceAll(lang, "-", "_")
	tags := []string{name}
	if i := strings.IndexByte(name, '_'); i > 0 {
		tags = append(tags, name[:i])
	}
	for _, tag := range tags {
		if avail[tag] {
			l = darwin.Send(darwin.String(tag), "retain")
			break
		}
	}
	dicts[lang] = l
	return l
}

// Check returns the misspelled ranges of text.
func Check(ctx context.Context, lang, text string) []Range {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	n := uint64(len(utf16.Encode([]rune(text))))
	var units []Range
	mu.Lock()
	defer mu.Unlock()
	darwin.WithPool(func() {
		l := language(lang)
		if l == 0 {
			return
		}
		s := darwin.String(text)
		for at := uint64(0); at < n && ctx.Err() == nil; {
			r := darwin.Get[darwin.NSRange](checker,
				"checkSpellingOfString:startingAt:language:wrap:inSpellDocumentWithTag:wordCount:",
				s, int64(at), l, false, docTag, uintptr(0))
			if r.Location == notFound || r.Length == 0 || r.Location < at {
				break
			}
			units = append(units, Range{int(r.Location), int(r.Location + r.Length)})
			at = r.Location + r.Length
		}
	})
	return utf16ToBytes(text, units)
}

// Suggest returns up to five replacements for word.
func Suggest(lang, word string) []string {
	if word == "" {
		return nil
	}
	var out []string
	mu.Lock()
	defer mu.Unlock()
	darwin.WithPool(func() {
		l := language(lang)
		if l == 0 {
			return
		}
		rng := darwin.NSRange{Length: uint64(len(utf16.Encode([]rune(word))))}
		list := darwin.Send(checker, "guessesForWordRange:inString:language:inSpellDocumentWithTag:",
			rng, darwin.String(word), l, docTag)
		for i := range min(darwin.Get[uint64](list, "count"), suggestions) {
			out = append(out, darwin.GoString(darwin.Send(list, "objectAtIndex:", i)))
		}
	})
	return out
}

// Add adds word to the user's dictionary, which macOS shares across
// languages and apps.
func Add(lang, word string) {
	mu.Lock()
	defer mu.Unlock()
	darwin.WithPool(func() {
		darwin.Send(spellChecker(), "learnWord:", darwin.String(word))
	})
}

// Ignore stops reporting word as misspelled until the program exits.
func Ignore(lang, word string) {
	mu.Lock()
	defer mu.Unlock()
	darwin.WithPool(func() {
		darwin.Send(spellChecker(), "ignoreWord:inSpellDocumentWithTag:", darwin.String(word), docTag)
	})
}
