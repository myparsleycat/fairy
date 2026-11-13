//go:build darwin

package darwin

import (
	"sync"
	"time"

	"github.com/ebitengine/purego"
)

var (
	dispatchAsyncF          func(queue, ctx, fn uintptr)
	dispatchTime            func(when uint64, delta int64) uint64
	dispatchSourceCreate    func(typ, handle, mask, queue uintptr) uintptr
	dispatchSourceSetTimer  func(source uintptr, start, interval, leeway uint64)
	dispatchSetContext      func(obj, ctx uintptr)
	dispatchSetEventHandler func(source, fn uintptr)
	dispatchResume          func(obj uintptr)
	dispatchSourceCancel    func(source uintptr)
	dispatchRelease         func(obj uintptr)

	mainQueue   uintptr
	timerType   uintptr
	runCallback uintptr

	callMu   sync.Mutex
	calls    = map[uintptr]func(){}
	nextCall uintptr
)

func init() {
	Func(&dispatchAsyncF, System, "dispatch_async_f")
	Func(&dispatchTime, System, "dispatch_time")
	Func(&dispatchSourceCreate, System, "dispatch_source_create")
	Func(&dispatchSourceSetTimer, System, "dispatch_source_set_timer")
	Func(&dispatchSetContext, System, "dispatch_set_context")
	Func(&dispatchSetEventHandler, System, "dispatch_source_set_event_handler_f")
	Func(&dispatchResume, System, "dispatch_resume")
	Func(&dispatchSourceCancel, System, "dispatch_source_cancel")
	Func(&dispatchRelease, System, "dispatch_release")
	mainQueue = Symbol(System, "_dispatch_main_q")
	timerType = Symbol(System, "_dispatch_source_type_timer")
	runCallback = purego.NewCallback(func(ctx uintptr) {
		callMu.Lock()
		f := calls[ctx]
		callMu.Unlock()
		if f != nil {
			f()
		}
	})
}

func register(f func()) uintptr {
	callMu.Lock()
	defer callMu.Unlock()
	nextCall++
	calls[nextCall] = f
	return nextCall
}

func unregister(id uintptr) {
	callMu.Lock()
	delete(calls, id)
	callMu.Unlock()
}

// Async runs f on the main thread from the main dispatch queue, which the
// main run loop serves in every common mode, so f also runs during live
// resizing and menu tracking. It is safe from any goroutine.
func Async(f func()) {
	var id uintptr
	id = register(func() {
		unregister(id)
		WithPool(f)
	})
	dispatchAsyncF(mainQueue, id, runCallback)
}

// Timer runs a function on the main thread at an interval until Stop.
type Timer struct {
	source uintptr
	id     uintptr
}

// NewTimer calls f on the main thread every interval, first after one interval.
func NewTimer(interval time.Duration, f func()) *Timer {
	t := &Timer{id: register(func() { WithPool(f) })}
	t.source = dispatchSourceCreate(timerType, 0, 0, mainQueue)
	ns := uint64(max(interval, time.Millisecond))
	dispatchSourceSetTimer(t.source, dispatchTime(0, int64(ns)), ns, ns/10)
	dispatchSetContext(t.source, t.id)
	dispatchSetEventHandler(t.source, runCallback)
	dispatchResume(t.source)
	return t
}

// Stop cancels the timer. A call already queued does not run.
func (t *Timer) Stop() {
	if t == nil || t.source == 0 {
		return
	}
	unregister(t.id)
	dispatchSourceCancel(t.source)
	dispatchRelease(t.source)
	t.source = 0
}
