//go:build windows

package render

import (
	"errors"
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"github.com/myparsleycat/fairy/d3d"
	"github.com/myparsleycat/fairy/internal/com"
)

var (
	iidIDXGIResource   = com.MustGUID("035f3ab4-482e-4e50-b41f-8a7f8bd8960b")
	iidIDXGIKeyedMutex = com.MustGUID("9d8e1289-d7b3-465f-8126-250e349af85d")
)

const (
	waitAbandoned = 0x80
	waitTimeout   = 0x102
)

var (
	// ErrSharedBusy reports that another device held a shared texture's keyed
	// mutex for the whole timeout.
	ErrSharedBusy = errors.New("render: shared texture busy")
	// ErrSharedAbandoned reports that a device released a shared texture while
	// holding its keyed mutex. The texture is left inconsistent; release it and
	// make a new one.
	ErrSharedAbandoned = errors.New("render: shared texture abandoned")
)

// SharedTexture is a premultiplied FORMAT_R8G8B8A8_UNORM texture that other
// Direct3D devices open by its legacy shared handle. Every user, here and on
// the other devices, takes its keyed mutex with key 0 around access.
type SharedTexture struct {
	tex   d3d.Texture2D
	mutex unsafe.Pointer // IDXGIKeyedMutex
	// Handle is the IDXGIResource::GetSharedHandle handle. It is not an NT
	// handle and is never closed; it lasts as long as the texture.
	Handle        uintptr
	Width, Height int
	Gen           uint64 // generation of the device it was made on
}

// NewSharedTexture creates a width x height shared texture on d.
func NewSharedTexture(d *Device, width, height int) (*SharedTexture, error) {
	tex, err := d.Dev.CreateTexture2D(&d3d.Texture2DDesc{
		Width:      uint32(width),
		Height:     uint32(height),
		MipLevels:  1,
		ArraySize:  1,
		Format:     d3d.FORMAT_R8G8B8A8_UNORM,
		SampleDesc: d3d.SampleDesc{Count: 1},
		Usage:      d3d.USAGE_DEFAULT,
		BindFlags:  d3d.BIND_SHADER_RESOURCE | d3d.BIND_RENDER_TARGET,
		MiscFlags:  d3d.RESOURCE_MISC_SHARED_KEYEDMUTEX,
	}, nil)
	if err != nil {
		return nil, err
	}
	s := &SharedTexture{tex: tex, Width: width, Height: height, Gen: d.Gen}
	res, err := com.QueryInterface(tex.P, &iidIDXGIResource)
	if err != nil {
		s.Release()
		return nil, err
	}
	hr := com.HR(res, 8, uintptr(unsafe.Pointer(&s.Handle))) // GetSharedHandle
	com.Release(res)
	if err := hr.Err("IDXGIResource::GetSharedHandle"); err != nil {
		s.Release()
		return nil, err
	}
	if s.mutex, err = com.QueryInterface(tex.P, &iidIDXGIKeyedMutex); err != nil {
		s.Release()
		return nil, err
	}
	return s, nil
}

func (s *SharedTexture) Release() {
	if s == nil {
		return
	}
	com.Release(s.mutex)
	s.tex.Release()
	*s = SharedTexture{}
}

// CopyToShared copies an offscreen renderer's target, which must have s's
// size and format, into s while holding its keyed mutex. It waits up to
// timeout for the mutex and returns ErrSharedBusy when it does not get it.
func (r *Renderer) CopyToShared(s *SharedTexture, timeout time.Duration) error {
	if r.target == nil || r.target.Width != s.Width || r.target.Height != s.Height {
		return errors.New("render: shared texture does not match the target")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r.ds.Lock()
	defer r.ds.Unlock()
	hr := com.HR(s.mutex, 8, 0, uintptr(max(timeout.Milliseconds(), 0))) // AcquireSync(0, ms)
	switch {
	case hr == waitTimeout:
		return ErrSharedBusy
	case hr == waitAbandoned:
		return ErrSharedAbandoned
	case hr.Failed():
		if r.ds.Lost() {
			return ErrDeviceLost
		}
		return hr.Err("IDXGIKeyedMutex::AcquireSync")
	}
	r.ctx.CopyResource(s.tex, r.target.tex)
	if hr := com.HR(s.mutex, 9, 0); hr.Failed() { // ReleaseSync(0)
		if r.ds.Lost() {
			return ErrDeviceLost
		}
		return hr.Err("IDXGIKeyedMutex::ReleaseSync")
	}
	return nil
}

// ReadPixels copies an offscreen renderer's target into dst as packed rows of
// four bytes per pixel in the target's format, with premultiplied alpha. dst
// must hold width*height*4 bytes.
func (r *Renderer) ReadPixels(dst []byte) error {
	if r.target == nil {
		return errors.New("render: ReadPixels needs an offscreen renderer")
	}
	w, h := r.target.Width, r.target.Height
	if len(dst) < w*h*4 {
		return fmt.Errorf("render: ReadPixels needs %d bytes, got %d", w*h*4, len(dst))
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r.ds.Lock()
	defer r.ds.Unlock()
	if r.staging.P == nil {
		var err error
		r.staging, err = r.dev.CreateTexture2D(&d3d.Texture2DDesc{
			Width:          uint32(w),
			Height:         uint32(h),
			MipLevels:      1,
			ArraySize:      1,
			Format:         r.target.Format,
			SampleDesc:     d3d.SampleDesc{Count: 1},
			Usage:          d3d.USAGE_STAGING,
			CPUAccessFlags: d3d.CPU_ACCESS_READ,
		}, nil)
		if err != nil {
			if r.ds.Lost() {
				return ErrDeviceLost
			}
			return err
		}
	}
	r.ctx.CopyResource(r.staging, r.target.tex)
	m, err := r.ctx.Map(r.staging, d3d.MAP_READ)
	if err != nil {
		if r.ds.Lost() {
			return ErrDeviceLost
		}
		return err
	}
	defer r.ctx.Unmap(r.staging)
	for y := range h {
		copy(dst[y*w*4:(y+1)*w*4], unsafe.Slice((*byte)(unsafe.Add(m.Data, y*int(m.RowPitch))), w*4))
	}
	return nil
}
