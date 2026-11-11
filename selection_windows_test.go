package fairy

import (
	"testing"

	"github.com/myparsleycat/fairy/keyboard"
)

func newSelectionWindow(t *testing.T, onLink func()) (*Window, *string) {
	w := newPointerWindow(t, func() *El {
		return Col(
			Text("first paragraph").FontSize(14).Key("a"),
			RichText(Span{Text: "see "}, Span{Text: "link", OnClick: onLink}, Span{Text: " here"}).FontSize(14).Key("b"),
		).Selectable().W(200).Gap(10)
	})
	clip := ""
	w.fakeClip = &clip
	w.width, w.height = 400, 200
	w.dirty = true
	w.build()
	w.placeAll()
	return w, &clip
}

func (w *Window) textEl(key string) *El {
	for _, e := range w.byID {
		if e.key == key {
			return e
		}
	}
	return nil
}

func ctrl(w *Window, k keyboard.Key) {
	m := keyboard.ModCtrl
	w.scriptMods = &m
	w.onKeyDown(k, false, "")
	w.scriptMods = nil
}

// A drag from the first paragraph into the second selects across both, and
// Ctrl+C copies the text with a line break between them.
func TestSelectAcrossTexts(t *testing.T) {
	w, clip := newSelectionWindow(t, nil)
	a, b := w.textEl("a"), w.textEl("b")
	w.onButtonDown(a.absX+1, a.absY+a.h/2, 0)
	w.onMouseMove(b.absX+b.w-1, b.absY+b.h/2)
	w.onButtonUp(b.absX+b.w-1, b.absY+b.h/2, 0)
	ctrl(w, keyboard.KeyC)
	if *clip != "first paragraph\nsee link here" {
		t.Fatalf("copied %q", *clip)
	}
	// Dragging back up selects in reverse; the copy is the same text.
	w.onButtonDown(b.absX+b.w-1, b.absY+b.h/2, 0)
	w.onMouseMove(a.absX+1, a.absY+a.h/2)
	w.onButtonUp(a.absX+1, a.absY+a.h/2, 0)
	if got := w.SelectedText(); got != "first paragraph\nsee link here" {
		t.Fatalf("reverse selection %q", got)
	}
	// A press outside any selectable element clears it.
	w.onButtonDown(390, 190, 0)
	w.onButtonUp(390, 190, 0)
	if w.SelectedText() != "" {
		t.Fatal("press outside kept the selection")
	}
}

func TestSelectWordAndAll(t *testing.T) {
	w, clip := newSelectionWindow(t, nil)
	a := w.textEl("a")
	x, y := a.absX+3, a.absY+a.h/2
	for range 2 {
		w.onButtonDown(x, y, 0)
		w.onButtonUp(x, y, 0)
	}
	if got := w.SelectedText(); got != "first" {
		t.Fatalf("double-click selected %q, want the word", got)
	}
	ctrl(w, keyboard.KeyA)
	ctrl(w, keyboard.KeyInsert)
	if *clip != "first paragraph\nsee link here" {
		t.Fatalf("Ctrl+A, Ctrl+Insert copied %q", *clip)
	}
}

// A click on a link without a drag still opens it; a drag across it selects
// instead.
func TestSelectableLinks(t *testing.T) {
	clicks := 0
	w, _ := newSelectionWindow(t, func() { clicks++ })
	b := w.textEl("b")
	l, ox, oy := w.textLayout(b)
	r := l.RangeRects(4, 8)[0]
	lx, ly := (ox+r.X+r.W/2)/w.scale, (oy+r.Y+r.H/2)/w.scale
	w.onButtonDown(lx, ly, 0)
	w.onButtonUp(lx, ly, 0)
	if clicks != 1 {
		t.Fatalf("link clicks %d after a click, want 1", clicks)
	}
	w.onButtonDown(lx, ly, 0)
	w.onMouseMove(b.absX+b.w-1, ly)
	w.onButtonUp(b.absX+b.w-1, ly, 0)
	if clicks != 1 {
		t.Fatalf("link clicks %d after a drag, want still 1", clicks)
	}
	if got := w.SelectedText(); got == "" {
		t.Fatal("drag from the link selected nothing")
	}
}
