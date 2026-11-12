//go:build windows

package render

import (
	"log"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/myparsleycat/fairy/d3d"
	"github.com/myparsleycat/fairy/internal/com"
	"github.com/myparsleycat/fairy/internal/win"
)

// Device is the Direct3D 11 device shared by every window, video and native
// surface, so their textures can be drawn in any window. Its immediate
// context is multithread protected because Media Foundation uses it from its
// own threads; a sequence of context calls that must not interleave with
// another thread's runs between Lock and Unlock.
type Device struct {
	Dev     d3d.Device
	Ctx     d3d.Context
	Gen     uint64 // numbers devices in creation order, starting at 1
	Info    d3d.DeviceInfo
	factory unsafe.Pointer
	mt      unsafe.Pointer // ID3D10Multithread
	depth   int            // Lock nesting; changed only while the lock is held
	owner   atomic.Uint32
	refs    int
	lost    bool
}

var (
	iidIDXGIDevice       = com.MustGUID("54ec77fa-1377-44e6-8c32-88fd5f44c84c")
	iidIDXGIFactory2     = com.MustGUID("50c83a1c-e072-4c48-87b0-3630fa36a6d0")
	iidID3D11Texture2D   = com.MustGUID("6f15aaf2-d208-4e89-9ab4-489535d34f9c")
	iidID3D10Multithread = com.MustGUID("9b7e4e00-342c-4106-a19f-4f2704f689f0")
	iidIDXGIDevice3      = com.MustGUID("6007896c-3244-4afd-bf18-a6d3beda5023")
)

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
	dev, ctx, info, err := d3d.CreateDeviceWithOptions(d3d.DeviceOptions{MinFeatureLevel: d3d.FEATURE_LEVEL_10_1})
	if err != nil {
		return nil, err
	}
	factory, err := dxgiFactory(dev)
	if err != nil {
		ctx.Release()
		dev.Release()
		return nil, err
	}
	mt, err := com.QueryInterface(dev.P, &iidID3D10Multithread)
	if err != nil {
		com.Release(factory)
		ctx.Release()
		dev.Release()
		return nil, err
	}
	shared = &Device{Dev: dev, Ctx: ctx, Info: info, Gen: gens.Add(1), factory: factory, mt: mt, refs: 1}
	if info.Software || Verbose {
		adapter := info.AdapterName
		if !info.AdapterKnown {
			adapter = "unknown"
		}
		log.Printf("fairy: graphics device generation=%d driver=%d software=%v feature_level=%#x creation_flags=%#x adapter=%q vendor=%#x device=%#x fallback=%q",
			shared.Gen, info.DriverType, info.Software, info.FeatureLevel, info.CreationFlags, adapter, info.VendorID, info.DeviceID, info.FallbackReason)
	}
	return shared, nil
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
	com.Release(d.factory)
	com.Release(d.mt)
	d.Ctx.ClearState()
	d.Ctx.Release()
	d.Dev.Release()
	if shared == d {
		shared = nil
	}
}

// Lock enters the device's multithread critical section. It is recursive on
// one OS thread, so the calling goroutine must stay locked to its thread
// until Unlock.
func (d *Device) Lock() {
	com.Call(d.mt, 3)
	if d.depth == 0 {
		d.owner.Store(win.GetCurrentThreadId())
	}
	d.depth++
}

// Unlock leaves the critical section Lock entered.
func (d *Device) Unlock() {
	d.depth--
	if d.depth == 0 {
		d.owner.Store(0)
	}
	com.Call(d.mt, 4)
}

func DeviceLockedByCurrentThread() bool {
	deviceMu.Lock()
	defer deviceMu.Unlock()
	return shared != nil && shared.owner.Load() == win.GetCurrentThreadId()
}

// Nested reports whether the lock is held more than once, by a caller that
// holds it. The outer holder still uses the pipeline state.
func (d *Device) Nested() bool { return d.depth > 1 }

// Trim calls IDXGIDevice3::Trim so the driver frees the memory it keeps for
// released resources. Hold the lock and clear the pipeline state first. It
// does nothing when the device lacks IDXGIDevice3 (before Windows 8.1).
func (d *Device) Trim() {
	dxgi, err := com.QueryInterface(d.Dev.P, &iidIDXGIDevice3)
	if err != nil {
		return
	}
	com.Call(dxgi, 17) // IDXGIDevice3::Trim
	com.Release(dxgi)
}

// MarkLost records that the device was removed. Holders release it and
// acquire a new one.
func (d *Device) MarkLost() {
	deviceMu.Lock()
	d.lost = true
	deviceMu.Unlock()
}

// Lost reports whether MarkLost was called or the driver reports removal.
func (d *Device) Lost() bool {
	deviceMu.Lock()
	defer deviceMu.Unlock()
	if !d.lost && d.Dev.RemovedReason() != nil {
		d.lost = true
	}
	return d.lost
}
