//go:build windows

package render

import (
	"unsafe"

	"github.com/myparsleycat/fairy/d3d"
	"github.com/myparsleycat/fairy/internal/com"
)

const (
	dxgiUsageRenderTargetOutput = 0x20
	dxgiScalingStretch          = 0
	dxgiSwapEffectFlipDiscard   = 4
	dxgiAlphaModePremultiplied  = 1
	dxgiAlphaModeIgnore         = 3
	dxgiMWANoAltEnter           = 0x2

	dxgiStatusOccluded = 0x087A0001
)

type swapChainDesc1 struct {
	Width, Height uint32
	Format        uint32
	Stereo        int32
	SampleDesc    d3d.SampleDesc
	BufferUsage   uint32
	BufferCount   uint32
	Scaling       uint32
	SwapEffect    uint32
	AlphaMode     uint32
	Flags         uint32
}

// swapChain wraps IDXGISwapChain1.
type swapChain struct{ p unsafe.Pointer }

// dxgiFactory returns the IDXGIFactory2 that owns the device's adapter.
func dxgiFactory(d d3d.Device) (unsafe.Pointer, error) {
	dxgiDev, err := com.QueryInterface(d.P, &iidIDXGIDevice)
	if err != nil {
		return nil, err
	}
	defer com.Release(dxgiDev)
	var adapter unsafe.Pointer
	if err := com.HR(dxgiDev, 7, uintptr(unsafe.Pointer(&adapter))).Err("IDXGIDevice.GetAdapter"); err != nil {
		return nil, err
	}
	defer com.Release(adapter)
	var factory unsafe.Pointer
	if err := com.HR(adapter, 6, uintptr(unsafe.Pointer(&iidIDXGIFactory2)), uintptr(unsafe.Pointer(&factory))).Err("IDXGIAdapter.GetParent"); err != nil {
		return nil, err
	}
	return factory, nil
}

// createSwapChainForHwnd creates a flip-model swap chain for a window.
func createSwapChainForHwnd(factory unsafe.Pointer, d d3d.Device, hwnd uintptr, desc *swapChainDesc1) (swapChain, error) {
	var sc unsafe.Pointer
	hr := com.HR(factory, 15, uintptr(d.P), hwnd, uintptr(unsafe.Pointer(desc)), 0, 0, uintptr(unsafe.Pointer(&sc)))
	if err := hr.Err("CreateSwapChainForHwnd"); err != nil {
		return swapChain{}, err
	}
	com.HR(factory, 8, hwnd, dxgiMWANoAltEnter) // MakeWindowAssociation
	return swapChain{sc}, nil
}

// createSwapChainForComposition creates a swap chain for DirectComposition.
func createSwapChainForComposition(factory unsafe.Pointer, d d3d.Device, desc *swapChainDesc1) (swapChain, error) {
	var sc unsafe.Pointer
	hr := com.HR(factory, 24, uintptr(d.P), uintptr(unsafe.Pointer(desc)), 0, uintptr(unsafe.Pointer(&sc)))
	if err := hr.Err("CreateSwapChainForComposition"); err != nil {
		return swapChain{}, err
	}
	return swapChain{sc}, nil
}

func (s swapChain) release() { com.Release(s.p) }

func (s swapChain) present(sync, flags uint32) com.HRESULT {
	return com.HR(s.p, 8, uintptr(sync), uintptr(flags))
}

func (s swapChain) buffer() (d3d.Texture2D, error) {
	var tex unsafe.Pointer
	err := com.HR(s.p, 9, 0, uintptr(unsafe.Pointer(&iidID3D11Texture2D)), uintptr(unsafe.Pointer(&tex))).Err("GetBuffer")
	return d3d.Texture2D{P: tex}, err
}

func (s swapChain) resizeBuffers(count, w, h, format, flags uint32) error {
	return com.HR(s.p, 13, uintptr(count), uintptr(w), uintptr(h), uintptr(format), uintptr(flags)).Err("ResizeBuffers")
}
