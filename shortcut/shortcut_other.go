//go:build !windows

package shortcut

import (
	"errors"

	"github.com/myparsleycat/fairy/keyboard"
)

// errUnsupported is returned where global shortcuts are not available yet.
var errUnsupported = errors.New("shortcut: global shortcuts are not supported on this platform yet")

// Register runs f on the UI thread whenever chord is pressed, even while
// another program has the keyboard.
func Register(chord keyboard.Chord, f func()) (ID, error) { return 0, errUnsupported }

// Unregister releases a shortcut registered with Register.
func Unregister(id ID) {}
