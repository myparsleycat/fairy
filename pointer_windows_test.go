package fairy

import (
	"math"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/myparsleycat/fairy/internal/win"
	"github.com/myparsleycat/fairy/keyboard"
)

func newPointerWindow(t *testing.T, view func() *El) *Window {
	w := newListWindow(t, view)
	w.tracking = true // keeps onMouseMove from arming TrackMouseEvent on the missing window
	w.build()
	w.placeAll()
	return w
}

func TestWindowChromeModalInput(t *testing.T) {
	for _, modals := range []int{0, 1, 2} {
		t.Run(strconv.Itoa(modals), func(t *testing.T) {
			var chromeClicks, excludedClicks, backdropClicks, wheelCalls int
			w := newListWindow(t, nil)
			w.tracking = true
			var mods keyboard.Mods
			w.scriptMods = &mods
			w.opts.View = func(cx *Cx) *El {
				for i := range modals {
					cx.Overlay(Box(Input("", func(string) {}).Key("field").Size(80, 20).Top(40)).
						Key(strconv.Itoa(i)).WFull().HFull().
						OnClick(func() { backdropClicks++ }).OnWheel(func(WheelEvent) bool {
						wheelCalls++
						return true
					}), nil)
				}
				return Col(Row(
					Box(Box().Key("excluded-child").Size(40, 20).WindowChrome()).
						Key("excluded").W(60).HFull().WindowChromeExclude().OnClick(func() { excludedClicks++ }),
					Box().Key("drag").Grow(1).HFull(),
					Box(Box().Size(20, 20)).Key("control").W(50).HFull().
						OnClick(func() { chromeClicks++ }).MaximizeButton(),
				).WFull().H(30).DragRegion().WindowChrome(), Box().Key("body").WFull().Grow(1)).WFull().HFull()
			}
			w.build()
			w.placeAll()
			if modals > 0 {
				w.Focus("field")
			}
			focused := w.focused
			if got := w.captionHitTest(90, 10); got != win.HTCAPTION {
				t.Errorf("title bar hit = %d, want HTCAPTION", got)
			}
			if got := w.captionHitTest(170, 10); got != win.HTMAXBUTTON {
				t.Errorf("control hit = %d, want HTMAXBUTTON", got)
			}
			w.onMouseMove(170, 10)
			if !w.hovered(w.findKey("control")) {
				t.Error("window control did not receive hover")
			}
			w.onButtonDown(170, 10, 0)
			w.onButtonUp(170, 10, 0)
			if chromeClicks != 1 || backdropClicks != 0 || excludedClicks != 0 {
				t.Errorf(
					"control click: chrome=%d backdrop=%d excluded=%d",
					chromeClicks,
					backdropClicks,
					excludedClicks,
				)
			}
			if w.focused != focused {
				t.Error("window control changed keyboard focus")
			}
			w.onWheel(90, 10, -1, false, false)
			if modals > 0 && wheelCalls != 1 {
				t.Errorf("wheel calls = %d, want 1", wheelCalls)
			}
			w.onButtonDown(20, 10, 0)
			w.onButtonUp(20, 10, 0)
			if modals == 0 && (excludedClicks != 1 || backdropClicks != 0) {
				t.Errorf("unblocked action: excluded=%d backdrop=%d", excludedClicks, backdropClicks)
			}
			if modals > 0 && (excludedClicks != 0 || backdropClicks != 1) {
				t.Errorf("blocked action: excluded=%d backdrop=%d", excludedClicks, backdropClicks)
			}
			if got := w.pointerHitTest(90, 70); got != w.hitTest(90, 70) {
				t.Error("body input bypassed normal layer order")
			}
		})
	}
}

func TestWindowChromeLayout(t *testing.T) {
	w := newListWindow(t, nil)
	visible, enabled := true, true
	w.opts.View = func(cx *Cx) *El {
		cx.Overlay(Box().Key("backdrop").WFull().HFull(), nil)
		bar := Row().Key("chrome").WFull().H(30).DragRegion()
		if enabled {
			bar.WindowChrome()
		}
		if visible {
			bar.Child(Box().Key("excluded").W(60).HFull().Right(0).WindowChromeExclude())
		}
		return Col(bar).WFull().HFull()
	}
	frame := func() {
		w.build()
		w.placeAll()
	}
	frame()
	if got := w.pointerHitTest(170, 10); got.key != "backdrop" {
		t.Fatalf("excluded hit = %q, want backdrop", got.key)
	}
	w.width, w.scale = 600, 2
	frame()
	if got := w.captionHitTest(170, 10); got != win.HTCAPTION {
		t.Errorf("resized title bar hit = %d, want HTCAPTION", got)
	}
	if got := w.pointerHitTest(270, 10); got.key != "backdrop" {
		t.Errorf("resized excluded hit = %q, want backdrop", got.key)
	}
	visible = false
	frame()
	if got := w.captionHitTest(270, 10); got != win.HTCAPTION {
		t.Errorf("removed exclusion hit = %d, want HTCAPTION", got)
	}
	enabled = false
	frame()
	if got := w.pointerHitTest(170, 10); got != w.hitTest(170, 10) {
		t.Error("unmarked title bar changed normal hit testing")
	}
}

func TestWindowChromeHitGeometry(t *testing.T) {
	for _, tc := range []struct {
		name string
		view func() *El
		x, y float32
		want string
	}{
		{
			name: "rounded corner",
			view: func() *El { return Box().Key("chrome").Size(40, 40).Radius(20).WindowChrome() },
			x:    2, y: 2, want: "backdrop",
		},
		{
			name: "rounded center",
			view: func() *El { return Box().Key("chrome").Size(40, 40).Radius(20).WindowChrome() },
			x:    20, y: 20, want: "chrome",
		},
		{
			name: "clipped child",
			view: func() *El {
				return Box(Box().Key("chrome").Size(80, 20).WindowChrome()).Size(40, 40).Clip()
			},
			x: 60, y: 10, want: "backdrop",
		},
		{
			name: "rotated child",
			view: func() *El {
				return Box(Box().Key("chrome").Size(100, 20).Left(50).Top(40).Rotate(math.Pi / 2).WindowChrome())
			},
			x: 100, y: 5, want: "chrome",
		},
		{
			name: "pass-through root",
			view: func() *El { return Box().Key("chrome").Size(40, 40).WindowChrome().PassThrough() },
			x:    20, y: 20, want: "backdrop",
		},
		{
			name: "pass-through child",
			view: func() *El {
				return Box(Box().Key("control").Size(20, 20)).Size(40, 40).WindowChrome().PassThrough()
			},
			x: 10, y: 10, want: "control",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newListWindow(t, nil)
			w.opts.View = func(cx *Cx) *El {
				cx.Overlay(Box().Key("backdrop").WFull().HFull(), nil)
				return Box(tc.view()).WFull().HFull()
			}
			w.build()
			w.placeAll()
			got := w.pointerHitTest(tc.x, tc.y)
			if got == nil {
				t.Fatal("pointer hit is nil")
			}
			if got.key != tc.want {
				t.Errorf("pointer hit = %q, want %q", got.key, tc.want)
			}
		})
	}
}

func TestWindowChromePreservesModalAndPopover(t *testing.T) {
	var chromeClicks, backgroundClicks, dismissals int
	value := ""
	w := newListWindow(t, nil)
	w.tracking = true
	var mods keyboard.Mods
	w.scriptMods = &mods
	w.opts.View = func(cx *Cx) *El {
		cx.Overlay(Box(
			Input(value, func(s string) { value = s }).Key("field").Size(80, 20).Top(40),
			Box().Key("anchor").Size(10, 10).Right(0).Bottom(0).Popover(
				Box().Key("popup").Size(30, 20),
				PopoverOptions{OnDismiss: func() { dismissals++ }},
			),
		).Key("modal").WFull().HFull().OnClick(func() { backgroundClicks++ }), func() { dismissals++ })
		return Box(
			Box().Key("control").Size(80, 30).WindowChrome().
				OnClick(func() { chromeClicks++ }),
		).WFull().HFull().OnPointerDown(func(PointerEvent) { backgroundClicks++ })
	}
	w.build()
	w.placeAll()
	w.Focus("field")
	w.sel = &textSelection{}
	selection := w.sel
	w.onButtonDown(10, 10, 0)
	w.onButtonUp(10, 10, 0)
	if chromeClicks != 1 || backgroundClicks != 0 || dismissals != 0 {
		t.Fatalf("chrome=%d background=%d dismissals=%d", chromeClicks, backgroundClicks, dismissals)
	}
	if w.sel != selection || w.focusKey != "field" {
		t.Fatal("window control changed modal selection or focus")
	}
	w.sel = nil
	w.onChar('a')
	if value != "a" {
		t.Errorf("modal input after window control = %q, want a", value)
	}
	w.onKeyDown(keyboard.KeyTab, false, "")
	if w.focusKey != "field" {
		t.Errorf("Tab left modal: focus = %q", w.focusKey)
	}
	w.onKeyDown(keyboard.KeyEscape, false, "")
	if dismissals != 1 {
		t.Errorf("Escape dismissals = %d, want 1", dismissals)
	}
}

func TestPopoverStaysBelowWindowChrome(t *testing.T) {
	w := newPointerWindow(t, func() *El {
		return Col(
			Box().Key("bar").WFull().H(30).DragRegion().WindowChrome(),
			Box().Key("anchor").Size(10, 10).Left(10).Top(60).Popover(Box().Key("popup").Size(40, 40), PopoverOptions{}),
		).WFull().HFull()
	})
	popup := w.findKey("popup")
	if popup == nil {
		t.Fatal("popup not placed")
	}
	if popup.absY < 34 {
		t.Errorf("popup top = %v, want at least 34 below the 30 pixel chrome", popup.absY)
	}
	if got := w.pointerHitTest(popup.absX+1, popup.absY+1); got != popup {
		t.Errorf("press on the popup's top edge reached %p, want popup %p", got, popup)
	}
}

func TestWindowChromeHover(t *testing.T) {
	for _, tc := range []struct {
		name  string
		modal bool
	}{
		{name: "without modal"},
		{name: "with modal", modal: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var backgroundHovers, backgroundMoves, chromeHovers, chromeMoves int
			w := newListWindow(t, nil)
			w.tracking = true
			w.opts.View = func(cx *Cx) *El {
				focused := Box().Key("focused").Size(80, 20).Top(40).Focusable()
				root := Box(Box(
					Box().Key("control").Size(40, 20).Focusable().FocusHighlight(RGB(255, 0, 0)),
					Box().Key("excluded").Size(40, 20).Left(40).
						WindowChromeExclude().Focusable().FocusHighlight(RGB(255, 0, 0)),
				).Size(80, 30).WindowChrome().OnHover(func(in bool) {
					if in {
						chromeHovers++
					}
				}).OnPointerMove(func(PointerEvent) bool { chromeMoves++; return false })).
					WFull().HFull().OnHover(func(in bool) {
					if in {
						backgroundHovers++
					}
				}).OnPointerMove(func(PointerEvent) bool { backgroundMoves++; return false })
				if tc.modal {
					cx.Overlay(Box(focused).WFull().HFull(), nil)
				} else {
					root.Child(focused)
				}
				return root
			}
			w.build()
			w.placeAll()
			w.Focus("focused")
			focused := w.keyTarget()
			w.onMouseMove(10, 10)
			w.flushPointerMove()
			if chromeHovers != 1 || chromeMoves != 1 || !w.hovered(w.findKey("control")) {
				t.Errorf("chrome hover: enters=%d moves=%d", chromeHovers, chromeMoves)
			}
			if backgroundHovers != 0 || backgroundMoves != 0 {
				t.Errorf("non-chrome ancestor received enters=%d moves=%d", backgroundHovers, backgroundMoves)
			}
			if w.keyTarget() != focused {
				t.Errorf("chrome hover changed keyboard focus to %q", w.focusKey)
			}
			w.onMouseMove(50, 10)
			w.flushPointerMove()
			if tc.modal {
				if w.keyTarget() != focused || backgroundHovers != 0 || backgroundMoves != 0 {
					t.Error("excluded hover bypassed the modal")
				}
			} else {
				if w.focusKey != "excluded" || backgroundHovers != 1 || backgroundMoves != 1 {
					t.Error("excluded hover did not retain normal propagation and focus")
				}
			}
		})
	}
}

func TestWindowChromePrecedesScrollbar(t *testing.T) {
	clicks := 0
	w := newListWindow(t, nil)
	w.tracking = true
	var mods keyboard.Mods
	w.scriptMods = &mods
	w.opts.View = func(cx *Cx) *El {
		cx.Overlay(Col(Box().WFull().H(400)).Key("scroll").WFull().HFull().ScrollY(), nil)
		return Col(Box().WFull().H(30).WindowChrome().OnClick(func() { clicks++ })).WFull().HFull()
	}
	w.build()
	w.placeAll()
	if len(w.bars) != 1 {
		t.Fatalf("scrollbars = %d, want 1", len(w.bars))
	}
	bar := w.bars[0]
	x, y := bar.thumb.x+bar.thumb.w/2, bar.thumb.y+bar.thumb.h/2
	w.onButtonDown(x, y, 0)
	if w.press == nil || w.press.bar != nil {
		t.Fatal("scrollbar took the chrome press")
	}
	w.onButtonUp(x, y, 0)
	if clicks != 1 {
		t.Errorf("chrome clicks = %d, want 1", clicks)
	}
}

func TestCoveredScrollbarTakesNoPress(t *testing.T) {
	clicks := 0
	w := newListWindow(t, nil)
	w.tracking = true
	var mods keyboard.Mods
	w.scriptMods = &mods
	w.opts.View = func(cx *Cx) *El {
		cx.Overlay(Box(Box().Key("button").WFull().H(40).OnClick(func() { clicks++ })).Key("dialog").
			WFull().HFull().Center().OnClick(func() {}), nil)
		return Col(Box().WFull().H(400)).Key("scroll").WFull().HFull().ScrollY()
	}
	w.build()
	w.placeAll()
	if len(w.bars) != 1 {
		t.Fatalf("scrollbars = %d, want 1", len(w.bars))
	}
	bar, button := w.bars[0], w.findKey("button")
	x, y := bar.track.x+bar.track.w/2, button.absY+button.h/2
	w.onButtonDown(x, y, 0)
	if w.press == nil || w.press.bar != nil {
		t.Fatal("the scroll bar under the dialog took the press")
	}
	w.onButtonUp(x, y, 0)
	if clicks != 1 {
		t.Errorf("button clicks = %d, want 1", clicks)
	}
}

func TestUncoveredScrollbarTakesPress(t *testing.T) {
	// The narrow content leaves the bar over the container itself, which the rounded corner and the pass-through
	// container exclude from hit testing.
	for _, tc := range []struct {
		name     string
		contentW float32
		scroll   func(*El) *El
		at       func(bar scrollBar) (float32, float32)
	}{
		{"over content", 200, func(e *El) *El { return e }, func(b scrollBar) (float32, float32) {
			return b.track.x - 2, b.track.y + b.track.h/2
		}},
		{"rounded corner", 100, func(e *El) *El { return e.Radius(20) }, func(b scrollBar) (float32, float32) {
			return b.track.x + b.track.w, b.track.y - 1
		}},
		{"pass-through container", 100, func(e *El) *El { return e.PassThrough() }, func(b scrollBar) (float32, float32) {
			return b.track.x + b.track.w/2, b.track.y + b.track.h/2
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newListWindow(t, nil)
			w.tracking = true
			var mods keyboard.Mods
			w.scriptMods = &mods
			w.opts.View = func(cx *Cx) *El {
				content := Box().W(tc.contentW).H(400).OnClick(func() {})
				return tc.scroll(Col(content).Key("scroll").WFull().HFull().ScrollY())
			}
			w.build()
			w.placeAll()
			if len(w.bars) != 1 {
				t.Fatalf("scrollbars = %d, want 1", len(w.bars))
			}
			x, y := tc.at(w.bars[0])
			w.onButtonDown(x, y, 0)
			if w.press == nil || w.press.bar == nil {
				t.Fatalf("press at (%v, %v) did not take the scroll bar", x, y)
			}
			w.onButtonUp(x, y, 0)
		})
	}
}

func TestWindowChromeNativeHitTest(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := registerClass(); err != nil {
		t.Fatal(err)
	}
	hwnd, err := win.CreateWindowEx(0, className, nil, win.WS_OVERLAPPEDWINDOW,
		0, 0, 400, 300, 0, 0, win.GetModuleHandle(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		delete(windows, hwnd)
		win.DestroyWindow(hwnd)
	}()
	w := newListWindow(t, nil)
	w.hwnd, w.opts.Frameless = hwnd, true
	w.opts.View = func(cx *Cx) *El {
		cx.Overlay(Box().WFull().HFull(), nil)
		return Col(Row(
			Box().W(60).HFull().WindowChromeExclude(),
			Box().Grow(1).HFull(),
			Box().W(50).HFull().MaximizeButton().OnClick(func() {}),
		).WFull().H(36).WindowChrome().DragRegion()).WFull().HFull()
	}
	w.build()
	w.placeAll()
	windows[hwnd] = w
	for _, tc := range []struct {
		name string
		x, y int32
		want uintptr
	}{
		{name: "caption", x: 90, y: 20, want: win.HTCAPTION},
		{name: "maximize", x: 170, y: 20, want: win.HTMAXBUTTON},
		{name: "excluded", x: 20, y: 20, want: win.HTCLIENT},
		{name: "top resize", x: 90, y: 0, want: win.HTTOP},
	} {
		p := win.POINT{X: tc.x, Y: tc.y}
		win.ClientToScreen(hwnd, &p)
		lp := uintptr(uint32(uint16(p.X)) | uint32(uint16(p.Y))<<16)
		if got := win.SendMessage(hwnd, win.WM_NCHITTEST, 0, lp); got != tc.want {
			t.Errorf("%s hit = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestPopoverAnchorPress(t *testing.T) {
	for _, tc := range []struct {
		name     string
		opts     PopoverOptions
		toggle   bool
		wantOpen bool
	}{
		{
			name: "context menu",
			opts: PopoverOptions{AtPoint: true, X: 40, Y: 10, Side: SideRight, DismissOnAnchorPress: true},
		},
		{name: "dropdown toggle", toggle: true},
		{
			name:     "point placement with default dismissal",
			opts:     PopoverOptions{AtPoint: true, X: 40, Y: 10, Side: SideRight},
			wantOpen: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := tc.opts
			open := false
			clicks := 0
			opts.OnDismiss = func() { open = false }
			w := newPointerWindow(t, func() *El {
				anchor := Box().Size(100, 30).OnClick(func() {
					clicks++
					if tc.toggle {
						open = !open
					}
				}).OnRightClick(func(PointerEvent) { open = true })
				if open {
					anchor.Popover(Box().Size(80, 30), opts)
				}
				return Col(anchor)
			})
			button := 1
			if tc.toggle {
				button = 0
			}
			w.onButtonDown(40, 10, button)
			w.onButtonUp(40, 10, button)
			w.build()
			w.placeAll()
			if !open {
				t.Fatal("opening click did not open the popover")
			}
			clicksBefore := clicks
			w.onButtonDown(10, 10, 0)
			w.build()
			w.placeAll()
			w.onButtonUp(10, 10, 0)
			if open != tc.wantOpen {
				t.Errorf("popover open after anchor click = %v, want %v", open, tc.wantOpen)
			}
			if clicks != clicksBefore+1 {
				t.Error("left click did not reach the anchor exactly once")
			}
		})
	}
}

// An OnWheel handler that takes the wheel stops the scroll container around
// it from scrolling; one that declines passes it on.
func TestWheelHandler(t *testing.T) {
	handled := true
	var got []WheelEvent
	w := newPointerWindow(t, func() *El {
		return Col(
			Box().Size(100, 60).Key("view").OnWheel(func(ev WheelEvent) bool {
				got = append(got, ev)
				return handled
			}),
			Box().Size(100, 300),
		).Size(200, 100).ScrollY().Key("scroller")
	})
	scroller := w.root
	m := keyboard.ModCtrl
	w.scriptMods = &m

	w.onWheel(30, 20, -0.05, false, true) // a touchpad step towards the user
	if len(got) != 1 {
		t.Fatalf("handler calls = %d, want 1", len(got))
	}
	ev := got[0]
	if ev.DY != 0.05 || ev.DX != 0 || ev.LocalX != 30 || ev.LocalY != 20 || ev.W != 100 || ev.H != 60 ||
		ev.Mods != keyboard.ModCtrl ||
		!ev.Precise {
		t.Fatalf("event = %+v", ev)
	}
	if scroller.state.scrollY != 0 {
		t.Fatalf("scrolled to %v although the handler took the wheel", scroller.state.scrollY)
	}

	handled = false
	w.onWheel(30, 20, -1, false, true)
	if scroller.state.scrollY <= 0 {
		t.Fatal("a declined wheel did not reach the scroll container")
	}

	// A tilt to the right is DX > 0.
	w.onWheel(30, 20, -0.5, true, true)
	if ev := got[len(got)-1]; ev.DX != 0.5 || ev.DY != 0 {
		t.Fatalf("tilt event = %+v", ev)
	}
}

// A wheel gesture stays with the container it scrolled: a list that passes
// under the pointer does not take over the page's gesture, and a list at its
// end does not hand the rest of its gesture to the page. Moving the pointer or
// pausing starts a new gesture.
func TestWheelLatchesNestedScroll(t *testing.T) {
	pageWheels := 0
	w := newPointerWindow(t, func() *El {
		return Col(
			Box().Size(100, 40),
			Col(Box().Size(100, 120)).Size(100, 60).ScrollY().Key("list"),
			Box().Size(100, 300),
		).Size(200, 100).ScrollY().Key("page").OnWheel(func(WheelEvent) bool {
			pageWheels++
			return false
		})
	})
	page, list := w.root.state, w.root.children[1].state
	w.clock = &scriptClock{now: time.Unix(0, 0)}
	lines := uint32(3) // a notch is 100 pixels
	w.scriptWheelLines = &lines
	wheel := func(x, y, notches float32) {
		w.onWheel(x, y, notches, false, true)
		w.placeAll()
	}

	for range 3 {
		wheel(30, 20, -0.125)
	}
	if got := w.hitTest(30, 20); got == nil || got.parent == nil || got.parent.state != list {
		t.Fatal("the list did not scroll under the pointer")
	}
	wheel(30, 20, -0.125)
	if page.scrollY != 50 || list.scrollY != 0 {
		t.Fatalf("page at %v, list at %v: the list took over the page's gesture", page.scrollY, list.scrollY)
	}

	wheel(30, 30, -1)
	wheel(30, 30, -1)
	if page.scrollY != 50 || list.scrollY != 60 {
		t.Fatalf("after moving onto the list: page at %v, list at %v", page.scrollY, list.scrollY)
	}

	before := pageWheels
	w.clock.now = w.clock.now.Add(wheelLatchTimeout / 2)
	wheel(30, 30, -1)
	if page.scrollY != 50 {
		t.Fatalf("the page scrolled to %v within the list's gesture", page.scrollY)
	}
	if pageWheels != before+1 {
		t.Fatal("the page's OnWheel did not see the step the list could not take")
	}

	w.clock.now = w.clock.now.Add(wheelLatchTimeout)
	wheel(30, 30, -1)
	if page.scrollY <= 50 {
		t.Fatal("a new gesture after a pause did not reach the page")
	}
}

type dragLog struct {
	drags  []DragEvent
	rights int
}

func dragTarget(l *dragLog, buttons MouseButtons) *El {
	return Box().Size(100, 60).DragButtons(buttons).
		OnDrag(func(d DragEvent) { l.drags = append(l.drags, d) }).
		OnRightClick(func(PointerEvent) { l.rights++ })
}

// Right and middle drags start only on elements that ask for them; a right
// press that does not move far enough is still a right click.
func TestButtonDrags(t *testing.T) {
	var l dragLog
	buttons := MouseLeft | MouseRight | MouseMiddle
	w := newPointerWindow(t, func() *El { return Col(dragTarget(&l, buttons)) })
	press := func(b int, path ...[2]float32) {
		w.onMouseMove(path[0][0], path[0][1])
		w.onButtonDown(path[0][0], path[0][1], b)
		for _, p := range path[1:] {
			w.onMouseMove(p[0], p[1])
		}
		last := path[len(path)-1]
		w.onButtonUp(last[0], last[1], b)
	}

	press(1, [2]float32{10, 10}, [2]float32{11, 11})
	if l.rights != 1 || len(l.drags) != 0 {
		t.Fatalf("right click without moving: %d right clicks, %d drag events", l.rights, len(l.drags))
	}

	press(1, [2]float32{10, 10}, [2]float32{20, 10}, [2]float32{30, 15})
	if l.rights != 1 {
		t.Fatal("a right drag also ran OnRightClick")
	}
	if len(l.drags) != 3 || l.drags[0].Phase != DragStart || l.drags[2].Phase != DragEnd {
		t.Fatalf("right drag events = %+v", l.drags)
	}
	for _, d := range l.drags {
		if d.Button != 1 {
			t.Fatalf("right drag event button = %d", d.Button)
		}
	}
	if d := l.drags[2]; d.DX != 20 || d.DY != 5 || d.Canceled {
		t.Fatalf("right drag end = %+v", d)
	}

	l.drags = nil
	m := keyboard.ModShift
	w.scriptMods = &m
	press(2, [2]float32{10, 10}, [2]float32{10, 30})
	w.scriptMods = nil
	if len(l.drags) != 2 || l.drags[0].Button != 2 || l.drags[0].Mods != keyboard.ModShift {
		t.Fatalf("middle drag events = %+v", l.drags)
	}

	// Escape cancels a right drag like a left one.
	l.drags = nil
	w.onMouseMove(10, 10)
	w.onButtonDown(10, 10, 1)
	w.onMouseMove(40, 10)
	w.onKeyDown(keyboard.KeyEscape, false, "")
	if w.press != nil {
		t.Fatal("Escape left the press active")
	}
	if n := len(l.drags); n != 2 || !l.drags[1].Canceled || l.drags[1].Button != 1 {
		t.Fatalf("canceled right drag events = %+v", l.drags)
	}
	w.onButtonUp(40, 10, 1)
	if l.rights != 1 {
		t.Fatal("releasing after a canceled right drag ran OnRightClick")
	}
}

// Without DragButtons, only the left button drags, as before.
func TestDragDefaultsToLeft(t *testing.T) {
	var l dragLog
	w := newPointerWindow(t, func() *El { return Col(dragTarget(&l, 0)) })
	for _, b := range []int{1, 2} {
		w.onMouseMove(10, 10)
		w.onButtonDown(10, 10, b)
		w.onMouseMove(40, 10)
		w.onButtonUp(40, 10, b)
	}
	if len(l.drags) != 0 {
		t.Fatalf("right and middle presses dragged: %+v", l.drags)
	}
	if l.rights != 1 {
		t.Fatalf("right clicks = %d, want 1", l.rights)
	}
	w.onMouseMove(10, 10)
	w.onButtonDown(10, 10, 0)
	w.onMouseMove(40, 10)
	w.onButtonUp(40, 10, 0)
	if len(l.drags) != 2 || l.drags[0].Button != 0 {
		t.Fatalf("left drag events = %+v", l.drags)
	}
}

// A middle press on a scroll container autoscrolls it toward the pointer.
// Releasing after a move ends it; releasing in place keeps it on until the
// next press. An element that drags with the middle button keeps its drag.
func TestMiddleAutoScroll(t *testing.T) {
	var l dragLog
	w := newPointerWindow(t, func() *El {
		return Col(
			dragTarget(&l, MouseMiddle),
			Box().Size(100, 400),
		).Size(200, 100).ScrollY()
	})
	scroller := w.root
	frames := func(n int) {
		for range n {
			w.stepAutoScroll(1.0 / 60)
		}
	}

	w.onMouseMove(10, 30)
	w.onButtonDown(10, 30, 2)
	w.onMouseMove(10, 40)
	w.onButtonUp(10, 40, 2)
	if w.autoScroll != nil || len(l.drags) != 2 || scroller.state.scrollY != 0 {
		t.Fatalf(
			"middle drag target: autoscroll %v, drags %+v, scrollY %v",
			w.autoScroll != nil,
			l.drags,
			scroller.state.scrollY,
		)
	}

	// Held: the view follows the pointer and the release ends it.
	w.onMouseMove(150, 50)
	w.onButtonDown(150, 50, 2)
	frames(10)
	if scroller.state.scrollY != 0 {
		t.Fatalf("scrolled to %v with the pointer on the press point", scroller.state.scrollY)
	}
	w.onMouseMove(150, 90)
	frames(10)
	down := scroller.state.scrollY
	if down <= 0 {
		t.Fatal("pointer below the press point did not scroll down")
	}
	w.onMouseMove(150, 10)
	frames(5)
	if scroller.state.scrollY >= down {
		t.Fatal("pointer above the press point did not scroll up")
	}
	w.onButtonUp(150, 10, 2)
	if w.autoScroll != nil {
		t.Fatal("release after moving left autoscroll on")
	}

	// Clicked in place: it stays on after the release; the next press ends it
	// without reaching anything else.
	w.onMouseMove(150, 50)
	w.onButtonDown(150, 50, 2)
	w.onButtonUp(150, 50, 2)
	if w.autoScroll == nil {
		t.Fatal("middle click in place did not keep autoscroll on")
	}
	w.onMouseMove(150, 90)
	from := scroller.state.scrollY
	frames(5)
	if scroller.state.scrollY <= from {
		t.Fatal("autoscroll after the release did not scroll")
	}
	w.onButtonDown(150, 90, 0)
	w.onButtonUp(150, 90, 0)
	if w.autoScroll != nil || w.press != nil {
		t.Fatal("a left press did not end autoscroll on its own")
	}
}

// Pointer moves reach OnPointerMove once per frame, only while no button is held.
func TestPointerMove(t *testing.T) {
	var moves []PointerEvent
	changed := false
	w := newPointerWindow(t, func() *El {
		return Col(
			Box().Size(100, 40),
			Box().Size(100, 50).OnPointerMove(func(p PointerEvent) bool { moves = append(moves, p); return changed }),
		)
	})

	w.onMouseMove(10, 10)
	w.flushPointerMove()
	if len(moves) != 0 || w.hoverMoves {
		t.Fatal("a move outside the element reached OnPointerMove")
	}

	w.onMouseMove(10, 50)
	w.onMouseMove(12, 55)
	w.dirty = false
	w.flushPointerMove()
	w.flushPointerMove()
	if len(moves) != 1 {
		t.Fatalf("moves in one frame = %d, want 1", len(moves))
	}
	if w.dirty {
		t.Fatal("a move whose handler changed nothing rebuilds the view")
	}
	if p := moves[0]; p.X != 12 || p.Y != 55 || p.LocalX != 12 || p.LocalY != 15 || p.Button != -1 {
		t.Fatalf("move = %+v", p)
	}

	w.onButtonDown(12, 55, 0)
	w.onMouseMove(20, 60)
	w.flushPointerMove()
	if len(moves) != 1 {
		t.Fatal("a move with a button held reached OnPointerMove")
	}
	// The release reports where the pointer ended up.
	changed = true
	w.onButtonUp(20, 60, 0)
	w.dirty = false
	w.flushPointerMove()
	if !w.dirty {
		t.Fatal("a move whose handler changed the view does not rebuild it")
	}
	if len(moves) != 2 || moves[1].LocalX != 20 || moves[1].LocalY != 20 {
		t.Fatalf("moves after the release = %+v", moves)
	}
}

// A press outside a rounded corner misses the element there.
func TestHitTestRoundedCorner(t *testing.T) {
	var round *El
	w := newPointerWindow(t, func() *El {
		round = Box().Size(40, 40).Radius(20).OnClick(func() {})
		return Col(round)
	})
	x, y := round.absX, round.absY
	for _, tc := range []struct {
		name   string
		dx, dy float32
		hit    bool
	}{
		{"center", 20, 20, true},
		{"top-left corner", 2, 2, false},
		{"top-right corner", 38, 2, false},
		{"bottom-right corner", 38, 38, false},
		{"bottom-left corner", 2, 38, false},
		{"edge midpoint", 20, 1, true},
	} {
		if got := w.hitTest(x+tc.dx, y+tc.dy) == round; got != tc.hit {
			t.Errorf("%s: hit = %v, want %v", tc.name, got, tc.hit)
		}
	}
}
