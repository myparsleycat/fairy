package keyboard

// ModPrimary is the modifier of the platform's shortcuts, such as copy and
// save: Command on macOS, Ctrl elsewhere.
const ModPrimary = ModSuper

// ModWord moves the caret and deletes by words with the arrow keys and
// Backspace: Option on macOS, Ctrl elsewhere.
const ModWord = ModAlt

// modGlobal are the modifiers that keep a global shortcut off keys other
// programs type with: Control and Command. Option types characters on
// macOS, as ⌥E starts an accent.
const modGlobal = ModCtrl | ModSuper

var superNames = []string{"cmd", "command", "super", "win", "meta", "logo"}

// modLabels are the labels of chordMods.
var modLabels = [4]string{"⌃", "⌥", "⇧", "⌘"}

const labelSep = ""

// platformKeyLabel is the symbol macOS draws for a key in menus.
func platformKeyLabel(k Key) (string, bool) {
	switch k {
	case KeyEnter:
		return "↩", true
	case KeyTab:
		return "⇥", true
	case KeyEscape:
		return "⎋", true
	case KeyBackspace:
		return "⌫", true
	case KeyDelete:
		return "⌦", true
	case KeyHome:
		return "↖", true
	case KeyEnd:
		return "↘", true
	case KeyPageUp:
		return "⇞", true
	case KeyPageDown:
		return "⇟", true
	}
	return "", false
}
