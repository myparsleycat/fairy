//go:build windows

package fairy

import (
	"sync"
	"sync/atomic"

	"github.com/myparsleycat/fairy/internal/win"
)

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
		if h := w.hwnd; h != 0 {
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
