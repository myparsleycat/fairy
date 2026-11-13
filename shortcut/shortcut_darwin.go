package shortcut

import (
	"errors"
	"fmt"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/myparsleycat/fairy/internal/apphost"
	"github.com/myparsleycat/fairy/internal/darwin"
	"github.com/myparsleycat/fairy/internal/mackey"
	"github.com/myparsleycat/fairy/keyboard"
)

// Carbon hot keys: the window server reports them to the app that
// registered them while any app has the keyboard, and the Carbon event
// dispatcher, which NSApplication runs, calls the handler on the main thread.
const (
	hotKeySignature       = 'f'<<24 | 'a'<<16 | 'i'<<8 | 'r' // OSType of fairy's hot key ids
	kEventClassKeyboard   = 'k'<<24 | 'e'<<16 | 'y'<<8 | 'b'
	kEventHotKeyPressed   = 5
	kEventParamDirectObj  = '-'<<24 | '-'<<16 | '-'<<8 | '-'
	typeEventHotKeyID     = 'h'<<24 | 'k'<<16 | 'i'<<8 | 'd'
	kEventHotKeyExclusive = 1

	eventHotKeyExistsErr = -9878
	eventNotHandledErr   = -9874

	cmdKey     = 1 << 8
	shiftKey   = 1 << 9
	optionKey  = 1 << 11
	controlKey = 1 << 12

	kVKEqual      = 0x18 // the key that types = and, with Shift, +
	kVKKeypadPlus = 0x45
)

// eventHotKeyID is Carbon's EventHotKeyID.
type eventHotKeyID struct{ signature, id uint32 }

// eventTypeSpec is Carbon's EventTypeSpec.
type eventTypeSpec struct{ class, kind uint32 }

var carbon struct {
	registerEventHotKey       func(code, mods uint32, id uint64, target uintptr, options uint32, out *uintptr) int32
	unregisterEventHotKey     func(ref uintptr) int32
	getApplicationEventTarget func() uintptr
	installEventHandler       func(target, handler uintptr, n uint64, types *eventTypeSpec, data uintptr, out *uintptr) int32
	getEventParameter         func(event uintptr, name, typ uint32, actual *uint32, size uint64, actualSize *uint64, data unsafe.Pointer) int32
}

var shortcuts struct {
	loaded   bool
	handler  uintptr
	handlers map[uint32]func()   // by hot key id
	refs     map[uint32]uintptr  // EventHotKeyRef by hot key id
	extra    map[uint32][]uint32 // further hot key ids registered for an ID
	next     uint32
}

func init() {
	apphost.OnShutdown(release)
}

// load loads Carbon and installs the hot key handler. Run on the UI thread.
func load() error {
	if shortcuts.loaded {
		return nil
	}
	lib, err := purego.Dlopen("/System/Library/Frameworks/Carbon.framework/Carbon", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return fmt.Errorf("shortcut: load Carbon: %w", err)
	}
	darwin.Func(&carbon.registerEventHotKey, lib, "RegisterEventHotKey")
	darwin.Func(&carbon.unregisterEventHotKey, lib, "UnregisterEventHotKey")
	darwin.Func(&carbon.getApplicationEventTarget, lib, "GetApplicationEventTarget")
	darwin.Func(&carbon.installEventHandler, lib, "InstallEventHandler")
	darwin.Func(&carbon.getEventParameter, lib, "GetEventParameter")
	cb := purego.NewCallback(func(_, event, _ uintptr) uintptr {
		var hk eventHotKeyID
		if carbon.getEventParameter(event, kEventParamDirectObj, typeEventHotKeyID, nil,
			uint64(unsafe.Sizeof(hk)), nil, unsafe.Pointer(&hk)) == 0 && hk.signature == hotKeySignature &&
			onHotKey(hk.id) {
			return 0 // noErr
		}
		// Another handler's hot key: let it through.
		status := int32(eventNotHandledErr)
		return uintptr(uint32(status))
	})
	spec := eventTypeSpec{kEventClassKeyboard, kEventHotKeyPressed}
	if st := carbon.installEventHandler(carbon.getApplicationEventTarget(), cb, 1, &spec, 0, &shortcuts.handler); st != 0 {
		return fmt.Errorf("shortcut: InstallEventHandler: OSStatus %d", st)
	}
	shortcuts.handlers = map[uint32]func(){}
	shortcuts.refs = map[uint32]uintptr{}
	shortcuts.extra = map[uint32][]uint32{}
	shortcuts.loaded = true
	return nil
}

// Register runs f on the UI thread whenever chord is pressed, even while
// another program has the keyboard. The chord needs Control or Command,
// unless its key is F1–F24, so that it cannot take a key other programs type
// with; Option alone types characters, as ⌥E starts an accent. Call it on
// the UI thread, inside fairy.Main. Shortcuts are released when Main returns.
func Register(chord keyboard.Chord, f func()) (ID, error) {
	if !apphost.OnUIThread() {
		return 0, errors.New("shortcut: Register must run on the UI thread inside fairy.Main")
	}
	if !chord.CanBeGlobal() {
		return 0, fmt.Errorf("shortcut: global shortcut %q needs Control or Command", chord)
	}
	code, ok := mackey.Code(chord.Key)
	if !ok {
		return 0, fmt.Errorf("shortcut: no Mac key for %q", chord)
	}
	if err := load(); err != nil {
		return 0, err
	}
	var mods uint32
	for _, m := range [...]struct {
		mod    keyboard.Mods
		native uint32
	}{{keyboard.ModCtrl, controlKey}, {keyboard.ModAlt, optionKey}, {keyboard.ModShift, shiftKey}, {keyboard.ModSuper, cmdKey}} {
		if chord.Mods&m.mod != 0 {
			mods |= m.native
		}
	}
	id, err := registerHotKey(uint32(code), mods, f)
	if err != nil {
		if errors.Is(err, ErrTaken) {
			return 0, ErrTaken
		}
		return 0, fmt.Errorf("shortcut: RegisterEventHotKey %q: %w", chord, err)
	}
	if chord.Rune() == '+' {
		// As in Matches, "+" also fires with the Shift that types it and on
		// the numeric keypad; these are added when no other program holds them.
		for _, k := range [...]struct{ mods, code uint32 }{
			{mods | shiftKey, kVKEqual}, {mods, kVKKeypadPlus}, {mods | shiftKey, kVKKeypadPlus},
		} {
			if k.mods == mods && k.code == uint32(code) {
				continue
			}
			if x, err := registerHotKey(k.code, k.mods, f); err == nil {
				shortcuts.extra[id] = append(shortcuts.extra[id], x)
			}
		}
	}
	return ID(id), nil
}

func registerHotKey(code, mods uint32, f func()) (uint32, error) {
	id := nextHotKeyID()
	var ref uintptr
	packed := uint64(hotKeySignature) | uint64(id)<<32 // EventHotKeyID by value
	switch st := carbon.registerEventHotKey(code, mods, packed, carbon.getApplicationEventTarget(), kEventHotKeyExclusive, &ref); {
	case st == eventHotKeyExistsErr:
		return 0, ErrTaken
	case st != 0:
		return 0, fmt.Errorf("OSStatus %d", st)
	}
	shortcuts.handlers[id] = f
	shortcuts.refs[id] = ref
	return id, nil
}

func nextHotKeyID() uint32 {
	id := shortcuts.next + 1
	for shortcuts.handlers[id] != nil || id > 1<<31-1 {
		if id > 1<<31-1 {
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
	if shortcuts.handlers[uint32(id)] == nil {
		return
	}
	for _, x := range append(shortcuts.extra[uint32(id)], uint32(id)) {
		carbon.unregisterEventHotKey(shortcuts.refs[x])
		delete(shortcuts.handlers, x)
		delete(shortcuts.refs, x)
	}
	delete(shortcuts.extra, uint32(id))
}

// onHotKey runs the handler of hot key id and reports whether there was one.
func onHotKey(id uint32) bool {
	f := shortcuts.handlers[id]
	if f != nil {
		apphost.Go(f)
	}
	return f != nil
}

func release() {
	for _, ref := range shortcuts.refs {
		carbon.unregisterEventHotKey(ref)
	}
	clear(shortcuts.handlers)
	clear(shortcuts.refs)
	clear(shortcuts.extra)
}
