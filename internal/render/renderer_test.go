//go:build windows

package render

import (
	"bytes"
	"image"
	"runtime"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/myparsleycat/fairy/internal/win"
)

// A window renderer with damage tracking keeps producing the frame a full
// redraw does across partial frames, resizes, swap chain replacement and
// damage tracking being turned off and on.
func TestWindowFrameMatchesFullRedraw(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	const w, h = 96, 64
	class, _ := windows.UTF16PtrFromString("STATIC")
	hwnd, err := win.CreateWindowEx(0, class, nil, win.WS_POPUP, 0, 0, w, h, 0, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer win.DestroyWindow(hwnd)
	wr, err := New(uintptr(hwnd), w, h, false)
	if err != nil {
		t.Fatal(err)
	}
	defer wr.Release()
	if !wr.SetDamageTracking(true) {
		t.Fatal("damage tracking unavailable")
	}
	full, err := NewOffscreen(w, h)
	if err != nil {
		t.Fatal(err)
	}
	defer full.Release()

	clear := [4]float32{0.9, 0.9, 0.92, 1}
	rect := func(x, y, rw, rh float32, c [4]float32) Prim {
		return Prim{Bounds: [4]float32{x, y, rw, rh}, Clip: NoClip, Color: c, Kind: KindRect}
	}
	red, blue := [4]float32{1, 0, 0, 1}, [4]float32{0, 0, 1, 0.5}
	partial := 0
	frame := func(step string, x float32) {
		t.Helper()
		var s Scene
		s.Add(rect(4, 4, 40, 24, red))
		s.Add(rect(x, 30, 20, 20, blue))
		got, want := snapshot(t, wr, &s, clear), snapshot(t, full, &s, clear)
		if st := wr.Stats(); st.DamageReason == "" && !st.Damage.Empty() {
			partial++
		}
		if !bytes.Equal(got.Pix, want.Pix) || got.Rect != want.Rect {
			x, y := firstDiff(got.Pix, want.Pix, got.Rect.Dx())
			t.Fatalf("%s: pixel (%d, %d) differs from a full redraw; damage %v reason %q",
				step, x, y, wr.Stats().Damage, wr.Stats().DamageReason)
		}
	}
	resize := func(width, height int) {
		t.Helper()
		for _, r := range []*Renderer{wr, full} {
			if err := r.Resize(width, height); err != nil {
				t.Fatal(err)
			}
		}
	}

	frame("first", 10)
	frame("moved", 30)
	frame("unchanged", 30)
	resize(2*w, h)
	frame("wider", 50)
	resize(w, h)
	frame("resized back", 50)
	frame("moved after resize", 20)
	if err := wr.SetTransparent(true); err != nil {
		t.Fatal(err)
	}
	frame("transparent", 20)
	frame("moved while transparent", 40)
	if err := wr.SetTransparent(false); err != nil {
		t.Fatal(err)
	}
	frame("opaque again", 40)
	wr.SetDamageTracking(false)
	frame("damage off", 10)
	wr.SetDamageTracking(true)
	frame("damage on again", 10)
	frame("moved with damage on again", 60)
	if partial == 0 {
		t.Fatal("no frame was drawn in part")
	}
}

func snapshot(t *testing.T, r *Renderer, s *Scene, clear [4]float32) *image.NRGBA {
	t.Helper()
	var img *image.NRGBA
	r.RequestSnapshot(func(i *image.NRGBA) { img = i })
	if err := r.Draw(s, clear); err != nil {
		t.Fatal(err)
	}
	if img == nil {
		t.Fatal("Draw delivered no snapshot")
	}
	return img
}
