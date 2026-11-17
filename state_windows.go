package fairy

import (
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/myparsleycat/fairy/internal/frameprof"
	"github.com/myparsleycat/fairy/internal/win"
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
		m.gen, m.ver, m.hit = w.gen, w.treeVer, true
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
	// hit is set when the last Memo call reused el. prepared, inh and grows
	// tell whether el's subtree was prepared before, with which inherited
	// text, and whether it holds an AnimateHeight, which moves laid-out
	// boxes between builds.
	hit, prepared, grows bool
	inh                  textProps
}

// prepareMemo notes the parent of a remembered subtree. When the parent's
// disabled state changed since, the entry is dropped and the view built
// again in the same frame.
func (w *Window) prepareMemo(e, parent *El) {
	m := e.memo
	pd := parent != nil && parent.disabled()
	if m.placed && m.parentDisabled != pd {
		m.el = nil
		w.markDirty(frameprof.CauseUpdate)
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

// Local returns state of type T kept under key in the window whose view is
// being built, creating it on first use. It survives rebuilds while some
// View keeps asking for it and is dropped a few builds after it stops.
// Call it from View, on the UI thread. Components use it for state the app
// does not care about, such as whether a dropdown is open.
func Local[T any](key string) *T {
	w := CurrentWindow()
	if w == nil {
		return new(T)
	}
	l, ok := w.locals[key]
	if ok {
		if v, ok := l.value.(*T); ok {
			l.gen = w.gen
			w.recordLocal(key)
			return v
		}
	}
	v := new(T)
	w.locals[key] = &localEntry{value: v, gen: w.gen}
	w.recordLocal(key)
	return v
}

type localEntry struct {
	value any
	gen   int
}

var viewBuilds = struct {
	sync.RWMutex
	windows map[uint32]*Window
}{windows: map[uint32]*Window{}}

func (w *Window) withBuildWindow(build func() *El) *El {
	// Offscreen views can build on different threads, and a view can build
	// another view on the same thread before returning.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	thread := win.GetCurrentThreadId()
	viewBuilds.Lock()
	prev := viewBuilds.windows[thread]
	viewBuilds.windows[thread] = w
	viewBuilds.Unlock()
	defer func() {
		viewBuilds.Lock()
		if prev == nil {
			delete(viewBuilds.windows, thread)
		} else {
			viewBuilds.windows[thread] = prev
		}
		viewBuilds.Unlock()
	}()
	return build()
}

// CurrentWindow returns the window whose view is being built, or nil outside
// View. Components keep it for handlers that act on the window, such as
// scrolling a row into view.
func CurrentWindow() *Window {
	thread := win.GetCurrentThreadId()
	viewBuilds.RLock()
	w := viewBuilds.windows[thread]
	viewBuilds.RUnlock()
	return w
}

// Point is a position or offset in logical pixels.
type Point struct{ X, Y float32 }

// Live holds a value that elements read each time they are drawn rather
// than when the view is built. Set draws the windows that showed the value
// again without building their views, laying them out or running View, so
// a value that changes every frame, such as a level meter or a playback
// position, costs only a redraw. Bind it with OpacityLive, OffsetLive,
// FillLive, SweepLive or TextLive. Live is safe for use from any goroutine.
type Live[T float32 | Color | Point | string] struct {
	v       atomic.Pointer[T]
	mu      sync.Mutex
	shownIn map[*Window]struct{}
}

// NewLive returns a Live holding v.
func NewLive[T float32 | Color | Point | string](v T) *Live[T] {
	l := &Live[T]{}
	l.v.Store(&v)
	return l
}

// Get returns the value.
func (l *Live[T]) Get() T {
	if p := l.v.Load(); p != nil {
		return *p
	}
	var zero T
	return zero
}

// Set changes the value and redraws the windows that showed it.
func (l *Live[T]) Set(v T) {
	l.v.Store(&v)
	l.mu.Lock()
	ws := l.shownIn
	l.shownIn = nil
	l.mu.Unlock()
	for w := range ws {
		if w.closed.Load() {
			// Its handle may belong to another window now.
			continue
		}
		switch h := w.hwnd; {
		case h != 0 && w.opts.Headless:
			// A message-only window gets no WM_PAINT; the window paces
			// its own frames on the UI thread.
			win.PostMessage(h, msgInvalidate, 0, 0)
		case h != 0:
			win.InvalidateRect(h, false)
		}
	}
}

// read returns the value and remembers that w shows it.
func (l *Live[T]) read(w *Window) T {
	l.mu.Lock()
	if l.shownIn == nil {
		l.shownIn = map[*Window]struct{}{}
	}
	l.shownIn[w] = struct{}{}
	l.mu.Unlock()
	return l.Get()
}

type liveProps struct {
	opacity, sweep *Live[float32]
	offset         *Live[Point]
	fill           *Live[Color]
	text           *Live[string]
}

func (e *El) liveProps() *liveProps {
	if e.live == nil {
		e.live = &liveProps{}
	}
	return e.live
}

// OpacityLive draws the element at l's opacity, as Opacity does.
func (e *El) OpacityLive(l *Live[float32]) *El { e.liveProps().opacity = l; return e }

// OffsetLive draws the element and its subtree moved by l, and hit tests
// them there; the layout keeps their place.
func (e *El) OffsetLive(l *Live[Point]) *El { e.liveProps().offset = l; return e }

// FillLive fills the element with l's color, as Fill does.
func (e *El) FillLive(l *Live[Color]) *El { e.liveProps().fill = l; return e }

// SweepLive sets an Arc's sweep from l, in radians.
func (e *El) SweepLive(l *Live[float32]) *El {
	if e.arc != nil {
		e.liveProps().sweep = l
	}
	return e
}

// TextLive shows l's text in a Text element. The element is laid out with
// the text it has when the view is built and keeps that size, so give it a
// size that fits every value, as with W or MinW.
func (e *El) TextLive(l *Live[string]) *El {
	if e.kind == kindText {
		e.liveProps().text = l
		e.text = l.Get()
	}
	return e
}

// applyLive copies the Live values e is bound to into the fields paint reads.
func (w *Window) applyLive(e *El) {
	p := e.live
	if p.opacity != nil {
		e.opacity = p.opacity.read(w)
	}
	if p.fill != nil {
		e.fill = p.fill.read(w)
	}
	if p.sweep != nil {
		e.arc.sweep = max(p.sweep.read(w), 0)
	}
	if p.text != nil {
		e.text = p.text.read(w)
	}
}
