//go:build windows

package render

import (
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/myparsleycat/fairy/d3d"
	"github.com/myparsleycat/fairy/internal/com"
)

var (
	dcomp                     = windows.NewLazySystemDLL("dcomp.dll")
	pDCompositionCreateDevice = dcomp.NewProc("DCompositionCreateDevice")

	iidIDCompositionDevice = com.MustGUID("c37ea93a-e7aa-450d-b16f-9746cb0407f3")
)

// composition shows a swap chain in a window through DirectComposition, the
// only way a flip-model swap chain can carry alpha to the desktop.
type composition struct {
	dev, target, visual unsafe.Pointer
}

func newComposition(dev d3d.Device, hwnd uintptr, swap swapChain) (*composition, error) {
	if err := pDCompositionCreateDevice.Find(); err != nil {
		return nil, err
	}
	dxgiDev, err := com.QueryInterface(dev.P, &iidIDXGIDevice)
	if err != nil {
		return nil, err
	}
	defer com.Release(dxgiDev)
	c := &composition{}
	r, _, _ := pDCompositionCreateDevice.Call(uintptr(dxgiDev), uintptr(unsafe.Pointer(&iidIDCompositionDevice)), uintptr(unsafe.Pointer(&c.dev)))
	if err := com.HRESULT(int32(r)).Err("DCompositionCreateDevice"); err != nil {
		return nil, err
	}
	fail := func(err error) (*composition, error) {
		c.release()
		return nil, err
	}
	// topmost FALSE keeps child windows (WebView2) above the swap chain, as
	// with an HWND swap chain.
	if err := com.HR(c.dev, 6, hwnd, 0, uintptr(unsafe.Pointer(&c.target))).Err("CreateTargetForHwnd"); err != nil {
		return fail(err)
	}
	if err := com.HR(c.dev, 7, uintptr(unsafe.Pointer(&c.visual))).Err("CreateVisual"); err != nil {
		return fail(err)
	}
	if err := com.HR(c.visual, 15, uintptr(swap.p)).Err("IDCompositionVisual.SetContent"); err != nil {
		return fail(err)
	}
	if err := com.HR(c.target, 3, uintptr(c.visual)).Err("IDCompositionTarget.SetRoot"); err != nil {
		return fail(err)
	}
	if err := com.HR(c.dev, 3).Err("IDCompositionDevice.Commit"); err != nil {
		return fail(err)
	}
	return c, nil
}

func (c *composition) release() {
	if c == nil {
		return
	}
	com.Release(c.visual)
	com.Release(c.target)
	com.Release(c.dev)
	*c = composition{}
}
