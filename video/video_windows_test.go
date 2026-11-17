package video

import (
	"testing"
	"time"

	"github.com/myparsleycat/fairy"
)

// A slow video must sleep until shortly before its next frame, ignore a
// dropped frame and a loop, and poll every refresh once a frame is late.
func TestVideoPacerWakesBeforeNextFrame(t *testing.T) {
	const step = 41_708_333 * time.Nanosecond // 23.976 fps
	var p videoPacer
	now := time.Unix(0, 0)
	pts := func(d time.Duration) int64 { return int64(d / 100) }
	for i := range 3 {
		p.frame(pts(time.Duration(i) * step))
	}
	if p.interval != step/100*100 {
		t.Fatalf("interval %v, want %v", p.interval, step)
	}
	p.frame(pts(4 * step)) // frame 3 was dropped
	p.frame(pts(0))        // the video looped
	if p.interval != step/100*100 {
		t.Fatalf("interval after a drop and a loop %v, want %v", p.interval, step)
	}

	at, ok := p.wake(now, true, 10*time.Millisecond)
	if want := now.Add(p.interval - 10*time.Millisecond - videoWakeEarly); !ok || !at.Equal(want) {
		t.Fatalf("wake after a new frame = %v, %v; want %v", at, ok, want)
	}
	if again, ok := p.wake(now.Add(5*time.Millisecond), false, 0); !ok || !again.Equal(at) {
		t.Fatalf("an unrelated paint moved the wake-up to %v, %v", again, ok)
	}
	if _, ok := p.wake(at, false, 0); ok {
		t.Fatal("a wake-up without its frame did not poll")
	}

	var fast videoPacer
	for i := range 4 {
		fast.frame(pts(time.Duration(i) * time.Second / 60))
	}
	if _, ok := fast.wake(now, true, 0); ok {
		t.Fatal("60 fps video was not polled every refresh")
	}
}

func TestViewOfNilDrawsNothing(t *testing.T) {
	if _, err := fairy.RenderImage(View(nil).W(8).H(8), 8, 8, 1); err != nil {
		t.Fatal(err)
	}
}
