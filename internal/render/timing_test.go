//go:build windows

package render

import (
	"errors"
	"image/color"
	"testing"
	"time"
	"unsafe"

	"github.com/myparsleycat/fairy/d3d"
	"github.com/myparsleycat/fairy/internal/com"
)

type testQuery struct {
	kind     uint32
	ticks    uint64
	released bool
}

type testQueries struct {
	queries                  []*testQuery
	ready                    bool
	disjoint                 d3d.QueryDataTimestampDisjoint
	polls, creates, releases int
	failCreate               int
	getError                 error
	ticks                    uint64
}

func (b *testQueries) create(kind uint32) (d3d.Query, error) {
	b.creates++
	if b.creates == b.failCreate {
		return d3d.Query{}, errors.New("query creation failed")
	}
	q := &testQuery{kind: kind}
	b.queries = append(b.queries, q)
	return d3d.Query{P: unsafe.Pointer(q)}, nil
}
func (b *testQueries) begin(q d3d.Query) {}
func (b *testQueries) end(q d3d.Query) {
	r := (*testQuery)(q.P)
	if r.kind == d3d.QUERY_TIMESTAMP {
		b.ticks += 1000
		r.ticks = b.ticks
	}
}
func (b *testQueries) data(q d3d.Query, dst unsafe.Pointer, size uint32) (bool, error) {
	b.polls++
	if b.getError != nil {
		return false, b.getError
	}
	if !b.ready {
		return false, nil
	}
	r := (*testQuery)(q.P)
	if r.released {
		panic("reading released query")
	}
	if r.kind == d3d.QUERY_TIMESTAMP_DISJOINT {
		if size != 16 {
			panic("invalid disjoint data size")
		}
		*(*d3d.QueryDataTimestampDisjoint)(dst) = b.disjoint
	} else {
		if size != 8 {
			panic("invalid timestamp data size")
		}
		*(*uint64)(dst) = r.ticks
	}
	return true, nil
}
func (b *testQueries) release(q d3d.Query) {
	r := (*testQuery)(q.P)
	if r.released {
		panic("query released twice")
	}
	r.released = true
	b.releases++
}

func TestGPUTimerPendingAndReuse(t *testing.T) {
	b := &testQueries{disjoint: d3d.QueryDataTimestampDisjoint{Frequency: 1e9}}
	timer := &gpuTimer{backend: b}
	defer timer.release()
	for frame := uint64(1); frame <= 4; frame++ {
		slot, err := timer.start(frame)
		if err != nil || slot < 0 {
			t.Fatalf("start: %d, %v", slot, err)
		}
		timer.finish(slot)
	}
	for range 20 {
		before := b.polls
		sample, err := timer.collect()
		if err != nil || sample.valid || b.polls-before != 4 {
			t.Fatalf("pending collection waited or returned data: %+v, %v", sample, err)
		}
		if slot, err := timer.start(5); err != nil || slot != -1 {
			t.Fatal("pending slot was reused")
		}
	}
	if b.creates != 12 {
		t.Fatalf("created %d queries, want 12", b.creates)
	}
	b.ready = true
	sample, err := timer.collect()
	if err != nil || !sample.valid || sample.frame != 4 || sample.duration != time.Microsecond {
		t.Fatalf("sample: %+v, %v", sample, err)
	}
	if sample, err = timer.collect(); err != nil || sample.valid {
		t.Fatal("old sample returned twice")
	}
	slot, err := timer.start(5)
	if err != nil || slot < 0 || b.creates != 12 {
		t.Fatal("completed queries were not reused")
	}
	timer.finish(slot)
	timer.release()
	if b.releases != 12 {
		t.Fatalf("released %d queries", b.releases)
	}
}

func TestGPUTimerInvalidSamples(t *testing.T) {
	for _, mode := range []string{"disjoint", "zero frequency", "reversed", "overflow", "discarded"} {
		t.Run(mode, func(t *testing.T) {
			b := &testQueries{ready: true, disjoint: d3d.QueryDataTimestampDisjoint{Frequency: 1e9}}
			timer := &gpuTimer{backend: b}
			defer timer.release()
			slot, err := timer.start(1)
			if err != nil {
				t.Fatal(err)
			}
			timer.finish(slot)
			switch mode {
			case "disjoint":
				b.disjoint.Disjoint = 1
			case "zero frequency":
				b.disjoint.Frequency = 0
			case "reversed":
				b.queries[1].ticks = b.queries[2].ticks + 1
			case "overflow":
				b.queries[2].ticks = ^uint64(0)
				b.disjoint.Frequency = 1
			case "discarded":
				timer.slots[slot].discard = true
			}
			sample, err := timer.collect()
			if err != nil || sample.valid || timer.slots[slot].pending {
				t.Fatalf("invalid sample retained: %+v, %v", sample, err)
			}
		})
	}
}

func timingRenderer(t *testing.T, level uint32) *Renderer {
	t.Helper()
	d := warpDeviceLevel(t, level)
	r := &Renderer{ds: d, dev: d.Dev, ctx: d.Ctx, width: 8, height: 8}
	t.Cleanup(func() { r.ds = nil; r.Release() })
	var err error
	if r.target, err = NewTarget(d, 8, 8); err != nil {
		t.Fatal(err)
	}
	r.rtv = r.target.rtv
	if err := r.init(); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestWARPMinimumLevelRenderAndTiming(t *testing.T) {
	r := timingRenderer(t, d3d.FEATURE_LEVEL_10_1)
	if r.timer != nil {
		t.Fatal("offscreen renderer enabled timing by default")
	}
	r.SetGPUTiming(true)
	var scene Scene
	scene.Add(Prim{Bounds: [4]float32{0, 0, 8, 8}, Clip: NoClip, Color: [4]float32{1, 0, 0, 1}, Kind: KindRect})
	gotSample := false
	for range 4 {
		if err := r.Draw(&scene, [4]float32{}); err != nil {
			t.Fatal(err)
		}
		st := r.Stats()
		if st.GPUValid {
			gotSample = true
			if st.GPUFrame == 0 || st.GPUFrame >= r.submission {
				t.Fatalf("sample belongs to wrong submission: %+v", st)
			}
		}
		// Explicit readback completes test work; the timer itself never flushes or waits.
		pixels, err := r.ReadTarget()
		if err != nil {
			t.Fatal(err)
		}
		if got := pixels.NRGBAAt(4, 4); got != (color.NRGBA{R: 255, A: 255}) {
			t.Fatalf("pixel %v", got)
		}
	}
	if !gotSample {
		t.Fatal("D3D queries produced no completed sample after explicit readback")
	}
	r.SetGPUTiming(false)
	if r.Stats().GPUValid {
		t.Fatal("disabled timer retained a sample")
	}
}

func TestRendererTimingFailureKeepsDrawing(t *testing.T) {
	for _, mode := range []string{"create", "get data", "device lost"} {
		t.Run(mode, func(t *testing.T) {
			r := timingRenderer(t, d3d.FEATURE_LEVEL_11_0)
			b := &testQueries{}
			r.timer = &gpuTimer{backend: b}
			if mode == "create" {
				b.failCreate = 2
			}
			if err := r.Draw(&Scene{}, [4]float32{0, 1, 0, 1}); err != nil {
				t.Fatal(err)
			}
			if mode != "create" {
				b.getError = errors.New("query read failed")
				if mode == "device lost" {
					b.getError = com.HRESULT(-2005270523)
				}
			}
			err := r.Draw(&Scene{}, [4]float32{0, 1, 0, 1})
			if mode == "device lost" {
				if !errors.Is(err, ErrDeviceLost) || !r.ds.Lost() {
					t.Fatalf("device loss not propagated: %v", err)
				}
				return
			}
			if err != nil || r.timer != nil || r.Stats().GPUValid {
				t.Fatalf("timing failure stopped drawing: %v", err)
			}
			if b.releases != len(b.queries) {
				t.Fatal("failed timer leaked queries")
			}
			created := b.creates
			if err := r.Draw(&Scene{}, [4]float32{0, 1, 0, 1}); err != nil || b.creates != created {
				t.Fatal("failed timing was retried")
			}
			pixels, err := r.ReadTarget()
			if err != nil {
				t.Fatal(err)
			}
			if got := pixels.NRGBAAt(0, 0); got != (color.NRGBA{G: 255, A: 255}) {
				t.Fatalf("rendering stopped: %v", got)
			}
		})
	}
}

func TestRendererTimingDoesNotRepeatSamples(t *testing.T) {
	r := timingRenderer(t, d3d.FEATURE_LEVEL_11_0)
	b := &testQueries{disjoint: d3d.QueryDataTimestampDisjoint{Frequency: 1e9}}
	r.timer = &gpuTimer{backend: b}
	if err := r.Draw(&Scene{}, [4]float32{}); err != nil {
		t.Fatal(err)
	}
	b.ready = true
	if err := r.Draw(&Scene{}, [4]float32{}); err != nil {
		t.Fatal(err)
	}
	if st := r.Stats(); !st.GPUValid || st.GPUFrame != 1 {
		t.Fatalf("missing earlier sample: %+v", st)
	}
	b.ready = false
	if err := r.Draw(&Scene{}, [4]float32{}); err != nil {
		t.Fatal(err)
	}
	if st := r.Stats(); st.GPUValid || st.GPUFrame != 0 || st.GPU != 0 {
		t.Fatalf("stale sample: %+v", st)
	}
	r.SetGPUTiming(false)
	if b.releases != len(b.queries) {
		t.Fatal("disabled timer leaked queries")
	}
	r.SetGPUTiming(true)
	if err := r.Draw(&Scene{}, [4]float32{}); err != nil {
		t.Fatal(err)
	}
	if r.Stats().GPUValid {
		t.Fatal("re-enabled timer retained old queries")
	}
}
