//go:build !darwin

package mackey

import "github.com/myparsleycat/fairy/keyboard"

func layoutLetter(code uint16) keyboard.Key { return 0 }
