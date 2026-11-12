package fairy

import (
	"context"
	"sync"
	"time"
)

// Latest runs background requests of which only the newest one's answer
// lands, for a search box or a completion list that asks on every key.
//
// Request runs work on its own goroutine. A newer request, or Cancel,
// cancels the context of every earlier one: one still waiting out the
// debounce never runs, and one running should return early; its answer is
// dropped either way. The answer of the newest request lands on the UI
// thread through Go, so windows update once: Value returns it from then on,
// and the OnLand function runs with it.
//
// Latest is safe for use from any goroutine.
type Latest[T any] struct {
	mu       sync.Mutex
	debounce time.Duration
	gen      uint64 // the newest request; Cancel takes one too
	settled  uint64 // the newest request that landed, returned nothing or was canceled
	cancel   context.CancelFunc
	key      any
	keyed    bool
	value    T
	has      bool
	onLand   func(T)
	land     func(func())
}

// NewLatest returns a Latest whose requests wait for debounce without a newer
// request before they start, so typing runs one request per pause.
func NewLatest[T any](debounce time.Duration) *Latest[T] {
	return &Latest[T]{debounce: debounce, land: Go}
}

// OnLand sets a function that runs on the UI thread with each answer that
// lands.
func (l *Latest[T]) OnLand(f func(T)) {
	l.mu.Lock()
	l.onLand = f
	l.mu.Unlock()
}

// Request starts work and supersedes every earlier request.
func (l *Latest[T]) Request(work func(ctx context.Context) T) {
	l.start(func(ctx context.Context) (T, bool) { return work(ctx), true }, nil, false)
}

// RequestMaybe is Request for work that may have no answer: when it returns
// false nothing lands and no window updates, but it still supersedes the
// requests before it.
func (l *Latest[T]) RequestMaybe(work func(ctx context.Context) (T, bool)) {
	l.start(work, nil, false)
}

// RequestIfChanged requests work for key unless the last request was made
// by RequestIfChanged with an equal key, so a view or handler can call it
// every time without restarting the work. Request, RequestMaybe and Cancel
// forget the key.
func RequestIfChanged[K comparable, T any](l *Latest[T], key K, work func(ctx context.Context, key K) T) {
	l.mu.Lock()
	if l.keyed {
		if k, ok := l.key.(K); ok && k == key {
			l.mu.Unlock()
			return
		}
	}
	l.mu.Unlock()
	l.start(func(ctx context.Context) (T, bool) { return work(ctx, key), true }, key, true)
}

// Cancel supersedes every request without starting a new one.
func (l *Latest[T]) Cancel() {
	l.mu.Lock()
	l.gen++
	l.settled = l.gen
	l.key, l.keyed = nil, false
	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
	}
	l.mu.Unlock()
}

// Value returns the answer that landed last, and whether one has.
func (l *Latest[T]) Value() (T, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.value, l.has
}

// Pending reports whether the newest request has not landed or settled yet.
func (l *Latest[T]) Pending() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.settled < l.gen
}

func (l *Latest[T]) start(work func(ctx context.Context) (T, bool), key any, keyed bool) {
	ctx, cancel := context.WithCancel(context.Background())
	l.mu.Lock()
	l.gen++
	g := l.gen
	if l.cancel != nil {
		l.cancel()
	}
	l.cancel = cancel
	l.key, l.keyed = key, keyed
	debounce, land := l.debounce, l.land
	l.mu.Unlock()
	go func() {
		defer cancel()
		if debounce > 0 {
			t := time.NewTimer(debounce)
			select {
			case <-t.C:
			case <-ctx.Done():
				t.Stop()
				return
			}
		}
		if ctx.Err() != nil {
			return
		}
		v, ok := work(ctx)
		if ctx.Err() != nil {
			return
		}
		if !ok {
			l.mu.Lock()
			if g == l.gen {
				l.settled = g
			}
			l.mu.Unlock()
			return
		}
		land(func() {
			l.mu.Lock()
			if g != l.gen {
				l.mu.Unlock()
				return
			}
			l.settled = g
			l.value, l.has = v, true
			f := l.onLand
			l.mu.Unlock()
			if f != nil {
				f(v)
			}
		})
	}()
}
