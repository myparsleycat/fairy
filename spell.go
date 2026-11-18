//go:build windows

package fairy

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/myparsleycat/fairy/internal/edit"
	"github.com/myparsleycat/fairy/internal/spell"
)

const spellDebounce = 150 * time.Millisecond

var spellColor = Hex(0xdc2626)

// spellResult is the misspellings found in text.
type spellResult struct {
	text  string
	errs  []spell.Range
	lang  string
	epoch int64 // spellEpoch when the check started
}

type spellKey struct{ lang, text string }

// spellEpoch counts changes to the user's dictionary. A result from an
// earlier epoch is not reused for the lines an edit left alone.
var spellEpoch atomic.Int64

// spellShift is the last spell.Shift result of an Input.
type spellShift struct {
	old, cur string
	src, out []spell.Range
}

var userLocale = sync.OnceValue(systemLocale)

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
func (w *Window) spellRequest(e *El, st *editState) []spell.Range {
	if !spellEnabled(e) {
		if st.spell != nil {
			st.spell.Cancel()
			st.spell = nil
		}
		return nil
	}
	if st.spell == nil {
		st.spell = NewLatest[spellResult](spellDebounce)
		// Misspellings are only drawn, so their answers need no build.
		st.spell.land = func(f func()) {
			w.PostQuiet(func() {
				f()
				w.invalidate()
			})
		}
	}
	prev, hasPrev := st.spell.Value()
	epoch := spellEpoch.Load()
	RequestIfChanged(st.spell, spellKey{w.spellLanguage(), st.Value}, func(ctx context.Context, k spellKey) spellResult {
		check := func(s string) []spell.Range { return spell.Check(ctx, k.lang, s) }
		var errs []spell.Range
		if hasPrev && prev.lang == k.lang && prev.epoch == epoch {
			errs = spell.Lines(prev.text, k.text, prev.errs, check)
		} else {
			errs = check(k.text)
		}
		return spellResult{k.text, errs, k.lang, epoch}
	})
	return st.spellErrs()
}

// spellErrs returns the last check's misspellings moved to the current
// value: ranges before the first changed byte stay, ranges after the last
// one shift with it, and ranges the edit touched are dropped until the next
// check lands.
func (st *editState) spellErrs() []spell.Range {
	if st.spell == nil {
		return nil
	}
	r, ok := st.spell.Value()
	if !ok || len(r.errs) == 0 {
		return nil
	}
	m := &st.spellShift
	if m.src == nil || &m.src[0] != &r.errs[0] || len(m.src) != len(r.errs) || m.old != r.text || m.cur != st.Value {
		*m = spellShift{old: r.text, cur: st.Value, src: r.errs, out: spell.Shift(r.text, st.Value, r.errs)}
	}
	return m.out
}

// spellAt returns the misspelling of e's value at window point (x, y).
func (w *Window) spellAt(e *El, st *editState, x, y float32) (spell.Range, bool) {
	g := st.geom
	if g == nil || g.password || !spellEnabled(e) {
		return spell.Range{}, false
	}
	l := w.ts.Layout(g.text, g.style, g.width)
	off := g.fromDisplay(l.HitTest(x*w.scale-g.originX, y*w.scale-g.originY))
	for _, r := range st.spellErrs() {
		if off >= r.Start && off <= r.End {
			return r, true
		}
	}
	return spell.Range{}, false
}

// spellMenu returns the spelling items of an Input's context menu for the
// misspelled word at window point (x, y): suggestions, then adding the word
// to the user's dictionary and ignoring it.
func (w *Window) spellMenu(e *El, st *editState, x, y float32, l EditMenuLabels) []MenuItem {
	r, ok := w.spellAt(e, st, x, y)
	if !ok || r.End > len(st.Value) {
		return nil
	}
	word, lang := st.Value[r.Start:r.End], w.spellLanguage()
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
			spellEpoch.Add(1)
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
func (w *Window) replaceRange(e *El, st *editState, r spell.Range, word, s string) {
	if r.End > len(st.Value) || st.Value[r.Start:r.End] != word || e.input.onChange == nil {
		return
	}
	st.PushUndo(edit.Other)
	st.Value = st.Value[:r.Start] + s + st.Value[r.End:]
	st.Caret = r.Start + len(s)
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
