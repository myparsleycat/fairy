package fairy

import "github.com/myparsleycat/fairy/keyboard"

// NSEventModifierFlags bits.
const (
	nsShift   = 1 << 17
	nsControl = 1 << 18
	nsOption  = 1 << 19
	nsCommand = 1 << 20
)

// macMods converts NSEventModifierFlags.
func macMods(flags uint64) keyboard.Mods {
	var m keyboard.Mods
	if flags&nsShift != 0 {
		m |= keyboard.ModShift
	}
	if flags&nsControl != 0 {
		m |= keyboard.ModCtrl
	}
	if flags&nsOption != 0 {
		m |= keyboard.ModAlt
	}
	if flags&nsCommand != 0 {
		m |= keyboard.ModSuper
	}
	return m
}
