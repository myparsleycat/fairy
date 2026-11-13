package kit

// macOS names the modifiers by their symbols: ⌃ Control, ⌘ Command. A
// global shortcut needs one of them; Option alone types characters there.
func init() {
	mac := map[string]string{
		EnglishStrings.ShortcutNeedsModifier:  "Add ⌘ or ⌃.",
		KoreanStrings.ShortcutNeedsModifier:   "⌘, ⌃ 중 하나를 함께 누르세요.",
		JapaneseStrings.ShortcutNeedsModifier: "⌘、⌃ のいずれかを一緒に押してください。",
		ChineseStrings.ShortcutNeedsModifier:  "请同时按下 ⌘ 或 ⌃。",
	}
	for _, s := range []*Strings{&EnglishStrings, &KoreanStrings, &JapaneseStrings, &ChineseStrings, &Messages} {
		if m, ok := mac[s.ShortcutNeedsModifier]; ok {
			s.ShortcutNeedsModifier = m
		}
	}
}
