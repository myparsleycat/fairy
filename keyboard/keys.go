// Package keyboard defines keys, modifier sets, key events and shortcut
// chords.
package keyboard

// Mods is a set of held modifier keys. On macOS, Control is ModCtrl, Option
// is ModAlt and Command is ModSuper. Bind app commands to ModPrimary, which
// is the modifier the platform's own shortcuts use.
type Mods uint8

const (
	ModShift Mods = 1 << iota
	ModCtrl
	ModAlt
	ModSuper
)

// Key identifies a physical key by its Windows virtual-key code.
type Key uint16

const (
	KeyBackspace Key = 0x08
	KeyTab       Key = 0x09
	KeyEnter     Key = 0x0D
	KeyEscape    Key = 0x1B
	KeySpace     Key = 0x20
	KeyPageUp    Key = 0x21
	KeyPageDown  Key = 0x22
	KeyEnd       Key = 0x23
	KeyHome      Key = 0x24
	KeyLeft      Key = 0x25
	KeyUp        Key = 0x26
	KeyRight     Key = 0x27
	KeyDown      Key = 0x28
	KeyInsert    Key = 0x2D
	KeyDelete    Key = 0x2E
	KeyA         Key = 'A'
	KeyC         Key = 'C'
	KeyV         Key = 'V'
	KeyX         Key = 'X'
	KeyY         Key = 'Y'
	KeyZ         Key = 'Z'
	KeyF1        Key = 0x70
	KeyF5        Key = 0x74
	KeyF12       Key = 0x7B
	// KeyBrowserBack and KeyBrowserForward also arrive from the mouse's back
	// and forward (X1, X2) buttons.
	KeyBrowserBack    Key = 0xA6
	KeyBrowserForward Key = 0xA7
)

// KeyEvent is a key press. Text input arrives through Input elements; Text
// only reports what the key types, for type-select in lists and menus.
type KeyEvent struct {
	Key    Key
	Mods   Mods
	Repeat bool
	// Text is the character the key types with the current keyboard layout
	// and modifiers, such as "a", "A" or "ㅁ", or "" for keys that type
	// nothing and while an input method composes.
	Text string
}

// Is reports whether the event is key with exactly mods held.
func (k KeyEvent) Is(key Key, mods Mods) bool { return k.Key == key && k.Mods == mods }
