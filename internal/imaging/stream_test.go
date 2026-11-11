package imaging

import (
	"bytes"
	"image"
	"image/gif"
	"testing"
	"testing/synctest"
	"time"
)

func TestStreamedAnimationMatchesKeptFrames(t *testing.T) {
	g := &gif.GIF{LoopCount: 0}
	for i := range 12 {
		r := image.Rect(i%5*4, i%3*4, 32+i%5*4, 24+i%3*4)
		g.Image = append(g.Image, solidPaletted(r, uint8(10+i*17)))
		g.Delay = append(g.Delay, 5)
		g.Disposal = append(g.Disposal, []byte{gif.DisposalNone, gif.DisposalBackground, gif.DisposalPrevious}[i%3])
	}
	g.Config.Width, g.Config.Height = 64, 48
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	bound := Bound{W: 48, H: 48}
	kept, err := Decode(buf.Bytes(), bound, maxAnimBytes)
	if err != nil || kept.FrameCount() != 12 || kept.Stream != nil {
		t.Fatalf("kept %d frames, %v", kept.FrameCount(), err)
	}
	synctest.Test(t, func(t *testing.T) {
		// One byte short of keeping every frame. The stream's channels must
		// be made in the bubble.
		budget := int64(len(kept.FrameData))*int64(len(kept.Pixels)) - 1
		streamed, err := Decode(buf.Bytes(), bound, budget)
		if err != nil || streamed.Stream == nil {
			t.Fatalf("not streamed within %d bytes: %v", budget, err)
		}
		if streamed.FrameCount() != 12 || streamed.Total != kept.Total || streamed.W != kept.W || streamed.H != kept.H {
			t.Fatalf("streamed %d frames of %v at %dx%d, want %d of %v at %dx%d", streamed.FrameCount(), streamed.Total,
				streamed.W, streamed.H, kept.FrameCount(), kept.Total, kept.W, kept.H)
		}
		if !bytes.Equal(streamed.Pixels, kept.FrameData[0].Pixels) {
			t.Fatal("first frame differs")
		}
		s := streamed.Stream
		frame := func(n int) []byte {
			t.Helper()
			var got []byte
			for range 3 {
				if s.WithFrame(n, func(p []byte) { got = bytes.Clone(p) }) {
					return got
				}
				synctest.Wait()
			}
			t.Fatalf("frame %d was not decoded", n)
			return nil
		}
		// In order, then after the animation repeats, a frame skipped to,
		// and an earlier frame.
		for _, n := range []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 0, 1, 7, 3} {
			if !bytes.Equal(frame(n), kept.FrameData[n].Pixels) {
				t.Fatalf("frame %d differs from the kept decode", n)
			}
		}
		s.mu.Lock()
		running := s.running
		s.mu.Unlock()
		if !running {
			t.Fatal("the decoder stopped while frames were asked for")
		}
		time.Sleep(2 * streamIdle)
		synctest.Wait()
		s.mu.Lock()
		running, out := s.running, s.out
		s.mu.Unlock()
		if running || out != nil {
			t.Fatal("the decoder kept running or its frames after nothing asked for one")
		}
	})
}

func TestStreamBudgetIncludesResampler(t *testing.T) {
	g := &gif.GIF{LoopCount: 0}
	for range 48 {
		g.Image = append(g.Image, solidPaletted(image.Rect(0, 0, 64, 48), 1))
		g.Delay = append(g.Delay, 5)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	in, err := Inspect(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	bound := Bound{W: 32, H: 24}
	a := in.Animation
	output := int64(32 * 24 * 4)
	withoutFilter := int64(len(buf.Bytes())) + int64(a.W*a.H)*4 + a.maxSaved*4 + a.maxFrame*8 + 3*output
	img, err := Decode(buf.Bytes(), bound, withoutFilter)
	if err != nil {
		t.Fatal(err)
	}
	if img.Stream != nil {
		t.Fatal("stream admitted without enough memory for its resampler")
	}
	img, err = Decode(buf.Bytes(), bound, withoutFilter+ResampleWork(a.W, a.H, 32, 24))
	if err != nil || img.Stream == nil {
		t.Fatalf("stream did not fit with its resampler: %v", err)
	}
}
