package fairy

import (
	"bytes"
	"fmt"
	"testing"
	"time"
	"unsafe"

	"github.com/myparsleycat/fairy/d3d"
	"github.com/myparsleycat/fairy/internal/com"
	"github.com/myparsleycat/fairy/keyboard"
)

var (
	testIIDTexture2D  = com.MustGUID("6f15aaf2-d208-4e89-9ab4-489535d34f9c")
	testIIDKeyedMutex = com.MustGUID("9d8e1289-d7b3-465f-8126-250e349af85d")
)

// holdShared opens a shared frame on dev as the program showing it does and
// takes its keyed mutex until release is called.
func holdShared(dev d3d.Device, handle uintptr) (tex d3d.Texture2D, release func(), err error) {
	var p unsafe.Pointer
	hr := com.HR(dev.P, 28, handle, uintptr(unsafe.Pointer(&testIIDTexture2D)), uintptr(unsafe.Pointer(&p))) // OpenSharedResource
	if err := hr.Err("OpenSharedResource"); err != nil {
		return tex, nil, err
	}
	tex = d3d.Texture2D{P: p}
	mutex, err := com.QueryInterface(p, &testIIDKeyedMutex)
	if err != nil {
		tex.Release()
		return tex, nil, err
	}
	if hr := com.HR(mutex, 8, 0, 1000); hr != 0 { // AcquireSync(0, 1000)
		com.Release(mutex)
		tex.Release()
		return tex, nil, fmt.Errorf("AcquireSync = %#x", uint32(hr))
	}
	return tex, func() {
		com.HR(mutex, 9, 0) // ReleaseSync(0)
		com.Release(mutex)
		tex.Release()
	}, nil
}

// readShared reads a shared frame on dev under its keyed mutex.
func readShared(dev d3d.Device, ctx d3d.Context, handle uintptr) ([]byte, error) {
	tex, release, err := holdShared(dev, handle)
	if err != nil {
		return nil, err
	}
	pix, desc, err := ctx.ReadPixels(tex)
	release()
	if err == nil && desc.Format != d3d.FORMAT_R8G8B8A8_UNORM {
		err = fmt.Errorf("shared texture format = %d, want R8G8B8A8_UNORM", desc.Format)
	}
	return pix, err
}

// A frame opened on a second device matches ReadFrame, and resizing to
// another size and DPI replaces the texture with one drawn at the new scale.
func TestHeadlessSharedFrame(t *testing.T) {
	frames := make(chan Frame, 64)
	runHeadless(t, Options{
		Width:   40,
		Height:  20,
		OnFrame: func(f Frame) { frames <- f },
		View: func(*Cx) *El {
			return Box(Box().Size(20, 10).Fill(Hex(0x3366cc))).WFull().HFull()
		},
	}, func(w *Window) {
		dev, ctx, info, err := d3d.CreateDeviceWithOptions(d3d.DeviceOptions{})
		if err != nil {
			t.Error(err)
			return
		}
		defer dev.Release()
		defer ctx.Release()
		check := func(f Frame, boxW int) {
			if info.AdapterKnown && info.AdapterLUID != f.AdapterLUID {
				t.Errorf("frame adapter %+v, second device's adapter %+v", f.AdapterLUID, info.AdapterLUID)
			}
			got, err := readShared(dev, ctx, f.Handle)
			if err != nil {
				t.Error(err)
				return
			}
			want := make([]byte, f.Width*f.Height*4)
			Do(func() { err = w.ReadFrame(want) })
			if err != nil {
				t.Error(err)
				return
			}
			if !bytes.Equal(got, want) {
				t.Error("shared texture pixels differ from ReadFrame")
			}
			if px := got[(boxW-1)*4 : boxW*4]; !bytes.Equal(px, []byte{0x33, 0x66, 0xcc, 0xff}) {
				t.Errorf("pixel %d of the first row = %v, want the box color", boxW-1, px)
			}
			if px := got[boxW*4 : boxW*4+4]; !bytes.Equal(px, []byte{0, 0, 0, 0}) {
				t.Errorf("pixel %d of the first row = %v, want transparent", boxW, px)
			}
		}

		first := <-frames
		if first.Handle == 0 || first.Seq != 1 {
			t.Errorf("first frame = %+v, want a handle and Seq 1", first)
			return
		}
		check(first, int(20*w.Scale()))

		// A frame drawn while the program holds the keyed mutex reaches
		// OnFrame once it is released, with no further change to the view.
		_, release, err := holdShared(dev, first.Handle)
		if err != nil {
			t.Error(err)
			return
		}
		last := first
		for len(frames) > 0 {
			last = <-frames
		}
		Do(w.Update)
		select {
		case f := <-frames:
			release()
			t.Errorf("frame %+v delivered while the program held the keyed mutex", f)
			return
		case <-time.After(200 * time.Millisecond):
		}
		release()
		select {
		case f := <-frames:
			if f.Seq != last.Seq+1 || f.Handle != last.Handle {
				t.Errorf("frame after release = %+v, want Seq %d in the texture of %+v", f, last.Seq+1, last)
			}
		case <-time.After(5 * time.Second):
			t.Error("no frame after the program released the keyed mutex")
			return
		}

		w.SetHeadlessSize(64, 32, 192)
		var f Frame
		for f.Width != 64 {
			select {
			case f = <-frames:
			case <-time.After(5 * time.Second):
				t.Error("no frame after SetHeadlessSize")
				return
			}
		}
		if f.Height != 32 || f.Seq <= first.Seq || f.Handle == 0 || f.Handle == first.Handle {
			t.Errorf("frame after resize = %+v, want 64x32 in a new texture after %+v", f, first)
		}
		check(f, 40)
	})
}

// A device released while holding the keyed mutex abandons the shared
// texture; the next frame arrives in a new one.
func TestHeadlessSharedAbandoned(t *testing.T) {
	frames := make(chan Frame, 64)
	runHeadless(t, Options{
		Width:   40,
		Height:  20,
		OnFrame: func(f Frame) { frames <- f },
		View:    func(*Cx) *El { return Box().Size(20, 10).Fill(Hex(0x3366cc)) },
	}, func(w *Window) {
		first := <-frames
		if first.Handle == 0 {
			t.Errorf("first frame = %+v, want a handle", first)
			return
		}
		dev, ctx, _, err := d3d.CreateDeviceWithOptions(d3d.DeviceOptions{})
		if err != nil {
			t.Error(err)
			return
		}
		var p unsafe.Pointer
		hr := com.HR(dev.P, 28, first.Handle, uintptr(unsafe.Pointer(&testIIDTexture2D)), uintptr(unsafe.Pointer(&p))) // OpenSharedResource
		if err := hr.Err("OpenSharedResource"); err != nil {
			t.Error(err)
			return
		}
		tex := d3d.Texture2D{P: p}
		mutex, err := com.QueryInterface(p, &testIIDKeyedMutex)
		if err != nil {
			t.Error(err)
			return
		}
		if hr := com.HR(mutex, 8, 0, 1000); hr != 0 { // AcquireSync(0, 1000)
			t.Errorf("AcquireSync = %#x", uint32(hr))
			return
		}
		com.Release(mutex)
		tex.Release()
		ctx.Release()
		dev.Release()

		for len(frames) > 0 {
			<-frames
		}
		Do(w.Update)
		select {
		case f := <-frames:
			if f.Handle == 0 || f.Handle == first.Handle {
				t.Errorf("frame after the holder went away = %+v, want a new handle after %+v", f, first)
			}
		case <-time.After(5 * time.Second):
			t.Error("no frame after the holder went away")
		}
	})
}

// runHeadless creates a Headless window, runs steps on a goroutine with the
// message loop running, and quits.
func runHeadless(t *testing.T, opts Options, steps func(w *Window)) {
	t.Helper()
	opts.Headless = true
	err := Main(func() error {
		w, err := NewWindow(opts)
		if err != nil {
			return err
		}
		go func() {
			defer Quit()
			steps(w)
		}()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestHeadlessInjectedClick(t *testing.T) {
	clicks := 0
	var cursors []Cursor
	runHeadless(t, Options{
		Width:    100,
		Height:   60,
		OnCursor: func(c Cursor) { cursors = append(cursors, c) },
		View: func(*Cx) *El {
			return Box(Box().Size(40, 20).Left(10).Top(10).Cursor(CursorPointer).OnClick(func() { clicks++ })).WFull().HFull()
		},
	}, func(w *Window) {
		s := w.Scale()
		w.InjectPointerMove(30*s, 20*s)
		w.InjectButton(0, true, 30*s, 20*s)
		w.InjectButton(0, false, 30*s, 20*s)
		w.InjectButton(0, true, 80*s, 50*s)
		w.InjectButton(0, false, 80*s, 50*s)
		Do(func() {
			if clicks != 1 {
				t.Errorf("clicks = %d, want 1 from the press inside the element", clicks)
			}
			if len(cursors) < 2 || cursors[len(cursors)-2] != CursorPointer || cursors[len(cursors)-1] != CursorDefault {
				t.Errorf("cursors = %v, want Pointer over the element, then Default", cursors)
			}
		})
	})
}

// A click queued right behind a resize lands on the element where the new
// size places it, before any frame is drawn at that size.
func TestHeadlessClickAfterResize(t *testing.T) {
	clicks := 0
	runHeadless(t, Options{
		Width:  100,
		Height: 60,
		View: func(*Cx) *El {
			return Box(Box().Size(40, 20).Right(10).Bottom(10).OnClick(func() { clicks++ })).WFull().HFull()
		},
	}, func(w *Window) {
		Do(func() {
			// Queued on the UI thread, so one message runs all three.
			w.SetHeadlessSize(200, 100, 96)
			w.InjectButton(0, true, 170, 80)
			w.InjectButton(0, false, 170, 80)
		})
		Do(func() {
			if clicks != 1 {
				t.Errorf("clicks = %d, want 1 on the element at its resized position", clicks)
			}
		})
	})
}

func TestHeadlessInjectedKeys(t *testing.T) {
	value := ""
	var focus []bool
	runHeadless(t, Options{
		Width:   200,
		Height:  40,
		OnFocus: func(f bool) { focus = append(focus, f) },
		View: func(*Cx) *El {
			return Input(value, func(s string) { value = s }).Key("field").AutoFocus().W(150)
		},
	}, func(w *Window) {
		w.SetFocused(true)
		for _, r := range "ab한😀" {
			w.InjectChar(r)
		}
		w.InjectKey(keyboard.KeyBackspace, true, 0, "")
		w.InjectChar('\b')
		w.InjectKey(keyboard.KeyBackspace, false, 0, "")
		w.InjectKey(keyboard.KeyA, true, keyboard.ModCtrl, "")
		w.InjectChar(1) // Ctrl+A arrives as U+0001
		w.InjectKey(keyboard.KeyA, false, keyboard.ModCtrl, "")
		w.InjectChar('z')
		w.SetFocused(false)
		Do(func() {
			if value != "z" {
				t.Errorf("value = %q, want %q: ab한😀, Backspace, then z over the Ctrl+A selection", value, "z")
			}
			if len(focus) != 2 || !focus[0] || focus[1] {
				t.Errorf("OnFocus calls = %v, want [true false]", focus)
			}
		})
	})
}
