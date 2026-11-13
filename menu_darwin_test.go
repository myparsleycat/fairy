package fairy

import (
	"testing"

	"github.com/myparsleycat/fairy/keyboard"
)

func TestMenuLabel(t *testing.T) {
	const (
		shift = nsShiftKeyMask
		ctrl  = nsControlKeyMask
		opt   = nsOptionKeyMask
		cmd   = nsCommandKeyMask
	)
	for _, tc := range []struct {
		label, title, key string
		mask              uint64
		ok                bool
	}{
		{"Paste", "Paste", "", 0, false},
		{"Copy\t" + keyboard.MustChord("mod+c").Label(), "Copy", "c", cmd, true},
		{"Redo\t⇧⌘Z", "Redo", "z", shift | cmd, true},
		{"Back\t⌘[", "Back", "[", cmd, true},
		{"Zoom in\t⌘+", "Zoom in", "+", cmd, true},
		{"Send\t⌘↩", "Send", "\r", cmd, true},
		{"Delete\t⌥⌫", "Delete", "\x08", opt, true},
		{"Top\t⌃⇞", "Top", "", ctrl, true},
		{"Reload\tF5", "Reload", "", 0, true},
		{"New\tCtrl+N", "New", "n", ctrl, true},
		{"Tab\tCtrl+Shift+Tab", "Tab", "\t", ctrl | shift, true},
		{"Next\tCtrl+Page Down", "Next", "", ctrl, true},
		{"Find\tcmd+alt+f", "Find", "f", cmd | opt, true},
		{"Palette\tCtrl+Space", "Palette", " ", ctrl, true},
		{"Select\tCtrl+Click", "Select  Ctrl+Click", "", 0, false},
		{"Options\tMenu", "Options  Menu", "", 0, false},
		{"&Open\tCtrl+O", "Open", "o", ctrl, true},
		{"Save && E&xit", "Save & Exit", "", 0, false},
		{"Trailing&", "Trailing", "", 0, false},
		{"Empty\t", "Empty", "", 0, false},
	} {
		title, key, mask, ok := menuLabel(tc.label)
		if title != tc.title || key != tc.key || mask != tc.mask || ok != tc.ok {
			t.Errorf("menuLabel(%q) = %q, %q, %#x, %v; want %q, %q, %#x, %v",
				tc.label, title, key, mask, ok, tc.title, tc.key, tc.mask, tc.ok)
		}
	}
}
