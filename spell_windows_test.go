package fairy

import (
	"reflect"
	"testing"
)

func TestSpellRanges(t *testing.T) {
	old := "teh cat adn dog"
	errs := []spellErr{{0, 3}, {8, 11}}
	for _, tc := range []struct {
		cur  string
		want []spellErr
	}{
		{old, errs},
		{"teh black cat adn dog", []spellErr{{0, 3}, {14, 17}}},
		{"tehh cat adn dog", []spellErr{{9, 12}}},
		{"teh cat and dog", []spellErr{{0, 3}}},
		{"teh cat adn dogs", []spellErr{{0, 3}, {8, 11}}},
	} {
		if got := shiftSpell(old, tc.cur, errs); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("shiftSpell(%q) = %v, want %v", tc.cur, got, tc.want)
		}
	}
}
