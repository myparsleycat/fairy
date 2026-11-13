package fairy

// Placeholder until the macOS backend has window backdrops: it keeps the public API of
// the Windows backend and does nothing.

import (
	"errors"
)

var errBackdrop = errors.New("fairy: window backdrop not supported on this platform yet")

// BackdropSupported reports whether the system draws b behind a window.
func BackdropSupported(b Backdrop) bool { return b == BackdropNone }

func (w *Window) applyBackdrop(b Backdrop) {}
