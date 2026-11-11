package fairy

import (
	"testing"
	"time"

	"github.com/myparsleycat/fairy/internal/edit"
	"github.com/myparsleycat/fairy/keyboard"
)

// Closing a dialog that took focus when it opened gives focus back to the
// element that had it before, including when two dialogs close at once.
func TestModalFocusReturn(t *testing.T) {
	var open, nested bool
	w := newListWindow(t, nil)
	w.opts.View = func(cx *Cx) *El {
		if open {
			cx.Overlay(Box(Box().Size(20, 20).Focusable().Key("ok").AutoFocus()).Key("dialog"), nil)
		}
		if nested {
			cx.Overlay(Box(Box().Size(20, 20).Focusable().Key("inner").AutoFocus()).Key("nested"), nil)
		}
		return Col(Box().Size(20, 20).Focusable().Key("opener"), Box().Size(20, 20).Focusable().Key("other"))
	}
	frame := func() {
		w.build()
		w.placeAll()
	}
	frame()
	w.Focus("opener")

	open = true
	frame()
	if w.focusKey != "ok" {
		t.Fatalf("focus after opening = %q, want ok", w.focusKey)
	}
	open = false
	frame()
	if w.focusKey != "opener" {
		t.Fatalf("focus after closing = %q, want opener", w.focusKey)
	}

	open = true
	frame()
	nested = true
	frame()
	if w.focusKey != "inner" {
		t.Fatalf("focus in nested dialog = %q, want inner", w.focusKey)
	}
	open, nested = false, false
	frame()
	if w.focusKey != "opener" {
		t.Fatalf("focus after closing both = %q, want opener", w.focusKey)
	}

	// A closing handler that moves focus elsewhere wins.
	open = true
	frame()
	open = false
	w.Focus("other")
	frame()
	if w.focusKey != "other" {
		t.Fatalf("focus moved by the handler = %q, want other", w.focusKey)
	}
}

// A dialog's field asks for focus before its default action, which also asks
// for it: the first one wins.
func TestFirstAutoFocusWins(t *testing.T) {
	open := false
	w := newListWindow(t, nil)
	w.opts.View = func(cx *Cx) *El {
		if open {
			cx.Overlay(Col(
				Box().Size(20, 20).Focusable().Key("field").AutoFocus(),
				Box().Size(20, 20).Focusable().Key("action").AutoFocus(),
			), nil)
		}
		return Box().Size(20, 20).Focusable().Key("opener")
	}
	w.build()
	w.placeAll()
	open = true
	w.build()
	if w.focusKey != "field" {
		t.Fatalf("focus = %q, want field", w.focusKey)
	}
}

// Arrow keys rove inside a focus group: menus wrap, lists stop at their ends,
// typing jumps to a row by name, and Tab enters a group only on its selected
// item.
func TestFocusGroupKeys(t *testing.T) {
	var role Role = RoleMenu
	var clicked string
	w := newListWindow(t, nil)
	w.opts.View = func(cx *Cx) *El {
		item := func(label string, selected bool) *El {
			return Text(label).H(20).Focusable().FocusSelected(selected).Key(label).
				OnClick(func() { clicked = label })
		}
		return Col(
			Box().Size(20, 20).Focusable().Key("before"),
			Col(item("apple", false), item("banana", true), item("blueberry", false)).Role(role).FocusGroup(),
			Box().Size(20, 20).Focusable().Key("after"),
		)
	}
	none := keyboard.Mods(0)
	w.scriptMods = &none
	frame := func() {
		w.build()
		w.placeAll()
	}
	key := func(k keyboard.Key, text string) {
		t.Helper()
		w.onKeyDown(k, false, text)
		frame()
	}
	frame()
	w.Focus("before")

	key(keyboard.KeyTab, "")
	if w.focusKey != "banana" {
		t.Fatalf("Tab into the group = %q, want the selected item banana", w.focusKey)
	}
	key(keyboard.KeyTab, "")
	if w.focusKey != "after" {
		t.Fatalf("Tab out of the group = %q, want after", w.focusKey)
	}
	w.Focus("blueberry")
	key(keyboard.KeyDown, "")
	if w.focusKey != "apple" {
		t.Fatalf("Down past the end of a menu = %q, want apple (wraps)", w.focusKey)
	}
	key(keyboard.KeyEnd, "")
	if w.focusKey != "blueberry" {
		t.Fatalf("End = %q, want blueberry", w.focusKey)
	}
	key(keyboard.KeyRight, "")
	if w.focusKey != "blueberry" {
		t.Fatalf("Right in a vertical group = %q, want no move", w.focusKey)
	}
	key(keyboard.KeyA, "a")
	if w.focusKey != "apple" {
		t.Fatalf(`typing "a" = %q, want apple`, w.focusKey)
	}
	w.typeahead.at = w.typeahead.at.Add(-time.Hour) // a pause starts a new prefix
	key(keyboard.Key('B'), "b")
	key(keyboard.Key('B'), "b")
	if w.focusKey != "blueberry" {
		t.Fatalf(`"b" twice = %q, want blueberry (a repeated letter cycles)`, w.focusKey)
	}
	if clicked != "" {
		t.Fatalf("moving in a menu clicked %q", clicked)
	}
	key(keyboard.KeyEnter, "")
	if clicked != "blueberry" {
		t.Fatalf("Enter clicked %q, want blueberry", clicked)
	}

	role = RoleList
	frame()
	key(keyboard.KeyDown, "")
	if w.focusKey != "blueberry" {
		t.Fatalf("Down at the end of a list = %q, want no move", w.focusKey)
	}
}

// In a group whose items are radios, the arrows select as they move. In a
// FocusFollows group, focus follows a selection the app makes.
func TestFocusGroupFollows(t *testing.T) {
	value := "a"
	w := newListWindow(t, nil)
	w.opts.View = func(cx *Cx) *El {
		g := Row().FocusGroup().FocusFollows()
		for _, v := range []string{"a", "b", "c"} {
			g.Child(Box().Size(20, 20).Role(RoleRadio).Focusable().FocusSelected(v == value).Key(v).
				OnClick(func() { value = v }))
		}
		return g
	}
	none := keyboard.Mods(0)
	w.scriptMods = &none
	frame := func() {
		w.build()
		w.placeAll()
	}
	frame()
	w.Focus("a")
	w.onKeyDown(keyboard.KeyRight, false, "")
	frame()
	if value != "b" || w.focusKey != "b" {
		t.Fatalf("Right: value %q, focus %q, want b and b", value, w.focusKey)
	}
	w.onKeyDown(keyboard.KeyLeft, false, "")
	w.onKeyDown(keyboard.KeyLeft, false, "")
	frame()
	if value != "c" {
		t.Fatalf("Left twice from b = %q, want c (radios wrap)", value)
	}
	value = "a"
	frame()
	if w.focusKey != "a" {
		t.Fatalf("focus after the app selected a = %q, want a", w.focusKey)
	}
}

// A pane that closes with the focus inside it hands the focus to its
// FocusReturn target instead of dropping it.
func TestFocusReturnWhenAPaneCloses(t *testing.T) {
	open := true
	w := newListWindow(t, nil)
	w.opts.View = func(cx *Cx) *El {
		root := Row(Box().Size(20, 20).Focusable().Key("toggle"))
		if open {
			root.Child(Col(Box().Size(20, 20).Focusable().Key("row")).Key("pane").FocusReturn("toggle"))
		}
		return root
	}
	frame := func() {
		w.build()
		w.placeAll()
	}
	frame()
	w.Focus("row")
	open = false
	frame()
	if w.focusKey != "toggle" {
		t.Fatalf("focus after the pane closed = %q, want toggle", w.focusKey)
	}
}

// OnBlur runs once when focus moves away, including to nothing, and not when
// the focused element leaves the view.
func TestOnBlur(t *testing.T) {
	blurs := 0
	shown := true
	w := newListWindow(t, nil)
	w.opts.View = func(cx *Cx) *El {
		col := Col(Box().Size(20, 20).Focusable().Key("other"))
		if shown {
			col.Child(Input("", func(string) {}).Size(40, 20).Key("field").OnBlur(func() { blurs++ }))
		}
		return col
	}
	frame := func() {
		w.build()
		w.placeAll()
	}
	frame()

	w.Focus("field")
	w.Focus("other")
	if blurs != 1 {
		t.Fatalf("blurs after moving focus = %d, want 1", blurs)
	}
	frame()
	w.Focus("field")
	w.Focus("")
	if blurs != 2 {
		t.Fatalf("blurs after clearing focus = %d, want 2", blurs)
	}

	frame()
	w.Focus("field")
	shown = false
	frame()
	if blurs != 2 {
		t.Fatalf("blurs after the field left = %d, want 2", blurs)
	}
}

// An element of another kind inserted before an unkeyed sibling does not take
// over that sibling's focus or edit state.
func TestInsertedSiblingOfOtherKindStartsAfresh(t *testing.T) {
	banner := false
	w := newListWindow(t, nil)
	w.opts.View = func(cx *Cx) *El {
		var kids []*El
		if banner {
			kids = append(kids, Text("banner"))
		}
		return Col(append(kids, Input("abc", func(string) {}))...)
	}
	frame := func() {
		w.build()
		w.placeAll()
	}
	frame()
	var input *El
	for _, e := range w.byID {
		if e.kind == kindInput {
			input = e
		}
	}
	if input == nil {
		t.Fatal("no input built")
	}
	w.focused = input.id
	w.editOf(input).Undo = []edit.Snap{{Value: "ab"}}

	banner = true
	frame()
	for _, e := range w.byID {
		if e.kind != kindText {
			continue
		}
		if w.focused == e.id {
			t.Error("the inserted text took the input's focus")
		}
		if s := w.states[e.id]; s != nil && s.edit != nil {
			t.Error("the inserted text took the input's edit state")
		}
	}
}

// Tab reaches a field its scroll container has scrolled out of sight, and
// scrolls it into view, instead of skipping to what follows the container.
func TestTabReachesScrolledOutField(t *testing.T) {
	var form, f3 *El
	w := newListWindow(t, nil)
	w.opts.View = func(cx *Cx) *El {
		f3 = Box().Size(20, 40).Focusable().Key("f3")
		form = Col(
			Box().Size(20, 40).Focusable().Key("f1"),
			Box().Size(20, 40).Focusable().Key("f2"),
			f3,
		).ScrollY().H(60)
		return Col(form, Box().Size(20, 20).Focusable().Key("save"))
	}
	none := keyboard.Mods(0)
	w.scriptMods = &none
	frame := func() { w.build(); w.placeAll() }
	frame()
	w.Focus("f2")
	w.onKeyDown(keyboard.KeyTab, false, "")
	frame()
	if !w.Focused("f3") {
		t.Fatalf("Tab from f2 focused %q, want f3", w.focusKey)
	}
	if f3.culled || form.state.scrollY < 60 {
		t.Fatalf("f3 not scrolled into view: culled=%v scrollY=%.1f", f3.culled, form.state.scrollY)
	}
}

// Focus asked for an element that is not built yet takes effect when it
// appears, even while another element holds focus.
func TestFocusAppliesWhenElementAppears(t *testing.T) {
	show := false
	w := newListWindow(t, nil)
	w.opts.View = func(cx *Cx) *El {
		col := Col(Box().Size(20, 20).Focusable().Key("button"))
		if show {
			col.Child(Input("", func(string) {}).Size(40, 20).Key("field"))
		}
		return col
	}
	frame := func() { w.build(); w.placeAll() }
	frame()
	w.Focus("button")
	show = true
	w.Focus("field")
	if !w.Focused("button") || w.Focused("field") {
		t.Fatalf("before the field is built: Focused(button)=%v Focused(field)=%v, want true, false", w.Focused("button"), w.Focused("field"))
	}
	frame()
	if e := w.focusedEl(); e == nil || e.key != "field" || !w.Focused("field") {
		t.Fatalf("focused element = %v, want field", e)
	}
}

// AutoFocus applies to an element of a window's first view too.
func TestAutoFocusInFirstView(t *testing.T) {
	w := newListWindow(t, nil)
	w.opts.View = func(cx *Cx) *El {
		return Col(Box().Size(20, 20).Focusable().Key("a"), Box().Size(20, 20).Focusable().Key("b").AutoFocus())
	}
	w.build()
	w.placeAll()
	if !w.Focused("b") {
		t.Fatalf("focus = %q, want b", w.focusKey)
	}
}

// While a modal overlay is open, keys reach neither the element focused
// beneath it nor its Tab order, also when the overlay has nothing to focus.
func TestModalBlocksKeysBeneath(t *testing.T) {
	value, clicks := "", 0
	modal, button := false, false
	w := newListWindow(t, nil)
	w.opts.View = func(cx *Cx) *El {
		if modal {
			content := Box().WFull().HFull()
			if button {
				content.Child(Box().Size(20, 20).Focusable().Key("ok"))
			}
			cx.Overlay(content, nil)
		}
		return Col(
			Input(value, func(s string) { value = s }).Key("field").Size(100, 20),
			Box().Size(20, 20).Focusable().Key("delete").OnClick(func() { clicks++ }),
		)
	}
	frame := func() {
		w.build()
		w.placeAll()
	}
	press := func(k keyboard.Key) {
		var m keyboard.Mods
		w.scriptMods = &m
		w.onKeyDown(k, false, "")
		w.scriptMods = nil
	}
	frame()
	w.Focus("field")
	modal = true
	frame()
	w.onChar('a')
	if value != "" {
		t.Fatalf("typing beneath a modal changed the field to %q", value)
	}
	w.Focus("delete")
	press(keyboard.KeyEnter)
	press(keyboard.KeySpace)
	if clicks != 0 {
		t.Fatalf("Enter or Space clicked an element beneath a modal")
	}
	press(keyboard.KeyTab)
	if w.focusKey == "field" {
		t.Fatal("Tab moved focus beneath a modal without focusable content")
	}

	button = true
	frame()
	press(keyboard.KeyTab)
	if w.focusKey != "ok" {
		t.Fatalf("Tab in a modal focused %q, want ok", w.focusKey)
	}
	press(keyboard.KeyTab)
	if w.focusKey != "ok" {
		t.Fatalf("Tab left the modal for %q", w.focusKey)
	}
}
