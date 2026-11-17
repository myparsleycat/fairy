package fairy

import (
	"testing"

	"github.com/myparsleycat/fairy/internal/render"
)

// Memo skips build while its version holds, yet the reused subtree follows
// what it inherits, its Local state stays alive, a disabled ancestor turning
// enabled gets working handlers back, and a key used twice builds twice.
func TestMemo(t *testing.T) {
	builds, version := 0, 1
	disabled, twice := false, false
	color := Hex(0xff0000)
	var label *El
	var local *int
	w := newListWindow(t, func() *El {
		memo := func() *El {
			return Memo("m", version, func() *El {
				builds++
				local = Local[int]("memo.local")
				label = Text("x")
				return Box(label).OnClick(func() {})
			})
		}
		root := Col(memo()).Color(color)
		if disabled {
			root.Disabled(true)
		}
		if twice {
			root.Child(memo())
		}
		return root
	})
	frame := func() {
		w.dirty = true
		for i := 0; i < 3 && w.dirty; i++ {
			w.build()
		}
	}
	frame()
	*local = 7
	for range 5 {
		frame()
	}
	if builds != 1 {
		t.Fatalf("built %d times with an unchanged version, want 1", builds)
	}
	if l, ok := w.locals["memo.local"]; !ok || *l.value.(*int) != 7 {
		t.Fatalf("Local inside a reused Memo was dropped")
	}

	color = Hex(0x0000ff)
	frame()
	if label.tp.color != color {
		t.Fatalf("reused text color %v, want the new inherited %v", label.tp.color, color)
	}

	version = 2
	frame()
	if builds != 2 {
		t.Fatalf("built %d times after the version changed, want 2", builds)
	}

	disabled = true
	frame()
	disabled = false
	frame()
	if box := label.parent; box.onClick == nil || box.disabled() {
		t.Fatalf("subtree still disabled after its ancestor was enabled")
	}

	n := builds
	twice = true
	frame()
	if builds != n+1 {
		t.Fatalf("a key used twice in one build ran build %d times, want once more", builds-n)
	}
}

// A reused Memo subtree keeps its laid-out sizes only while it is laid out
// in the same room with the same inherited text: a narrower window or a
// larger inherited font size lays it out as a fresh build of it would.
func TestMemoLaysOutAgainWhenRoomOrTextChanges(t *testing.T) {
	version, size := 1, float32(14)
	var label *El
	w := newListWindow(t, func() *El {
		return Col(Memo("m", version, func() *El {
			label = Text("one two three four five six seven eight nine ten eleven twelve")
			return Col(label)
		})).FontSize(size)
	})
	frame := func() {
		w.dirty = true
		for i := 0; i < 3 && w.dirty; i++ {
			w.build()
		}
	}
	fresh := func() float32 {
		version++
		frame()
		return label.node.H
	}
	frame()
	frame()
	wide := label.node.H

	w.width = 100
	frame()
	got := label.node.H
	if want := fresh(); got != want || got <= wide {
		t.Fatalf("reused text in a narrower window is %v tall, want %v (it was %v)", got, want, wide)
	}

	frame()
	size = 22
	frame()
	got = label.node.H
	if want := fresh(); got != want {
		t.Fatalf("reused text after the inherited size grew is %v tall, want %v", got, want)
	}
}

// Setting a Live value redraws with it without building the view again.
func TestLiveSetPaintsWithoutBuild(t *testing.T) {
	fill := NewLive(Hex(0xff0000))
	builds := 0
	w := newListWindow(t, func() *El {
		builds++
		return Box().Size(20, 20).FillLive(fill)
	})
	painted := func() [4]float32 {
		w.placeAll()
		w.scene.Reset()
		w.paint(w.root, 1, w.opts.TextColor)
		for _, p := range w.scene.Prims {
			if p.Kind == render.KindRect {
				return p.Color
			}
		}
		t.Fatal("no fill drawn")
		return [4]float32{}
	}
	w.build()
	painted()
	fill.Set(Hex(0x0000ff))
	if w.dirty {
		t.Fatal("Set marked the view for building")
	}
	if c := painted(); c != Hex(0x0000ff).arr(1) || builds != 1 {
		t.Fatalf("after Set: fill %v with %d builds, want blue with 1", c, builds)
	}
}
