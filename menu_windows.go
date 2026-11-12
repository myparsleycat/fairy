package fairy

import (
	"errors"

	"github.com/myparsleycat/fairy/internal/win"
)

func buildMenu(items []MenuItem, actions *[]func()) win.HANDLE {
	m := win.CreatePopupMenu()
	for _, it := range items {
		switch {
		case it.Separator:
			win.AppendMenu(m, win.MF_SEPARATOR, 0, "")
		case len(it.Items) > 0:
			sub := buildMenu(it.Items, actions)
			flags := uint32(win.MF_POPUP)
			if it.Disabled {
				flags |= win.MF_GRAYED
			}
			win.AppendMenu(m, flags, uintptr(sub), it.Label)
		default:
			*actions = append(*actions, it.OnClick)
			flags := uint32(win.MF_STRING)
			if it.Disabled {
				flags |= win.MF_GRAYED
			}
			if it.Checked {
				flags |= win.MF_CHECKED
			}
			win.AppendMenu(m, flags, uintptr(len(*actions)), it.Label)
		}
	}
	return m
}

// showMenu runs a popup menu at screen point (x, y) owned by owner and calls
// the chosen item's OnClick after the menu closes.
func showMenu(owner win.HWND, x, y int32, items []MenuItem) {
	if len(items) == 0 {
		return
	}
	var actions []func()
	m := buildMenu(items, &actions)
	cmd := win.TrackPopupMenu(m, win.TPM_RETURNCMD|win.TPM_RIGHTBUTTON|win.TPM_NONOTIFY, x, y, owner)
	win.DestroyMenu(m) // destroys submenus too
	if cmd > 0 && int(cmd) <= len(actions) && actions[cmd-1] != nil {
		actions[cmd-1]()
	}
}

// PopupMenu shows a native menu at (x, y) in the window's logical pixels and
// runs the chosen item. It returns after the menu closes. Call it on the UI
// thread, typically from OnRightClick with the event's position.
func (w *Window) PopupMenu(x, y float32, items []MenuItem) error {
	if !onUIThread() {
		return errors.New("fairy: PopupMenu must run on the UI thread")
	}
	p := win.POINT{X: int32(x * w.scale), Y: int32(y * w.scale)}
	win.ClientToScreen(w.hwnd, &p)
	w.cancelPress()
	showMenu(w.hwnd, p.X, p.Y, items)
	w.Update()
	return nil
}

// ShowMenu shows a native menu at screen point (x, y) in physical pixels, runs
// the chosen item and then updates every window. It returns after the menu
// closes. Use it for menus not tied to a window, such as a tray icon's; a
// window's own menus use Window.PopupMenu. Call it on the UI thread.
func ShowMenu(x, y int, items []MenuItem) error {
	if !onUIThread() {
		return errors.New("fairy: ShowMenu must run on the UI thread")
	}
	showMenu(app.hwnd, int32(x), int32(y), items)
	updateWindows()
	return nil
}
