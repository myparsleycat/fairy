package fairy

import (
	"strings"
	"testing"

	"github.com/myparsleycat/fairy/internal/edit"
	"github.com/myparsleycat/fairy/keyboard"
)

// The key that confirms an input method composition reaches the IME, not the
// handler: Windows reports it as VK_PROCESSKEY, and keys pressed while a
// composition is open belong to the IME as well.
func TestInputHookSees(t *testing.T) {
	cases := []struct {
		ev        keyboard.KeyEvent
		composing bool
		want      bool
	}{
		{keyboard.KeyEvent{Key: keyboard.KeyEnter}, false, true},
		{keyboard.KeyEvent{Key: keyboard.KeyEnter, Mods: keyboard.ModShift}, false, true},
		{keyboard.KeyEvent{Key: keyboard.KeyEnter}, true, false},
		{keyboard.KeyEvent{Key: keyProcess}, false, false},
		{keyboard.KeyEvent{Key: keyProcess}, true, false},
	}
	for _, c := range cases {
		if got := inputHookSees(c.ev, c.composing); got != c.want {
			t.Errorf("inputHookSees(%+v, composing=%v) = %v, want %v", c.ev, c.composing, got, c.want)
		}
	}
}

type hookField struct {
	t     *testing.T
	w     *Window
	value string
	keys  []keyboard.KeyEvent
	sends []string
	paste int
	// takePaste is what OnPaste returns; nil leaves OnPaste unset.
	takePaste *bool
	hook      bool
	// normalize, when set, is applied to every edit the app stores.
	normalize func(string) string
}

func newHookField(t *testing.T, hook bool) *hookField {
	f := &hookField{t: t, hook: hook}
	f.w = newListWindow(t, func() *El {
		in := Input(f.value, func(s string) {
			if f.normalize != nil {
				s = f.normalize(s)
			}
			f.value = s
		}).Multiline().Key("field").Size(180, 60)
		if f.hook {
			in.OnInputKey(func(ev keyboard.KeyEvent) bool {
				f.keys = append(f.keys, ev)
				if ev.Key == keyboard.KeyEnter && ev.Mods == 0 {
					f.sends = append(f.sends, f.value)
					f.value = ""
					return true
				}
				return false
			})
		}
		if f.takePaste != nil {
			in.OnPaste(func() bool {
				f.paste++
				return *f.takePaste
			})
		}
		return in
	})
	f.frame()
	f.w.Focus("field")
	f.frame()
	return f
}

// frame builds the view and syncs the field as painting does.
func (f *hookField) frame() {
	f.w.build()
	f.w.placeAll()
	if e := f.w.focusedEl(); e != nil && e.kind == kindInput {
		f.w.editOf(e).Sync(e.input.value)
	}
}

func (f *hookField) key(k keyboard.Key, mods keyboard.Mods) {
	f.w.scriptMods = &mods
	f.w.onKeyDown(k, false, "")
	f.w.scriptMods = nil
	f.frame()
}

func (f *hookField) typeText(s string) {
	for _, r := range s {
		f.w.onChar(uint16(r))
	}
	f.frame()
}

func (f *hookField) want(value string) {
	f.t.Helper()
	if f.value != value {
		f.t.Fatalf("value = %q, want %q", f.value, value)
	}
}

func TestInputKeyHook(t *testing.T) {
	f := newHookField(t, true)
	f.typeText("hi")
	f.key(keyboard.KeyEnter, keyboard.ModShift)
	f.want("hi\n")
	f.typeText("yo")
	f.key(keyboard.KeyEnter, 0)
	f.want("")
	if len(f.sends) != 1 || f.sends[0] != "hi\nyo" {
		t.Fatalf("sends = %q, want [\"hi\\nyo\"]", f.sends)
	}

	// While the IME composes, and for the key it processes, the handler is
	// not called and nothing is sent.
	f.typeText("x")
	n := len(f.keys)
	st := f.w.editOf(f.w.focusedEl())
	st.comp = "가"
	f.key(keyboard.KeyEnter, 0)
	st.comp = ""
	f.key(keyProcess, 0)
	if len(f.keys) != n || len(f.sends) != 1 {
		t.Fatalf("handler saw %v while composing; sends = %q", f.keys[n:], f.sends)
	}
	f.want("x")
}

// Without a handler, Enter in a multiline Input inserts a newline and undo
// steps back through the edits as before.
func TestInputKeyWithoutHook(t *testing.T) {
	f := newHookField(t, false)
	f.typeText("ab")
	f.key(keyboard.KeyEnter, 0)
	f.want("ab\n")
	f.key(keyboard.KeyZ, keyboard.ModCtrl)
	f.want("ab")
	f.key(keyboard.KeyY, keyboard.ModCtrl)
	f.want("ab\n")
}

// A value that a handler replaces becomes one undo step, so Ctrl+Z after
// Enter sent the text brings it back instead of an older edit.
func TestInputHookUndo(t *testing.T) {
	f := newHookField(t, true)
	f.typeText("abc")
	f.key(keyboard.KeyEnter, 0)
	f.want("")
	f.key(keyboard.KeyZ, keyboard.ModCtrl)
	f.want("abc")
	f.key(keyboard.KeyY, keyboard.ModCtrl)
	f.want("")
	f.key(keyboard.KeyZ, keyboard.ModCtrl)
	f.key(keyboard.KeyZ, keyboard.ModCtrl)
	f.want("")

	// Text typed and sent before the next paint leaves the field empty,
	// although the app's value is the same as at the last paint.
	f.key(keyboard.KeyEnter, 0)
	f.w.onChar('x')
	f.key(keyboard.KeyEnter, 0)
	if st := f.w.editOf(f.w.focusedEl()); st.Value != "" {
		t.Fatalf("field shows %q after sending, want it empty", st.Value)
	}
}

// A handler that leaves a key to the Input does not change how the Input's
// own edits are undone, also when the app rewrites what it stores.
func TestInputHookDeclinedUndo(t *testing.T) {
	f := newHookField(t, true)
	f.normalize = strings.ToUpper
	f.typeText("ab")
	// The handler sees C go down and leaves it; the letter it types is
	// stored upper-case.
	var m keyboard.Mods
	f.w.scriptMods = &m
	f.w.onKeyDown(keyboard.Key('C'), false, "c")
	f.w.scriptMods = nil
	f.typeText("c")
	f.want("ABC")
	f.key(keyboard.KeyZ, keyboard.ModCtrl)
	f.want("")
}

func TestInputPasteHook(t *testing.T) {
	take := true
	f := newHookField(t, false)
	f.takePaste = &take
	f.frame()
	f.typeText("a")
	f.key(keyboard.KeyV, keyboard.ModCtrl)
	f.key(keyboard.KeyInsert, keyboard.ModShift)
	if f.paste != 2 {
		t.Fatalf("OnPaste ran %d times, want 2 (Ctrl+V and Shift+Insert)", f.paste)
	}
	f.want("a")
	// A paste the hook took ends the typing run even though the value is unchanged.
	f.typeText("b")
	f.key(keyboard.KeyZ, keyboard.ModCtrl)
	f.want("a")

	// A read-only Input does not offer the paste.
	f.w.opts.View = func(*Cx) *El {
		return Input("a", nil).Key("field").OnPaste(func() bool { f.paste++; return true })
	}
	f.frame()
	f.key(keyboard.KeyV, keyboard.ModCtrl)
	if f.paste != 2 {
		t.Fatalf("OnPaste ran for a read-only Input")
	}
	// Shift+Insert in a read-only Input still reaches the app.
	var seen bool
	f.w.opts.OnKey = func(ev keyboard.KeyEvent) bool { seen = ev.Is(keyboard.KeyInsert, keyboard.ModShift); return seen }
	f.key(keyboard.KeyInsert, keyboard.ModShift)
	if !seen {
		t.Fatalf("Shift+Insert in a read-only Input did not reach the window's OnKey")
	}
}

// Typing and deleting are undone a run at a time. A run ends at a change of
// kind or a caret move, and a new line is a step of its own.
func TestInputUndoRuns(t *testing.T) {
	f := newHookField(t, false)
	f.typeText("ab")
	f.key(keyboard.KeyBackspace, 0)
	f.typeText("c")
	f.want("ac")
	for _, want := range []string{"a", "ab", ""} {
		f.key(keyboard.KeyZ, keyboard.ModCtrl)
		f.want(want)
	}

	f.typeText("xz")
	f.key(keyboard.KeyLeft, 0)
	f.typeText("y")
	f.want("xyz")
	f.key(keyboard.KeyZ, keyboard.ModCtrl)
	f.want("xz")

	f.value = ""
	f.frame()
	f.typeText("hi")
	f.key(keyboard.KeyEnter, 0)
	f.typeText("yo")
	for _, want := range []string{"hi\n", "hi", ""} {
		f.key(keyboard.KeyZ, keyboard.ModCtrl)
		f.want(want)
	}
}

func TestInputUndoRedoReleasesPoppedSnapshots(t *testing.T) {
	before := edit.Snap{Value: "before", Caret: 5, Anchor: 1}
	after := edit.Snap{Value: "after", Caret: 2, Anchor: 4}
	undo := []edit.Snap{before}
	redo := make([]edit.Snap, 0, 1)
	s := &editState{Value: after.Value, Caret: after.Caret, Anchor: after.Anchor,
		Undo: undo, Redo: redo, UndoBytes: len(before.Value)}
	var reported string
	e := Input(after.Value, func(value string) { reported = value })
	w := &Window{}
	w.undo(e, s)
	if s.Snap() != before || reported != before.Value || s.UndoBytes != 0 || len(s.Undo) != 0 || len(s.Redo) != 1 || s.Redo[0] != after {
		t.Fatalf("undo did not restore the snapshot and transfer history: %+v", s)
	}
	if undo[0] != (edit.Snap{}) {
		t.Errorf("popped undo slot still retains %+v", undo[0])
	}
	w.redo(e, s)
	if s.Snap() != after || reported != after.Value || s.UndoBytes != len(before.Value) || len(s.Redo) != 0 || len(s.Undo) != 1 || s.Undo[0] != before {
		t.Fatalf("redo did not restore the snapshot and transfer history: %+v", s)
	}
	if redo[:cap(redo)][0] != (edit.Snap{}) {
		t.Errorf("popped redo slot still retains %+v", redo[:cap(redo)][0])
	}
}

// The field shows what the app stores whenever that differs from the last
// value it reported, also when the app keeps its previous value or an
// OnSubmit handler clears it before the next paint.
func TestInputShowsAppValue(t *testing.T) {
	f := newHookField(t, false)
	f.normalize = func(s string) string { return strings.Trim(s, "x") }
	f.typeText("1")
	f.typeText("x")
	if st := f.w.editOf(f.w.focusedEl()); st.Value != "1" {
		t.Fatalf("field shows %q after the app rejected an edit, want %q", st.Value, "1")
	}

	value := ""
	w := newListWindow(t, func() *El {
		return Input(value, func(s string) { value = s }).OnSubmit(func(string) { value = "" }).Key("field").Size(180, 24)
	})
	frame := func() {
		w.build()
		w.placeAll()
		w.editOf(w.focusedEl()).Sync(w.focusedEl().input.value)
	}
	w.build()
	w.placeAll()
	w.Focus("field")
	frame()
	w.typeText(w.focusedEl(), "네")
	var m keyboard.Mods
	w.scriptMods = &m
	w.onKeyDown(keyboard.KeyEnter, false, "")
	frame()
	if st := w.editOf(w.focusedEl()); st.Value != "" {
		t.Fatalf("field shows %q after OnSubmit cleared it, want it empty", st.Value)
	}
}

// Keys handled before the next paint, after an edit that shortened the
// value, must not place the caret with the geometry of the last paint.
func TestInputStaleGeometry(t *testing.T) {
	f := newHookField(t, false)
	f.value = "hello world, this is a line"
	f.frame()
	e := f.w.focusedEl()
	content := rectF{0, 0, 180, 60}
	f.w.paintInput(e, content, content, Hex(0), 1)
	ctrl, none := keyboard.ModCtrl, keyboard.Mods(0)
	f.w.scriptMods = &ctrl
	f.w.onKeyDown(keyboard.KeyA, false, "")
	f.w.scriptMods = &none
	f.w.onKeyDown(keyboard.KeyDelete, false, "")
	f.w.onKeyDown(keyboard.KeyEnd, false, "")
	f.w.onChar('x')
	f.w.pressInput(e, 170, 30, 1)
	f.w.onChar('y')
	f.want("xy")
}

// An input method never composes into a password Input.
func TestPasswordTakesNoIME(t *testing.T) {
	w := newListWindow(t, func() *El {
		return Input("", func(string) {}).Password().Key("field").Size(180, 24)
	})
	w.build()
	w.placeAll()
	w.Focus("field")
	if e, _ := w.imeInput(); e != nil {
		t.Fatal("a password Input takes IME composition")
	}
}

// Home and End in a wrapped multiline Input go to the ends of the line as
// shown. A line that wraps without a space ends where the next one starts, so
// End must keep the caret on its own line for Home to come back to its start.
func TestInputVisualHomeEnd(t *testing.T) {
	f := newHookField(t, false)
	f.value = strings.Repeat("あいうえおかきくけこ", 6)
	f.frame()
	e := f.w.focusedEl()
	content := rectF{0, 0, 180, 60}
	f.w.paintInput(e, content, content, Hex(0), 1)
	st := f.w.editOf(e)
	l := f.w.ts.Layout(st.geom.text, st.geom.style, st.geom.width)
	if l.Lines() < 3 {
		t.Fatalf("text takes %d lines, want it wrapped", l.Lines())
	}
	ms := l.LineMetrics()
	second := l.HitTest(1, ms[0].Height+1)
	st.Caret, st.Anchor = second+3, second+3
	f.key(keyboard.KeyEnd, 0)
	end := st.Caret
	if end <= second || end >= len(f.value) {
		t.Fatalf("End moved the caret to %d, want the end of the second line after %d", end, second)
	}
	f.w.paintInput(e, content, content, Hex(0), 1)
	_, y0, _ := l.Caret(second)
	if _, y, _ := l.CaretAt(end, st.upstream); y != y0 {
		t.Fatalf("caret after End drawn at y=%v, want the second line's y=%v", y, y0)
	}
	f.key(keyboard.KeyHome, 0)
	if st.Caret != second {
		t.Fatalf("Home after End moved the caret to %d, want %d", st.Caret, second)
	}
	f.key(keyboard.KeyEnd, keyboard.ModCtrl)
	if st.Caret != len(f.value) {
		t.Fatalf("Ctrl+End moved the caret to %d, want %d", st.Caret, len(f.value))
	}
}

// A high surrogate whose low half never comes does not swallow the next
// character, and a pair still makes one character.
func TestInputSurrogatePairs(t *testing.T) {
	f := newHookField(t, false)
	f.w.onChar(0xD83D)
	f.w.onChar('a')
	f.w.onChar(0xDE00)
	f.w.onChar(0xD83D)
	f.w.onChar(0xDE00)
	f.frame()
	f.want("a😀")
}
