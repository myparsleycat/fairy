package fairy

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/myparsleycat/fairy/keyboard"
)

type focusProps struct {
	group     bool
	selected  bool
	follows   bool
	highlight Color
	returnTo  string
}

// FocusGroup makes arrow keys move focus among the focusable elements inside
// this one, and Tab visit only one of them: the focused item, else the one
// marked FocusSelected, else the first. Groups with RoleMenu, RoleList or
// RoleTree move on Up and Down only; others (tabs, radios, segments,
// toolbars, menu bars) on Left and Right as well. Home and End jump to the ends. Lists
// and trees stop at their ends; menus, tab lists and other groups wrap. Page
// Up and Page Down move by the items their scroll container shows in menus
// and lists. In menus, lists, trees and tab lists, typing jumps to the item
// whose accessible name starts with the typed text.
func (e *El) FocusGroup() *El { e.focus.group = true; return e }

// FocusSelected marks the item of a FocusGroup that Tab enters the group on
// and that a highlight appears on first.
func (e *El) FocusSelected(selected bool) *El { e.focus.selected = selected; return e }

// FocusFollows makes the arrow keys and type-select of a FocusGroup also
// click the item they focus, so selection follows focus. Items with
// RoleRadio or RoleTab always do. When the app selects another item, focus
// in the group moves to it.
func (e *El) FocusFollows() *El { e.focus.follows = true; return e }

// FocusHighlight fills a menu or list row with c while it has focus from the
// keyboard or the pointer. Moving the pointer over the row focuses it, so
// keys and pointer share one highlight. Use it instead of HoverFill. The row
// must be Focusable.
func (e *El) FocusHighlight(c Color) *El { e.focus.highlight = c; return e }

// FocusReturn moves focus to the element with key k, or the first focusable
// element inside it (preferring one marked FocusSelected), when this element
// leaves the tree while the focus is inside it, as when a side pane closes.
func (e *El) FocusReturn(k string) *El { e.focus.returnTo = k; return e }

func verticalGroup(r Role) bool { return r == RoleMenu || r == RoleList || r == RoleTree }

func typeSelectGroup(r Role) bool { return verticalGroup(r) || r == RoleTabList }

// clampedGroup reports whether a group stops at its ends, as a table or an
// outline does; other groups wrap.
func clampedGroup(r Role) bool { return r == RoleList || r == RoleTree }

func pagedGroup(r Role) bool { return r == RoleMenu || r == RoleList }

// navigates reports whether ev is a navigation key (Tab, an arrow, Home, End,
// Page Up or Page Down with no modifier but Shift), which shows the focus
// highlight from then on.
func navigates(ev keyboard.KeyEvent) bool {
	if ev.Mods&^keyboard.ModShift != 0 {
		return false
	}
	switch ev.Key {
	case keyboard.KeyTab, keyboard.KeyUp, keyboard.KeyDown, keyboard.KeyLeft, keyboard.KeyRight, keyboard.KeyHome, keyboard.KeyEnd, keyboard.KeyPageUp, keyboard.KeyPageDown:
		return true
	}
	return false
}

// typeSelectPause is how long type-select waits for the next key before it
// starts a new prefix.
const typeSelectPause = time.Second

type typeahead struct {
	buf string
	at  time.Time
}

// match returns the item typing moves to among n items whose names label
// gives, from item pos: the first whose name starts with everything typed
// since the last pause, while the same letter again cycles through the items
// starting with it. It returns -1 when none matches.
func (t *typeahead) match(n, pos int, label func(int) string, typed string, now time.Time) int {
	if now.Sub(t.at) > typeSelectPause {
		t.buf = ""
	}
	t.at = now
	t.buf += strings.ToLower(typed)
	first, size := utf8.DecodeRuneInString(t.buf)
	if size == 0 {
		return -1
	}
	cycle := strings.Trim(t.buf, string(first)) == ""
	prefix := t.buf
	if cycle {
		prefix = string(first)
	}
	for k := range n {
		j := k
		if cycle {
			j = (pos + 1 + k) % n
		}
		if strings.HasPrefix(strings.ToLower(label(j)), prefix) {
			return j
		}
	}
	return -1
}
