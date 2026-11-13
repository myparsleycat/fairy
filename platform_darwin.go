package fairy

import (
	"strings"
	"sync/atomic"
	"time"

	"github.com/ebitengine/purego/objc"

	"github.com/myparsleycat/fairy/internal/darwin"
	"github.com/myparsleycat/fairy/keyboard"
)

// nonTypingMods are the modifiers that keep a key from typing into an
// Input. Option types characters on macOS.
const nonTypingMods = keyboard.ModCtrl | keyboard.ModSuper

var processStart = time.Now()

// monoTicks returns a monotonic time for frame statistics.
func monoTicks() time.Duration { return time.Since(processStart) }

// doubleClickMs is the longest gap between the clicks of a double click.
func doubleClickMs() uint32 {
	s := darwin.Get[float64](darwin.ID(darwin.Cls("NSEvent")), "doubleClickInterval")
	return uint32(s * 1000)
}

// wheelScrollLines is how many lines a wheel notch scrolls; wheelPageScroll
// means a page.
func wheelScrollLines() uint32 { return 3 }

var pthreadThreadID func(thread uintptr, id *uint64) int32

func init() {
	darwin.Func(&pthreadThreadID, darwin.System, "pthread_threadid_np")
	loadReducedMotion()
}

// currentThreadID identifies the OS thread the caller runs on.
func currentThreadID() uint32 {
	var id uint64
	pthreadThreadID(0, &id)
	return uint32(id)
}

// systemLocale is the user's locale as a BCP 47 tag, such as "en-US".
func systemLocale() string {
	var tag string
	darwin.WithPool(func() {
		loc := darwin.SendClass("NSLocale", "currentLocale")
		tag = darwin.GoString(darwin.Send(loc, "localeIdentifier"))
	})
	tag, _, _ = strings.Cut(tag, "@")
	if tag == "" {
		return "en-US"
	}
	return strings.ReplaceAll(tag, "_", "-")
}

// screenPixels is the pixel count of all displays, which sizes the image
// memory budget.
func screenPixels() int64 {
	var n int64
	darwin.WithPool(func() {
		screens := darwin.SendClass("NSScreen", "screens")
		for i := range darwin.Get[uint64](screens, "count") {
			s := darwin.Send(screens, "objectAtIndex:", i)
			f := darwin.Get[darwin.CGRect](s, "frame")
			scale := darwin.Get[float64](s, "backingScaleFactor")
			n += int64(f.Size.W * scale * f.Size.H * scale)
		}
	})
	if n == 0 {
		n = 2560 * 1600
	}
	return n
}

var systemDark atomic.Bool

func init() { loadSystemDark() }

// SystemDark reports whether macOS is set to the dark appearance. When the
// user changes it, every window builds its view again, so a View that
// follows the system theme can read it on each build.
func SystemDark() bool { return systemDark.Load() }

func loadSystemDark() {
	darwin.WithPool(func() {
		v := darwin.Send(darwin.SendClass("NSUserDefaults", "standardUserDefaults"), "stringForKey:",
			darwin.String("AppleInterfaceStyle"))
		systemDark.Store(darwin.GoString(v) == "Dark")
	})
}

func loadReducedMotion() {
	reducedMotion.Store(darwin.Get[bool](darwin.SendClass("NSWorkspace", "sharedWorkspace"),
		"accessibilityDisplayShouldReduceMotion"))
}

// watchSystemSettings rebuilds every window when the appearance or the
// reduce motion setting changes.
func watchSystemSettings() {
	c, err := objc.RegisterClass("FairySettingsObserver", darwin.Cls("NSObject"), nil, nil, []objc.MethodDef{
		{Cmd: darwin.Sel("themeChanged:"), Fn: func(darwin.ID, darwin.SEL, darwin.ID) {
			loadSystemDark()
			settingsChanged()
		}},
		{Cmd: darwin.Sel("motionChanged:"), Fn: func(darwin.ID, darwin.SEL, darwin.ID) {
			loadReducedMotion()
			settingsChanged()
		}},
	})
	if err != nil {
		panic(err)
	}
	obs := darwin.ID(c).Send(darwin.Sel("new"))
	darwin.Send(darwin.SendClass("NSDistributedNotificationCenter", "defaultCenter"),
		"addObserver:selector:name:object:", obs, darwin.Sel("themeChanged:"),
		darwin.String("AppleInterfaceThemeChangedNotification"), darwin.ID(0))
	darwin.Send(darwin.Send(darwin.SendClass("NSWorkspace", "sharedWorkspace"), "notificationCenter"),
		"addObserver:selector:name:object:", obs, darwin.Sel("motionChanged:"),
		darwin.SymbolID(darwin.AppKit, "NSWorkspaceAccessibilityDisplayOptionsDidChangeNotification"), darwin.ID(0))
}

// settingsChanged builds every window again after a system setting changed.
func settingsChanged() {
	InvalidateMemos()
	for _, w := range openWindows {
		w.Update()
	}
}
