package fairy

import (
	"strings"
	"testing"

	"github.com/myparsleycat/fairy/keyboard"
)

func TestShiftDeleteCutsAndEditInputPastes(t *testing.T) {
	f := newHookField(t, false)
	clip := ""
	f.w.fakeClip = &clip
	f.typeText("hello world")
	f.key(keyboard.KeyLeft, keyboard.ModShift|keyboard.ModCtrl) // selects "world"
	f.key(keyboard.KeyDelete, keyboard.ModShift)
	f.want("hello ")
	if clip != "world" {
		t.Fatalf("clipboard %q, want the cut text", clip)
	}
	if !f.w.EditInput("field", EditPaste) {
		t.Fatal("EditInput paste refused")
	}
	f.frame()
	f.want("hello world")
	if !f.w.EditInput("field", EditUndo) {
		t.Fatal("EditInput undo refused")
	}
	f.frame()
	f.want("hello ")
	if f.w.EditInput("missing", EditPaste) {
		t.Fatal("EditInput on a missing key reported success")
	}
}

// A paste with no text on the clipboard, such as an image, leaves the
// selection in place.
func TestPasteWithoutTextKeepsSelection(t *testing.T) {
	f := newHookField(t, false)
	clip := ""
	f.w.fakeClip = &clip
	f.typeText("keep")
	f.key(keyboard.KeyA, keyboard.ModCtrl)
	f.key(keyboard.KeyV, keyboard.ModCtrl)
	f.want("keep")
	if f.w.EditInput("field", EditPaste) {
		t.Fatal("EditInput reported a paste of nothing")
	}
	f.frame()
	f.want("keep")
}

func TestEditInputPasteUsesHook(t *testing.T) {
	f := newHookField(t, false)
	take := true
	f.takePaste = &take
	f.frame()
	clip := "text"
	f.w.fakeClip = &clip
	f.w.EditInput("field", EditPaste)
	f.frame()
	if f.paste != 1 {
		t.Fatalf("OnPaste ran %d times, want 1", f.paste)
	}
	f.want("")
}

func TestInputMenuItems(t *testing.T) {
	f := newHookField(t, false)
	clip := ""
	f.w.fakeClip = &clip
	old := editMenuText
	SetEditMenuText(func() EditMenuLabels { return EditMenuLabels{Cut: "잘라내기", Paste: "붙여넣기"} })
	defer SetEditMenuText(old)

	enabled := func() map[string]bool {
		got := map[string]bool{}
		for _, it := range f.w.inputMenu(f.w.focusedEl(), 0, 0) {
			if !it.Separator {
				got[strings.Split(it.Label, "\t")[0]] = !it.Disabled
			}
		}
		return got
	}
	got := enabled()
	want := map[string]bool{"잘라내기": false, "Copy": false, "붙여넣기": false, "Select all": false}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("empty field menu %v, want %v", got, want)
		}
	}
	f.typeText("abc")
	f.key(keyboard.KeyA, keyboard.ModCtrl)
	clip = "x"
	got = enabled()
	for _, k := range []string{"잘라내기", "Copy", "붙여넣기", "Select all"} {
		if !got[k] {
			t.Fatalf("menu with a selection and clipboard text %v, want all enabled", got)
		}
	}
}
