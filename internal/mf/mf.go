//go:build windows

// Package mf binds the parts of Media Foundation that play video into a
// Direct3D 11 texture: IMFMediaEngine with a DXGI device manager.
package mf

import (
	"sync"
	"syscall"
	"unsafe"

	"github.com/myparsleycat/fairy/d3d"
	"github.com/myparsleycat/fairy/internal/com"
)

var (
	mfplat                     = syscall.NewLazyDLL("mfplat.dll")
	oleaut32                   = syscall.NewLazyDLL("oleaut32.dll")
	pMFStartup                 = mfplat.NewProc("MFStartup")
	pMFCreateAttributes        = mfplat.NewProc("MFCreateAttributes")
	pMFCreateDXGIDeviceManager = mfplat.NewProc("MFCreateDXGIDeviceManager")
	pSysAllocString            = oleaut32.NewProc("SysAllocString")
	pSysFreeString             = oleaut32.NewProc("SysFreeString")
)

var (
	CLSID_MFMediaEngineClassFactory     = com.MustGUID("b44392da-499b-446b-a4cb-005fead0e6d5")
	IID_IMFMediaEngineClassFactory      = com.MustGUID("4d645ace-26aa-4688-9be1-df3516990b93")
	IID_IMFMediaEngineNotify            = com.MustGUID("fee7c112-e776-42b5-9bbf-0048524e2bd5")
	MF_MEDIA_ENGINE_CALLBACK            = com.MustGUID("c60381b8-83a4-41f8-a3d0-de05076849a9")
	MF_MEDIA_ENGINE_DXGI_MANAGER        = com.MustGUID("065702da-1094-486d-8617-ee7cc4ee4648")
	MF_MEDIA_ENGINE_VIDEO_OUTPUT_FORMAT = com.MustGUID("5066893c-8cf9-42bc-8b8a-472212e52726")
)

// Media engine events.
const (
	EventLoadStart      = 1
	EventError          = 5
	EventPlay           = 8
	EventPause          = 9
	EventLoadedMetadata = 10
	EventLoadedData     = 11
	EventWaiting        = 12
	EventPlaying        = 13
	EventCanPlay        = 14
	EventSeeked         = 17
	EventTimeUpdate     = 18
	EventEnded          = 19
	EventDurationChange = 21
	EventVolumeChange   = 22
)

var startup = sync.OnceValue(func() error {
	r, _, _ := pMFStartup.Call(0x00020070, 0) // MF_VERSION, MFSTARTUP_FULL
	return com.HRESULT(int32(r)).Err("MFStartup")
})

// Engine wraps IMFMediaEngine.
type Engine struct {
	p       unsafe.Pointer
	manager unsafe.Pointer
	notify  unsafe.Pointer
}

// NewEngine creates a media engine that decodes on device (an ID3D11Device
// with multithread protection) and reports events to onEvent from Media
// Foundation threads.
func NewEngine(device unsafe.Pointer, onEvent func(event uint32, param1 uintptr, param2 uint32)) (*Engine, error) {
	if err := startup(); err != nil {
		return nil, err
	}
	e := &Engine{}
	var token uint32
	r, _, _ := pMFCreateDXGIDeviceManager.Call(uintptr(unsafe.Pointer(&token)), uintptr(unsafe.Pointer(&e.manager)))
	if err := com.HRESULT(int32(r)).Err("MFCreateDXGIDeviceManager"); err != nil {
		return nil, err
	}
	if err := com.HR(e.manager, 7, uintptr(device), uintptr(token)).Err("IMFDXGIDeviceManager.ResetDevice"); err != nil {
		e.Release()
		return nil, err
	}
	var attr unsafe.Pointer
	r, _, _ = pMFCreateAttributes.Call(uintptr(unsafe.Pointer(&attr)), 3)
	if err := com.HRESULT(int32(r)).Err("MFCreateAttributes"); err != nil {
		e.Release()
		return nil, err
	}
	defer com.Release(attr)
	e.notify = com.NewHandler(IID_IMFMediaEngineNotify, func(a, b, c uintptr) uintptr {
		onEvent(uint32(a), b, uint32(c))
		return 0
	})
	com.HR(attr, 27, uintptr(unsafe.Pointer(&MF_MEDIA_ENGINE_CALLBACK)), uintptr(e.notify))
	com.HR(attr, 27, uintptr(unsafe.Pointer(&MF_MEDIA_ENGINE_DXGI_MANAGER)), uintptr(e.manager))
	com.HR(attr, 21, uintptr(unsafe.Pointer(&MF_MEDIA_ENGINE_VIDEO_OUTPUT_FORMAT)), d3d.FORMAT_B8G8R8A8_UNORM)

	var factory unsafe.Pointer
	if err := comCreate(&CLSID_MFMediaEngineClassFactory, &IID_IMFMediaEngineClassFactory, &factory); err != nil {
		e.Release()
		return nil, err
	}
	defer com.Release(factory)
	if err := com.HR(factory, 3, 0, uintptr(attr), uintptr(unsafe.Pointer(&e.p))).Err("IMFMediaEngineClassFactory.CreateInstance"); err != nil {
		e.Release()
		return nil, err
	}
	return e, nil
}

var pCoCreateInstance = syscall.NewLazyDLL("ole32.dll").NewProc("CoCreateInstance")

func comCreate(clsid, iid *com.GUID, out *unsafe.Pointer) error {
	r, _, _ := pCoCreateInstance.Call(uintptr(unsafe.Pointer(clsid)), 0, 1, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(out)))
	return com.HRESULT(int32(r)).Err("CoCreateInstance")
}

// Release shuts the engine down and frees it.
func (e *Engine) Release() {
	if e.p != nil {
		com.Call(e.p, 42) // Shutdown
		com.Release(e.p)
		e.p = nil
	}
	com.Release(e.manager)
	e.manager = nil
	com.Release(e.notify)
	e.notify = nil
}

// SetSource starts loading a file path or URL.
func (e *Engine) SetSource(url string) error {
	u, err := syscall.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	bstr, _, _ := pSysAllocString.Call(uintptr(unsafe.Pointer(u)))
	defer pSysFreeString.Call(bstr)
	return com.HR(e.p, 6, bstr).Err("IMFMediaEngine.SetSource")
}

// Error returns the engine's error code (MF_MEDIA_ENGINE_ERR_*) and extended HRESULT.
func (e *Engine) Error() (code uint16, hr com.HRESULT) {
	var me unsafe.Pointer
	if com.HR(e.p, 3, uintptr(unsafe.Pointer(&me))).Failed() || me == nil {
		return 0, 0
	}
	defer com.Release(me)
	return uint16(com.Call(me, 3)), com.HR(me, 4)
}

func (e *Engine) Play()                { com.Call(e.p, 32) }
func (e *Engine) Pause()               { com.Call(e.p, 33) }
func (e *Engine) Paused() bool         { return com.Call(e.p, 20)&0xff != 0 }
func (e *Engine) SetLoop(on bool)      { com.Call(e.p, 31, boolArg(on)) }
func (e *Engine) SetAutoPlay(on bool)  { com.Call(e.p, 29, boolArg(on)) }
func (e *Engine) SetMuted(on bool)     { com.Call(e.p, 35, boolArg(on)) }
func (e *Engine) Muted() bool          { return com.Call(e.p, 34)&0xff != 0 }
func (e *Engine) SetVolume(v float64)  { com.Call(e.p, 37, com.F64(v)) }
func (e *Engine) Volume() float64      { return com.CallF64(e.p, 36) }
func (e *Engine) CurrentTime() float64 { return com.CallF64(e.p, 16) }
func (e *Engine) Duration() float64    { return com.CallF64(e.p, 19) }
func (e *Engine) Seek(seconds float64) { com.Call(e.p, 17, com.F64(seconds)) }

// VideoSize returns the native frame size.
func (e *Engine) VideoSize() (int, int) {
	var w, h uint32
	com.Call(e.p, 40, uintptr(unsafe.Pointer(&w)), uintptr(unsafe.Pointer(&h)))
	return int(w), int(h)
}

// NewFrame reports whether a frame newer than the last transferred one is ready.
func (e *Engine) NewFrame() bool {
	var pts int64
	return com.HR(e.p, 44, uintptr(unsafe.Pointer(&pts))) == 0
}

type rect struct{ left, top, right, bottom int32 }

// Transfer draws the current frame into texture (an ID3D11Texture2D), scaled to width x height.
func (e *Engine) Transfer(texture unsafe.Pointer, width, height int) error {
	dst := rect{0, 0, int32(width), int32(height)}
	border := [4]byte{0, 0, 0, 255}
	return com.HR(e.p, 43, uintptr(texture), 0, uintptr(unsafe.Pointer(&dst)), uintptr(unsafe.Pointer(&border))).Err("TransferVideoFrame")
}

func boolArg(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}
