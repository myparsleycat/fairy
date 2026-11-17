package fairy

import (
	"errors"

	"github.com/myparsleycat/fairy/internal/edit"
	"github.com/myparsleycat/fairy/internal/win"
)

// MenuItem is an entry of a native popup menu.
type MenuItem struct {
	Label     string
	OnClick   func()
	Disabled  bool
	Checked   bool
	Separator bool       // draws a line; other fields are ignored
	Items     []MenuItem // makes the item a submenu
}

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
	if w.hl != nil {
		return errors.New("fairy: PopupMenu needs a window on screen, not a Headless one")
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

// EditCommand is an editing action on a text Input, as its keys and its
// built-in context menu run them.
type EditCommand uint8

const (
	EditCut EditCommand = iota + 1
	EditCopy
	EditPaste // through the Input's OnPaste handler first
	EditSelectAll
	EditUndo
	EditRedo
	EditDelete // removes the selection
)

// EditMenuLabels are the texts of an Input's built-in context menu. The
// spelling items show on a misspelled word.
type EditMenuLabels struct {
	Cut, Copy, Paste, SelectAll                string
	NoSuggestions, AddToDictionary, IgnoreWord string
}

var defaultEditMenu = EditMenuLabels{
	Cut: "Cut", Copy: "Copy", Paste: "Paste", SelectAll: "Select all",
	NoSuggestions: "No suggestions", AddToDictionary: "Add to dictionary", IgnoreWord: "Ignore",
}

var editMenuText = func() EditMenuLabels { return defaultEditMenu }

// SetEditMenuText sets the function that returns the labels of an Input's
// built-in context menu. It is called each time the menu opens; empty labels
// fall back to English. The kit package sets it to follow kit.Messages. Call
// it before the first window opens.
func SetEditMenuText(f func() EditMenuLabels) { editMenuText = f }

// EditInput runs cmd on the Input with key, as its keys and context menu do:
// Cut and Copy use the clipboard and need a selection in an input that is not
// a password, Paste goes through OnPaste and then inserts the clipboard text,
// and Cut, Paste, Undo, Redo and Delete need an editable input. It reports
// whether the input exists and cmd applied. Call it on the UI thread, such as
// from an app-made menu.
func (w *Window) EditInput(key string, cmd EditCommand) bool {
	for _, e := range w.focusables {
		if e.key == key && e.kind == kindInput {
			ok := w.runEdit(e, w.editOf(e), cmd)
			w.Update()
			return ok
		}
	}
	return false
}

// editAllowed reports whether cmd can apply to the input in its state.
func editAllowed(e *El, st *editState, cmd EditCommand, clipboardHasText bool) bool {
	p := e.input
	editable := p.onChange != nil
	switch cmd {
	case EditCut:
		return editable && st.HasSelection() && !p.password
	case EditCopy:
		return st.HasSelection() && !p.password
	case EditPaste:
		return editable && (clipboardHasText || p.onPaste != nil)
	case EditSelectAll:
		return st.Value != ""
	case EditUndo:
		return editable && len(st.Undo) > 0
	case EditRedo:
		return editable && len(st.Redo) > 0
	case EditDelete:
		return editable && st.HasSelection()
	}
	return false
}

func (w *Window) runEdit(e *El, st *editState, cmd EditCommand) bool {
	if !editAllowed(e, st, cmd, true) {
		return false
	}
	switch cmd {
	case EditCut:
		if !w.setClipboardText(st.SelectedText()) {
			return false
		}
		w.deleteSelection(e, st)
	case EditCopy:
		w.setClipboardText(st.SelectedText())
	case EditPaste:
		return w.paste(e, st)
	case EditSelectAll:
		st.Anchor, st.Caret = 0, len(st.Value)
		st.upstream = false
		w.invalidate()
	case EditUndo:
		w.undo(e, st)
	case EditRedo:
		w.redo(e, st)
	case EditDelete:
		w.deleteSelection(e, st)
	}
	return true
}

func (w *Window) deleteSelection(e *El, st *editState) {
	a, b := st.Selection()
	st.PushUndo(edit.Other)
	st.Value = st.Value[:a] + st.Value[b:]
	st.Caret, st.Anchor = a, a
	w.commitEdit(e, st, st.Value)
}

// inputMenu returns the built-in context menu of an Input opened at window
// point (x, y).
func (w *Window) inputMenu(e *El, x, y float32) []MenuItem {
	st := w.editOf(e)
	l := editMenuText()
	hasText := w.clipboardHasText()
	item := func(label, fallback, chord string, cmd EditCommand) MenuItem {
		if label == "" {
			label = fallback
		}
		return MenuItem{
			Label:    label + "\t" + chord,
			Disabled: !editAllowed(e, st, cmd, hasText),
			OnClick:  func() { w.runEdit(e, st, cmd) },
		}
	}
	return append(w.spellMenu(e, st, x, y, l),
		item(l.Cut, defaultEditMenu.Cut, "Ctrl+X", EditCut),
		item(l.Copy, defaultEditMenu.Copy, "Ctrl+C", EditCopy),
		item(l.Paste, defaultEditMenu.Paste, "Ctrl+V", EditPaste),
		MenuItem{Separator: true},
		item(l.SelectAll, defaultEditMenu.SelectAll, "Ctrl+A", EditSelectAll),
	)
}

// showInputMenu opens an Input's built-in context menu at window point (x, y).
func (w *Window) showInputMenu(e *El, x, y float32) {
	if w.hwnd == 0 || w.hl != nil {
		return
	}
	items := w.inputMenu(e, x, y)
	p := win.POINT{X: int32(x * w.scale), Y: int32(y * w.scale)}
	win.ClientToScreen(w.hwnd, &p)
	showMenu(w.hwnd, p.X, p.Y, items)
	w.Update()
}
