//go:build !darwin

package keyboard

// ModPrimary is the modifier of the platform's shortcuts, such as copy and
// save: Command on macOS, Ctrl elsewhere.
const ModPrimary = ModCtrl

// ModWord moves the caret and deletes by words with the arrow keys and
// Backspace: Option on macOS, Ctrl elsewhere.
const ModWord = ModCtrl

// modGlobal are the modifiers that keep a global shortcut off keys other
// programs type with: Ctrl, Alt and Win.
const modGlobal = ModCtrl | ModAlt | ModSuper

var superNames = []string{"super", "win", "cmd", "command", "meta", "logo"}

// modLabels are the labels of chordMods.
var modLabels = [4]string{"Ctrl", "Alt", "Shift", "Win"}

const labelSep = "+"

func platformKeyLabel(k Key) (string, bool) { return "", false }
