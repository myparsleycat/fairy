//go:build darwin

package render

import (
	"errors"
	"log"
	"sync"
	"sync/atomic"

	"github.com/myparsleycat/fairy/internal/darwin"
)

// Device is the Metal device and command queue shared by every window, so
// their textures can be drawn in any window. Metal objects are safe to use
// from any thread; Lock serializes renderer frames with app work that must
// not interleave with them, as on Windows.
type Device struct {
	Gen   uint64 // numbers devices in creation order, starting at 1
	Info  DeviceInfo
	dev   id // id<MTLDevice>
	queue id // id<MTLCommandQueue>

	mu    sync.Mutex
	depth int // Lock nesting; changed only while the lock is held
	owner atomic.Uintptr
	refs  int
	lost  bool
	// removable is set for a GPU that can be unplugged, as an external one,
	// which reports it with isRemoved.
	removable bool

	pipeMu sync.Mutex
	pipes  *pipelines
}

// DeviceInfo describes the GPU a Device runs on.
type DeviceInfo struct {
	Software      bool   // the device renders on the CPU; never for Metal
	Name          string // the GPU's name, as "Apple M2"
	UnifiedMemory bool   // the CPU and GPU share memory, as on Apple silicon
}

var (
	deviceMu sync.Mutex
	shared   *Device
	gens     atomic.Uint64
)

// AcquireDevice returns the shared device, creating it when there is none or
// the current one was lost. Pair it with Release.
func AcquireDevice() (*Device, error) {
	deviceMu.Lock()
	defer deviceMu.Unlock()
	if shared != nil && !shared.lost {
		shared.refs++
		return shared, nil
	}
	var d *Device
	var err error
	darwin.WithPool(func() {
		dev := mtlCreateSystemDefaultDevice()
		if dev == 0 {
			// Processes without a window server connection may get no default device.
			if all := mtlCopyAllDevices(); all != 0 {
				if darwin.Get[uint64](all, "count") > 0 {
					dev = darwin.Retain(darwin.Send(all, "objectAtIndex:", uint64(0)))
				}
				darwin.Release(all)
			}
		}
		if dev == 0 {
			err = errors.New("render: no Metal device")
			return
		}
		queue := darwin.Send(dev, "newCommandQueue")
		if queue == 0 {
			darwin.Release(dev)
			err = errors.New("render: could not create a Metal command queue")
			return
		}
		d = &Device{dev: dev, queue: queue, Gen: gens.Add(1), refs: 1}
		d.Info.Name = darwin.GoString(darwin.Send(dev, "name"))
		d.Info.UnifiedMemory = darwin.Get[bool](dev, "hasUnifiedMemory")
		// Only GPUs that can be unplugged implement isRemoved.
		d.removable = darwin.Get[bool](dev, "respondsToSelector:", darwin.Sel("isRemoved"))
	})
	if err != nil {
		return nil, err
	}
	shared = d
	if Verbose {
		log.Printf("fairy: graphics device generation=%d metal=%q unified_memory=%v", d.Gen, d.Info.Name, d.Info.UnifiedMemory)
	}
	return d, nil
}

// AcquireCurrentDevice retains the existing shared device without creating
// one. It returns nil when no device exists or it was lost. Pair with Release.
func AcquireCurrentDevice() *Device {
	deviceMu.Lock()
	defer deviceMu.Unlock()
	if shared == nil || shared.lost {
		return nil
	}
	shared.refs++
	return shared
}

// CurrentGen returns the generation of the shared device, or 0 when none
// exists or it was lost.
func CurrentGen() uint64 {
	deviceMu.Lock()
	defer deviceMu.Unlock()
	if shared == nil || shared.lost {
		return 0
	}
	return shared.Gen
}

// Retain adds a reference, released with Release.
func (d *Device) Retain() {
	deviceMu.Lock()
	d.refs++
	deviceMu.Unlock()
}

// Release drops a reference; the device is freed with the last one.
func (d *Device) Release() {
	deviceMu.Lock()
	defer deviceMu.Unlock()
	d.refs--
	if d.refs > 0 {
		return
	}
	d.pipeMu.Lock()
	d.pipes.release()
	d.pipes = nil
	d.pipeMu.Unlock()
	darwin.Release(d.queue)
	darwin.Release(d.dev)
	d.queue, d.dev = 0, 0
	if shared == d {
		shared = nil
	}
}

// threadID identifies the calling OS thread.
func threadID() uintptr { return pthreadSelf() }

// Lock takes the device lock. It is recursive on one OS thread, so the
// calling goroutine must stay locked to its thread until Unlock.
func (d *Device) Lock() {
	self := threadID()
	if d.owner.Load() == self {
		d.depth++
		return
	}
	d.mu.Lock()
	d.owner.Store(self)
	d.depth = 1
}

// Unlock releases the lock Lock took.
func (d *Device) Unlock() {
	d.depth--
	if d.depth == 0 {
		d.owner.Store(0)
		d.mu.Unlock()
	}
}

// DeviceLockedByCurrentThread reports whether the calling thread holds the
// shared device's lock.
func DeviceLockedByCurrentThread() bool {
	deviceMu.Lock()
	defer deviceMu.Unlock()
	return shared != nil && shared.owner.Load() == threadID()
}

// Nested reports whether the lock is held more than once, by a caller that
// holds it. The outer holder still uses the pipeline state.
func (d *Device) Nested() bool { return d.depth > 1 }

// Trim does nothing: Metal returns the memory of released resources itself.
func (d *Device) Trim() {}

// MarkLost records that the device was removed. Holders release it and
// acquire a new one.
func (d *Device) MarkLost() {
	deviceMu.Lock()
	d.lost = true
	deviceMu.Unlock()
}

// Lost reports whether MarkLost was called or the GPU was removed, as an
// unplugged external GPU is.
func (d *Device) Lost() bool {
	deviceMu.Lock()
	defer deviceMu.Unlock()
	if !d.lost && d.removable && d.dev != 0 && darwin.Get[bool](d.dev, "isRemoved") {
		d.lost = true
	}
	return d.lost
}
