package kit

import (
	"testing"

	"github.com/myparsleycat/fairy/keyboard"
)

func TestBarStep(t *testing.T) {
	left, right := keyboard.KeyEvent{Key: keyboard.KeyLeft}, keyboard.KeyEvent{Key: keyboard.KeyRight}
	tests := []struct {
		name    string
		depth   int
		submenu bool
		key     keyboard.KeyEvent
		want    int
	}{
		{"Left in the menu", 0, false, left, -1},
		{"Left on a submenu row of the menu", 0, true, left, -1},
		{"Left in a submenu closes it", 1, false, left, 0},
		{"Right in the menu", 0, false, right, 1},
		{"Right on a submenu row opens it", 0, true, right, 0},
		{"Right in a submenu", 1, false, right, 1},
		{"Right on a submenu row of a submenu opens it", 1, true, right, 0},
		{"Shift+Right", 0, false, keyboard.KeyEvent{Key: keyboard.KeyRight, Mods: keyboard.ModShift}, 0},
		{"Down", 0, false, keyboard.KeyEvent{Key: keyboard.KeyDown}, 0},
	}
	for _, tt := range tests {
		if got := barStep(tt.depth, tt.submenu, tt.key); got != tt.want {
			t.Errorf("%s: barStep = %d, want %d", tt.name, got, tt.want)
		}
	}
}
