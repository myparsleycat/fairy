package fairy

import (
	"strconv"
	"sync/atomic"
)

// Memo returns the subtree build made the last time the view was built with
// the same key and an equal version, instead of calling build again. Use it
// for parts of a view that are costly to build and change rarely, such as a
// long rendered document: put everything the subtree shows in version, which
// must be comparable (a struct of the inputs works).
//
// The reused subtree keeps the handlers it was built with, so they see the
// values captured then; values a handler reads must be in version or read
// through a pointer. Keys need only be unique among the Memo calls of one
// build: a Memo inside another, or inside a virtual list row, is told apart
// by the outer key or the row. A second Memo with the same key in one build
// builds afresh. The subtree is built again after InvalidateMemos, when the
// window's DPI or the system's color mode or animation setting changes,
// when an ancestor's Disabled state changes, and when it was not in the
// previous build. Overlays added and focus moved while build runs are not
// remembered, so build calls that do them run every time.
func Memo[V comparable](key string, version V, build func() *El) *El {
	w := CurrentWindow()
	if w == nil {
		return build()
	}
	full := w.memoPath + "\x00" + key
	m := w.memos[full]
	if m != nil && m.ver != w.treeVer && m.version == any(version) && m.epoch == memoEpoch.Load() &&
		w.gen-m.gen <= 1 && m.el != nil {
		m.gen, m.ver = w.gen, w.treeVer
		w.touchLocals(m.locals)
		return m.el
	}
	if m != nil && m.ver == w.treeVer {
		// The same key twice in one build: the first one keeps the entry.
		return build()
	}

	path, rec := w.memoPath, w.memoRec
	layers, focusKey, focused := len(w.layers), w.focusKey, w.focused
	var locals []string
	w.memoPath, w.memoRec = full, &locals
	el := build()
	w.memoPath, w.memoRec = path, rec
	if rec != nil {
		*rec = append(*rec, locals...)
	}
	if len(w.layers) != layers || w.focusKey != focusKey || w.focused != focused || el == nil || el.memo != nil {
		delete(w.memos, full)
		return el
	}
	if w.memos == nil {
		w.memos = map[string]*memoEntry{}
	}
	m = &memoEntry{version: version, el: el, gen: w.gen, ver: w.treeVer, epoch: memoEpoch.Load(), locals: locals}
	el.memo = m
	w.memos[full] = m
	return el
}

type memoEntry struct {
	version any
	el      *El
	gen     int // the build that last used it
	ver     int // the tree version that last used it, to spot a key used twice
	epoch   int64
	locals  []string // Local keys the build touched, kept alive while it is reused
	// Whether the parent was disabled when the subtree was last prepared:
	// preparing disables a subtree for good, so a change needs a new build.
	placed, parentDisabled bool
}

// prepareMemo notes the parent of a remembered subtree. When the parent's
// disabled state changed since, the entry is dropped and the view built
// again in the same frame.
func (w *Window) prepareMemo(e, parent *El) {
	m := e.memo
	pd := parent != nil && parent.disabled()
	if m.placed && m.parentDisabled != pd {
		m.el = nil
		w.markDirty(dirtyUpdate)
	}
	m.placed, m.parentDisabled = true, pd
}

var memoEpoch atomic.Int64

// InvalidateMemos makes every Memo build its subtree again, as after a
// change its builds read but its version does not hold, such as the theme.
func InvalidateMemos() { memoEpoch.Add(1) }

func (w *Window) touchLocals(keys []string) {
	for _, k := range keys {
		if l, ok := w.locals[k]; ok {
			l.gen = w.gen
		}
	}
	if w.memoRec != nil {
		*w.memoRec = append(*w.memoRec, keys...)
	}
}

func (w *Window) recordLocal(key string) {
	if w.memoRec != nil {
		*w.memoRec = append(*w.memoRec, key)
	}
}

// sweepMemos drops entries the last build did not use.
func (w *Window) sweepMemos() {
	for k, m := range w.memos {
		if w.gen-m.gen > 1 {
			delete(w.memos, k)
		}
	}
}

// rowMemoPath names the Memo calls inside row i of the virtual list e.
func rowMemoPath(e *El, i int, key string) string {
	if key == "" {
		key = strconv.Itoa(i)
	}
	return strconv.FormatUint(e.id, 36) + "/" + key
}
