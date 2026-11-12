package fairy

import (
	"os"
	"strings"
	"time"
)

var processStart = time.Now()

// monoTicks returns a monotonic time for frame statistics.
func monoTicks() time.Duration { return time.Since(processStart) }

// doubleClickMs is the longest gap between the clicks of a double click.
func doubleClickMs() uint32 { return 500 }

// wheelScrollLines is how many lines a wheel notch scrolls; wheelPageScroll
// means a page.
func wheelScrollLines() uint32 { return 3 }

// currentThreadID identifies the OS thread the caller runs on.
func currentThreadID() uint32 { return 0 }

// systemLocale is the user's locale as a BCP 47 tag, such as "en-US".
func systemLocale() string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" && v != "C" && v != "POSIX" {
			v, _, _ = strings.Cut(v, ".")
			return strings.ReplaceAll(v, "_", "-")
		}
	}
	return "en-US"
}

// screenPixels is the pixel count of all displays, which sizes the image
// memory budget.
func screenPixels() int64 { return 2560 * 1600 }

// watchMemoryPressure trims image memory when the system runs low.
func watchMemoryPressure() {}

// SystemDark reports whether the system uses dark mode.
func SystemDark() bool { return false }
