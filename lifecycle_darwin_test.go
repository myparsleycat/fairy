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
				want   bool
			}{
				{0, false}, {0x71756961, false}, // ordinary quit, quit all
				{0x72657374, true}, {0x73687574, true}, {0x726c676f, true},
			} {
				ev := darwin.SendClass("NSAppleEventDescriptor", "appleEventWithEventClass:eventID:targetDescriptor:returnID:transactionID:", uint32(0x61657674), uint32(0x71756974), darwin.ID(0), int16(-1), int32(0))
				if tc.reason != 0 {
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
