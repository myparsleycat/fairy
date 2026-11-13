package fairy

import (
	"strings"
	"time"

	"github.com/myparsleycat/fairy/internal/darwin"
)

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
	reducedMotion.Store(darwin.Get[bool](darwin.SendClass("NSWorkspace", "sharedWorkspace"),
		"accessibilityDisplayShouldReduceMotion"))
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

// watchMemoryPressure trims image memory when the system runs low.
func watchMemoryPressure() {}

// SystemDark reports whether the system uses dark mode.
func SystemDark() bool {
	var dark bool
	darwin.WithPool(func() {
		v := darwin.Send(darwin.SendClass("NSUserDefaults", "standardUserDefaults"), "stringForKey:",
			darwin.String("AppleInterfaceStyle"))
		dark = darwin.GoString(v) == "Dark"
	})
	return dark
}
