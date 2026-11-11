package fairy

import (
	"testing"

	"github.com/myparsleycat/fairy/motion"
)

// A popover closed and opened again within a few builds runs its enter
// transition again instead of appearing at once.
func TestPopoverReopenRunsEnter(t *testing.T) {
	open := true
	var pop *El
	w := newListWindow(t, nil)
	w.opts.View = func(cx *Cx) *El {
		a := Box().Size(20, 20)
		if open {
			pop = Box().Size(50, 30).Key("pop").Enter(Transition{Motion: motion.Popup})
			a.Popover(pop, PopoverOptions{})
		}
		return a
	}
	frame := func(dt float32) {
		w.build()
		w.dt = dt
		w.placeAll()
	}
	frame(0)
	frame(0)
	for range 30 {
		frame(1.0 / 60)
	}
	if pop.fade != 1 {
		t.Fatalf("popover fade after opening = %.2f, want 1", pop.fade)
	}
	open = false
	frame(0)
	open = true
	frame(0)
	if pop.fade != 0 {
		t.Fatalf("reopened popover starts at fade %.2f, want 0", pop.fade)
	}
}

// A popover that flips above its anchor slides in from below and grows from
// its bottom edge.
func TestPopoverFlipTurnsSlide(t *testing.T) {
	var pop *El
	open := false
	w := newListWindow(t, nil)
	w.opts.View = func(cx *Cx) *El {
		a := Box().Size(20, 20)
		if open {
			pop = Box().Size(50, 40).Key("pop").Enter(Transition{Slide: 8, Scale: 0.95, Motion: motion.Popup})
			a.Popover(pop, PopoverOptions{})
		}
		return Col(Spacer(), a).HFull()
	}
	w.build()
	w.placeAll()
	open = true
	w.build()
	w.placeAll()
	if Side(pop.popSide-1) != SideTop {
		t.Fatalf("popover side = %d, want SideTop", pop.popSide-1)
	}
	if ox, oy := pop.origin(); oy != 1 || ox <= 0 || ox >= 1 {
		t.Fatalf("popover origin = (%.2f, %.2f), want the bottom edge at the anchor", ox, oy)
	}
	if d := pop.absY - pop.node.Y; d != 8 {
		t.Fatalf("popover starts %.1f below its place, want 8", d)
	}
}

// A scrolling popover taller than the room beside its anchor is cut to the
// roomier side instead of covering the anchor.
func TestScrollingPopoverFitsRoom(t *testing.T) {
	var a, pop *El
	w := newListWindow(t, nil)
	w.opts.View = func(cx *Cx) *El {
		a = Box().Size(20, 20)
		pop = Col(Box().H(300)).ScrollY()
		a.Popover(pop, PopoverOptions{})
		return Col(Box().H(30), a)
	}
	w.build()
	w.placeAll()
	// The anchor spans 30 to 50 of the 100 high window: 42 below, 22 above.
	if pop.node.H != 42 || pop.absY != 54 {
		t.Fatalf("popover %v high at %v, want 42 high at 54", pop.node.H, pop.absY)
	}
}

// Elements with an Exit inside a leaving overlay run it together with the
// overlay's own.
func TestNestedExitRuns(t *testing.T) {
	open := true
	w := newListWindow(t, nil)
	w.opts.View = func(cx *Cx) *El {
		if open {
			cx.Overlay(Box(Box().Size(40, 40).Key("panel").Exit(ScaleFade(0.5).With(motion.Ease(1)))).
				Key("scrim").Exit(Fade().With(motion.Ease(1))), nil)
		}
		return Box()
	}
	w.build()
	w.placeAll()
	w.build()
	w.placeAll()
	open = false
	w.build()
	w.placeAll()
	if len(w.ghosts) != 1 || len(w.ghosts[0].subs) != 1 {
		t.Fatalf("ghosts = %d, want the overlay with its panel inside", len(w.ghosts))
	}
	w.dt = 0.5
	w.stepGhosts()
	sub := w.ghosts[0].subs[0].el
	if !sub.xfOwn || sub.ownXf.a >= 1 || sub.fade >= 1 {
		t.Fatalf("panel inside the leaving overlay is not leaving: scale %.2f fade %.2f", sub.ownXf.a, sub.fade)
	}
}

// FocusRing shows for focus the keyboard moved and for any focus of an
// Input, as :focus-visible.
func TestFocusVisible(t *testing.T) {
	var btn, field, wrap *El
	w := newListWindow(t, func() *El {
		btn = Box().Size(20, 20).Focusable().Key("btn")
		field = Input("", nil).Key("field")
		wrap = Row(field).FocusWithin()
		return Col(btn, wrap)
	})
	w.build()
	w.placeAll()
	w.focused, w.keyFocus = btn.id, false
	if f, v := w.focusOf(btn); !f || v {
		t.Fatalf("clicked button: focused %v visible %v, want true false", f, v)
	}
	w.keyFocus = true
	if _, v := w.focusOf(btn); !v {
		t.Fatal("button focused by the keyboard does not show focus")
	}
	w.focused, w.keyFocus = field.id, false
	if f, v := w.focusOf(wrap); !f || !v {
		t.Fatalf("clicked input inside FocusWithin: focused %v visible %v, want true true", f, v)
	}
}

// An element with AnimateHeight shows a new height over several frames: the
// element after it follows the shown height, and a StickToBottom scroller
// holding it stays at the end while it grows.
func TestAnimateHeight(t *testing.T) {
	tall := false
	var section, after *El
	w := newListWindow(t, nil)
	w.opts.View = func(cx *Cx) *El {
		h := float32(20)
		if tall {
			h = 80
		}
		section = Col(Box().H(h)).AnimateHeight(motion.Ease(0.2))
		after = Box().H(10)
		return Col(Box().H(60), section, after).ScrollY().StickToBottom()
	}
	frame := func(dt float32) {
		if w.dirty {
			w.build()
		}
		w.dt = dt
		w.placeAll()
	}
	check := func(when string) {
		t.Helper()
		if d := after.absY - (section.absY + section.h); d < -0.01 || d > 0.01 {
			t.Errorf("%s: next element at %.2f, want right below the shown height at %.2f", when, after.absY, section.absY+section.h)
		}
		s := w.root.state
		if end := max(60+section.h+10-100, 0); s.scrollY < end-0.01 || s.scrollY > end+0.01 {
			t.Errorf("%s: scrolled to %.2f, want the end at %.2f", when, s.scrollY, end)
		}
	}
	frame(0)
	frame(0)
	tall = true
	w.dirty = true
	frame(0)
	if section.h != 20 {
		t.Fatalf("section shows %.2f right after growing, want the old 20", section.h)
	}
	check("start")
	frame(0.1)
	if section.h <= 20 || section.h >= 80 {
		t.Fatalf("section shows %.2f halfway, want between 20 and 80", section.h)
	}
	check("halfway")
	frame(0.1)
	frame(0)
	if section.h != 80 {
		t.Fatalf("section shows %.2f at the end, want 80", section.h)
	}
	check("end")
}

// A scroll container whose last child collapses with AnimateHeight never gets
// more content than it shows: the child's growth counts once per frame.
func TestAnimateHeightCountsGrowthOnce(t *testing.T) {
	tall := true
	var sc, panel *El
	w := newListWindow(t, nil)
	w.height = 400
	w.opts.View = func(cx *Cx) *El {
		h := float32(0)
		if tall {
			h = 250
		}
		panel = Col(Box().H(h)).AnimateHeight(motion.Ease(0.5))
		sc = Col(Box().H(50), Box().H(50), panel).ScrollY().H(400)
		return sc
	}
	frame := func(dt float32) {
		if w.dirty {
			w.build()
		}
		w.dt = dt
		w.placeAll()
	}
	frame(0)
	frame(0)
	tall = false
	w.dirty = true
	frame(0)
	for i := range 6 {
		frame(0.05)
		if want := max(400, 100+panel.h); sc.node.ContentH > want+0.5 || len(w.bars) != 0 {
			t.Fatalf("frame %d: content %.1f with %d scroll bars, want %.1f and none", i, sc.node.ContentH, len(w.bars), want)
		}
	}
}
