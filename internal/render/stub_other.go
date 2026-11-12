//go:build !windows

package render

import (
	"errors"
	"image"
	"sync"
)

// errUnsupported is returned until the platform has a GPU backend.
var errUnsupported = errors.New("render: no GPU backend for this platform yet")

// Device is the GPU device shared by every window.
type Device struct {
	Gen  uint64 // numbers devices in creation order, starting at 1
	Info DeviceInfo
	mu   sync.Mutex
	refs int
	lost bool
}

func AcquireDevice() (*Device, error) { return nil, errUnsupported }
func AcquireCurrentDevice() *Device   { return nil }
func CurrentGen() uint64              { return 0 }
func DeviceLockedByCurrentThread() bool {
	return false
}

func (d *Device) Retain()      {}
func (d *Device) Release()     {}
func (d *Device) Lock()        { d.mu.Lock() }
func (d *Device) Unlock()      { d.mu.Unlock() }
func (d *Device) Nested() bool { return false }
func (d *Device) Trim()        {}
func (d *Device) MarkLost()    { d.lost = true }
func (d *Device) Lost() bool   { return d.lost }

// DeviceInfo describes the adapter a Device runs on.
type DeviceInfo struct {
	Software bool // the device renders on the CPU
}

// Texture is an image on the GPU.
type Texture struct {
	Width, Height int
	Format        uint32
	Premultiplied bool
}

func NewTexture(d *Device, width, height int, rgba []byte, mutable bool) (*Texture, error) {
	return nil, errUnsupported
}

func (t *Texture) Update(d *Device, rgba []byte)        {}
func (t *Texture) CanMip() bool                         { return false }
func (t *Texture) ByteSize() int64                      { return int64(t.Width) * int64(t.Height) * 4 }
func (t *Texture) MipWork() int64                       { return 0 }
func (t *Texture) EnsureMips(d *Device) error           { return errUnsupported }
func (t *Texture) PromoteMips(d *Device) (int64, error) { return 0, errUnsupported }
func (t *Texture) Release()                             {}
func (r *Renderer) NewTexture(w, h int, rgba []byte, mutable bool) (*Texture, error) {
	return nil, errUnsupported
}

// Target is a texture that can be drawn into.
type Target struct{ Texture }

func NewTarget(d *Device, width, height int) (*Target, error) { return nil, errUnsupported }
func (t *Target) Release()                                    {}

// Atlas is a single-channel glyph texture.
type Atlas struct {
	// Generation changes whenever the atlas is cleared or replaced; cached
	// tiles from another generation must be re-rasterized.
	Generation int
}

func (a *Atlas) Size() int                                 { return 0 }
func (a *Atlas) Insert(w, h int, data []byte) (Tile, bool) { return Tile{}, false }
func (a *Atlas) Grow() error                               { return errUnsupported }

// Renderer draws scenes into a window or an offscreen target.
type Renderer struct {
	Atlas *Atlas
}

// New creates a renderer for the native view handle.
func New(view uintptr, width, height int, transparent bool) (*Renderer, error) {
	return nil, errUnsupported
}

func NewOffscreen(width, height int) (*Renderer, error) { return nil, errUnsupported }

func (r *Renderer) Transparent() bool                     { return false }
func (r *Renderer) SetTransparent(on bool) error          { return errUnsupported }
func (r *Renderer) Device() *Device                       { return nil }
func (r *Renderer) Size() (int, int)                      { return 0, 0 }
func (r *Renderer) Resize(width, height int) error        { return errUnsupported }
func (r *Renderer) HasTarget() bool                       { return false }
func (r *Renderer) Draw(s *Scene, clear [4]float32) error { return errUnsupported }
func (r *Renderer) SetGPUTiming(enabled bool)             {}
func (r *Renderer) Stats() Stats                          { return Stats{} }
func (r *Renderer) Occluded() bool                        { return false }
func (r *Renderer) Release()                              {}
func (r *Renderer) RequestSnapshot(f func(*image.NRGBA))  {}
func (r *Renderer) ReadTarget() (*image.NRGBA, error)     { return nil, errUnsupported }
