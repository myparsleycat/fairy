package fairy

// Native menus. Placeholders until NSMenu backs them.

// PopupMenu shows items as a native menu at window point (x, y).
func (w *Window) PopupMenu(x, y float32, items []MenuItem) error { return errNoBackend }

// ShowMenu shows items as a native menu at screen point (x, y).
func ShowMenu(x, y int, items []MenuItem) error { return errNoBackend }

// popupMenu shows items as a native menu at window point (x, y) and waits
// until it closes.
func (w *Window) popupMenu(x, y float32, items []MenuItem) {}
