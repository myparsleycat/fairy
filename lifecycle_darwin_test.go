package fairy

import (
	"github.com/myparsleycat/fairy/internal/darwin"
	"testing"
)

func TestDarwinCloseDuringFrame(t *testing.T) {
	w := newTestWindow(t, func() *El { return Box() })
	Do(func() {
		w.mouseIn, w.moveDue = true, true
		w.hoverPath = []*El{Box().OnPointerMove(func(PointerEvent) bool { w.Close(); return true })}
		w.renderFrame()
		if w.closed {
			t.Error("Close destroyed resources during the frame")
		}
	})
	waitFor(t, "deferred close", func() bool { return w.closed })
}

func TestDarwinCloseCanBeVetoed(t *testing.T) {
	w := newTestWindow(t, func() *El { return Box() })
	calls := 0
	Do(func() { w.opts.OnClose = func() bool { calls++; return false }; w.Close() })
	waitFor(t, "OnClose", func() bool { return calls == 1 })
	Do(func() {
		if w.closed {
			t.Error("closed despite OnClose veto")
		}
	})
}

func TestDarwinPostDiscardedOnClose(t *testing.T) {
	w := newTestWindow(t, func() *El { return Box() })
	ran := false
	Do(func() { w.Post(func() { ran = true }); w.closeNow() })
	for i := 0; i < 100; i++ {
		w.Post(func() { ran = true })
	}
	Do(func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		if len(w.posted) != 0 || ran {
			t.Errorf("closed window retains %d callbacks, ran=%v", len(w.posted), ran)
		}
	})
}

func TestDarwinSessionQuitReason(t *testing.T) {
	Do(func() {
		darwin.WithPool(func() {
			for _, tc := range []struct {
				reason uint32
				attr   bool // as loginwindow sends it: an attribute of type typeType
				want   bool
			}{
				{0, false, false}, {0x71756961, true, false}, // ordinary quit, quit all
				{0x72657374, true, true}, {0x73687574, true, true}, {0x726c676f, true, true},
				{0x726c676f, false, true}, // the parameter AERegistry.h documents
			} {
				ev := darwin.SendClass("NSAppleEventDescriptor", "appleEventWithEventClass:eventID:targetDescriptor:returnID:transactionID:", uint32(0x61657674), uint32(0x71756974), darwin.ID(0), int16(-1), int32(0))
				if tc.reason != 0 && tc.attr {
					reason := darwin.SendClass("NSAppleEventDescriptor", "descriptorWithTypeCode:", tc.reason)
					darwin.Send(ev, "setAttributeDescriptor:forKeyword:", reason, uint32(keyAEQuitReason))
				} else if tc.reason != 0 {
					reason := darwin.SendClass("NSAppleEventDescriptor", "descriptorWithEnumCode:", tc.reason)
					darwin.Send(ev, "setParamDescriptor:forKeyword:", reason, uint32(keyAEQuitReason))
				}
				if got := sessionQuitEvent(ev); got != tc.want {
					t.Errorf("reason %x: got %v, want %v", tc.reason, got, tc.want)
				}
			}
		})
	})
}

func TestDarwinSessionEndOnce(t *testing.T) {
	Do(func() {
		app.mu.Lock()
		old, ended := app.onSessionEnd, app.sessionEnded
		app.sessionEnded = false
		app.mu.Unlock()
		defer func() { app.mu.Lock(); app.onSessionEnd, app.sessionEnded = old, ended; app.mu.Unlock() }()
		calls := 0
		OnSessionEnd(func() { calls++; OnSessionEnd(nil) }) // callback may acquire app.mu
		notifySessionEnd()
		notifySessionEnd()
		if calls != 1 {
			t.Errorf("session end calls=%d", calls)
		}
	})
}

func TestDarwinClosedWindowCalls(t *testing.T) {
	w := newTestWindow(t, func() *El { return Box() })
	Do(func() { w.SetProgress(ProgressNormal, 0.5) })
	Do(func() {
		if dockProgress.owner != w {
			t.Error("progress not shown")
		}
		w.closeNow()
		if dockProgress.owner != nil {
			t.Error("closing the window left its Dock progress")
		}
	})
	// After the NSWindow is released, calls on the closed Window message
	// nil instead of a freed object.
	Do(func() {})
	Do(func() {
		if w.nsWindow != 0 || w.view != 0 {
			t.Error("closed window keeps its native handles")
		}
		w.SetColors(!w.opts.Dark, Color{}, Color{})
		w.SetProgress(ProgressNormal, 0.5)
	})
	Do(func() {
		if dockProgress.owner == w {
			t.Error("a closed window set the Dock progress")
		}
	})
}

func TestDarwinDragRegionFramelessOnly(t *testing.T) {
	w := newTestWindow(t, func() *El { return Box().Size(200, 200).DragRegion() })
	Do(func() {
		w.renderFrame()
		if w.inDragRegion(50, 50) {
			t.Error("DragRegion moves a framed window")
		}
		w.opts.Frameless = true
		if !w.inDragRegion(50, 50) {
			t.Error("DragRegion does not move a frameless window")
		}
		w.opts.Frameless = false
	})
}

func TestDarwinEditMenuUndoDisabledForView(t *testing.T) {
	w := newTestWindow(t, func() *El { return Box() })
	Do(func() {
		darwin.WithPool(func() {
			darwin.Send(w.nsWindow, "makeFirstResponder:", w.view)
			for _, action := range []string{"undo:", "redo:"} {
				item := darwin.Send(darwin.Send(darwin.SendClass("NSMenuItem", "alloc"), "initWithTitle:action:keyEquivalent:",
					darwin.String("x"), darwin.Sel(action), darwin.String("")), "autorelease")
				target := darwin.Send(nsApp, "targetForAction:to:from:", darwin.Sel(action), darwin.ID(0), item)
				if target != 0 && target != w.view {
					t.Errorf("%s goes to %#x, not the view", action, target)
				}
				if darwin.Get[bool](w.view, "validateMenuItem:", item) {
					t.Errorf("%s is enabled while the view has the keyboard", action)
				}
			}
		})
	})
}
