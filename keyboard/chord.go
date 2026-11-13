package keyboard

import (
	"fmt"
	"strings"
	"unicode"
)

// Chord is a key with modifiers, as an app binds it: ParseChord("ctrl+shift+t").
// Parsing is case-insensitive and ignores spaces around each "+". Modifiers
// are mod (alias primary: ModPrimary, ⌘ on macOS and Ctrl elsewhere), ctrl
// (control), alt (option, opt), shift and super (win, cmd, command, meta,
// logo; ⌘ on macOS). Keys are a character or a name:
// enter (return), tab, escape (esc), backspace, delete (del), insert (ins),
// home, end, pageup (pgup), pagedown (pgdn), left/right/up/down (arrowleft,
// …), space, plus (or a trailing "+": "ctrl++"), comma, the punctuation names
// (minus, equal, period, slash, backslash, backquote, semicolon, quote,
// bracketleft, bracketright), f1–f24, contextmenu (menu), printscreen
// (prtsc), scrolllock and pause (break). A character is the one the key types
// without Shift on a US layout; "{" and "}" read as Shift with "[" and "]".
// String writes the canonical form back ("ctrl+shift+t", "cmd+shift+t" on
// macOS) for settings files.
type Chord struct {
	Mods Mods
	Key  Key
	ch   rune // the character the chord names, for char keys parsed from text
}

// Virtual-key codes of keys a chord can name beyond the Key constants.
const (
	vkPause       Key = 0x13
	vkPrintScreen Key = 0x2C
	vkApps        Key = 0x5D
	vkAdd         Key = 0x6B
	vkScrollLock  Key = 0x91
	vkOEMPlus     Key = 0xBB
)

// chordMods are the modifiers in the order labels and String write them,
// which is also the order macOS draws them in: ⌃⌥⇧⌘.
var chordMods = []struct {
	mod   Mods
	names []string
}{
	{ModCtrl, []string{"ctrl", "control"}},
	{ModAlt, []string{"alt", "option", "opt"}},
	{ModShift, []string{"shift"}},
	{ModSuper, superNames},
}

var chordKeys = []struct {
	key   Key
	names []string
	label string
}{
	{KeyEnter, []string{"enter", "return"}, "Enter"},
	{KeyTab, []string{"tab"}, "Tab"},
	{KeyEscape, []string{"escape", "esc"}, "Esc"},
	{KeyBackspace, []string{"backspace"}, "Backspace"},
	{KeyDelete, []string{"delete", "del"}, "Delete"},
	{KeyInsert, []string{"insert", "ins"}, "Insert"},
	{KeyHome, []string{"home"}, "Home"},
	{KeyEnd, []string{"end"}, "End"},
	{KeyPageUp, []string{"pageup", "pgup"}, "Page Up"},
	{KeyPageDown, []string{"pagedown", "pgdn"}, "Page Down"},
	{KeyLeft, []string{"left", "arrowleft"}, "←"},
	{KeyRight, []string{"right", "arrowright"}, "→"},
	{KeyUp, []string{"up", "arrowup"}, "↑"},
	{KeyDown, []string{"down", "arrowdown"}, "↓"},
	{vkApps, []string{"contextmenu", "menu"}, "Menu"},
	{vkPrintScreen, []string{"printscreen", "prtsc"}, "Print Screen"},
	{vkScrollLock, []string{"scrolllock"}, "Scroll Lock"},
	{vkPause, []string{"pause", "break"}, "Pause"},
}

// chordChars are the characters a chord can name, their virtual keys on a US
// layout and their names. String writes the name only for space, "+" and ",",
// which the syntax itself uses.
var chordChars = []struct {
	ch   rune
	key  Key
	name string
}{
	{' ', KeySpace, "space"}, {'+', vkOEMPlus, "plus"}, {',', 0xBC, "comma"}, {'-', 0xBD, "minus"},
	{'=', vkOEMPlus, "equal"}, {'.', 0xBE, "period"}, {'/', 0xBF, "slash"}, {'\\', 0xDC, "backslash"},
	{'`', 0xC0, "backquote"}, {';', 0xBA, "semicolon"}, {'\'', 0xDE, "quote"},
	{'[', 0xDB, "bracketleft"}, {']', 0xDD, "bracketright"},
}

// ParseChord reads a chord such as "ctrl+shift+t" or "alt+f4".
func ParseChord(s string) (Chord, error) {
	invalid := fmt.Errorf("fairy: invalid key chord: %q", s)
	parts := strings.Split(s, "+")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	// Two empty parts at the end are the plus key: "ctrl++", "ctrl + +", "+".
	var c Chord
	hasKey, plus := false, false
	if n := len(parts); n >= 2 && parts[n-1] == "" && parts[n-2] == "" {
		parts = parts[:n-2]
		if len(parts) == 1 && parts[0] == "" {
			parts = nil
		}
		plus = true
	}
	for _, p := range parts {
		p = strings.ToLower(p)
		if m, ok := chordMod(p); ok {
			if hasKey {
				return Chord{}, invalid
			}
			c.Mods |= m
			continue
		}
		if hasKey {
			return Chord{}, invalid
		}
		k, ok := chordKey(p)
		if !ok {
			return Chord{}, invalid
		}
		k.Mods |= c.Mods
		c, hasKey = k, true
	}
	if plus {
		if hasKey {
			return Chord{}, invalid
		}
		c, hasKey = Chord{Mods: c.Mods, Key: vkOEMPlus, ch: '+'}, true
	}
	if !hasKey {
		return Chord{}, invalid
	}
	return c, nil
}

// MustChord parses a chord written in the app's code and panics if it does
// not parse. Parse user input with ParseChord.
func MustChord(s string) Chord {
	c, err := ParseChord(s)
	if err != nil {
		panic(err)
	}
	return c
}

func chordMod(name string) (Mods, bool) {
	if name == "mod" || name == "primary" {
		return ModPrimary, true
	}
	for _, m := range chordMods {
		for _, n := range m.names {
			if n == name {
				return m.mod, true
			}
		}
	}
	return 0, false
}

func chordKey(name string) (Chord, bool) {
	for _, k := range chordKeys {
		for _, n := range k.names {
			if n == name {
				return Chord{Key: k.key}, true
			}
		}
	}
	if len(name) >= 2 && name[0] == 'f' {
		var n int
		if _, err := fmt.Sscanf(name[1:], "%d", &n); err == nil && n >= 1 && n <= 24 && fmt.Sprint(n) == name[1:] {
			return Chord{Key: KeyF1 + Key(n-1)}, true
		}
	}
	for _, c := range chordChars {
		if c.name == name {
			return Chord{Key: c.key, ch: c.ch}, true
		}
	}
	r := []rune(name)
	if len(r) != 1 {
		return Chord{}, false
	}
	switch ch := r[0]; {
	case ch >= 'a' && ch <= 'z':
		return Chord{Key: Key(unicode.ToUpper(ch)), ch: ch}, true
	case ch >= '0' && ch <= '9':
		return Chord{Key: Key(ch), ch: ch}, true
	case ch == '{':
		return Chord{Mods: ModShift, Key: 0xDB, ch: '['}, true
	case ch == '}':
		return Chord{Mods: ModShift, Key: 0xDD, ch: ']'}, true
	default:
		for _, c := range chordChars {
			if c.ch == ch {
				return Chord{Key: c.key, ch: ch}, true
			}
		}
	}
	return Chord{}, false
}

// ChordOf returns the chord a key press makes, as a shortcut recorder reads
// it: the key with the modifiers held. Letters, digits and punctuation are
// named by the character they type without Shift on a US layout, so the
// chord equals the one ParseChord reads from its String.
func ChordOf(ev KeyEvent) Chord {
	c := Chord{Mods: ev.Mods, Key: ev.Key}
	switch {
	case ev.Key >= 'A' && ev.Key <= 'Z':
		c.ch = rune(ev.Key) + ('a' - 'A')
	case ev.Key >= '0' && ev.Key <= '9':
		c.ch = rune(ev.Key)
	default:
		for _, ch := range chordChars {
			if ch.key == ev.Key && ch.ch != '+' {
				c.ch = ch.ch
				break
			}
		}
	}
	return c
}

// Matches reports whether ev is this chord: the same modifiers held, no more
// and no fewer, and the same key. A chord on "+" also matches with the Shift
// that types it on a US layout, and the numeric keypad's plus.
func (c Chord) Matches(ev KeyEvent) bool {
	if c.ch == '+' {
		return (ev.Key == vkOEMPlus || ev.Key == vkAdd) && (ev.Mods == c.Mods || ev.Mods == c.Mods|ModShift)
	}
	return ev.Key == c.Key && ev.Mods == c.Mods
}

// CanBeGlobal reports whether c can be a global shortcut without taking a
// key other programs type with: its key is F1–F24, or it has Ctrl, Alt or
// Win, which on macOS means Control or Command, since Option types
// characters there.
func (c Chord) CanBeGlobal() bool {
	return c.Key >= KeyF1 && c.Key < KeyF1+24 || c.Mods&modGlobal != 0
}

// Keycaps returns one label per key, modifiers first in Ctrl, Alt, Shift, Win
// order (⌃, ⌥, ⇧, ⌘ on macOS), for drawing each as a keycap.
func (c Chord) Keycaps() []string {
	var caps []string
	for i, m := range chordMods {
		if c.Mods&m.mod != 0 {
			caps = append(caps, modLabels[i])
		}
	}
	return append(caps, c.keyLabel())
}

// Label is how the chord reads: "Ctrl+Shift+T", or "⇧⌘T" on macOS.
func (c Chord) Label() string { return strings.Join(c.Keycaps(), labelSep) }

func (c Chord) keyLabel() string {
	if s, ok := platformKeyLabel(c.Key); ok && c.ch != ' ' {
		return s
	}
	switch {
	case c.ch == ' ' || c.Key == KeySpace:
		return "Space"
	case c.ch != 0:
		return string(unicode.ToUpper(c.ch))
	case c.Key >= KeyF1 && c.Key < KeyF1+24:
		return fmt.Sprintf("F%d", c.Key-KeyF1+1)
	case c.Key >= 'A' && c.Key <= 'Z' || c.Key >= '0' && c.Key <= '9':
		return string(rune(c.Key))
	}
	for _, k := range chordKeys {
		if k.key == c.Key {
			return k.label
		}
	}
	for _, ch := range chordChars {
		if ch.key == c.Key {
			return string(ch.ch)
		}
	}
	return fmt.Sprintf("0x%02X", uint16(c.Key))
}

// String is the canonical form ParseChord reads back: modifiers in ctrl, alt,
// shift, super order, then the key ("ctrl+shift+t", "ctrl+comma").
func (c Chord) String() string {
	var b strings.Builder
	for _, m := range chordMods {
		if c.Mods&m.mod != 0 {
			b.WriteString(m.names[0])
			b.WriteByte('+')
		}
	}
	switch {
	case c.ch == ' ' || c.ch == '+' || c.ch == ',':
		for _, ch := range chordChars {
			if ch.ch == c.ch {
				b.WriteString(ch.name)
			}
		}
	case c.ch != 0:
		b.WriteRune(c.ch)
	default:
		b.WriteString(strings.ToLower(strings.ReplaceAll(c.keyName(), " ", "")))
	}
	return b.String()
}

func (c Chord) keyName() string {
	for _, k := range chordKeys {
		if k.key == c.Key {
			return k.names[0]
		}
	}
	for _, ch := range chordChars {
		if ch.key == c.Key {
			if ch.ch == '+' || ch.ch == ' ' || ch.ch == ',' {
				continue
			}
			return string(ch.ch)
		}
	}
	return c.keyLabel()
}

// Keybind is a command's chords: any of several alternatives.
type Keybind []Chord

// ParseKeybind reads chords separated by commas: "ctrl+t, ctrl+n". "" and
// "none" bind nothing.
func ParseKeybind(s string) (Keybind, error) {
	t := strings.TrimSpace(s)
	if t == "" || strings.EqualFold(t, "none") {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	var kb Keybind
	for i := 0; i < len(parts); i++ {
		p := parts[i]
		// "ctrl+," splits into "ctrl+" and "": the comma is the key.
		if strings.HasSuffix(strings.TrimSpace(p), "+") && i+1 < len(parts) && strings.TrimSpace(parts[i+1]) == "" {
			p += ","
			i++
		}
		c, err := ParseChord(p)
		if err != nil {
			return nil, err
		}
		kb = append(kb, c)
	}
	return kb, nil
}

// MustKeybind parses a keybind written in the app's code and panics if it
// does not parse.
func MustKeybind(s string) Keybind {
	kb, err := ParseKeybind(s)
	if err != nil {
		panic(err)
	}
	return kb
}

// Matches reports whether ev is any of the chords.
func (k Keybind) Matches(ev KeyEvent) bool {
	for _, c := range k {
		if c.Matches(ev) {
			return true
		}
	}
	return false
}

// Label is the first chord's label, or "" when nothing is bound.
func (k Keybind) Label() string {
	if len(k) == 0 {
		return ""
	}
	return k[0].Label()
}

// String is the form ParseKeybind reads back: "ctrl+t, ctrl+n", or "none".
func (k Keybind) String() string {
	if len(k) == 0 {
		return "none"
	}
	s := make([]string, len(k))
	for i, c := range k {
		s[i] = c.String()
	}
	return strings.Join(s, ", ")
}

// Rune returns the canonical US-layout character, or zero for a non-character key.
func (c Chord) Rune() rune { return c.ch }
