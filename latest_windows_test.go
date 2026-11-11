package fairy

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func newTestLatest[T any](debounce time.Duration) (*Latest[T], chan T) {
	l := NewLatest[T](debounce)
	l.land = func(f func()) { f() }
	landed := make(chan T, 16)
	l.OnLand(func(v T) { landed <- v })
	return l, landed
}

// A request superseded while it runs has its context canceled, and its
// answer is dropped even when it arrives after the newer one's.
func TestLatestSupersede(t *testing.T) {
	l, landed := newTestLatest[int](0)
	started, release := make(chan struct{}), make(chan struct{})
	var canceled atomic.Bool
	l.Request(func(ctx context.Context) int {
		close(started)
		<-ctx.Done()
		canceled.Store(true)
		<-release
		return 1
	})
	<-started
	l.Request(func(context.Context) int { return 2 })
	if v := <-landed; v != 2 {
		t.Fatalf("landed %d, want 2", v)
	}
	close(release)
	time.Sleep(20 * time.Millisecond)
	if !canceled.Load() {
		t.Fatal("the superseded request's context was not canceled")
	}
	if v, _ := l.Value(); v != 2 || len(landed) != 0 {
		t.Fatalf("value %d with %d more landings, want 2 and none", v, len(landed))
	}
	if l.Pending() {
		t.Fatal("still pending after the newest request landed")
	}
}

// Requests made within the debounce run once, with the newest work.
func TestLatestDebounce(t *testing.T) {
	l, landed := newTestLatest[int](40 * time.Millisecond)
	var runs atomic.Int32
	for i := range 5 {
		l.Request(func(context.Context) int { runs.Add(1); return i })
	}
	if v := <-landed; v != 4 {
		t.Fatalf("landed %d, want 4", v)
	}
	if n := runs.Load(); n != 1 {
		t.Fatalf("work ran %d times, want 1", n)
	}
}

// RequestIfChanged with the same key keeps the request in flight; Cancel
// drops it and forgets the key.
func TestLatestKeyAndCancel(t *testing.T) {
	l, landed := newTestLatest[string](0)
	release, started := make(chan struct{}), make(chan struct{}, 2)
	var runs atomic.Int32
	work := func(ctx context.Context, k string) string {
		runs.Add(1)
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return k
	}
	RequestIfChanged(l, "a", work)
	<-started
	RequestIfChanged(l, "a", work)
	l.Cancel()
	if l.Pending() {
		t.Fatal("pending after Cancel")
	}
	RequestIfChanged(l, "a", work)
	close(release)
	if v := <-landed; v != "a" {
		t.Fatalf("landed %q, want \"a\"", v)
	}
	if n := runs.Load(); n != 2 {
		t.Fatalf("work ran %d times, want 2 (once before Cancel, once after)", n)
	}
}
