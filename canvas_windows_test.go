package fairy

import (
	"math"
	"slices"
	"testing"

	"github.com/myparsleycat/fairy/internal/render"
	"github.com/myparsleycat/fairy/internal/text"
)

// A retained canvas moved by whole device pixels replays what it drew,
// matching a fresh drawing there, and draws again when moved within a pixel.
func TestCanvasRetainReplay(t *testing.T) {
	paint := func(top float32, retain bool, calls *int) (*Window, func(float32) []render.Prim) {
		w := newListWindow(t, nil)
		w.opts.View = func(*Cx) *El {
			c := Canvas(func(p *Painter) {
				*calls++
				p.Rect(5, 5, 20, 10, Hex(0xff0000))
				p.Line(0, 0, 40, 30, 2, Hex(0x00ff00))
			}).Size(100, 50)
			if retain {
				c.Retain(1)
			}
			return Col(Box().H(top), c)
		}
		frame := func(at float32) []render.Prim {
			top = at
			w.build()
			w.placeAll()
			w.scene.Reset()
			w.paint(w.root, 1, w.opts.TextColor)
			return slices.Clone(w.scene.Prims)
		}
		return w, frame
	}
	var retained, fresh int
	_, frame := paint(10, true, &retained)
	_, want := paint(13, false, &fresh)
	frame(10)
	if got, want := frame(13), want(13); !slices.Equal(got, want) || retained != 1 {
		for i := range got {
			if got[i] != want[i] {
				t.Logf("prim %d: got %+v, want %+v", i, got[i], want[i])
			}
		}
		t.Fatalf("after a whole-pixel move: %d draws, prims match %v", retained, slices.Equal(got, want))
	}
	frame(13.5)
	if retained != 2 {
		t.Fatalf("a move within a pixel drew %d times, want 2", retained)
	}
}

// clippedCanvas shows a 100x50 canvas drawn by draw below a gap, in a box
// that clips at 40 pixels, retained with *version when version is not nil.
// The function it returns paints a frame with a gap of top pixels and
// returns its prims.
func clippedCanvas(t *testing.T, r *render.Renderer, draw func(*Painter), version *uint64) (*Window, func(top float32) []render.Prim) {
	w := newListWindow(t, nil)
	w.r = r
	ts, err := text.NewSystem(r.Atlas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ts.Release)
	w.ts = ts
	t.Cleanup(w.releasePathTextures)
	var top float32
	w.opts.View = func(*Cx) *El {
		c := Canvas(draw).Size(100, 50).Shrink(0)
		if version != nil {
			c.Retain(*version)
		}
		return Col(Col(Box().H(top).Shrink(0), c).H(40).Clip())
	}
	return w, func(at float32) []render.Prim {
		top = at
		w.frame++
		w.build()
		w.placeAll()
		w.scene.Reset()
		w.paint(w.root, 1, w.opts.TextColor)
		w.sweepCanvases()
		w.sweepPathTextures()
		return slices.Clone(w.scene.Prims)
	}
}

func offscreenRenderer(t *testing.T) *render.Renderer {
	r, err := render.NewOffscreen(8, 8)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Release)
	return r
}

func rectPath(x, y, w, h float32) *Path {
	var p Path
	p.MoveTo(x, y)
	p.LineTo(x+w, y)
	p.LineTo(x+w, y+h)
	p.LineTo(x, y+h)
	p.Close()
	return &p
}

// A retained canvas drawn partly clipped shows the rest of its paths and
// text when a whole-pixel move brings it into view.
func TestCanvasRetainClipped(t *testing.T) {
	r := offscreenRenderer(t)
	bar := rectPath(0, 0, 80, 45)
	paint := func(calls *int, version *uint64) func(float32) [][6]float32 {
		_, frame := clippedCanvas(t, r, func(p *Painter) {
			*calls++
			p.FillPath(bar, Hex(0xff0000))
			p.Text(0, 30, "Hi", 10, Hex(0x0000ff))
		}, version)
		return func(top float32) [][6]float32 {
			// What each prim shows: its kind, color and bounds within its clip.
			var shown [][6]float32
			for _, p := range frame(top) {
				b := intersect4(p.Bounds, p.Clip)
				if b[2] > 0 && b[3] > 0 {
					shown = append(shown, [6]float32{float32(p.Kind), p.Color[2], b[0], b[1], b[2], b[3]})
				}
			}
			return shown
		}
	}
	var retained, fresh int
	version := uint64(1)
	frame := paint(&retained, &version)
	want := paint(&fresh, nil)
	frame(10)
	got := frame(0)
	if w := want(0); !slices.Equal(got, w) || retained != 1 {
		t.Fatalf("after a move into view, %d draws:\n got %v\nwant %v", retained, got, w)
	}
}

func imagePrims(ps []render.Prim) []render.Prim {
	var out []render.Prim
	for _, p := range ps {
		if p.Kind == render.KindImage {
			out = append(out, p)
		}
	}
	return out
}

// A replayed drawing leaves out a path out of view and keeps its texture for
// when it comes back.
func TestCanvasReplayCullsOffClip(t *testing.T) {
	r := offscreenRenderer(t)
	bar := rectPath(0, 32, 80, 13)
	calls := 0
	version := uint64(1)
	w, frame := clippedCanvas(t, r, func(p *Painter) {
		calls++
		p.FillPath(bar, Hex(0xff0000))
	}, &version)
	frame(10)
	if n := len(imagePrims(frame(11))); n != 0 || len(w.pathTextures) != 1 || calls != 1 {
		t.Fatalf("out of view: %d images, %d textures kept, %d draws; want 0, 1 and 1", n, len(w.pathTextures), calls)
	}
	if n := len(imagePrims(frame(0))); n != 1 || calls != 1 {
		t.Fatalf("back in view: %d images, %d draws; want 1 and 1", n, calls)
	}
}

// A retained canvas whose version changes before its drawing is replayed
// draws only the part in view, and the whole canvas once the version holds.
func TestCanvasRetainChurnDrawsInView(t *testing.T) {
	r := offscreenRenderer(t)
	bar := rectPath(0, 0, 80, 45)
	calls := 0
	version := uint64(1)
	_, frame := clippedCanvas(t, r, func(p *Painter) {
		calls++
		p.FillPath(bar, Hex(0xff0000))
	}, &version)
	height := func(ps []render.Prim) float32 {
		if im := imagePrims(ps); len(im) == 1 {
			return im[0].Bounds[3]
		}
		return -1
	}
	// The canvas spans 10 to 60 and the clip ends at 40; the bar with its
	// fringe spans 9 to 56.
	if h := height(frame(10)); h != 46 {
		t.Fatalf("first drawing is %v high, want 46", h)
	}
	version++
	if h := height(frame(10)); h != 30 {
		t.Fatalf("drawing for a new version is %v high, want 30", h)
	}
	if h := height(frame(10)); h != 46 || calls != 3 {
		t.Fatalf("once the version holds: %v high after %d draws, want 46 after 3", h, calls)
	}
	if h := height(frame(10)); h != 46 || calls != 3 {
		t.Fatalf("replay: %v high after %d draws, want 46 after 3", h, calls)
	}
}

// Filled and stroked paths and lines land where they are drawn.
func TestCanvasDrawsOffscreen(t *testing.T) {
	var tri Path
	tri.MoveTo(10, 10)
	tri.LineTo(50, 10)
	tri.LineTo(10, 50)
	tri.Close()
	var ring Path
	ring.Arc(75, 30, 15, 0, 2*math.Pi)
	el := Canvas(func(p *Painter) {
		p.FillPath(&tri, Hex(0xff0000))
		p.StrokePath(&ring, 4, Hex(0x0000ff))
		p.Line(10, 70, 90, 90, 3, Hex(0x00ff00))
	}).Size(100, 100)
	img, err := RenderImage(el, 100, 100, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		x, y    int
		r, g, b uint8
		opaque  bool
	}{
		{20, 20, 255, 0, 0, true}, // inside the triangle
		{45, 45, 0, 0, 0, false},  // beyond its hypotenuse
		{75, 15, 0, 0, 255, true}, // on the ring
		{75, 30, 0, 0, 0, false},  // its middle
		{50, 80, 0, 255, 0, true}, // on the line
		{50, 70, 0, 0, 0, false},  // off it
		{37, 7, 0, 0, 0, false},   // between the corner and the ring's start
	} {
		px := img.NRGBAAt(c.x, c.y)
		if c.opaque && (px.A < 200 || px.R != c.r || px.G != c.g || px.B != c.b) || !c.opaque && px.A > 30 {
			t.Errorf("pixel (%d, %d) = %v", c.x, c.y, px)
		}
	}
}
