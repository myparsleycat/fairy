package fairy

import "testing"

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
