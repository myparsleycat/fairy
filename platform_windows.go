package fairy

import (
	"time"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/win"
)

// systemLocale is the user's locale as a BCP 47 tag, such as "en-US".
func systemLocale() string { return win.UserLocale() }

// doubleClickMs is the longest gap between the clicks of a double click.
func doubleClickMs() uint32 { return win.GetDoubleClickTime() }

// wheelScrollLines is how many lines a wheel notch scrolls, as the user set
// it; scroll.WheelStep reads the page setting.
func wheelScrollLines() uint32 {
	lines := uint32(3)
	win.SystemParametersInfo(win.SPI_GETWHEELSCROLLLINES, 0, unsafe.Pointer(&lines))
	return lines
}

// currentThreadID identifies the OS thread the caller runs on.
func currentThreadID() uint32 { return win.GetCurrentThreadId() }

// monoTicks returns a monotonic time for frame statistics.
func monoTicks() time.Duration { return win.Ticks() }

// waitForCompositor blocks until the next composition and reports whether
// it waited.
func waitForCompositor() bool { return win.DwmFlush() }
