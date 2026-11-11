package fairy

import (
	"context"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/myparsleycat/fairy/internal/edit"
	"github.com/myparsleycat/fairy/internal/spell"
	"github.com/myparsleycat/fairy/internal/win"
)

const spellDebounce = 150 * time.Millisecond

var spellColor = Hex(0xdc2626)

// spellErr is a misspelled range of an Input's value, in bytes.
type spellErr struct{ start, end int }

// spellResult is the misspellings found in text.
type spellResult struct {
	text string
	errs []spellErr
}

type spellKey struct{ lang, text string }

var userLocale = sync.OnceValue(win.UserLocale)

func (w *Window) spellLanguage() string {
	if w.opts.SpellLanguage != "" {
		return w.opts.SpellLanguage
	}
	return userLocale()
}

// spellEnabled reports whether e's text is spell checked.
func spellEnabled(e *El) bool {
	p := e.input
	if p.password || p.onChange == nil {
		return false
	}
	if p.spell != nil {
		return *p.spell
	}
	return p.multiline
}

// spellRequest starts checking st's value if it changed, and returns the
// misspellings known for it.
func (w *Window) spellRequest(e *El, st *editState) []spellErr {
	if !spellEnabled(e) {
		if st.spell != nil {
			st.spell.Cancel()
			st.spell = nil
		}
		return nil
	}
	if st.spell == nil {
		st.spell = NewLatest[spellResult](spellDebounce)
	}
	RequestIfChanged(st.spell, spellKey{w.spellLanguage(), st.Value}, func(ctx context.Context, k spellKey) spellResult {
		ranges := spell.Check(ctx, k.lang, k.text)
		var errs []spellErr
		for _, r := range ranges {
			errs = append(errs, spellErr{r.Start, r.End})
		}
		return spellResult{k.text, errs}
	})
	return st.spellErrs()
}

// spellErrs returns the last check's misspellings moved to the current
// value: ranges before the first changed byte stay, ranges after the last
// one shift with it, and ranges the edit touched are dropped until the next
// check lands.
func (st *editState) spellErrs() []spellErr {
	if st.spell == nil {
		return nil
	}
	r, ok := st.spell.Value()
	if !ok || len(r.errs) == 0 {
		return nil
	}
	return shiftSpell(r.text, st.Value, r.errs)
}

func shiftSpell(old, cur string, errs []spellErr) []spellErr {
	if old == cur {
		return errs
	}
	p := 0
	for p < len(old) && p < len(cur) && old[p] == cur[p] {
		p++
	}
	s := 0
	for s < len(old)-p && s < len(cur)-p && old[len(old)-1-s] == cur[len(cur)-1-s] {
		s++
	}
	d := len(cur) - len(old)
	var out []spellErr
	for _, e := range errs {
		switch {
		case e.end < p:
			out = append(out, e)
		case e.start > len(old)-s:
			out = append(out, spellErr{e.start + d, e.end + d})
		}
	}
	return out
}

// spellAt returns the misspelling of e's value at window point (x, y).
func (w *Window) spellAt(e *El, st *editState, x, y float32) (spellErr, bool) {
	g := st.geom
	if g == nil || g.password || !spellEnabled(e) {
		return spellErr{}, false
	}
	l := w.ts.Layout(g.text, g.style, g.width)
	off := g.fromDisplay(l.HitTest(x*w.scale-g.originX, y*w.scale-g.originY))
	for _, r := range st.spellErrs() {
		if off >= r.start && off <= r.end {
			return r, true
		}
	}
	return spellErr{}, false
}

// spellMenu returns the spelling items of an Input's context menu for the
// misspelled word at window point (x, y): suggestions, then adding the word
// to the user's dictionary and ignoring it.
func (w *Window) spellMenu(e *El, st *editState, x, y float32, l EditMenuLabels) []MenuItem {
	r, ok := w.spellAt(e, st, x, y)
	if !ok || r.end > len(st.Value) {
		return nil
	}
	word, lang := st.Value[r.start:r.end], w.spellLanguage()
	var items []MenuItem
	for _, s := range spell.Suggest(lang, word) {
		items = append(items, MenuItem{Label: s, OnClick: func() { w.replaceRange(e, st, r, word, s) }})
	}
	if len(items) == 0 {
		items = append(items, MenuItem{Label: or(l.NoSuggestions, defaultEditMenu.NoSuggestions), Disabled: true})
	}
	recheck := func(update func(string, string)) func() {
		return func() {
			update(lang, word)
			// Forget the checked key so the next paint checks again.
			if st.spell != nil {
				st.spell.Cancel()
			}
			w.invalidate()
		}
	}
	return append(items,
		MenuItem{Separator: true},
		MenuItem{Label: or(l.AddToDictionary, defaultEditMenu.AddToDictionary), OnClick: recheck(spell.Add)},
		MenuItem{Label: or(l.IgnoreWord, defaultEditMenu.IgnoreWord), OnClick: recheck(spell.Ignore)},
		MenuItem{Separator: true},
	)
}

// replaceRange replaces r of st's value, if it still holds word, with s.
func (w *Window) replaceRange(e *El, st *editState, r spellErr, word, s string) {
	if r.end > len(st.Value) || st.Value[r.start:r.end] != word || e.input.onChange == nil {
		return
	}
	st.PushUndo(edit.Other)
	st.Value = st.Value[:r.start] + s + st.Value[r.end:]
	st.Caret = r.start + len(s)
	st.Anchor = st.Caret
	st.upstream = false
	w.commitEdit(e, st, st.Value)
}

func or(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// validSpellRange reports whether r lies on rune boundaries of s.
func validSpellRange(s string, r spellErr) bool {
	return r.start >= 0 && r.start < r.end && r.end <= len(s) &&
		(r.start == len(s) || utf8.RuneStart(s[r.start])) && (r.end == len(s) || utf8.RuneStart(s[r.end]))
}
