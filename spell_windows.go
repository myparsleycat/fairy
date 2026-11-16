package fairy

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
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
	text  string
	errs  []spellErr
	lang  string
	epoch int64 // spellEpoch when the check started
}

type spellKey struct{ lang, text string }

// spellEpoch counts changes to the user's dictionary. A result from an
// earlier epoch is not reused for the lines an edit left alone.
var spellEpoch atomic.Int64

// spellShift is the last shiftSpell result of an Input.
type spellShift struct {
	old, cur string
	src, out []spellErr
}

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
	prev, hasPrev := st.spell.Value()
	epoch := spellEpoch.Load()
	RequestIfChanged(st.spell, spellKey{w.spellLanguage(), st.Value}, func(ctx context.Context, k spellKey) spellResult {
		check := func(s string) []spellErr {
			var errs []spellErr
			for _, r := range spell.Check(ctx, k.lang, s) {
				errs = append(errs, spellErr{r.Start, r.End})
			}
			return errs
		}
		var errs []spellErr
		if hasPrev && prev.lang == k.lang && prev.epoch == epoch {
			errs = spellLines(prev.text, k.text, prev.errs, check)
		} else {
			errs = check(k.text)
		}
		return spellResult{k.text, errs, k.lang, epoch}
	})
	return st.spellErrs()
}

// spellLines returns the misspellings of cur given those of old: it checks
// the lines of cur that differ from old and moves the ranges on the other
// lines. The checker also reports errors that depend on the next or previous
// line, such as a word repeated across a line break, so the ranges on the
// lines next to the changed ones are taken from a check that includes the
// lines next to those.
func spellLines(old, cur string, errs []spellErr, check func(string) []spellErr) []spellErr {
	p, s := commonEnds(old, cur)
	lo, hi := linesBefore(cur, p, 1), linesAfter(cur, len(cur)-s, 1)
	from, to := linesBefore(cur, p, 2), linesAfter(cur, len(cur)-s, 2)
	d := len(cur) - len(old)
	var out []spellErr
	for _, e := range errs {
		if e.end <= lo {
			out = append(out, e)
		}
	}
	for _, e := range check(cur[from:to]) {
		if e.start+from < hi && e.end+from > lo {
			out = append(out, spellErr{e.start + from, e.end + from})
		}
	}
	for _, e := range errs {
		if e.start >= hi-d {
			out = append(out, spellErr{e.start + d, e.end + d})
		}
	}
	return out
}

// linesBefore returns the start of the line n lines before the one holding
// s[i], or 0.
func linesBefore(s string, i, n int) int {
	start := strings.LastIndexByte(s[:i], '\n') + 1
	for ; n > 0 && start > 0; n-- {
		start = strings.LastIndexByte(s[:start-1], '\n') + 1
	}
	return start
}

// linesAfter returns the end, before its line break, of the line n lines
// after the one holding s[i], or len(s).
func linesAfter(s string, i, n int) int {
	end := strings.IndexByte(s[i:], '\n')
	if end < 0 {
		return len(s)
	}
	end += i
	for ; n > 0; n-- {
		next := strings.IndexByte(s[end+1:], '\n')
		if next < 0 {
			return len(s)
		}
		end += 1 + next
	}
	return end
}

// commonEnds returns the lengths of the common prefix of a and b and of the
// common suffix of what follows it.
func commonEnds(a, b string) (p, s int) {
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	return p, s
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
	m := &st.spellShift
	if m.src == nil || &m.src[0] != &r.errs[0] || len(m.src) != len(r.errs) || m.old != r.text || m.cur != st.Value {
		*m = spellShift{old: r.text, cur: st.Value, src: r.errs, out: shiftSpell(r.text, st.Value, r.errs)}
	}
	return m.out
}

func shiftSpell(old, cur string, errs []spellErr) []spellErr {
	if old == cur {
		return errs
	}
	p, s := commonEnds(old, cur)
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
