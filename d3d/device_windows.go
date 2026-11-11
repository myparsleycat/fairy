package d3d

import (
	"errors"
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/myparsleycat/fairy/internal/com"
)

// DeviceOptions adjusts CreateDeviceWithOptions.
type DeviceOptions struct {
	// MinFeatureLevel is the lowest acceptable FEATURE_LEVEL_*. Zero means
	// FEATURE_LEVEL_10_0.
	MinFeatureLevel uint32
}

// DeviceInfo describes a device made by CreateDeviceWithOptions.
type DeviceInfo struct {
	// DriverType is DRIVER_TYPE_HARDWARE or DRIVER_TYPE_WARP, FeatureLevel is
	// the feature level the device runs at, and CreationFlags are the
	// CREATE_DEVICE_* flags it was made with.
	DriverType, FeatureLevel, CreationFlags uint32
	// AdapterName, VendorID and DeviceID identify the adapter when
	// AdapterKnown is set.
	AdapterName        string
	VendorID, DeviceID uint32
	// Software reports a WARP device or a software adapter. AdapterKnown
	// reports whether the adapter description could be read.
	Software, AdapterKnown bool
	// FallbackReason lists why the hardware attempts failed when the device
	// is WARP.
	FallbackReason string
}

// CreateDeviceWithOptions is CreateDevice that also returns a description of
// the device. Both the hardware device and the WARP fallback must support at
// least opts.MinFeatureLevel.
func CreateDeviceWithOptions(opts DeviceOptions) (Device, Context, DeviceInfo, error) {
	d, c, info, err := createDeviceWithOptions(opts, createNativeDevice)
	if err == nil {
		readAdapterInfo(d, &info)
	}
	return d, c, info, err
}

const (
	// eInvalidArg is E_INVALIDARG, which runtimes before Direct3D 11.1
	// return for a feature level list that includes FEATURE_LEVEL_11_1.
	eInvalidArg = com.HRESULT(-2147024809)

	dxgiAdapterFlagSoftware = 2 // DXGI_ADAPTER_FLAG_SOFTWARE
)

type deviceCreator func(driver, flags uint32, levels []uint32) (Device, Context, uint32, error)

func createDeviceWithOptions(opts DeviceOptions, create deviceCreator) (Device, Context, DeviceInfo, error) {
	minimum := opts.MinFeatureLevel
	if minimum == 0 {
		minimum = FEATURE_LEVEL_10_0
	}
	switch minimum {
	case FEATURE_LEVEL_10_0, FEATURE_LEVEL_10_1, FEATURE_LEVEL_11_0, FEATURE_LEVEL_11_1:
	default:
		return Device{}, Context{}, DeviceInfo{}, fmt.Errorf("d3d: unsupported minimum feature level %#x", minimum)
	}
	levels := []uint32{FEATURE_LEVEL_11_1, FEATURE_LEVEL_11_0, FEATURE_LEVEL_10_1, FEATURE_LEVEL_10_0}
	for levels[len(levels)-1] < minimum {
		levels = levels[:len(levels)-1]
	}
	var failures, hardwareFailures []error
	for _, driver := range []uint32{DRIVER_TYPE_HARDWARE, DRIVER_TYPE_WARP} {
		for _, flags := range []uint32{CREATE_DEVICE_BGRA_SUPPORT | CREATE_DEVICE_VIDEO_SUPPORT, CREATE_DEVICE_BGRA_SUPPORT} {
			candidates := levels
			for len(candidates) > 0 {
				d, c, fl, err := create(driver, flags, candidates)
				if err == nil {
					info := DeviceInfo{DriverType: driver, FeatureLevel: fl, CreationFlags: flags, Software: driver == DRIVER_TYPE_WARP}
					if driver == DRIVER_TYPE_WARP {
						info.FallbackReason = strings.ReplaceAll(errors.Join(hardwareFailures...).Error(), "\n", "; ")
					}
					return d, c, info, nil
				}
				failure := fmt.Errorf("d3d: driver=%d flags=%#x levels=%#x: %w", driver, flags, candidates, err)
				failures = append(failures, failure)
				if driver == DRIVER_TYPE_HARDWARE {
					hardwareFailures = append(hardwareFailures, failure)
				}
				if candidates[0] != FEATURE_LEVEL_11_1 || !errors.Is(err, eInvalidArg) {
					break
				}
				candidates = candidates[1:]
			}
		}
	}
	return Device{}, Context{}, DeviceInfo{}, errors.Join(failures...)
}

func createNativeDevice(driver, flags uint32, levels []uint32) (Device, Context, uint32, error) {
	var dev, ctx unsafe.Pointer
	var fl uint32
	r, _, _ := pD3D11CreateDevice.Call(0, uintptr(driver), 0, uintptr(flags),
		uintptr(unsafe.Pointer(unsafe.SliceData(levels))), uintptr(len(levels)), sdkVersion,
		uintptr(unsafe.Pointer(&dev)), uintptr(unsafe.Pointer(&fl)), uintptr(unsafe.Pointer(&ctx)))
	if err := com.HRESULT(int32(r)).Err("D3D11CreateDevice"); err != nil {
		return Device{}, Context{}, 0, err
	}
	mt, err := com.QueryInterface(dev, &iidID3D10Multithread)
	if err != nil {
		com.Release(ctx)
		com.Release(dev)
		return Device{}, Context{}, 0, err
	}
	com.Call(mt, 5, 1) // ID3D10Multithread::SetMultithreadProtected(TRUE)
	com.Release(mt)
	return Device{dev}, Context{ctx}, fl, nil
}

func readAdapterInfo(d Device, info *DeviceInfo) {
	dxgi, err := com.QueryInterface(d.P, &iidIDXGIDevice)
	if err != nil {
		return
	}
	defer com.Release(dxgi)
	var adapter unsafe.Pointer
	if com.HR(dxgi, 7, uintptr(unsafe.Pointer(&adapter))).Failed() {
		return
	}
	defer com.Release(adapter)
	a, err := com.QueryInterface(adapter, &iidIDXGIAdapter1)
	if err != nil {
		return
	}
	defer com.Release(a)
	var desc struct {
		Description                                                     [128]uint16
		VendorID, DeviceID, SubSysID, Revision                          uint32
		DedicatedVideoMemory, DedicatedSystemMemory, SharedSystemMemory uintptr
		LUID                                                            [2]uint32
		Flags                                                           uint32
	}
	if com.HR(a, 10, uintptr(unsafe.Pointer(&desc))).Failed() {
		return
	}
	info.AdapterName = windows.UTF16ToString(desc.Description[:])
	info.VendorID, info.DeviceID = desc.VendorID, desc.DeviceID
	info.Software = info.Software || desc.Flags&dxgiAdapterFlagSoftware != 0
	info.AdapterKnown = true
}
