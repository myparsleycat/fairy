package fairy

import (
	"errors"
	"strings"
	"sync"

	"github.com/ebitengine/purego/objc"

	"github.com/myparsleycat/fairy/internal/darwin"
	"github.com/myparsleycat/fairy/keyboard"
)

// menuChoices maps the target of an open menu to where its chosen item's tag
// goes.
var menuChoices = map[darwin.ID]*int64{}

// menuTargetClass is the target of popup menu items. Its action records the
// tag of the chosen item, whose OnClick runs once the menu has closed.
var menuTargetClass = sync.OnceValue(func() darwin.Class {
	c, err := objc.RegisterClass("FairyMenuTarget", darwin.Cls("NSObject"), nil, nil, []objc.MethodDef{
		{
			Cmd: darwin.Sel("fairyMenuItem:"),
			Fn: func(self darwin.ID, _ darwin.SEL, item darwin.ID) {
				if p := menuChoices[self]; p != nil {
					*p = darwin.Get[int64](item, "tag")
				}
			},
		},
	})
	if err != nil {
		panic(err)
	}
	return c
})

// NSEventModifierFlags of key equivalents.
const (
	nsShiftKeyMask   = 1 << 17
	nsControlKeyMask = 1 << 18
	nsOptionKeyMask  = 1 << 19
	nsCommandKeyMask = 1 << 20
)

// buildNSMenu makes an NSMenu of items whose item tags index actions, from
// 1. The caller releases the menu.
func buildNSMenu(items []MenuItem, actions *[]func(), target darwin.ID) darwin.ID {
	m := darwin.Send(darwin.SendClass("NSMenu", "alloc"), "initWithTitle:", darwin.String(""))
	darwin.Send(m, "setAutoenablesItems:", false)
	for _, it := range items {
		if it.Separator {
			darwin.Send(m, "addItem:", darwin.SendClass("NSMenuItem", "separatorItem"))
			continue
		}
		title, key, mask, ok := menuLabel(it.Label)
		item := darwin.Send(darwin.SendClass("NSMenuItem", "alloc"), "initWithTitle:action:keyEquivalent:",
			darwin.String(title), darwin.SEL(0), darwin.String(""))
		if ok {
			darwin.Send(item, "setKeyEquivalent:", darwin.String(key))
			darwin.Send(item, "setKeyEquivalentModifierMask:", mask)
		}
		if len(it.Items) > 0 {
			sub := buildNSMenu(it.Items, actions, target)
			darwin.Send(item, "setSubmenu:", sub)
			darwin.Release(sub)
		} else {
			*actions = append(*actions, it.OnClick)
			darwin.Send(item, "setTag:", int64(len(*actions)))
			darwin.Send(item, "setTarget:", target)
			darwin.Send(item, "setAction:", darwin.Sel("fairyMenuItem:"))
			if it.Checked {
				darwin.Send(item, "setState:", int64(1)) // NSControlStateValueOn
			}
		}
		darwin.Send(item, "setEnabled:", !it.Disabled)
		darwin.Send(m, "addItem:", item)
		darwin.Release(item)
	}
	return m
}

// showNSMenu shows items as a menu with its top left corner at p, in view's
// coordinates or, when view is 0, in screen coordinates. It returns after the
// menu closes and then calls the chosen item's OnClick.
func showNSMenu(view darwin.ID, p darwin.CGPoint, items []MenuItem) {
	if len(items) == 0 {
		return
	}
	menuTargetClass()
	var run func()
	darwin.WithPool(func() {
		target := darwin.New("FairyMenuTarget")
		var chosen int64
		menuChoices[target] = &chosen
		var actions []func()
		m := buildNSMenu(items, &actions, target)
		darwin.Get[bool](m, "popUpMenuPositioningItem:atLocation:inView:", darwin.ID(0), p, view)
		delete(menuChoices, target)
		darwin.Release(m)
		darwin.Release(target)
		if chosen > 0 && int(chosen) <= len(actions) {
			run = actions[chosen-1]
		}
	})
	if run != nil {
		run()
	}
}

// PopupMenu shows a native menu at (x, y) in the window's logical points and
// runs the chosen item. It returns after the menu closes. Call it on the UI
// thread, typically from OnRightClick with the event's position.
func (w *Window) PopupMenu(x, y float32, items []MenuItem) error {
	if !onUIThread() {
		return errors.New("fairy: PopupMenu must run on the UI thread")
	}
	if !w.hasNativeWindow() {
		return errors.New("fairy: PopupMenu needs an open window")
	}
	w.cancelPress()
	w.popupMenu(x, y, items)
	w.Update()
	return nil
}

// ShowMenu shows a native menu at screen point (x, y), in points with y
// measured down from the top of the main screen as in Window.Bounds, runs the
// chosen item and then updates every window. It returns after the menu
// closes. Use it for menus not tied to a window, such as a tray icon's; a
// window's own menus use Window.PopupMenu. Call it on the UI thread.
func ShowMenu(x, y int, items []MenuItem) error {
	if !onUIThread() {
		return errors.New("fairy: ShowMenu must run on the UI thread")
	}
	showNSMenu(0, darwin.CGPoint{X: float64(x), Y: mainScreenHeight() - float64(y)}, items)
	updateWindows()
	return nil
}

// popupMenu shows items as a native menu at window point (x, y) and waits
// until it closes.
func (w *Window) popupMenu(x, y float32, items []MenuItem) {
	if !w.hasNativeWindow() {
		return
	}
	// The view is flipped, so its coordinates are the window's logical ones.
	showNSMenu(w.view, darwin.CGPoint{X: float64(x), Y: float64(y)}, items)
}

// menuLabel splits a MenuItem label into the title a macOS menu shows and,
// when the text after a tab names a shortcut, its key equivalent and
// modifier mask. Text after a tab that is no shortcut stays in the title.
// Windows "&" mnemonic markers are removed from the title.
func menuLabel(label string) (title, key string, mask uint64, ok bool) {
	title, shortcut, _ := strings.Cut(label, "\t")
	title = stripMnemonics(title)
	shortcut = strings.TrimSpace(shortcut)
	if shortcut == "" {
		return title, "", 0, false
	}
	if c, found := parseMenuShortcut(shortcut); found {
		if key, mask, ok = keyEquivalent(c); ok {
			return title, key, mask, true
		}
	}
	return title + "  " + shortcut, "", 0, false
}

// stripMnemonics removes the "&" that marks a mnemonic in a Windows menu
// label, keeping the "&" that "&&" writes.
func stripMnemonics(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '&' {
			if i+1 < len(s) && s[i+1] == '&' {
				b.WriteByte('&')
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// macKeySymbols are the key names of the symbols keyboard.Chord.Label draws
// on macOS.
var macKeySymbols = map[string]string{
	"↩": "enter", "⇥": "tab", "⎋": "esc", "⌫": "backspace", "⌦": "delete",
	"↖": "home", "↘": "end", "⇞": "pageup", "⇟": "pagedown",
	"←": "left", "→": "right", "↑": "up", "↓": "down", "+": "plus",
}

// macModSymbols are the modifier symbols keyboard.Chord.Label draws on macOS.
var macModSymbols = map[string]keyboard.Mods{
	"⌃": keyboard.ModCtrl, "⌥": keyboard.ModAlt, "⇧": keyboard.ModShift, "⌘": keyboard.ModSuper,
}

// parseMenuShortcut reads the shortcut text of a menu label: a chord as
// keyboard.ParseChord reads it ("ctrl+shift+t", "Ctrl+Page Up"), or as
// keyboard.Chord.Label writes it on macOS ("⇧⌘T", "⌘↩", "F5").
func parseMenuShortcut(s string) (keyboard.Chord, bool) {
	if c, err := keyboard.ParseChord(s); err == nil {
		return c, true
	}
	if c, err := keyboard.ParseChord(strings.ReplaceAll(s, " ", "")); err == nil {
		return c, true
	}
	var mods keyboard.Mods
	rest := s
	for more := true; more; {
		more = false
		for sym, m := range macModSymbols {
			if r, found := strings.CutPrefix(rest, sym); found {
				mods |= m
				rest, more = r, true
			}
		}
	}
	if rest == "" {
		return keyboard.Chord{}, false
	}
	name, ok := macKeySymbols[rest]
	if !ok {
		name = strings.ReplaceAll(rest, " ", "")
	}
	c, err := keyboard.ParseChord(name)
	if err != nil {
		return keyboard.Chord{}, false
	}
	c.Mods |= mods
	return c, true
}

// keyEquivalentChars are the key equivalents of keys that type no character.
var keyEquivalentChars = map[keyboard.Key]rune{
	keyboard.KeyEnter:     '\r',
	keyboard.KeyTab:       '\t',
	keyboard.KeyEscape:    0x1b,
	keyboard.KeyBackspace: 0x08,
	keyboard.KeySpace:     ' ',
	keyboard.KeyDelete:    0xF728, // NSDeleteFunctionKey
	keyboard.KeyInsert:    0xF727,
	keyboard.KeyHome:      0xF729,
	keyboard.KeyEnd:       0xF72B,
	keyboard.KeyPageUp:    0xF72C,
	keyboard.KeyPageDown:  0xF72D,
	keyboard.KeyUp:        0xF700,
	keyboard.KeyDown:      0xF701,
	keyboard.KeyLeft:      0xF702,
	keyboard.KeyRight:     0xF703,
	0x2C:                  0xF72E, // print screen
	0x91:                  0xF72F, // scroll lock
	0x13:                  0xF730, // pause
}

// keyEquivalent returns the NSMenuItem key equivalent and modifier mask of a
// chord, and false for keys a menu cannot show.
func keyEquivalent(c keyboard.Chord) (string, uint64, bool) {
	var key string
	if r := c.Rune(); r != 0 {
		key = string(r)
	} else {
		var ch rune
		switch k := c.Key; {
		case k >= keyboard.KeyF1 && k < keyboard.KeyF1+24:
			ch = 0xF704 + rune(k-keyboard.KeyF1) // NSF1FunctionKey
		default:
			ch = keyEquivalentChars[k]
		}
		if ch == 0 {
			return "", 0, false
		}
		key = string(ch)
	}
	var mask uint64
	if c.Mods&keyboard.ModShift != 0 {
		mask |= nsShiftKeyMask
	}
	if c.Mods&keyboard.ModCtrl != 0 {
		mask |= nsControlKeyMask
	}
	if c.Mods&keyboard.ModAlt != 0 {
		mask |= nsOptionKeyMask
	}
	if c.Mods&keyboard.ModSuper != 0 {
		mask |= nsCommandKeyMask
	}
	return key, mask, true
}
