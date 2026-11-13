package fairy

import (
	"testing"

	"github.com/myparsleycat/fairy/internal/darwin"
)

// axString returns an accessibility getter's NSString result, or the
// description of another object.
func axString(obj darwin.ID, sel string) string {
	v := darwin.Send(obj, sel)
	if v == 0 {
		return ""
	}
	if !darwin.Get[bool](v, "isKindOfClass:", darwin.ID(darwin.Cls("NSString"))) {
		v = darwin.Send(v, "description")
	}
	return darwin.GoString(v)
}

func axChildren(obj darwin.ID) []darwin.ID {
	arr := darwin.Send(obj, "accessibilityChildren")
	n := darwin.Get[uint64](arr, "count")
	out := make([]darwin.ID, n)
	for i := range out {
		out[i] = darwin.Send(arr, "objectAtIndex:", uint64(i))
	}
	return out
}

func axAllowed(obj darwin.ID, sel string) bool {
	return darwin.Get[bool](obj, "isAccessibilitySelectorAllowed:", darwin.Sel(sel))
}

// The view exposes the element tree through AppKit's accessibility API, as
// VoiceOver reads it: roles, names, values, frames and actions.
func TestDarwinAccessibilityTree(t *testing.T) {
	clicks, checked, text := 0, false, "hello"
	w := newTestWindow(t, func() *El {
		return Col(
			Box(Text("Save")).OnClick(func() { clicks++ }).Key("save").Size(80, 24),
			Box().Role(RoleCheckBox).Checked(checked).Label("Sync").OnClick(func() { checked = !checked }).Size(20, 20),
			Input(text, func(s string) { text = s }).Placeholder("Name").Key("in").Size(200, 30),
			Col(
				Text("One").Role(RoleListItem).Selected(true),
				Text("Two").Role(RoleListItem).Selected(false),
			).Role(RoleList).Label("Items"),
			Text("Title").Heading(1),
		)
	})
	var view darwin.ID
	var kids []darwin.ID
	Do(func() {
		w.renderFrame()
		view = w.view
		if darwin.Get[bool](view, "isAccessibilityElement") {
			t.Error("the view is an accessibility element itself")
		}
		kids = axChildren(view)
		for _, k := range kids {
			darwin.Retain(k) // kept past the window's close below
		}
	})
	if len(kids) != 5 {
		t.Fatalf("view has %d accessibility children, want 5", len(kids))
	}
	btn, check, input, list, heading := kids[0], kids[1], kids[2], kids[3], kids[4]

	Do(func() {
		type want struct{ role, sub, label, value string }
		for i, wt := range []want{
			{"AXButton", "", "Save", ""},
			{"AXCheckBox", "", "Sync", "0"},
			{"AXTextField", "", "Name", "hello"},
			{"AXList", "", "Items", ""},
			{"AXHeading", "", "Title", "1"},
		} {
			k := kids[i]
			got := want{axString(k, "accessibilityRole"), axString(k, "accessibilitySubrole"),
				axString(k, "accessibilityLabel"), axString(k, "accessibilityValue")}
			if got != wt {
				t.Errorf("child %d: got %+v, want %+v", i, got, wt)
			}
			if p := darwin.Send(k, "accessibilityParent"); p != view {
				t.Errorf("child %d: parent %#x, want the view %#x", i, p, view)
			}
			if win := darwin.Send(k, "accessibilityWindow"); win != w.nsWindow {
				t.Errorf("child %d: window %#x, want %#x", i, win, w.nsWindow)
			}
		}
		if d := axString(heading, "accessibilityRoleDescription"); d != "heading" {
			t.Errorf("heading role description %q", d)
		}
		if id := axString(btn, "accessibilityIdentifier"); id != "save" {
			t.Errorf("button identifier %q", id)
		}

		items := axChildren(list)
		if len(items) != 2 || axString(items[0], "accessibilityRole") != "AXGroup" ||
			axString(items[0], "accessibilityLabel") != "One" || axString(items[1], "accessibilityLabel") != "Two" {
			t.Errorf("list items wrong: %d", len(items))
		} else {
			if !darwin.Get[bool](items[0], "isAccessibilitySelected") || darwin.Get[bool](items[1], "isAccessibilitySelected") {
				t.Error("list item selection wrong")
			}
			if darwin.Send(items[0], "accessibilityParent") != list {
				t.Error("list item's parent is not the list")
			}
			sel := darwin.Send(list, "accessibilitySelectedChildren")
			if darwin.Get[uint64](sel, "count") != 1 || darwin.Send(sel, "objectAtIndex:", uint64(0)) != items[0] {
				t.Error("list's selected children wrong")
			}
		}

		// Frames are in screen coordinates, inside the window's content.
		content := darwin.Get[darwin.CGRect](w.nsWindow, "convertRectToScreen:",
			darwin.Get[darwin.CGRect](view, "convertRect:toView:", darwin.Get[darwin.CGRect](view, "bounds"), darwin.ID(0)))
		top := content.Origin.Y + content.Size.H
		f := darwin.Get[darwin.CGRect](btn, "accessibilityFrame")
		if f.Size.W != 80 || f.Size.H != 24 || f.Origin.X != content.Origin.X || f.Origin.Y+f.Size.H != top {
			t.Errorf("button frame %+v, window content %+v", f, content)
		}
		g := darwin.Get[darwin.CGRect](check, "accessibilityFrame")
		if g.Size.W != 20 || g.Origin.Y+g.Size.H != f.Origin.Y {
			t.Errorf("check box frame %+v below button frame %+v", g, f)
		}
		// Hit testing finds the button at its center.
		center := darwin.CGPoint{X: f.Origin.X + f.Size.W/2, Y: f.Origin.Y + f.Size.H/2}
		if hit := darwin.Send(view, "accessibilityHitTest:", center); hit != btn {
			t.Errorf("hit test at the button found %q", axString(hit, "accessibilityRole"))
		}

		// Actions are offered only where they do something.
		if !axAllowed(btn, "accessibilityPerformPress") || axAllowed(input, "accessibilityPerformPress") ||
			axAllowed(heading, "accessibilityPerformPress") {
			t.Error("press allowed on the wrong elements")
		}
		if axAllowed(btn, "accessibilityValue") || !axAllowed(input, "accessibilityNumberOfCharacters") ||
			axAllowed(btn, "accessibilityNumberOfCharacters") {
			t.Error("getters allowed on the wrong elements")
		}
	})

	// Pressing runs OnClick; the check box reports its new value and keeps
	// its element.
	Do(func() {
		if !darwin.Get[bool](btn, "accessibilityPerformPress") || !darwin.Get[bool](check, "accessibilityPerformPress") {
			t.Error("press failed")
		}
		w.runPosted()
		w.renderFrame()
	})
	if clicks != 1 || !checked {
		t.Fatalf("after pressing: clicks %d, checked %v", clicks, checked)
	}
	Do(func() {
		if k := axChildren(view); len(k) != 5 || k[1] != check {
			t.Error("the check box's element changed after a rebuild")
		}
		if v := axString(check, "accessibilityValue"); v != "1" {
			t.Errorf("checked value %q", v)
		}
	})

	// Focus follows the window's focus both ways.
	Do(func() {
		w.osFocus = true
		darwin.Send(input, "setAccessibilityFocused:", true)
		w.renderFrame()
		if f := w.focusedEl(); f == nil || f.key != "in" {
			t.Error("setAccessibilityFocused: did not focus the input")
		}
		if !darwin.Get[bool](input, "isAccessibilityFocused") || darwin.Get[bool](btn, "isAccessibilityFocused") {
			t.Error("isAccessibilityFocused wrong")
		}
		if f := darwin.Send(view, "accessibilityFocusedUIElement"); f != input {
			t.Errorf("focused element is %q", axString(f, "accessibilityRole"))
		}
		if w.acc.focus != w.a11yTree().exposedAncestor(w.focusedEl()) {
			t.Error("a11yAfterFrame did not track the focus")
		}
	})

	// Text fields expose their text and selection.
	Do(func() {
		if n := darwin.Get[int64](input, "accessibilityNumberOfCharacters"); n != 5 {
			t.Errorf("number of characters %d", n)
		}
		if r := darwin.Get[darwin.NSRange](input, "accessibilitySelectedTextRange"); r != (darwin.NSRange{Location: 5}) {
			t.Errorf("selected range %+v", r)
		}
		darwin.Send(input, "setAccessibilitySelectedTextRange:", darwin.NSRange{Location: 1, Length: 2})
		if s := axString(input, "accessibilitySelectedText"); s != "el" {
			t.Errorf("selected text %q", s)
		}
		if r := darwin.Get[darwin.NSRange](input, "accessibilitySelectedTextRange"); r != (darwin.NSRange{Location: 1, Length: 2}) {
			t.Errorf("selected range after setting %+v", r)
		}
		if s := darwin.GoString(darwin.Send(input, "accessibilityStringForRange:", darwin.NSRange{Location: 0, Length: 4})); s != "hell" {
			t.Errorf("string for range %q", s)
		}
		if r := darwin.Get[darwin.NSRange](input, "accessibilityRangeForLine:", int64(0)); r != (darwin.NSRange{Length: 5}) {
			t.Errorf("range for line 0 %+v", r)
		}
		darwin.Send(input, "setAccessibilityValue:", darwin.String("world"))
		w.runPosted()
		w.renderFrame()
	})
	if text != "world" {
		t.Errorf("setAccessibilityValue: set %q", text)
	}
	Do(func() {
		if v := axString(input, "accessibilityValue"); v != "world" {
			t.Errorf("value after setting %q", v)
		}
		w.Announce("Saved")
		Announce("Saved everywhere")
	})

	// Elements kept by a client answer safely once the window is gone.
	Do(func() {
		w.closeNow()
		for _, k := range kids {
			if r := axString(k, "accessibilityRole"); r != "AXUnknown" {
				t.Errorf("role after close %q", r)
			}
			if c := darwin.Send(k, "accessibilityChildren"); c != 0 {
				t.Error("children after close")
			}
			_ = darwin.Get[darwin.CGRect](k, "accessibilityFrame")
			darwin.Get[bool](k, "accessibilityPerformPress")
			darwin.Release(k)
		}
	})
}

// Other controls map onto their AppKit roles; sliders step and take values;
// elements that leave the tree are let go.
func TestDarwinAccessibilityControls(t *testing.T) {
	volume, status, extra := 40.0, "Idle", true
	w := newTestWindow(t, func() *El {
		col := Col(
			Box().Role(RoleSwitch).Checked(true).Label("Wi-Fi").Size(20, 20),
			Box(Text("Bold")).Role(RoleButton).Checked(false).OnClick(func() {}),
			Box().Label("Volume").RangeValue(volume, 0, 100, 5, func(v float64) { volume = v }).ValueText("loud").Size(100, 10),
			Box().RangeValue(0.5, 0, 1, 0, nil).Label("Loading").Size(100, 4),
			Input("a\nb", func(string) {}).Multiline().Label("Notes").Size(200, 40),
			Input("secret", func(string) {}).Password().Label("Password").Size(200, 30),
			Text(status).Role(RoleStatusBar),
		)
		if extra {
			col.children = append(col.children, Box(Text("Extra")).OnClick(func() {}))
		}
		return col
	})
	var kids []darwin.ID
	Do(func() {
		w.renderFrame()
		kids = axChildren(w.view)
	})
	if len(kids) != 8 {
		t.Fatalf("%d children, want 8", len(kids))
	}
	sw, toggle, slider, progress, notes, password, statusBar, extraBtn := kids[0], kids[1], kids[2], kids[3], kids[4], kids[5], kids[6], kids[7]
	Do(func() {
		type want struct{ role, sub, value string }
		for _, c := range []struct {
			el darwin.ID
			want
		}{
			{sw, want{"AXCheckBox", "AXSwitch", "1"}},
			{toggle, want{"AXCheckBox", "AXToggle", "0"}},
			{slider, want{"AXSlider", "", "40"}},
			{progress, want{"AXProgressIndicator", "", "0.5"}},
			{notes, want{"AXTextArea", "", "a\nb"}},
			{password, want{"AXTextField", "AXSecureTextField", ""}},
			{statusBar, want{"AXGroup", "AXApplicationStatus", ""}},
		} {
			got := want{axString(c.el, "accessibilityRole"), axString(c.el, "accessibilitySubrole"), axString(c.el, "accessibilityValue")}
			if got != c.want {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		}
		if d := axString(slider, "accessibilityValueDescription"); d != "loud" {
			t.Errorf("slider value description %q", d)
		}
		if lo, hi := axString(slider, "accessibilityMinValue"), axString(slider, "accessibilityMaxValue"); lo != "0" || hi != "100" {
			t.Errorf("slider range %s..%s", lo, hi)
		}
		if axAllowed(progress, "accessibilityPerformIncrement") || !axAllowed(slider, "accessibilityPerformIncrement") {
			t.Error("increment allowed on the wrong elements")
		}
		if l := darwin.Get[int64](notes, "accessibilityInsertionPointLineNumber"); l != 1 {
			t.Errorf("insertion point line %d", l)
		}
		if l := darwin.Get[int64](notes, "accessibilityLineForIndex:", int64(2)); l != 1 {
			t.Errorf("line for index 2: %d", l)
		}
		if n := darwin.Get[int64](password, "accessibilityNumberOfCharacters"); n != 0 {
			t.Errorf("a password exposes %d characters", n)
		}

		darwin.Get[bool](slider, "accessibilityPerformIncrement")
		w.runPosted()
	})
	if volume != 45 {
		t.Errorf("increment set %v, want 45", volume)
	}
	Do(func() {
		darwin.Send(slider, "setAccessibilityValue:", darwin.Number(250))
		w.runPosted()
	})
	if volume != 100 {
		t.Errorf("setting 250 set %v, want 100", volume)
	}

	// A live region's new text is announced; a removed element is let go
	// once its state would be.
	Do(func() {
		w.renderFrame()
		status, extra = "Done", false
		darwin.Retain(extraBtn)
		for range stateTTL + 2 {
			w.Update()
			w.renderFrame()
		}
		if _, ok := w.acc.nodes[w.acc.tree.kids[0][0]]; !ok {
			t.Error("a kept element was let go")
		}
		if len(w.acc.nodes) != 7 {
			t.Errorf("%d elements kept, want 7", len(w.acc.nodes))
		}
		if r := axString(extraBtn, "accessibilityRole"); r != "AXUnknown" {
			t.Errorf("removed element answers role %q", r)
		}
		darwin.Release(extraBtn)
		if w.acc.live[w.acc.tree.kids[0][6]] != "Done" {
			t.Errorf("live region text %q", w.acc.live[w.acc.tree.kids[0][6]])
		}
	})
}

// Nothing is built for a window that no assistive client has asked about.
func TestDarwinAccessibilityLazy(t *testing.T) {
	w := newTestWindow(t, func() *El { return Box(Text("Save")).OnClick(func() {}) })
	Do(func() {
		w.renderFrame()
		if w.acc != nil {
			t.Error("accessibility state built without a client")
		}
		w.Announce("nobody listens")
	})
}
