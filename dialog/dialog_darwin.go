package dialog

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/internal/apphost"
	"github.com/myparsleycat/fairy/internal/darwin"
)

// NSModalResponse values.
const (
	modalOK          = 1
	modalStop        = -1000
	alertFirstButton = 1000
)

// loadUTTypes loads the framework of UTType, which allowedContentTypes takes.
var loadUTTypes = sync.OnceFunc(func() {
	_, _ = purego.Dlopen("/System/Library/Frameworks/UniformTypeIdentifiers.framework/UniformTypeIdentifiers",
		purego.RTLD_NOW|purego.RTLD_GLOBAL)
})

// OpenFiles asks for one file, or several with Multiple. It blocks until the
// dialog closes and may be called from any goroutine while fairy.Main runs.
// owner may be nil. The panel shows Title as its message and lists the files
// any of the Filters matches; a filter pattern of "*" or "*.*" allows every
// file.
func OpenFiles(owner *fairy.Window, d FileOptions) ([]string, error) {
	return runFileDialog(owner, d, false, false)
}

// OpenFolders asks for one folder, or several with Multiple.
func OpenFolders(owner *fairy.Window, d FileOptions) ([]string, error) {
	return runFileDialog(owner, d, false, true)
}

// SaveFile asks for a path to save to, confirming before overwriting.
func SaveFile(owner *fairy.Window, d FileOptions) (string, error) {
	paths, err := runFileDialog(owner, d, true, false)
	if err != nil {
		return "", err
	}
	return paths[0], nil
}

// pendingModals are the modal dialogs other goroutines wait for that have
// not been shown yet. Main ending answers them with false.
var pendingModals = map[chan bool]bool{}

func init() {
	apphost.OnShutdown(func() {
		for done := range pendingModals {
			delete(pendingModals, done)
			done <- false
		}
	})
}

// runModal runs a panel or alert modally and returns its response.
// fairy.Quit stops the app with [NSApp stop:], which inside a modal session
// ends only the session, so the quit is asked for again.
func runModal(panel darwin.ID) int64 {
	r := darwin.Get[int64](panel, "runModal")
	if r == modalStop {
		fairy.Quit()
	}
	return r
}

// onUIModal runs f, which shows a modal panel, on the UI thread and waits. It
// reports false when no UI loop ran f. From another goroutine, f runs from a
// run loop timer rather than inside the call apphost.Do dispatches: the main
// dispatch queue runs one call at a time, so fairy.Go calls and window
// updates would wait until the panel closes.
func onUIModal(f func()) bool {
	if apphost.OnUIThread() {
		darwin.WithPool(f)
		return true
	}
	done := make(chan bool, 1)
	if !apphost.Do(func() {
		pendingModals[done] = true
		darwin.Perform(func() {
			if !pendingModals[done] {
				return
			}
			delete(pendingModals, done)
			f()
			done <- true
		})
	}) {
		return false
	}
	return <-done
}

func runFileDialog(owner *fairy.Window, d FileOptions, save, folders bool) (paths []string, err error) {
	if !onUIModal(func() { paths = showFileDialog(d, save, folders) }) {
		return nil, apphost.ErrUIUnavailable
	}
	if len(paths) == 0 {
		return nil, ErrCanceled
	}
	return paths, nil
}

func showFileDialog(d FileOptions, save, folders bool) []string {
	var panel darwin.ID
	if save {
		panel = darwin.SendClass("NSSavePanel", "savePanel")
		darwin.Send(panel, "setCanCreateDirectories:", true)
		darwin.Send(panel, "setExtensionHidden:", false)
	} else {
		panel = darwin.SendClass("NSOpenPanel", "openPanel")
		darwin.Send(panel, "setCanChooseFiles:", !folders)
		darwin.Send(panel, "setCanChooseDirectories:", folders)
		darwin.Send(panel, "setCanCreateDirectories:", folders)
		darwin.Send(panel, "setAllowsMultipleSelection:", d.Multiple)
		darwin.Send(panel, "setResolvesAliases:", true)
	}
	if d.Title != "" {
		// Panels have no visible title; the message shows above the files.
		darwin.Send(panel, "setTitle:", darwin.String(d.Title))
		darwin.Send(panel, "setMessage:", darwin.String(d.Title))
	}
	if save && d.FileName != "" {
		darwin.Send(panel, "setNameFieldStringValue:", darwin.String(d.FileName))
	}
	if !folders {
		if types := contentTypes(d, save); types != 0 {
			darwin.Send(panel, "setAllowedContentTypes:", types)
			// A save dialog appends the first type's extension to a name
			// typed without one, and asks before using another one.
			darwin.Send(panel, "setAllowsOtherFileTypes:", save)
		}
	}
	folder := dialogFolder(d.Folder)
	if folder == "" {
		folder = rememberedFolder(d.Role)
	}
	if folder != "" {
		darwin.Send(panel, "setDirectoryURL:", fileURL(folder, true))
	}
	activate()
	if runModal(panel) != modalOK {
		return nil
	}
	if d.Role != "" {
		if dir := darwin.Send(panel, "directoryURL"); dir != 0 {
			rememberFolder(d.Role, darwin.GoString(darwin.Send(dir, "path")))
		}
	}
	if save {
		if p := urlPath(darwin.Send(panel, "URL")); p != "" {
			return []string{p}
		}
		return nil
	}
	urls := darwin.Send(panel, "URLs")
	n := darwin.Get[uint64](urls, "count")
	paths := make([]string, 0, n)
	for i := range n {
		if p := urlPath(darwin.Send(urls, "objectAtIndex:", i)); p != "" {
			paths = append(paths, p)
		}
	}
	return paths
}

// contentTypes returns an NSArray of the UTTypes the filters name, with
// DefaultExt's first in a save dialog, or 0 when every file is allowed.
func contentTypes(d FileOptions, save bool) darwin.ID {
	exts, all := filterExtensions(d.Filters)
	if def := strings.ToLower(strings.TrimPrefix(d.DefaultExt, ".")); save && def != "" {
		// Other types stay allowed through allowsOtherFileTypes.
		exts, all = append([]string{def}, exts...), false
	}
	if all || len(exts) == 0 {
		return 0
	}
	loadUTTypes()
	arr := darwin.SendClass("NSMutableArray", "array")
	seen := map[string]bool{}
	for _, ext := range exts {
		if seen[ext] {
			continue
		}
		seen[ext] = true
		if t := darwin.SendClass("UTType", "typeWithFilenameExtension:", darwin.String(ext)); t != 0 {
			darwin.Send(arr, "addObject:", t)
		}
	}
	if darwin.Get[uint64](arr, "count") == 0 {
		return 0
	}
	return arr
}

// filterExtensions returns the extensions of filter patterns such as "*.png",
// and whether a pattern allows every file: "*", "*.*" or one that names no
// plain extension.
func filterExtensions(filters []Filter) (exts []string, all bool) {
	for _, f := range filters {
		for _, p := range f.Patterns {
			p = strings.TrimSpace(p)
			ext, ok := strings.CutPrefix(p, "*.")
			if p == "*" || !ok || ext == "*" || ext == "" || strings.ContainsAny(ext, "*?[") {
				all = true
				continue
			}
			exts = append(exts, strings.ToLower(ext))
		}
	}
	return exts, all
}

func fileURL(path string, dir bool) darwin.ID {
	return darwin.SendClass("NSURL", "fileURLWithPath:isDirectory:", darwin.String(path), dir)
}

func urlPath(url darwin.ID) string {
	if url == 0 {
		return ""
	}
	return darwin.GoString(darwin.Send(url, "path"))
}

// activate brings the app forward so a modal panel or alert gets the keys.
func activate() {
	darwin.Send(darwin.SendClass("NSApplication", "sharedApplication"), "activateIgnoringOtherApps:", true)
}

// dialogFolder returns path if it is a folder, else its parent if that is one.
func dialogFolder(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		return path
	}
	if parent := filepath.Dir(path); parent != path {
		if fi, err := os.Stat(parent); err == nil && fi.IsDir() {
			return parent
		}
	}
	return ""
}

// roleKey is the user defaults key holding a role's last folder.
func roleKey(role string) string { return "fairy.dialog.folder." + role }

// rememberedFolder is the last folder a dialog of role showed, if it still
// exists. Dialogs without a role use the one the panels themselves remember.
func rememberedFolder(role string) string {
	if role == "" {
		return ""
	}
	defaults := darwin.SendClass("NSUserDefaults", "standardUserDefaults")
	dir := darwin.GoString(darwin.Send(defaults, "stringForKey:", darwin.String(roleKey(role))))
	if dir == "" {
		return ""
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return ""
	}
	return dir
}

func rememberFolder(role, dir string) {
	if role == "" || dir == "" {
		return
	}
	defaults := darwin.SendClass("NSUserDefaults", "standardUserDefaults")
	darwin.Send(defaults, "setObject:forKey:", darwin.String(dir), darwin.String(roleKey(role)))
}

// Message shows a native alert and returns the button pressed. The title is
// the alert's bold message text and message its informative text below.
// Closing the alert with Escape returns ButtonCancel (ButtonOK for
// ButtonsOK, ButtonNo for ButtonsYesNo). It may be called from any goroutine
// while fairy.Main runs, and on the main goroutine before it does. The
// buttons are labeled in English.
func Message(owner *fairy.Window, title, message string, kind MessageKind, buttons Buttons) Button {
	labels := [...][]Button{
		ButtonsOK:          {ButtonOK},
		ButtonsOKCancel:    {ButtonOK, ButtonCancel},
		ButtonsYesNo:       {ButtonYes, ButtonNo},
		ButtonsYesNoCancel: {ButtonYes, ButtonNo, ButtonCancel},
	}[buttons]
	escape := [...]Button{ButtonOK, ButtonCancel, ButtonNo, ButtonCancel}[buttons]
	r := escape
	show := func() { r = showAlert(title, message, kind, labels, escape) }
	switch {
	case apphost.Running():
		onUIModal(show)
	case darwin.Get[bool](darwin.ID(darwin.Cls("NSThread")), "isMainThread"):
		darwin.WithPool(show)
	}
	return r
}

// buttonTitles are the labels of the alert buttons.
var buttonTitles = [...]string{ButtonOK: "OK", ButtonCancel: "Cancel", ButtonYes: "Yes", ButtonNo: "No"}

// alertStyles are the NSAlertStyle of each MessageKind: informational (1),
// warning (0) or critical (2).
var alertStyles = [...]uint64{MessageInfo: 1, MessageWarning: 0, MessageError: 2, MessageQuestion: 1}

func showAlert(title, message string, kind MessageKind, buttons []Button, escape Button) Button {
	app := darwin.SendClass("NSApplication", "sharedApplication")
	if darwin.Get[int64](app, "activationPolicy") != 0 {
		// Before fairy.Main, a process without a bundle shows no windows
		// in front until it is a regular app.
		darwin.Send(app, "setActivationPolicy:", int64(0))
	}
	alert := darwin.New("NSAlert")
	defer darwin.Release(alert)
	darwin.Send(alert, "setAlertStyle:", alertStyles[kind])
	darwin.Send(alert, "setMessageText:", darwin.String(title))
	darwin.Send(alert, "setInformativeText:", darwin.String(message))
	for _, b := range buttons {
		btn := darwin.Send(alert, "addButtonWithTitle:", darwin.String(buttonTitles[b]))
		// The first button takes Return; Escape presses the button it maps
		// to, unless that is the first one.
		if b == escape && b != buttons[0] {
			darwin.Send(btn, "setKeyEquivalent:", darwin.String("\x1b"))
		}
	}
	if escape == buttons[0] {
		// A button has one key equivalent, and the first one's is Return,
		// so Escape ends the alert through a key monitor instead.
		stop := closeOnEscape(darwin.Send(alert, "window"))
		defer stop()
	}
	activate()
	i := runModal(alert) - alertFirstButton
	if i < 0 || i >= int64(len(buttons)) {
		return escape
	}
	return buttons[i]
}

// closeOnEscape makes Escape end the modal session of window, which then
// returns NSModalResponseAbort, until the returned function is called.
func closeOnEscape(window darwin.ID) func() {
	const (
		keyEscape    = 53
		modifierKeys = 0xf << 17 // Shift, Control, Option, Command
	)
	handler := objc.NewBlock(func(_ objc.Block, ev darwin.ID) darwin.ID {
		app := darwin.SendClass("NSApplication", "sharedApplication")
		if darwin.Get[uint16](ev, "keyCode") == keyEscape && darwin.Send(app, "modalWindow") == window &&
			darwin.Get[uint64](ev, "modifierFlags")&modifierKeys == 0 {
			darwin.Send(app, "abortModal")
			return 0
		}
		return ev
	})
	monitor := darwin.SendClass("NSEvent", "addLocalMonitorForEventsMatchingMask:handler:", uint64(1<<10), handler) // NSEventMaskKeyDown
	darwin.Retain(monitor)
	return func() {
		darwin.SendClass("NSEvent", "removeMonitor:", monitor)
		darwin.Release(monitor)
		handler.Release()
	}
}
