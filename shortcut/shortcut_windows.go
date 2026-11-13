package shortcut

import (
	"errors"
	"fmt"
	"syscall"

	"github.com/myparsleycat/fairy/internal/apphost"
	"github.com/myparsleycat/fairy/internal/win"
	"github.com/myparsleycat/fairy/keyboard"
)

var shortcuts struct {
	handlers map[int32]func()  // by hotkey id
	extra    map[int32][]int32 // further hotkey ids registered for an ID
	next     int32
}

func init() {
	apphost.Handle(win.WM_HOTKEY, func(wp, _ uintptr) uintptr {
		onHotKey(int32(wp))
		return 0
	})
	apphost.OnShutdown(release)
}

// Register runs f on the UI thread whenever chord is pressed, even while
// another program has the keyboard. The chord needs Ctrl, Alt or Win, unless
// its key is F1–F24, so that it cannot take a key other programs type with.
// Call it on the UI thread, inside fairy.Main. Shortcuts are released when
// Main returns.
func Register(chord keyboard.Chord, f func()) (ID, error) {
	if !apphost.OnUIThread() {
		return 0, errors.New("shortcut: Register must run on the UI thread inside fairy.Main")
	}
	if !chord.CanBeGlobal() {
		return 0, fmt.Errorf("shortcut: global shortcut %q needs Ctrl, Alt or Win", chord)
	}
	mods := uint32(win.MOD_NOREPEAT)
	for _, m := range [...]struct {
		mod    keyboard.Mods
		native uint32
	}{{keyboard.ModCtrl, win.MOD_CONTROL}, {keyboard.ModAlt, win.MOD_ALT}, {keyboard.ModShift, win.MOD_SHIFT}, {keyboard.ModSuper, win.MOD_WIN}} {
		if chord.Mods&m.mod != 0 {
			mods |= m.native
		}
	}
	if shortcuts.handlers == nil {
		shortcuts.handlers, shortcuts.extra = map[int32]func(){}, map[int32][]int32{}
	}
	hwnd := apphost.HWND()
	id := nextHotKeyID()
	if err := win.RegisterHotKey(hwnd, id, mods, uint32(chord.Key)); err != nil {
		if errno, ok := err.(syscall.Errno); ok && errno == win.ERROR_HOTKEY_ALREADY_REGISTERED {
			return 0, ErrTaken
		}
		return 0, fmt.Errorf("shortcut: RegisterHotKey %q: %w", chord, err)
	}
	shortcuts.handlers[id] = f
	if chord.Rune() == '+' {
		// As in Matches, "+" also fires with the Shift that types it and on
		// the numeric keypad; these are added when no other program holds them.
		for _, k := range [...]struct{ mods, vk uint32 }{
			{mods | win.MOD_SHIFT, uint32(win.VK_OEM_PLUS)}, {mods, uint32(win.VK_ADD)}, {mods | win.MOD_SHIFT, uint32(win.VK_ADD)},
		} {
			if k.mods == mods && k.vk == uint32(win.VK_OEM_PLUS) {
				continue
			}
			x := nextHotKeyID()
			if win.RegisterHotKey(hwnd, x, k.mods, k.vk) == nil {
				shortcuts.handlers[x] = f
				shortcuts.extra[id] = append(shortcuts.extra[id], x)
			}
		}
	}
	return ID(id), nil
}

func nextHotKeyID() int32 {
	id := shortcuts.next + 1
	for shortcuts.handlers[id] != nil || id > 0xBFFF {
		if id > 0xBFFF {
			id = 1
		} else {
			id++
		}
	}
	shortcuts.next = id
	return id
}

// Unregister releases a shortcut registered with Register. Call it on the UI
// thread.
func Unregister(id ID) {
	if shortcuts.handlers[int32(id)] == nil {
		return
	}
	for _, x := range append(shortcuts.extra[int32(id)], int32(id)) {
		win.UnregisterHotKey(apphost.HWND(), x)
		delete(shortcuts.handlers, x)
	}
	delete(shortcuts.extra, int32(id))
}

func onHotKey(id int32) {
	if f := shortcuts.handlers[id]; f != nil {
		apphost.Go(f)
	}
}

func release() {
	for id := range shortcuts.handlers {
		win.UnregisterHotKey(apphost.HWND(), id)
	}
	clear(shortcuts.handlers)
	clear(shortcuts.extra)
}
