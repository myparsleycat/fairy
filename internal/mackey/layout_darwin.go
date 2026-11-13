//go:build darwin

package mackey

import (
	"sync"
	"unicode/utf16"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/myparsleycat/fairy/keyboard"
)

var (
	tisCopyCurrentKeyboardLayoutInputSource             func() uintptr
	tisCopyCurrentASCIICapableKeyboardLayoutInputSource func() uintptr
	tisGetInputSourceProperty                           func(src, key uintptr) uintptr
	lmGetKbdType                                        func() uint8
	ucKeyTranslate                                      func(layout unsafe.Pointer, code, action uint16, mods, kbdType, options uint32, dead *uint32, max uint64, n *uint64, chars *uint16) int32
	cfDataGetBytePtr                                    func(data uintptr) unsafe.Pointer
	cfRelease                                           func(p uintptr)
	propUnicodeKeyLayoutData                            uintptr

	loadLayout = sync.OnceValue(func() bool {
		hi, err := purego.Dlopen("/System/Library/Frameworks/Carbon.framework/Frameworks/HIToolbox.framework/HIToolbox", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return false
		}
		cs, err := purego.Dlopen("/System/Library/Frameworks/CoreServices.framework/CoreServices", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return false
		}
		cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return false
		}
		prop, err := purego.Dlsym(hi, "kTISPropertyUnicodeKeyLayoutData")
		if err != nil {
			return false
		}
		propUnicodeKeyLayoutData = *(*uintptr)(unsafe.Add(nil, prop))
		purego.RegisterLibFunc(&tisCopyCurrentKeyboardLayoutInputSource, hi, "TISCopyCurrentKeyboardLayoutInputSource")
		purego.RegisterLibFunc(&tisCopyCurrentASCIICapableKeyboardLayoutInputSource, hi, "TISCopyCurrentASCIICapableKeyboardLayoutInputSource")
		purego.RegisterLibFunc(&tisGetInputSourceProperty, hi, "TISGetInputSourceProperty")
		purego.RegisterLibFunc(&lmGetKbdType, hi, "LMGetKbdType")
		purego.RegisterLibFunc(&ucKeyTranslate, cs, "UCKeyTranslate")
		purego.RegisterLibFunc(&cfDataGetBytePtr, cf, "CFDataGetBytePtr")
		purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")
		return true
	})
)

// Modifier bits of UCKeyTranslate's modifierKeyState: the Carbon event
// modifiers shifted right by 8.
const (
	ucShift  = 0x02
	ucOption = 0x08
)

// withLayout calls f with the 'uchr' data of the current keyboard layout,
// or of the current ASCII-capable one when ascii is set or the current
// input source has none (input methods such as Japanese).
func withLayout(ascii bool, f func(layout unsafe.Pointer)) bool {
	if !loadLayout() {
		return false
	}
	var src uintptr
	if !ascii {
		src = tisCopyCurrentKeyboardLayoutInputSource()
	}
	var data uintptr
	if src != 0 {
		data = tisGetInputSourceProperty(src, propUnicodeKeyLayoutData)
	}
	if data == 0 {
		if src != 0 {
			cfRelease(src)
		}
		src = tisCopyCurrentASCIICapableKeyboardLayoutInputSource()
		if src == 0 {
			return false
		}
		data = tisGetInputSourceProperty(src, propUnicodeKeyLayoutData)
	}
	defer cfRelease(src)
	if data == 0 {
		return false
	}
	f(cfDataGetBytePtr(data))
	return true
}

func translate(layout unsafe.Pointer, code uint16, mods uint32, dead *uint32, noDead bool) string {
	var opts uint32
	if noDead {
		opts = 1 // kUCKeyTranslateNoDeadKeysMask
	}
	var buf [8]uint16
	var n uint64
	if ucKeyTranslate(layout, code, 0 /* kUCKeyActionDown */, mods, uint32(lmGetKbdType()), opts, dead, uint64(len(buf)), &n, &buf[0]) != 0 {
		return ""
	}
	return string(utf16.Decode(buf[:n]))
}

// layoutLetter returns the letter key that code types on the current
// ASCII-capable keyboard layout, without modifiers, or 0.
func layoutLetter(code uint16) keyboard.Key {
	var k keyboard.Key
	withLayout(true, func(layout unsafe.Pointer) {
		var dead uint32
		if s := translate(layout, code, 0, &dead, true); len(s) == 1 {
			switch c := s[0]; {
			case c >= 'a' && c <= 'z':
				k = keyboard.Key(c - 'a' + 'A')
			case c >= 'A' && c <= 'Z':
				k = keyboard.Key(c)
			}
		}
	})
	return k
}

// DeadKeys holds the dead-key state of keys typed through Type.
type DeadKeys struct{ state uint32 }

// Type returns the text the key code types on the current keyboard layout
// with Shift and Option as in modifierFlags (NSEventModifierFlags), applying
// and keeping dead-key state the way the text input system does: a dead key
// types nothing, and the next key types the composed character. It is for
// keys that bypass the text input system. ok is false when the layout can't
// be read.
func (d *DeadKeys) Type(code uint16, modifierFlags uint64) (s string, ok bool) {
	var mods uint32
	if modifierFlags&(1<<17) != 0 { // NSEventModifierFlagShift
		mods |= ucShift
	}
	if modifierFlags&(1<<19) != 0 { // NSEventModifierFlagOption
		mods |= ucOption
	}
	ok = withLayout(false, func(layout unsafe.Pointer) {
		s = translate(layout, code, mods, &d.state, false)
	})
	return s, ok
}

// Reset forgets a pending dead key.
func (d *DeadKeys) Reset() { d.state = 0 }
