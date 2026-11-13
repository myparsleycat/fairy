package fairy

// Input methods on macOS: the view is an NSTextInputClient. Keys go through
// interpretKeyEvents: while an Input that takes the input method has focus,
// and the input method answers with setMarkedText: for the composition,
// insertText: for finished text, and doCommandBySelector: for keys it does
// not want.
//
// AppKit asks for ranges and rectangles with methods that return structs,
// which purego callbacks cannot return. Those methods are added with
// _objc_msgForward as their implementation, so the runtime packs each call
// into an NSInvocation and hands it to forwardInvocation:, which sets the
// return value through the invocation.

import (
	"runtime"
	"sync"
	"unicode/utf8"
	"unsafe"

	"github.com/ebitengine/purego/objc"

	"github.com/myparsleycat/fairy/internal/darwin"
)

// imeState is the input method part of a window.
type imeState struct {
	inKey      bool // interpretKeyEvents: runs for a key press
	command    bool // the input method passed the key on as a command
	discarding bool // discardMarkedText runs; its callbacks are ignored
}

var (
	selMarkedRange   = darwin.Sel("markedRange")
	selSelectedRange = darwin.Sel("selectedRange")
	selFirstRect     = darwin.Sel("firstRectForCharacterRange:actualRange:")
	// selOpaqueTitleBar is the private NSView method that tells AppKit
	// which part of the title bar the view handles itself.
	selOpaqueTitleBar = darwin.Sel("_opaqueRectForWindowMoveWhenInTitlebar")
)

// Type encodings of the NSTextInputClient methods with struct arguments or
// results.
const (
	encRange     = "{_NSRange=QQ}"
	encRect      = "{CGRect={CGPoint=dd}{CGSize=dd}}"
	encRangeFn   = encRange + "@:"
	encFirstRect = encRect + "@:" + encRange + "^" + encRange
)

// addTextInputClient makes the view class an NSTextInputClient.
func addTextInputClient(c objc.Class) {
	type id = darwin.ID
	type sel = darwin.SEL
	if p := objc.GetProtocol("NSTextInputClient"); p != nil {
		c.AddProtocol(p)
	}
	win := func(self id) *Window {
		if w := byView[self]; w != nil && !w.closed {
			return w
		}
		return nil
	}
	add := func(name, types string, fn any) {
		if !c.AddMethod(darwin.Sel(name), objc.NewIMP(fn), types) {
			panic("fairy: add method " + name)
		}
	}
	add("hasMarkedText", "B@:", func(self id, _ sel) bool {
		w := win(self)
		return w != nil && w.composition() != ""
	})
	add("unmarkText", "v@:", func(self id, _ sel) {
		if w := win(self); w != nil && !w.ime.discarding {
			w.finishComposition()
		}
	})
	add("validAttributesForMarkedText", "@@:", func(id, sel) id {
		return darwin.SendClass("NSArray", "array")
	})
	add("attributedSubstringForProposedRange:actualRange:", "@@:"+encRange+"^"+encRange,
		func(id, sel, uint64, uint64) id { return 0 })
	add("characterIndexForPoint:", "Q@:{CGPoint=dd}", func(id, sel) uint64 { return darwin.NSNotFound })
	// Replacement ranges are not read, so input methods that replace text
	// before the caret, such as reconversion, are not supported. On amd64
	// the second range of setMarkedText: does not arrive in registers.
	add("insertText:replacementRange:", "v@:@"+encRange, func(self id, _ sel, str id) {
		if w := win(self); w != nil && !w.ime.discarding {
			w.imeInsert(nsText(str))
		}
	})
	add("setMarkedText:selectedRange:replacementRange:", "v@:@"+encRange+encRange,
		func(self id, _ sel, str id, selLoc, selLen uint64) {
			if w := win(self); w != nil && !w.ime.discarding {
				w.imeMark(nsText(str), selLoc)
			}
		})
	add("doCommandBySelector:", "v@::", func(self id, _ sel, cmd sel) {
		if w := win(self); w != nil {
			w.ime.command = true
		}
	})
	add("forwardInvocation:", "v@:@", func(self id, cmd sel, inv id) {
		if !imeInvocation(win(self), inv) {
			self.SendSuper(cmd, inv)
		}
	})
	fwd := darwin.Symbol(darwin.Objc, "_objc_msgForward")
	rectFwd := fwd
	if runtime.GOARCH == "amd64" {
		// A CGRect is returned through memory on x86-64.
		rectFwd = darwin.Symbol(darwin.Objc, "_objc_msgForward_stret")
	}
	c.AddMethod(selMarkedRange, objc.IMP(fwd), encRangeFn)
	c.AddMethod(selSelectedRange, objc.IMP(fwd), encRangeFn)
	c.AddMethod(selFirstRect, objc.IMP(rectFwd), encFirstRect)
	c.AddMethod(selOpaqueTitleBar, objc.IMP(rectFwd), encRect+"@:")
}

// imeInvocation answers a forwarded call to a method that returns a struct:
// NSTextInputClient's, and the title bar's. It reports false
// for a selector it does not answer. w is nil once the window is closed.
func imeInvocation(w *Window, inv darwin.ID) bool {
	switch darwin.Get[darwin.SEL](inv, "selector") {
	case selMarkedRange:
		r := &darwin.NSRange{Location: darwin.NSNotFound}
		if w != nil {
			*r = w.markedRange()
		}
		darwin.Send(inv, "setReturnValue:", unsafe.Pointer(r))
	case selSelectedRange:
		r := &darwin.NSRange{Location: darwin.NSNotFound}
		if w != nil {
			*r = w.selectedRange()
		}
		darwin.Send(inv, "setReturnValue:", unsafe.Pointer(r))
	case selFirstRect:
		rng := new(darwin.NSRange)
		actual := new(*darwin.NSRange)
		darwin.Send(inv, "getArgument:atIndex:", unsafe.Pointer(rng), int64(2))
		darwin.Send(inv, "getArgument:atIndex:", unsafe.Pointer(actual), int64(3))
		if *actual != nil {
			**actual = *rng
		}
		r := new(darwin.CGRect)
		if w != nil {
			*r = w.caretScreenRect()
		}
		darwin.Send(inv, "setReturnValue:", unsafe.Pointer(r))
	case selOpaqueTitleBar:
		r := new(darwin.CGRect)
		*r = w.opaqueTitleBarRect()
		darwin.Send(inv, "setReturnValue:", unsafe.Pointer(r))
	default:
		return false
	}
	return true
}

// nsText returns the text of an NSString or NSAttributedString.
func nsText(obj darwin.ID) string {
	if obj == 0 {
		return ""
	}
	if darwin.Get[bool](obj, "isKindOfClass:", darwin.ID(darwin.Cls("NSAttributedString"))) {
		obj = darwin.Send(obj, "string")
	}
	return darwin.GoString(obj)
}

var nsArrayOf = sync.OnceValue(func() darwin.ID { return darwin.ID(darwin.Cls("NSArray")) })

// imeKey runs a key press through the input method. composing is whether a
// composition was open before it.
func (w *Window) imeKey(ev darwin.ID, composing bool) (passedOn bool) {
	w.ime.inKey, w.ime.command = true, false
	darwin.Send(w.view, "interpretKeyEvents:", darwin.Send(nsArrayOf(), "arrayWithObject:", ev))
	w.ime.inKey = false
	// A key that ends a composition, such as an arrow or Enter, commits the
	// composed text and is then passed on.
	return composing && w.ime.command && w.composition() == ""
}

// composition is the focused Input's open composition, or "".
func (w *Window) composition() string {
	if _, st := w.imeInput(); st != nil {
		return st.comp
	}
	return ""
}

// imeMark shows s as the composition, with the input method's cursor cursor
// UTF-16 units into it.
func (w *Window) imeMark(s string, cursor uint64) {
	_, st := w.imeInput()
	if st == nil {
		return
	}
	st.comp = s
	st.compCursor = len(s)
	if cursor < uint64(utf16Len(s)) {
		st.compCursor = utf16ToByteOffset(s, int(cursor))
	}
	w.resetBlink()
	w.invalidate()
}

// imeInsert types finished text from the input method, which ends the
// composition.
func (w *Window) imeInsert(s string) {
	if _, st := w.imeInput(); st != nil {
		st.comp = ""
	}
	for _, r := range s {
		w.onTypedRune(r)
	}
	w.resetBlink()
	w.invalidate()
}

// finishComposition types the open composition as it stands.
func (w *Window) finishComposition() {
	e, st := w.imeInput()
	if st == nil || st.comp == "" {
		return
	}
	s := st.comp
	st.comp = ""
	w.typeText(e, s)
	w.resetBlink()
	w.invalidate()
}

// commitComposition ends the input method's composition, keeping its text.
func (w *Window) commitComposition() {
	if w.composition() == "" {
		return
	}
	w.finishComposition()
	w.discardMarkedText()
}

// discardMarkedText tells the input method its composition is gone.
func (w *Window) discardMarkedText() {
	if ctx := w.inputContext(); ctx != 0 {
		w.ime.discarding = true
		darwin.Send(ctx, "discardMarkedText")
		w.ime.discarding = false
	}
}

func (w *Window) inputContext() darwin.ID {
	if !w.hasNativeWindow() {
		return 0
	}
	return darwin.Send(w.view, "inputContext")
}

// enableIME turns the input method on for the window or off. Keys only go
// through it while it is on.
func (w *Window) enableIME(on bool) {
	if !on {
		w.discardMarkedText()
	}
}

// updateIMEPosition makes the input method ask where the caret is again.
func (w *Window) updateIMEPosition() {
	if ctx := w.inputContext(); ctx != 0 {
		darwin.Send(ctx, "invalidateCharacterCoordinates")
	}
}

// markedRange is the composition's range in the Input's text, in UTF-16
// units, with the composition in place of the selection.
func (w *Window) markedRange() darwin.NSRange {
	_, st := w.imeInput()
	if st == nil || st.comp == "" {
		return darwin.NSRange{Location: darwin.NSNotFound}
	}
	a, _ := st.Selection()
	return darwin.NSRange{Location: uint64(utf16Len(st.Value[:a])), Length: uint64(utf16Len(st.comp))}
}

// selectedRange is the selection in the Input's text in UTF-16 units, or
// the input method's cursor inside the composition.
func (w *Window) selectedRange() darwin.NSRange {
	_, st := w.imeInput()
	if st == nil {
		return darwin.NSRange{Location: darwin.NSNotFound}
	}
	a, b := st.Selection()
	if st.comp != "" {
		at := utf16Len(st.Value[:a]) + utf16Len(st.comp[:st.compCursor])
		return darwin.NSRange{Location: uint64(at)}
	}
	at := utf16Len(st.Value[:a])
	return darwin.NSRange{Location: uint64(at), Length: uint64(utf16Len(st.Value[a:b]))}
}

// caretScreenRect is the caret in screen coordinates, under which the input
// method shows its candidates.
func (w *Window) caretScreenRect() darwin.CGRect {
	if !w.hasNativeWindow() {
		return darwin.CGRect{}
	}
	r := darwin.Rect(0, 0, 0, 0)
	if _, st := w.imeInput(); st != nil && st.geom != nil {
		c := st.geom.caret
		r = darwin.Rect(float64(c.x), float64(c.y), 0, float64(c.h))
	}
	r = darwin.Get[darwin.CGRect](w.view, "convertRect:toView:", r, darwin.ID(0))
	return darwin.Get[darwin.CGRect](w.nsWindow, "convertRectToScreen:", r)
}

// utf16Len is the length of s in UTF-16 units.
func utf16Len(s string) int {
	n := 0
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		s = s[size:]
		n++
		if r >= 0x10000 {
			n++
		}
	}
	return n
}
