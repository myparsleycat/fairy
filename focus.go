//go:build windows

package fairy

import (
	"math"
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

// focusGroupOf returns the nearest ancestor of e marked FocusGroup, or nil.
func focusGroupOf(e *El) *El {
	for p := e.parent; p != nil; p = p.parent {
		if p.focus.group {
			return p
		}
	}
	return nil
}

func groupRole(g *El) Role {
	if g.a11y != nil {
		return g.a11y.role
	}
	return RoleAuto
}

// groupItems returns the focusable elements of group g that Tab can reach, in order.
func (w *Window) groupItems(g *El) []*El {
	var items []*El
	for _, e := range w.focusScope() {
		if focusGroupOf(e) == g {
			items = append(items, e)
		}
	}
	return items
}

// tabStops returns scope with each focus group reduced to one item: the
// focused one, else the one marked FocusSelected, else the first.
func (w *Window) tabStops(scope []*El) []*El {
	var chosen map[*El]*El
	for _, e := range scope {
		g := focusGroupOf(e)
		if g == nil {
			continue
		}
		if chosen == nil {
			chosen = map[*El]*El{}
		}
		c, ok := chosen[g]
		switch {
		case !ok, e.id == w.focused:
			chosen[g] = e
		case c.id != w.focused && !c.focus.selected && e.focus.selected:
			chosen[g] = e
		}
	}
	if chosen == nil {
		return scope
	}
	stops := make([]*El, 0, len(scope))
	for _, e := range scope {
		if g := focusGroupOf(e); g == nil || chosen[g] == e {
			stops = append(stops, e)
		}
	}
	return stops
}

// groupKey moves focus inside the focused element's focus group: menus wrap,
// lists stop at their ends, and typing jumps to an item by name. shown reports whether the focus highlight was visible
// before this key arrived.
func (w *Window) groupKey(ev keyboard.KeyEvent, shown bool) bool {
	if ev.Mods&(keyboard.ModCtrl|keyboard.ModAlt|keyboard.ModSuper) != 0 {
		return false
	}
	e := w.focusedEl()
	if e == nil || e.kind == kindInput {
		return false
	}
	g := focusGroupOf(e)
	if g == nil {
		return false
	}
	typed := strings.TrimSpace(ev.Text)
	// Past Tab, Shift navigates nothing, but Shift+A still types "A".
	if ev.Mods&keyboard.ModShift != 0 && typed == "" {
		return false
	}
	items := w.groupItems(g)
	pos := -1
	for i, it := range items {
		if it == e {
			pos = i
			break
		}
	}
	if pos < 0 {
		return false
	}
	role := groupRole(g)
	vertical := verticalGroup(role)
	back := ev.Key == keyboard.KeyUp || !vertical && ev.Key == keyboard.KeyLeft
	fwd := ev.Key == keyboard.KeyDown || !vertical && ev.Key == keyboard.KeyRight
	page := (ev.Key == keyboard.KeyPageUp || ev.Key == keyboard.KeyPageDown) && pagedGroup(role)
	next := -1
	switch {
	case back || fwd || page || ev.Key == keyboard.KeyHome || ev.Key == keyboard.KeyEnd:
		w.typeahead.buf = ""
		n := len(items)
		switch {
		case ev.Key == keyboard.KeyHome:
			next = 0
		case ev.Key == keyboard.KeyEnd:
			next = n - 1
		case page:
			next = w.pageItem(items, pos, ev.Key == keyboard.KeyPageDown)
		case e.focus.highlight.isSet() && !shown:
			// A highlight the user cannot see yet appears where it is (a
			// popup's checked item, else the first) before arrows move it;
			// Up from nothing is the last.
			next = pos
			if back && !e.focus.selected {
				next = n - 1
			}
		case clampedGroup(role):
			if back {
				next = max(pos-1, 0)
			} else {
				next = min(pos+1, n-1)
			}
		case back:
			next = (pos + n - 1) % n
		default:
			next = (pos + 1) % n
		}
	case typed != "" && typeSelectGroup(role):
		next = w.typeahead.match(len(items), pos, func(j int) string {
			return items[j].a11yName(items[j].a11yRole())
		}, typed, time.Now())
	}
	if next < 0 {
		return false
	}
	it := items[next]
	r := it.a11yRole()
	follows := g.focus.follows || r == RoleRadio || r == RoleTab
	w.setFocusBy(it, true)
	w.scrollIntoView(it)
	if follows && it.onClick != nil {
		it.onClick()
	}
	return true
}

// pageItem returns the item Page Down (or Up) moves to from items[pos]: the
// farthest one still inside a viewport's height of it, at least one item on
// and stopping at the ends. Without a scroll container around the items, it
// is the last (or first).
func (w *Window) pageItem(items []*El, pos int, down bool) int {
	at := items[pos]
	page := float32(math.Inf(1))
	for p := at.parent; p != nil; p = p.parent {
		if p.style.ScrollY {
			page = p.h
			break
		}
	}
	within := func(j int) bool {
		n := items[j]
		if down {
			return n.absY+n.h-at.absY <= page+0.5
		}
		return at.absY+at.h-n.absY <= page+0.5
	}
	far := pos
	if down {
		for j := pos; j < len(items) && within(j); j++ {
			far = j
		}
		if far == pos {
			far = min(pos+1, len(items)-1)
		}
	} else {
		for j := pos; j >= 0 && within(j); j-- {
			far = j
		}
		if far == pos {
			far = max(pos-1, 0)
		}
	}
	return far
}

// syncHoverFocus gives focus to the FocusHighlight row under the pointer, so
// pointer and keys share one highlight. A row never takes focus from a text
// field, such as a search field above its results.
func (w *Window) syncHoverFocus(path []*El) {
	var row *El
	for _, e := range path {
		if e.focusable {
			if e.focus.highlight.isSet() {
				row = e
			}
			break
		}
	}
	if f := w.focusedEl(); f != nil && f.kind == kindInput {
		row = nil
	}
	switch {
	case row != nil && (row.id != w.focused || !w.hoverFocus):
		w.setFocusBy(row, false)
		w.hoverFocus = true
	case row == nil && w.hoverFocus:
		w.hoverFocus = false
		w.invalidate()
	}
}

// syncFollows moves focus in a FocusFollows group to its selected item when
// the app selected another one, as by a shortcut.
func (w *Window) syncFollows() {
	e := w.focusedEl()
	if e == nil || e.kind == kindInput || e.focus.selected {
		return
	}
	g := focusGroupOf(e)
	if g == nil || !g.focus.follows {
		return
	}
	if s := selectedItem(g, g); s != nil {
		w.focused, w.focusKey = s.id, s.key
	}
}

func selectedItem(g, e *El) *El {
	for _, c := range e.children {
		if c.style.Hidden {
			continue
		}
		if c.focusable && c.focus.selected && focusGroupOf(c) == g {
			return c
		}
		if s := selectedItem(g, c); s != nil {
			return s
		}
	}
	return nil
}

// syncReturnTo applies FocusReturn: when the focused element left the tree
// with an ancestor that set it, focus moves to that ancestor's target.
func (w *Window) syncReturnTo() {
	if w.focused == 0 || w.byID[w.focused] != nil {
		return
	}
	for p := w.prevByID[w.focused]; p != nil; p = p.parent {
		if p.focus.returnTo == "" || w.byID[p.id] != nil {
			continue
		}
		if e := returnTarget(w.byKey(p.focus.returnTo)); e != nil {
			w.focused, w.focusKey = e.id, e.key
			w.updateIME()
			return
		}
	}
}

// returnTarget is the element under e that takes the focus: the selected
// focusable if there is one, else the first focusable, skipping hidden and
// disabled subtrees.
func returnTarget(e *El) *El {
	var first *El
	var walk func(*El) *El
	walk = func(e *El) *El {
		if e == nil || e.disabled() || e.style.Hidden {
			return nil
		}
		if e.focusable || e.kind == kindInput {
			if e.focus.selected {
				return e
			}
			if first == nil {
				first = e
			}
		}
		for _, c := range e.children {
			if s := walk(c); s != nil {
				return s
			}
		}
		return nil
	}
	if s := walk(e); s != nil {
		return s
	}
	return first
}

func (w *Window) byKey(k string) *El {
	for _, e := range w.byID {
		if e.key == k {
			return e
		}
	}
	return nil
}

// highlighted reports whether e paints its FocusHighlight.
func (w *Window) highlighted(e *El) bool {
	return e.focus.highlight.isSet() && w.focused == e.id && (w.keyFocus || w.hoverFocus)
}

// applyFocusWant moves focus to the element Window.Focus asked for before it
// was built, once it is.
func (w *Window) applyFocusWant() {
	if w.focusWant == "" {
		return
	}
	var found *El
	var walk func(*El) bool
	walk = func(e *El) bool {
		if e.style.Hidden {
			return false
		}
		if e.focusable && e.key == w.focusWant {
			found = e
			return true
		}
		for _, c := range e.children {
			if walk(c) {
				return true
			}
		}
		return false
	}
	if !walk(w.root) {
		for _, l := range w.layers {
			if walk(l.root) {
				break
			}
		}
	}
	if found != nil {
		w.focusWant = ""
		w.setFocus(found)
	}
}

// focusReturn remembers where focus goes back to when a modal overlay or a
// popover closes.
type focusReturn struct {
	id      uint64 // the overlay's root, or the popover's anchor
	popover bool
	restore uint64
}

// syncFocusReturns returns focus to where it was before a modal overlay or a
// popover opened once it closes. Overlays that
// left are popped from the top; when the closing handler moved focus to a
// live element outside, that focus stays. focusBefore is the focus at the
// start of this build: an overlay that took focus in the build that opened
// it (AutoFocus) restores to it rather than to its own element.
func (w *Window) syncFocusReturns(focusBefore uint64) {
	open := func(r focusReturn) bool {
		for _, l := range w.layers {
			if r.popover && l.pop != nil && l.anchor == r.id || !r.popover && l.modal && l.root.id == r.id {
				return true
			}
		}
		return false
	}
	for i := len(w.returns) - 1; i >= 0; i-- {
		r := w.returns[i]
		if open(r) {
			continue
		}
		w.returns = append(w.returns[:i], w.returns[i+1:]...)
		// Focus comes back only when it was inside what closed: it was set
		// and its element is gone.
		if w.focused == 0 || w.byID[w.focused] != nil {
			continue
		}
		// A restore target that left too passes the turn to the overlay below.
		if e := firstFocusable(w.byID[r.restore]); e != nil {
			w.focused, w.focusKey = e.id, e.key
			w.updateIME()
		}
	}
	for _, l := range w.layers {
		r := focusReturn{id: l.root.id}
		switch {
		case l.pop != nil:
			r = focusReturn{id: l.anchor, popover: true, restore: l.anchor}
		case l.modal:
			r.restore = w.focused
			if w.focused != 0 && rootOf(w.byID[w.focused]) == l.root {
				r.restore = focusBefore
			}
		default:
			continue
		}
		found := false
		for _, o := range w.returns {
			if o.id == r.id && o.popover == r.popover {
				found = true
				break
			}
		}
		if !found {
			w.returns = append(w.returns, r)
		}
	}
}

func rootOf(e *El) *El {
	for e != nil && e.parent != nil {
		e = e.parent
	}
	return e
}

// firstFocusable returns e when it takes focus, else its first descendant that does.
func firstFocusable(e *El) *El {
	if e == nil || e.disabled() {
		return nil
	}
	if e.focusable || e.kind == kindInput {
		return e
	}
	for _, c := range e.children {
		if f := firstFocusable(c); f != nil {
			return f
		}
	}
	return nil
}
