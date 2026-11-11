package kit

import (
	"math"
	"testing"

	"github.com/myparsleycat/fairy"
)

func TestPaneWidthsClampRoundAndCollapse(t *testing.T) {
	p := NewPane(276).Range(136, 640)
	p.SetWidth(1000)
	if p.Width() != 640 {
		t.Fatalf("width %v, want 640", p.Width())
	}
	p.SetWidth(300.6)
	if p.Width() != 301 {
		t.Fatalf("width %v, want 301", p.Width())
	}
	p.DragTo(80)
	if !p.Open() || p.Width() != 136 {
		t.Fatalf("above half the minimum: open %v width %v, want clamped to 136", p.Open(), p.Width())
	}
	p.DragTo(60)
	if p.Open() || p.Width() != 136 || p.ShownWidth() != 0 {
		t.Fatalf("below half the minimum: open %v width %v, want collapsed keeping 136", p.Open(), p.Width())
	}
	p.DragTo(200)
	if !p.Open() || p.Width() != 200 {
		t.Fatalf("dragged back out: open %v width %v", p.Open(), p.Width())
	}
	p.SetWidth(float32(math.NaN()))
	p.ResetWidth()
	if p.Width() != 276 {
		t.Fatalf("reset to %v, want 276", p.Width())
	}
}

func TestPaneDefaultFollowsTheRange(t *testing.T) {
	p := NewPane(640.4).WithWidth(500)
	if p.Width() != 480 || p.DefaultWidth() != 480 {
		t.Fatalf("got %v/%v, want 480/480", p.Width(), p.DefaultWidth())
	}
	p = p.Range(100, 200).Range(360, 960)
	if p.Width() != 500 || p.DefaultWidth() != 640 {
		t.Fatalf("ranges accumulated: %v/%v, want 500/640", p.Width(), p.DefaultWidth())
	}
}

func TestPaneDragCancelRestores(t *testing.T) {
	p := NewPane(240)
	paneDrag(&p, 1, fairy.DragEvent{Phase: fairy.DragStart})
	paneDrag(&p, 1, fairy.DragEvent{Phase: fairy.DragMove, DX: -200})
	if p.Open() || !p.Resizing() {
		t.Fatalf("dragged past half the minimum: open %v resizing %v", p.Open(), p.Resizing())
	}
	paneDrag(&p, 1, fairy.DragEvent{Phase: fairy.DragEnd, Canceled: true})
	if !p.Open() || p.Width() != 240 || p.Resizing() {
		t.Fatalf("after Escape: open %v width %v resizing %v, want open at 240", p.Open(), p.Width(), p.Resizing())
	}

	q := NewPane(300).Collapsed()
	paneDrag(&q, -1, fairy.DragEvent{Phase: fairy.DragStart})
	paneDrag(&q, -1, fairy.DragEvent{Phase: fairy.DragMove, DX: -50})
	if !q.Open() || q.Width() != 350 {
		t.Fatalf("inspector drag: open %v width %v, want open at 350", q.Open(), q.Width())
	}
	paneDrag(&q, -1, fairy.DragEvent{Phase: fairy.DragEnd, Canceled: true})
	if q.Open() || q.Width() != 300 {
		t.Fatalf("after Escape: open %v width %v, want closed at 300", q.Open(), q.Width())
	}
}
