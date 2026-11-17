// Package shortcut registers global keyboard shortcuts, which work while
// another program has the keyboard.
package shortcut

import "errors"

// ID identifies a shortcut registered with Register.
type ID int32

// ErrTaken is returned when another program already holds the chord.
var ErrTaken = errors.New("shortcut: the shortcut is registered by another program")
