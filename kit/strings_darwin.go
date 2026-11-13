package kit

import "strings"

// macOS names the modifiers by their symbols: ⌃ Control, ⌥ Option, ⌘ Command.
func init() {
	r := strings.NewReplacer("Ctrl", "⌘", "Alt", "⌥", "Win", "⌃")
	for _, s := range []*Strings{&EnglishStrings, &KoreanStrings, &JapaneseStrings, &ChineseStrings, &Messages} {
		s.ShortcutNeedsModifier = r.Replace(s.ShortcutNeedsModifier)
	}
}
