package fairy

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/myparsleycat/fairy/internal/apphost"
)

// app is the state of the UI loop that Main runs.
var app struct {
	appPlat

	main    sync.Mutex
	thread  atomic.Uint32
	closing bool
	done    chan struct{} // closed when this Main run begins shutting down

	mu    sync.Mutex
	calls []appCall
	quiet []appCall // like calls, but no window updates afterwards

	onLaunch func(Launch)

	onSessionEnd func() // guarded by mu
}

type appCall struct {
	run     func()
	cancel  func()
	session <-chan struct{}
}

var errUIUnavailable = apphost.ErrUIUnavailable

func init() {
	apphost.OnUIThread = onUIThread
	apphost.Running = func() bool { return app.thread.Load() != 0 }
	apphost.Do = func(f func()) bool { return doAppCall(f, false) }
	apphost.Go = Go
	apphost.UpdateWindows = updateWindows
	apphost.QuitIfIdle = quitIfIdle
}

// OnSessionEnd sets f to run when the system ends the user session, at sign-out, restart or shutdown. f runs on the
// UI thread while Main is running. The system may end the process as soon as f returns, without Main returning, so f
// must finish the cleanup that would otherwise follow Main. It may be called from any goroutine; nil removes it.
func OnSessionEnd(f func()) {
	app.mu.Lock()
	app.onSessionEnd = f
	app.mu.Unlock()
}

// Go runs f on the UI thread without waiting, then updates every window.
// It may be called from any goroutine; before Main starts, f is queued until it does.
// Calls still queued when Main exits are discarded.
func Go(f func()) {
	enqueueAppCall(appCall{run: f}, false)
}

// Do runs f on the UI thread and waits for it to return. On the UI thread it
// calls f directly. If no UI loop is running, or it exits before f runs, Do
// returns without calling f.
func Do(f func()) {
	doAppCall(f, false)
}

// doQuiet runs f on the UI thread and waits, without updating windows
// afterwards. It is for queries that change no state.
func doQuiet(f func()) {
	doAppCall(f, true)
}

func doAppCall(f func(), quiet bool) bool {
	if onUIThread() {
		f()
		return true
	}
	done := make(chan bool, 1)
	call := appCall{
		run: func() {
			defer func() { done <- true }()
			f()
		},
		cancel: func() { done <- false },
	}
	return enqueueAppCall(call, quiet) && <-done
}

func enqueueAppCall(call appCall, quiet bool) bool {
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.closing || (!appLoopReady() && call.cancel != nil) || (call.session != nil && call.session != app.done) {
		return false
	}
	queue := &app.calls
	if quiet {
		queue = &app.quiet
	}
	if len(*queue) == 0 && appLoopReady() && !wakeAppLoop(quiet) {
		return false
	}
	*queue = append(*queue, call)
	return true
}

func cancelAppCalls(calls []appCall) {
	for _, call := range calls {
		if call.cancel != nil {
			call.cancel()
		}
	}
}

func onUIThread() bool {
	thread := app.thread.Load()
	return thread != 0 && currentThreadID() == thread
}

func runAppCalls() {
	app.mu.Lock()
	calls := app.calls
	app.calls = nil
	app.mu.Unlock()
	for _, call := range calls {
		t := time.Now()
		call.run()
		if debugSlow > 0 {
			slow("Go call", t)
		}
	}
	// Calls usually change app state, so every window rebuilds its view.
	updateWindows()
}

func runQuietCalls() {
	app.mu.Lock()
	calls := app.quiet
	app.quiet = nil
	app.mu.Unlock()
	for _, call := range calls {
		call.run()
	}
}

// Launch is a later start of the program, forwarded to the running instance.
type Launch struct {
	Args []string // os.Args of the new process
	Dir  string   // its working directory
}
