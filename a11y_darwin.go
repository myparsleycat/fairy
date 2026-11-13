package fairy

// Placeholder until the macOS backend has accessibility: it keeps the public API of
// the Windows backend and does nothing.

type a11yState struct{ focus uint64 }

func (w *Window) a11yAfterFrame() {}

// Announce asks screen readers to speak text.
func (w *Window) Announce(text string) {}

// Announce asks screen readers to speak text in the active window.
func Announce(text string) {}
