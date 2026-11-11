//go:build windows

package render

import (
	"time"
	"unsafe"

	"github.com/myparsleycat/fairy/d3d"
)

type queryBackend interface {
	create(uint32) (d3d.Query, error)
	begin(d3d.Query)
	end(d3d.Query)
	data(d3d.Query, unsafe.Pointer, uint32) (bool, error)
	release(d3d.Query)
}

type deviceQueries struct {
	dev d3d.Device
	ctx d3d.Context
}

func (b deviceQueries) create(kind uint32) (d3d.Query, error) {
	return b.dev.CreateQuery(&d3d.QueryDesc{Query: kind})
}
func (b deviceQueries) begin(q d3d.Query) { b.ctx.Begin(q) }
func (b deviceQueries) end(q d3d.Query)   { b.ctx.End(q) }
func (b deviceQueries) data(q d3d.Query, dst unsafe.Pointer, size uint32) (bool, error) {
	return b.ctx.GetData(q, dst, size, d3d.ASYNC_GETDATA_DONOTFLUSH)
}
func (b deviceQueries) release(q d3d.Query) { q.Release() }

type querySlot struct {
	disjoint, start, end d3d.Query
	frame                uint64
	pending, discard     bool
}

type gpuTimer struct {
	backend queryBackend
	slots   [4]querySlot
}

type gpuSample struct {
	duration time.Duration
	frame    uint64
	valid    bool
}

func (t *gpuTimer) collect() (gpuSample, error) {
	var sample gpuSample
slots:
	for i := range t.slots {
		s := &t.slots[i]
		if !s.pending {
			continue
		}
		var disjoint d3d.QueryDataTimestampDisjoint
		var start, end uint64
		for _, read := range []struct {
			q    d3d.Query
			dst  unsafe.Pointer
			size uint32
		}{
			{s.disjoint, unsafe.Pointer(&disjoint), uint32(unsafe.Sizeof(disjoint))},
			{s.start, unsafe.Pointer(&start), 8},
			{s.end, unsafe.Pointer(&end), 8},
		} {
			ready, err := t.backend.data(read.q, read.dst, read.size)
			if err != nil {
				return gpuSample{}, err
			}
			if !ready {
				continue slots
			}
		}
		s.pending = false
		if !s.discard && disjoint.Disjoint == 0 && disjoint.Frequency != 0 && end >= start && (!sample.valid || s.frame > sample.frame) {
			ns := float64(end-start) / float64(disjoint.Frequency) * float64(time.Second)
			if ns < float64(1<<63) {
				sample = gpuSample{duration: time.Duration(ns), frame: s.frame, valid: true}
			}
		}
	}
	return sample, nil
}

func (t *gpuTimer) start(frame uint64) (int, error) {
	for i := range t.slots {
		s := &t.slots[i]
		if s.pending {
			continue
		}
		for _, q := range []struct {
			target *d3d.Query
			kind   uint32
		}{
			{&s.disjoint, d3d.QUERY_TIMESTAMP_DISJOINT}, {&s.start, d3d.QUERY_TIMESTAMP}, {&s.end, d3d.QUERY_TIMESTAMP},
		} {
			if q.target.P == nil {
				created, err := t.backend.create(q.kind)
				if err != nil {
					return -1, err
				}
				*q.target = created
			}
		}
		s.frame, s.pending, s.discard = frame, true, false
		t.backend.begin(s.disjoint)
		t.backend.end(s.start)
		return i, nil
	}
	return -1, nil
}

func (t *gpuTimer) finish(slot int) {
	s := &t.slots[slot]
	t.backend.end(s.end)
	t.backend.end(s.disjoint)
}

func (t *gpuTimer) release() {
	for i := range t.slots {
		s := &t.slots[i]
		for _, q := range []d3d.Query{s.disjoint, s.start, s.end} {
			if q.P != nil {
				t.backend.release(q)
			}
		}
		*s = querySlot{}
	}
}
