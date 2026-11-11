package keyboard

import "testing"

func TestParseChord(t *testing.T) {
	for _, c := range []struct {
		in, label, canon string
		key              Key
		mods             Mods
	}{
		{"ctrl+shift+t", "Ctrl+Shift+T", "ctrl+shift+t", 'T', ModCtrl | ModShift},
		{"Mod + B", "Ctrl+B", "ctrl+b", 'B', ModCtrl},
		{"alt+f4", "Alt+F4", "alt+f4", KeyF1 + 3, ModAlt},
		{"ctrl++", "Ctrl++", "ctrl+plus", vkOEMPlus, ModCtrl},
		{"ctrl+plus", "Ctrl++", "ctrl+plus", vkOEMPlus, ModCtrl},
		{"ctrl+,", "Ctrl+,", "ctrl+comma", 0xBC, ModCtrl},
		{"ctrl+{", "Ctrl+Shift+[", "ctrl+shift+[", 0xDB, ModCtrl | ModShift},
		{"win+space", "Win+Space", "super+space", KeySpace, ModSuper},
		{"shift+pgdn", "Shift+Page Down", "shift+pagedown", KeyPageDown, ModShift},
		{"esc", "Esc", "escape", KeyEscape, 0},
		{"ctrl+1", "Ctrl+1", "ctrl+1", '1', ModCtrl},
	} {
		ch, err := ParseChord(c.in)
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if ch.Key != c.key || ch.Mods != c.mods {
			t.Errorf("%q = key %#x mods %v, want %#x %v", c.in, ch.Key, ch.Mods, c.key, c.mods)
		}
		if got := ch.Label(); got != c.label {
			t.Errorf("%q label = %q, want %q", c.in, got, c.label)
		}
		if got := ch.String(); got != c.canon {
			t.Errorf("%q string = %q, want %q", c.in, got, c.canon)
		}
		if back, err := ParseChord(ch.String()); err != nil || back != ch {
			t.Errorf("%q does not read back from %q: %v %v", c.in, ch.String(), back, err)
		}
	}
	for _, bad := range []string{"", "ctrl", "ctrl+t+u", "t+ctrl", "ctrl+f25", "ctrl+!", "hyper+t"} {
		if _, err := ParseChord(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestChordMatches(t *testing.T) {
	ev := func(k Key, m Mods) KeyEvent { return KeyEvent{Key: k, Mods: m} }
	for _, c := range []struct {
		chord string
		ev    KeyEvent
		want  bool
	}{
		{"ctrl+t", ev('T', ModCtrl), true},
		{"ctrl+t", ev('T', ModCtrl|ModShift), false},
		{"ctrl+t", ev('T', 0), false},
		{"ctrl+plus", ev(vkOEMPlus, ModCtrl|ModShift), true},
		{"ctrl+plus", ev(vkOEMPlus, ModCtrl), true},
		{"ctrl+plus", ev(vkAdd, ModCtrl), true},
		{"ctrl+equal", ev(vkOEMPlus, ModCtrl|ModShift), false},
		{"shift+tab", ev(KeyTab, ModShift), true},
	} {
		if got := MustChord(c.chord).Matches(c.ev); got != c.want {
			t.Errorf("%q matches %+v = %v, want %v", c.chord, c.ev, got, c.want)
		}
	}
}

func TestParseKeybind(t *testing.T) {
	kb, err := ParseKeybind("ctrl+t, ctrl+, ,ctrl+n")
	if err != nil {
		t.Fatal(err)
	}
	if got := kb.String(); got != "ctrl+t, ctrl+comma, ctrl+n" {
		t.Errorf("keybind = %q", got)
	}
	if !kb.Matches(KeyEvent{Key: 'N', Mods: ModCtrl}) || kb.Label() != "Ctrl+T" {
		t.Errorf("keybind %v does not match Ctrl+N or label Ctrl+T", kb)
	}
	if kb, err := ParseKeybind("none"); err != nil || len(kb) != 0 || kb.String() != "none" {
		t.Errorf("none = %v, %v", kb, err)
	}
}

// A recorded key press round-trips through String and ParseChord, so a
// shortcut saved to settings reads back as the same chord.
func TestChordOfRoundTrip(t *testing.T) {
	for _, ev := range []KeyEvent{
		{Key: 'K', Mods: ModCtrl | ModShift},
		{Key: '5', Mods: ModAlt},
		{Key: 0xBC, Mods: ModCtrl}, // comma
		{Key: vkOEMPlus, Mods: ModCtrl},
		{Key: KeySpace, Mods: ModCtrl | ModAlt},
		{Key: KeyF1 + 4},
		{Key: KeyPageDown, Mods: ModSuper},
	} {
		c := ChordOf(ev)
		back, err := ParseChord(c.String())
		if err != nil {
			t.Errorf("%v: ParseChord(%q): %v", ev, c.String(), err)
			continue
		}
		if back != c || !back.Matches(ev) {
			t.Errorf("%v: %q reads back as %+v, want %+v", ev, c.String(), back, c)
		}
	}
}
