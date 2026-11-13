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
	inKey      bool   // interpretKeyEvents: runs for a key press
	command    bool   // the input method passed the key on as a command
	discarding bool   // discardMarkedText runs; its callbacks are ignored
	romanOnly  bool   // a password Input has limited the input sources
	source     string // the input source to go back to after it
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
	add := func(name, types string, fn any) {
		if !c.AddMethod(darwin.Sel(name), objc.NewIMP(fn), types) {
			panic("fairy: add method " + name)
		}
	}
	add("hasMarkedText", "B@:", func(self id, _ sel) bool {
		w := viewWindow(self)
		return w != nil && w.composition() != ""
	})
	add("unmarkText", "v@:", func(self id, _ sel) {
		if w := viewWindow(self); w != nil && !w.ime.discarding {
			w.finishComposition()
		}
	})
	// An empty list makes input methods such as Korean commit every key
	// as it is, without composing.
	add("validAttributesForMarkedText", "@@:", func(id, sel) id {
		arr := darwin.SendClass("NSMutableArray", "array")
		for _, n := range []string{"NSUnderlineStyleAttributeName", "NSUnderlineColorAttributeName", "NSMarkedClauseSegmentAttributeName"} {
			darwin.Send(arr, "addObject:", darwin.SymbolID(darwin.AppKit, n))
		}
		return arr
	})
	add("attributedSubstringForProposedRange:actualRange:", "@@:"+encRange+"^"+encRange,
		func(self id, _ sel, rng darwin.NSRange, actual *darwin.NSRange) id {
			if w := viewWindow(self); w != nil {
				return w.imeSubstring(rng.Location, rng.Length, actual)
			}
			return 0
		})
	add("characterIndexForPoint:", "Q@:{CGPoint=dd}", func(id, sel) uint64 { return darwin.NSNotFound })
	// Input methods such as Korean read the text before the caret and
	// replace it with insertText:'s replacement range.
	add("insertText:replacementRange:", "v@:@"+encRange, func(self id, _ sel, str id, rep darwin.NSRange) {
		if w := viewWindow(self); w != nil && !w.ime.discarding {
			if s, ok := nsText(str); ok {
				w.imeInsert(s, rep.Location, rep.Length)
			}
		}
	})
	// The ranges are structs, not pairs of integers: on amd64 the second
	// one does not fit in the remaining registers and goes on the stack.
	add("setMarkedText:selectedRange:replacementRange:", "v@:@"+encRange+encRange,
		func(self id, _ sel, str id, selected, rep darwin.NSRange) {
			if w := viewWindow(self); w != nil && !w.ime.discarding {
				if s, ok := nsText(str); ok {
					w.imeMark(s, selected.Location, rep.Location, rep.Length)
				}
			}
		})
	add("doCommandBySelector:", "v@::", func(self id, _ sel, cmd sel) {
		if w := viewWindow(self); w != nil {
			w.ime.command = true
		}
	})
	add("forwardInvocation:", "v@:@", func(self id, cmd sel, inv id) {
		if !imeInvocation(viewWindow(self), inv) {
			darwin.SendSuper(self, c, cmd, inv)
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

// nsText returns the text of an NSString or NSAttributedString, or of an
// NSNumber, which assistive apps may set as a value; nil is no text. Other
// objects report false: reading one as a string raises an exception that
// aborts the process.
func nsText(obj darwin.ID) (string, bool) {
	if obj == 0 {
		return "", true
	}
	kind := func(name string) bool {
		return darwin.Get[bool](obj, "isKindOfClass:", darwin.ID(darwin.Cls(name)))
	}
	switch {
	case kind("NSString"):
	case kind("NSAttributedString"):
		obj = darwin.Send(obj, "string")
	case kind("NSNumber"):
		obj = darwin.Send(obj, "stringValue")
	default:
		return "", false
	}
	return darwin.GoString(obj), true
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
// UTF-16 units into it, in place of the text in the replacement range when
// it has one.
func (w *Window) imeMark(s string, cursor, repLoc, repLen uint64) {
	e, st := w.imeInput()
	if st == nil {
		return
	}
	if repLoc != darwin.NSNotFound && st.comp == "" {
		w.selectUTF16(st, repLoc, repLen)
		if st.HasSelection() {
			w.typeText(e, "")
			e, st = w.imeInput()
			if st == nil {
				return
			}
		}
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
// composition. With a replacement range, the text replaces that range. The
// text is typed at once, as on Windows, so dictated line breaks stay and
// the Input changes once.
func (w *Window) imeInsert(s string, repLoc, repLen uint64) {
	e, st := w.imeInput()
	if st == nil {
		for _, r := range s {
			w.onTypedRune(r)
		}
	} else {
		replace := repLoc != darwin.NSNotFound && st.comp == ""
		st.comp = ""
		if replace {
			w.selectUTF16(st, repLoc, repLen)
		}
		w.typeText(e, s)
	}
	w.resetBlink()
	w.invalidate()
}

// selectUTF16 selects the range of the Input's text at loc of n UTF-16
// units, clamped to the text.
func (w *Window) selectUTF16(st *editState, loc, n uint64) {
	total := uint64(utf16Len(st.Value))
	loc = min(loc, total)
	st.Anchor = utf16ToByteOffset(st.Value, int(loc))
	st.Caret = utf16ToByteOffset(st.Value, int(loc+min(n, total-loc)))
	st.HasPreferredX = false
}

// imeSubstring returns the Input's text in a UTF-16 range as an
// NSAttributedString, and the part of the range it covers in actual.
func (w *Window) imeSubstring(loc, n uint64, actual *darwin.NSRange) darwin.ID {
	_, st := w.imeInput()
	if st == nil || st.comp != "" {
		return 0
	}
	total := uint64(utf16Len(st.Value))
	if loc > total {
		return 0
	}
	end := loc + min(n, total-loc) // n may be near NSUIntegerMax
	if actual != nil {
		*actual = darwin.NSRange{Location: loc, Length: end - loc}
	}
	text := st.Value[utf16ToByteOffset(st.Value, int(loc)):utf16ToByteOffset(st.Value, int(end))]
	str := darwin.Send(darwin.SendClass("NSAttributedString", "alloc"), "initWithString:", darwin.String(text))
	return darwin.Send(str, "autorelease")
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

// securePassword lets a focused password Input take only Roman input
// sources, as NSSecureTextField does, so that keys type the letters on
// them; other focus allows every source again.
func (w *Window) securePassword(e *El) {
	password := e != nil && e.kind == kindInput && e.input.password
	ctx := w.inputContext()
	if ctx == 0 || password == w.ime.romanOnly {
		return
	}
	w.ime.romanOnly = password
	if password {
		w.ime.source = darwin.GoString(darwin.Send(ctx, "selectedKeyboardInputSource"))
		darwin.Send(ctx, "setAllowedInputSourceLocales:", darwin.SendClass("NSArray", "arrayWithObject:",
			darwin.SymbolID(darwin.AppKit, "NSAllRomanInputSourcesLocaleIdentifier")))
		return
	}
	darwin.Send(ctx, "setAllowedInputSourceLocales:", darwin.ID(0))
	// Go back to the input source the user had before the password.
	if w.ime.source != "" {
		darwin.Send(ctx, "setSelectedKeyboardInputSource:", darwin.String(w.ime.source))
		w.ime.source = ""
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
