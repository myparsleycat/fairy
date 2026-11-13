//go:build darwin

package darwin

import (
	"sync"

	"github.com/ebitengine/purego"
)

var (
	cfRunLoopGetMain         func() uintptr
	cfRunLoopTimerCreate     func(alloc uintptr, fire, interval float64, flags uint64, order int64, callout, ctx uintptr) uintptr
	cfRunLoopAddTimer        func(rl, timer, mode uintptr)
	cfRunLoopWakeUp          func(rl uintptr)
	cfAbsoluteTimeGetCurrent func() float64

	commonModes   uintptr
	timerCallout  uintptr
	performMu     sync.Mutex
	performs      = map[uintptr]func(){}
	initRunLoopFn = sync.OnceFunc(func() {
		Func(&cfRunLoopGetMain, CoreFoundation, "CFRunLoopGetMain")
		Func(&cfRunLoopTimerCreate, CoreFoundation, "CFRunLoopTimerCreate")
		Func(&cfRunLoopAddTimer, CoreFoundation, "CFRunLoopAddTimer")
		Func(&cfRunLoopWakeUp, CoreFoundation, "CFRunLoopWakeUp")
		Func(&cfAbsoluteTimeGetCurrent, CoreFoundation, "CFAbsoluteTimeGetCurrent")
		commonModes = uintptr(SymbolID(CoreFoundation, "kCFRunLoopCommonModes"))
		timerCallout = purego.NewCallback(func(timer, _ uintptr) {
			performMu.Lock()
			f := performs[timer]
			delete(performs, timer)
			performMu.Unlock()
			CFRelease(timer)
			if f != nil {
				WithPool(f)
			}
		})
	})
)

// Perform runs f on the main thread from a run loop timer in the common
// modes. Unlike Async, whose main dispatch queue does not run a block while
// another one runs, f also runs inside a nested run loop, such as a modal
// panel or a menu that a dispatched block opened, and a modal loop f itself
// runs does not hold up Async. It is safe from any goroutine.
func Perform(f func()) {
	initRunLoopFn()
	performMu.Lock()
	t := cfRunLoopTimerCreate(0, cfAbsoluteTimeGetCurrent(), 0, 0, 0, timerCallout, 0)
	performs[t] = f
	performMu.Unlock()
	rl := cfRunLoopGetMain()
	cfRunLoopAddTimer(rl, t, commonModes)
	cfRunLoopWakeUp(rl)
}
