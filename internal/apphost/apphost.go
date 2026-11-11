// Package apphost connects the packages that use the app window (tray,
// shortcut, dialog) to package fairy, which owns it and fills in the hooks
// below when it is initialized.
//
// Handlers, shutdown functions and holds are only touched on the UI thread,
// or from package init before Main starts, so they need no lock.
package apphost

import (
	"errors"

	"github.com/myparsleycat/fairy/internal/win"
)

// MsgTray is the callback message of tray icons.
const MsgTray = win.WM_APP + 11

// ErrUIUnavailable is returned when no UI loop runs a call.
var ErrUIUnavailable = errors.New("fairy: UI thread unavailable")

var (
	// HWND returns the app window, or 0 outside Main.
	HWND = func() win.HWND { return 0 }
	// OnUIThread reports whether the caller runs on the UI thread.
	OnUIThread = func() bool { return false }
	// Running reports whether Main runs.
	Running = func() bool { return false }
	// Do runs f on the UI thread and waits. It reports false when no UI loop
	// ran f.
	Do = func(f func()) bool { return false }
	// Go runs f on the UI thread without waiting, then updates every window.
	Go = func(f func()) {}
	// QuitIfIdle ends Main once no window and no hold is left.
	QuitIfIdle = func() {}
	// UpdateWindows rebuilds every window's view, as after a Go call. Call it
	// after app code that runs outside Go, Do and window events.
	UpdateWindows = func() {}
)

var (
	handlers = map[uint32]func(wp, lp uintptr) uintptr{}
	shutdown []func()
	holds    int
)

// Handle routes msg sent to the app window to h. A msg of 0 is ignored.
func Handle(msg uint32, h func(wp, lp uintptr) uintptr) {
	if msg != 0 {
		handlers[msg] = h
	}
}

func Handler(msg uint32) func(wp, lp uintptr) uintptr { return handlers[msg] }

// OnShutdown adds f to the functions Main runs after closing its windows and
// before destroying the app window.
func OnShutdown(f func()) { shutdown = append(shutdown, f) }

func Shutdown() {
	for _, f := range shutdown {
		f()
	}
}

// Hold keeps Main running after the last window closes, until Release.
func Hold() { holds++ }

// Release ends a Hold, and ends Main if nothing else keeps it running.
func Release() {
	holds--
	QuitIfIdle()
}

func Holds() int { return holds }
