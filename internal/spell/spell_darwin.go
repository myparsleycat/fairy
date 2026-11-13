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
// goroutine that makes it; mu keeps one call at a time and guards the
// state below.
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

// chunkUnits is about how many UTF-16 units Check hands the spell checker at
// once. One call scans from where it starts to the next misspelling, so
// chunks bound how long a call, and so mu, can be held.
const chunkUnits = 4096

// Check returns the misspelled ranges of text. It takes mu for one spell
// checker call at a time, so that Suggest, Add and Ignore, which the context
// menu calls on the UI thread, wait for a call, not for the whole text.
func Check(ctx context.Context, lang, text string) []Range {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	u := utf16.Encode([]rune(text))
	var units []Range
	for start := 0; start < len(u) && ctx.Err() == nil; {
		end := chunkEnd(u, start)
		ok := true
		darwin.WithPool(func() {
			mu.Lock()
			l := language(lang)
			mu.Unlock()
			if l == 0 {
				ok = false
				return
			}
			s := darwin.String(string(utf16.Decode(u[start:end])))
			n := uint64(end - start)
			for at := uint64(0); at < n && ctx.Err() == nil; {
				mu.Lock()
				r := darwin.Get[darwin.NSRange](checker,
					"checkSpellingOfString:startingAt:language:wrap:inSpellDocumentWithTag:wordCount:",
					s, int64(at), l, false, docTag, uintptr(0))
				mu.Unlock()
				if r.Location == notFound || r.Length == 0 || r.Location < at {
					break
				}
				units = append(units, Range{start + int(r.Location), start + int(r.Location+r.Length)})
				at = r.Location + r.Length
			}
		})
		if !ok {
			return nil
		}
		start = end
	}
	return utf16ToBytes(text, units)
}

// chunkEnd returns where the chunk of u that starts at start ends: after the
// first space or line break past chunkUnits, so that no word is split, or
// past twice that, before a surrogate pair rather than inside one.
func chunkEnd(u []uint16, start int) int {
	if len(u)-start <= 2*chunkUnits {
		return len(u)
	}
	for i := start + chunkUnits; i < start+2*chunkUnits; i++ {
		if c := u[i]; c == ' ' || c == '\n' || c == '\r' || c == '\t' || c == 0x3000 {
			return i + 1
		}
	}
	end := start + 2*chunkUnits
	if utf16.IsSurrogate(rune(u[end-1])) && u[end-1] < 0xDC00 {
		end--
	}
	return end
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
