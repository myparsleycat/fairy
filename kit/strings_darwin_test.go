package kit

import (
	"strings"
	"testing"
)

// Option alone can't make a global shortcut on macOS, so the hint names
// only ⌘ and ⌃.
func TestShortcutNeedsModifierDarwin(t *testing.T) {
	for _, s := range []Strings{EnglishStrings, KoreanStrings, JapaneseStrings, ChineseStrings, Messages} {
		m := s.ShortcutNeedsModifier
		if strings.ContainsAny(m, "⌥") || strings.Contains(m, "Alt") || !strings.Contains(m, "⌘") || !strings.Contains(m, "⌃") {
			t.Errorf("hint %q", m)
		}
	}
}
