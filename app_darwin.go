package fairy

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/ebitengine/purego/objc"

	"github.com/myparsleycat/fairy/internal/apphost"
	"github.com/myparsleycat/fairy/internal/darwin"
)

// appPlat is the macOS part of app.
type appPlat struct {
	running      bool // the NSApplication run loop runs
	sessionEnded bool // guarded by app.mu
}

func init() {
	// AppKit runs only on the main thread. Package init runs on it, so
	// locking here keeps the main goroutine there for Main.
	runtime.LockOSThread()
}

// openWindows are the native windows that are open, by NSWindow.
var openWindows = map[darwin.ID]*Window{}

var nsApp darwin.ID

// launchApp makes the shared NSApplication once: a regular app with a Dock
// icon, the standard app and Window menus, and watchers of the system's
// appearance settings.
var launchApp = sync.OnceFunc(func() {
	nsApp = darwin.SendClass("NSApplication", "sharedApplication")
	darwin.Send(nsApp, "setActivationPolicy:", int64(0)) // NSApplicationActivationPolicyRegular
	delegate := darwin.New("FairyAppDelegate")
	darwin.Send(nsApp, "setDelegate:", delegate)
	darwin.Send(nsApp, "setMainMenu:", newMainMenu())
	watchSystemSettings()
	darwin.Send(nsApp, "finishLaunching")
})

// newMainMenu makes the menu bar: the app menu, with Hide and Quit, the Edit
// menu, and the Window menu, which AppKit fills with the open windows. Other
// shortcuts, and ⌘C and the like while the Edit menu has nothing to send
// them to, reach the focused window's key handlers.
func newMainMenu() darwin.ID {
	name := filepath.Base(os.Args[0])
	bar := darwin.New("NSMenu")
	item := func(menu darwin.ID, title, action, key string, mods uint64) {
		it := darwin.Send(darwin.SendClass("NSMenuItem", "alloc"), "initWithTitle:action:keyEquivalent:",
			darwin.String(title), darwin.Sel(action), darwin.String(key))
		if mods != 0 {
			darwin.Send(it, "setKeyEquivalentModifierMask:", mods)
		}
		darwin.Send(menu, "addItem:", it)
		darwin.Release(it)
	}
	separator := func(menu darwin.ID) { darwin.Send(menu, "addItem:", darwin.SendClass("NSMenuItem", "separatorItem")) }
	submenu := func(title string) darwin.ID {
		top := darwin.New("NSMenuItem")
		menu := darwin.Send(darwin.SendClass("NSMenu", "alloc"), "initWithTitle:", darwin.String(title))
		darwin.Send(top, "setSubmenu:", menu)
		darwin.Send(bar, "addItem:", top)
		darwin.Release(top)
		darwin.Release(menu)
		return menu
	}

	app := submenu(name)
	item(app, "About "+name, "orderFrontStandardAboutPanel:", "", 0)
	separator(app)
	services := darwin.Send(darwin.SendClass("NSMenu", "alloc"), "initWithTitle:", darwin.String("Services"))
	servicesItem := darwin.Send(darwin.SendClass("NSMenuItem", "alloc"), "initWithTitle:action:keyEquivalent:",
		darwin.String("Services"), darwin.SEL(0), darwin.String(""))
	darwin.Send(servicesItem, "setSubmenu:", services)
	darwin.Send(app, "addItem:", servicesItem)
	darwin.Send(nsApp, "setServicesMenu:", services)
	darwin.Release(servicesItem)
	darwin.Release(services)
	separator(app)
	item(app, "Hide "+name, "hide:", "h", nsCommand)
	item(app, "Hide Others", "hideOtherApplications:", "h", nsCommand|nsOption)
	item(app, "Show All", "unhideAllApplications:", "", 0)
	separator(app)
	item(app, "Quit "+name, "terminate:", "q", nsCommand)

	// The Edit menu gives AppKit's text fields and web views their editing
	// shortcuts. Its items are disabled while the window's own view has the
	// keyboard, so the keys reach its handlers.
	edit := submenu("Edit")
	item(edit, "Undo", "undo:", "z", nsCommand)
	item(edit, "Redo", "redo:", "z", nsCommand|nsShift)
	separator(edit)
	item(edit, "Cut", "cut:", "x", nsCommand)
	item(edit, "Copy", "copy:", "c", nsCommand)
	item(edit, "Paste", "paste:", "v", nsCommand)
	item(edit, "Select All", "selectAll:", "a", nsCommand)

	window := submenu("Window")
	item(window, "Minimize", "performMiniaturize:", "m", nsCommand)
	item(window, "Zoom", "performZoom:", "", 0)
	separator(window)
	item(window, "Bring All to Front", "arrangeInFront:", "", 0)
	darwin.Send(nsApp, "setWindowsMenu:", window)
	return bar
}

var appDelegateClass = sync.OnceValue(func() darwin.Class {
	c, err := objc.RegisterClass("FairyAppDelegate", darwin.Cls("NSObject"), nil, nil, []objc.MethodDef{
		{
			// Quit from the app menu or the Dock ends Main rather than the
			// process, as Quit does. Logging out, restarting or shutting
			// down must not be refused: Main's windows and shutdown
			// functions are closed, and then the process ends.
			Cmd: darwin.Sel("applicationShouldTerminate:"),
			Fn: func(self darwin.ID, _ darwin.SEL, sender darwin.ID) uint64 {
				if systemQuit() {
					notifySessionEnd()
					finishMain()
					return 1 // NSTerminateNow
				}
				// ⌘Q closes every window, so each window's OnClose may
				// keep the app running, as closing it would.
				for _, w := range windowsFrontToBack() {
					if !w.closed && !w.mayClose() {
						return 0 // NSTerminateCancel
					}
				}
				Quit()
				return 0 // NSTerminateCancel
			},
		},
	})
	if err != nil {
		panic(err)
	}
	return c
})

func init() { appDelegateClass() }

// appLoopReady reports whether the UI loop can be woken.
func appLoopReady() bool { return app.running }

// wakeAppLoop asks the UI loop to run the queued calls, or the quiet ones.
// They run from a run loop timer, not the main dispatch queue, so that they
// also run while a call shows a modal dialog or a menu, as on Windows.
func wakeAppLoop(quiet bool) bool {
	darwin.Perform(func() {
		if quiet {
			runQuietCalls()
		} else {
			runAppCalls()
		}
	})
	return true
}

// Main runs the UI loop on the main thread, where it must be called. start
// runs on that thread first and typically opens windows and a tray icon.
// Main returns after Quit, or once no window and no tray icon is left.
func Main(start func() error) error {
	if !app.main.TryLock() {
		return errors.New("fairy: Main is already running")
	}
	defer app.main.Unlock()
	if !darwin.Get[bool](darwin.ID(darwin.Cls("NSThread")), "isMainThread") {
		return errors.New("fairy: Main must run on the main goroutine")
	}
	pool := darwin.NewPool()
	defer pool.Drain()
	launchApp()

	app.mu.Lock()
	app.running = true
	app.sessionEnded = false
	app.done = make(chan struct{})
	app.thread.Store(currentThreadID())
	queued := len(app.calls) != 0
	app.mu.Unlock()
	screenPixels() // measured on the UI thread for other goroutines
	if queued {
		wakeAppLoop(false)
	}
	defer finishMain()
	if err := start(); err != nil {
		return err
	}
	if len(openWindows) == 0 && apphost.Holds() == 0 {
		return nil
	}
	// An app that starts with only hidden windows, such as a tray app,
	// does not take the focus from the app in front.
	for _, w := range openWindows {
		if w.IsVisible() {
			darwin.Send(nsApp, "activateIgnoringOtherApps:", true)
			break
		}
	}
	darwin.Send(nsApp, "run")
	return nil
}

// finishMain cancels the queued calls, closes the windows and runs the
// shutdown functions as Main ends. It does nothing outside Main.
func finishMain() {
	app.mu.Lock()
	if app.done == nil || app.closing {
		app.mu.Unlock()
		return
	}
	app.closing = true
	close(app.done)
	calls, quiet := app.calls, app.quiet
	app.calls, app.quiet = nil, nil
	app.mu.Unlock()
	cancelAppCalls(calls)
	cancelAppCalls(quiet)
	for _, w := range openWindows {
		w.closeNow()
	}
	apphost.Shutdown()
	app.mu.Lock()
	app.running = false
	app.done = nil
	app.thread.Store(0)
	app.closing = false
	app.mu.Unlock()
}

// keyAEQuitReason ('why?') holds the reason of the quit Apple event that
// the system sends when it logs out, restarts or shuts down. loginwindow
// stores it as an attribute of the event (AEPutAttributePtr, typeType), not
// as the parameter AERegistry.h describes, so both places are read.
const keyAEQuitReason = 'w'<<24 | 'h'<<16 | 'y'<<8 | '?'

// systemQuit reports whether the termination being asked for comes from
// the system logging out, restarting or shutting down.
func systemQuit() bool {
	ev := darwin.Send(darwin.SendClass("NSAppleEventManager", "sharedAppleEventManager"), "currentAppleEvent")
	return sessionQuitEvent(ev)
}

func sessionQuitEvent(ev darwin.ID) bool {
	if ev == 0 {
		return false
	}
	reason := darwin.Send(ev, "attributeDescriptorForKeyword:", uint32(keyAEQuitReason))
	if reason == 0 {
		reason = darwin.Send(ev, "paramDescriptorForKeyword:", uint32(keyAEQuitReason))
	}
	if reason == 0 {
		return false
	}
	code := darwin.Get[uint32](reason, "typeCodeValue")
	if code == 0 {
		code = darwin.Get[uint32](reason, "enumCodeValue")
	}
	switch code {
	case 0x72657374, 0x73687574, 0x726c676f: // rest, shut, rlgo
		return true
	}
	return false
}

func notifySessionEnd() {
	app.mu.Lock()
	if !app.running || app.sessionEnded {
		app.mu.Unlock()
		return
	}
	app.sessionEnded = true
	f := app.onSessionEnd
	app.mu.Unlock()
	if f != nil {
		f()
	}
}

// stopApp makes [NSApp run] return. stop: takes effect after the next
// event, so an empty one is posted.
func stopApp() {
	if nsApp == 0 {
		return
	}
	darwin.Send(nsApp, "stop:", darwin.ID(0))
	ev := darwin.SendClass("NSEvent",
		"otherEventWithType:location:modifierFlags:timestamp:windowNumber:context:subtype:data1:data2:",
		uint64(15), darwin.CGPoint{}, uint64(0), float64(0), int64(0), darwin.ID(0), int16(0), int64(0), int64(0))
	darwin.Send(nsApp, "postEvent:atStart:", ev, true)
}

// Quit ends Main. It may be called from any goroutine.
func Quit() { Go(stopApp) }

// quitIfIdle ends Main once no window and no tray icon is left.
func quitIfIdle() {
	if !app.closing && len(openWindows) == 0 && apphost.Holds() == 0 {
		stopApp()
	}
}

// updateWindows rebuilds every window's view, as after a Go call.
func updateWindows() {
	for _, w := range openWindows {
		w.markDirty(dirtyGo)
		w.invalidate()
	}
}

// windowsFrontToBack returns the open windows, frontmost first.
func windowsFrontToBack() []*Window {
	var ws []*Window
	seen := map[*Window]bool{}
	list := darwin.Send(nsApp, "orderedWindows")
	for i := range darwin.Get[uint64](list, "count") {
		if w := openWindows[darwin.Send(list, "objectAtIndex:", i)]; w != nil && !seen[w] {
			ws, seen[w] = append(ws, w), true
		}
	}
	for _, w := range openWindows {
		if !seen[w] {
			ws = append(ws, w)
		}
	}
	return ws
}
