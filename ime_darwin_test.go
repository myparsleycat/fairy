package fairy

import (
	"testing"
	"unsafe"

	"github.com/ebitengine/purego/objc"

	"github.com/myparsleycat/fairy/internal/darwin"
)

// newTestWindow opens a hidden window showing view, on the UI thread.
func newTestWindow(t *testing.T, view func() *El) *Window {
	t.Helper()
	var w *Window
	var err error
	Do(func() {
		w, err = NewWindow(Options{Width: 320, Height: 200, StartHidden: true, View: func(*Cx) *El { return view() }})
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Do(w.closeNow) })
	return w
}

// An input method composes into the focused Input through the view's
// NSTextInputClient methods, which AppKit calls with objc_msgSend, including
// the ones that return structs through forwardInvocation:.
func TestDarwinIMEComposition(t *testing.T) {
	value := "ab"
	w := newTestWindow(t, func() *El {
		return Input(value, func(s string) { value = s }).Key("in").Size(200, 30)
	})
	view := darwin.ID(0)
	Do(func() {
		w.Focus("in")
		w.renderFrame()
		view = w.view
	})
	str := func(s string) darwin.ID { return darwin.String(s) }
	mark := func(s string, cursor uint64) {
		Do(func() {
			darwin.Send(view, "setMarkedText:selectedRange:replacementRange:", str(s),
				darwin.NSRange{Location: cursor}, darwin.NSRange{Location: darwin.NSNotFound})
		})
	}
	ranges := func() (marked, selected darwin.NSRange, has bool) {
		Do(func() {
			marked = darwin.Get[darwin.NSRange](view, "markedRange")
			selected = darwin.Get[darwin.NSRange](view, "selectedRange")
			has = darwin.Get[bool](view, "hasMarkedText")
		})
		return
	}
	Do(func() {
		if !darwin.Get[bool](view, "conformsToProtocol:", darwin.ID(unsafe.Pointer(objc.GetProtocol("NSTextInputClient")))) {
			t.Error("view is not an NSTextInputClient")
		}
	})

	if m, s, has := ranges(); has || m.Location != darwin.NSNotFound || s != (darwin.NSRange{Location: 2}) {
		t.Fatalf("before composing: marked %v selected %v has %v", m, s, has)
	}
	mark("ㅎ", 1)
	mark("하", 1)
	mark("한", 1)
	if m, s, has := ranges(); !has || m != (darwin.NSRange{Location: 2, Length: 1}) || s != (darwin.NSRange{Location: 3}) {
		t.Fatalf("composing: marked %v selected %v has %v", m, s, has)
	}
	if value != "ab" || w.composition() != "한" {
		t.Fatalf("composing changed the value to %q, composition %q", value, w.composition())
	}
	var rect darwin.CGRect
	actual := new(darwin.NSRange)
	Do(func() {
		rect = darwin.Get[darwin.CGRect](view, "firstRectForCharacterRange:actualRange:",
			darwin.NSRange{Location: 2, Length: 1}, unsafe.Pointer(actual))
	})
	frame := darwin.CGRect{}
	Do(func() { frame = darwin.Get[darwin.CGRect](w.nsWindow, "frame") })
	if rect.Size.H <= 0 || rect.Origin.X < frame.Origin.X || rect.Origin.X > frame.Origin.X+frame.Size.W ||
		rect.Origin.Y < frame.Origin.Y || rect.Origin.Y > frame.Origin.Y+frame.Size.H {
		t.Errorf("caret rect %v is not inside the window %v", rect, frame)
	}
	if *actual != (darwin.NSRange{Location: 2, Length: 1}) {
		t.Errorf("actual range = %v", *actual)
	}

	// The next syllable starts: the input method commits the first one.
	Do(func() {
		darwin.Send(view, "insertText:replacementRange:", str("한"), darwin.NSRange{Location: darwin.NSNotFound})
	})
	mark("ㄱ", 1)
	mark("글", 1)
	if value != "ab한" || w.composition() != "글" {
		t.Fatalf("after commit: value %q, composition %q", value, w.composition())
	}
	// Focus moving away keeps the composed text.
	Do(func() { w.commitComposition() })
	if value != "ab한글" || w.composition() != "" {
		t.Fatalf("after commitComposition: value %q, composition %q", value, w.composition())
	}
	// An emptied composition is canceled.
	mark("ㅁ", 1)
	mark("", 0)
	if value != "ab한글" || w.composition() != "" {
		t.Fatalf("canceled composition: value %q, composition %q", value, w.composition())
	}
	if _, s, _ := ranges(); s != (darwin.NSRange{Location: 4}) {
		t.Errorf("selected range after typing = %v", s)
	}
}

var (
	cgEventCreateKeyboardEvent      func(source uintptr, key uint16, down bool) uintptr
	cgEventKeyboardSetUnicodeString func(ev uintptr, n uint64, s *uint16)
	cgEventSetFlags                 func(ev uintptr, flags uint64)
)

func init() {
	darwin.Func(&cgEventCreateKeyboardEvent, darwin.CoreGraphics, "CGEventCreateKeyboardEvent")
	darwin.Func(&cgEventKeyboardSetUnicodeString, darwin.CoreGraphics, "CGEventKeyboardSetUnicodeString")
	darwin.Func(&cgEventSetFlags, darwin.CoreGraphics, "CGEventSetFlags")
}

// keyEvent makes a key down NSEvent for macOS key code code typing ch.
func keyEvent(code uint16, ch rune, flags uint64) darwin.ID {
	cg := cgEventCreateKeyboardEvent(0, code, true)
	if ch != 0 {
		u := uint16(ch)
		cgEventKeyboardSetUnicodeString(cg, 1, &u)
	}
	cgEventSetFlags(cg, flags)
	ev := darwin.SendClass("NSEvent", "eventWithCGEvent:", cg)
	darwin.CFRelease(cg)
	return ev
}

// Keys reach an Input through interpretKeyEvents: and type their text once;
// shortcuts with Command run edit commands instead of typing.
func TestDarwinKeysThroughInputMethod(t *testing.T) {
	value := ""
	w := newTestWindow(t, func() *El {
		return Input(value, func(s string) { value = s }).Key("in").Size(200, 30)
	})
	press := func(code uint16, ch rune, flags uint64) {
		Do(func() { darwin.WithPool(func() { w.keyDown(keyEvent(code, ch, flags)) }) })
	}
	Do(func() {
		w.Focus("in")
		w.renderFrame()
	})
	if !w.imeOn {
		t.Fatal("the focused Input does not take the input method")
	}
	press(0x04, 'h', 0)
	press(0x22, 'i', 0)
	press(0x31, ' ', 0)
	press(0x00, 'a', 0)
	if value != "hi a" {
		t.Fatalf("typed %q, want %q", value, "hi a")
	}
	press(0x33, 0x7f, 0)       // Backspace
	press(0x7B, 0xF702, 0)     // Left
	press(0x0E, '´', nsOption) // Option types characters on macOS
	if value != "hi´ " {
		t.Fatalf("Backspace, ← and Option+E = %q, want %q", value, "hi´ ")
	}
	press(0x00, 'a', nsCommand) // ⌘A selects all
	press(0x04, 'h', 0)
	if value != "h" {
		t.Fatalf("⌘A then h = %q, want %q", value, "h")
	}
}
