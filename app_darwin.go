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
	running bool // the NSApplication run loop runs
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

// newMainMenu makes the menu bar: the app menu, with Hide and Quit, and the
// Window menu, which AppKit fills with the open windows. Other shortcuts,
// such as ⌘C, reach the focused window's key handlers.
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
			// process, as Quit does.
			Cmd: darwin.Sel("applicationShouldTerminate:"),
			Fn: func(self darwin.ID, _ darwin.SEL, sender darwin.ID) uint64 {
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
func wakeAppLoop(quiet bool) bool {
	darwin.Async(func() {
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
	app.done = make(chan struct{})
	app.thread.Store(currentThreadID())
	queued := len(app.calls) != 0
	app.mu.Unlock()
	if queued {
		wakeAppLoop(false)
	}
	defer func() {
		app.mu.Lock()
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
	}()
	if err := start(); err != nil {
		return err
	}
	if len(openWindows) == 0 && apphost.Holds() == 0 {
		return nil
	}
	darwin.Send(nsApp, "activateIgnoringOtherApps:", true)
	darwin.Send(nsApp, "run")
	return nil
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

// errNoBackend is returned by features the macOS backend does not have yet.
var errNoBackend = errors.New("fairy: not implemented on macOS yet")
