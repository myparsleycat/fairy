package shortcut

import (
	"errors"
	"os"
	"testing"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/internal/apphost"
	"github.com/myparsleycat/fairy/internal/darwin"
	"github.com/myparsleycat/fairy/keyboard"
)

// TestMain runs the tests inside fairy.Main, whose UI loop dispatches the
// Carbon events, on the main thread.
func TestMain(m *testing.M) {
	code := 0
	err := fairy.Main(func() error {
		apphost.Hold()
		go func() {
			code = m.Run()
			fairy.Go(apphost.Release)
		}()
		return nil
	})
	if err != nil {
		panic(err)
	}
	os.Exit(code)
}

// pressHotKey sends the app the Carbon event the window server sends when
// the hot key with id is pressed.
func pressHotKey(t *testing.T, id ID) {
	lib, err := purego.Dlopen("/System/Library/Frameworks/Carbon.framework/Carbon", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		t.Fatal(err)
	}
	var (
		createEvent       func(alloc uintptr, class, kind uint32, when float64, attrs uint32, out *uintptr) int32
		setEventParameter func(event uintptr, name, typ uint32, size uint64, data unsafe.Pointer) int32
		sendEvent         func(event, target uintptr) int32
		releaseEvent      func(event uintptr)
	)
	darwin.Func(&createEvent, lib, "CreateEvent")
	darwin.Func(&setEventParameter, lib, "SetEventParameter")
	darwin.Func(&sendEvent, lib, "SendEventToEventTarget")
	darwin.Func(&releaseEvent, lib, "ReleaseEvent")
	var ev uintptr
	if st := createEvent(0, kEventClassKeyboard, kEventHotKeyPressed, 0, 0, &ev); st != 0 {
		t.Fatalf("CreateEvent: %d", st)
	}
	defer releaseEvent(ev)
	hk := eventHotKeyID{hotKeySignature, uint32(id)}
	setEventParameter(ev, kEventParamDirectObj, typeEventHotKeyID, uint64(unsafe.Sizeof(hk)), unsafe.Pointer(&hk))
	if st := sendEvent(ev, carbon.getApplicationEventTarget()); st != 0 {
		t.Fatalf("SendEventToEventTarget: %d", st)
	}
}

func TestShortcutDarwin(t *testing.T) {
	if _, err := Register(keyboard.MustChord("ctrl+alt+shift+f19"), func() {}); err == nil {
		t.Fatal("Register off the UI thread succeeded")
	}
	if _, err := Register(keyboard.MustChord("shift+k"), func() {}); err == nil {
		t.Fatal("Register of a typing chord succeeded")
	}
	chord := keyboard.MustChord("ctrl+alt+shift+f19")
	fired := make(chan struct{}, 1)
	var id ID
	var err, again error
	fairy.Do(func() {
		id, err = Register(chord, func() { fired <- struct{}{} })
		if err == nil {
			_, again = Register(chord, func() {})
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer fairy.Do(func() { Unregister(id) })
	if !errors.Is(again, ErrTaken) {
		t.Errorf("registering a held chord again: %v, want ErrTaken", again)
	}
	fairy.Do(func() { pressHotKey(t, id) })
	select {
	case <-fired:
	case <-time.After(5 * time.Second):
		t.Fatal("the shortcut did not run")
	}
	fairy.Do(func() {
		Unregister(id)
		id, err = Register(chord, func() {})
	})
	if err != nil {
		t.Errorf("Register after Unregister: %v", err)
	}
}

// "+" also registers Shift and the keypad.
func TestShortcutPlusDarwin(t *testing.T) {
	var id ID
	var err error
	var n int
	fairy.Do(func() {
		id, err = Register(keyboard.MustChord("ctrl+alt+plus"), func() {})
		n = len(shortcuts.extra[uint32(id)])
		Unregister(id)
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("extra hot keys for ctrl+alt+plus = %d, want 3", n)
	}
	fairy.Do(func() {
		if len(shortcuts.refs) != 0 {
			t.Errorf("%d hot keys left after Unregister", len(shortcuts.refs))
		}
	})
}
