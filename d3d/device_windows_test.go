package d3d

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/myparsleycat/fairy/internal/com"
)

func TestDeviceMinimumAndFallback(t *testing.T) {
	for _, minimum := range []uint32{0, FEATURE_LEVEL_10_1} {
		t.Run(fmt.Sprintf("minimum_%x", minimum), func(t *testing.T) {
			create := func(driver, flags uint32, levels []uint32) (Device, Context, uint32, error) {
				limit := uint32(FEATURE_LEVEL_11_0)
				if driver == DRIVER_TYPE_HARDWARE {
					limit = FEATURE_LEVEL_10_0
				}
				for _, fl := range levels {
					if fl <= limit {
						return Device{}, Context{}, fl, nil
					}
				}
				return Device{}, Context{}, 0, com.HRESULT(-2005270524)
			}
			_, _, info, err := createDeviceWithOptions(DeviceOptions{MinFeatureLevel: minimum}, create)
			if err != nil {
				t.Fatal(err)
			}
			if minimum == 0 {
				if info.DriverType != DRIVER_TYPE_HARDWARE || info.FeatureLevel != FEATURE_LEVEL_10_0 {
					t.Fatalf("default changed: %+v", info)
				}
			} else if info.DriverType != DRIVER_TYPE_WARP || info.FeatureLevel < minimum || info.FallbackReason == "" {
				t.Fatalf("incompatible device selected instead of WARP: %+v", info)
			}
		})
	}
}

func TestDeviceCreationAttempts(t *testing.T) {
	var calls int
	create := func(driver, flags uint32, levels []uint32) (Device, Context, uint32, error) {
		calls++
		if len(levels) != 1 || levels[0] != FEATURE_LEVEL_11_1 {
			t.Fatal("retry lowered the minimum feature level")
		}
		return Device{}, Context{}, 0, com.HRESULT(-2147024809)
	}
	_, _, _, err := createDeviceWithOptions(DeviceOptions{MinFeatureLevel: 123}, create)
	if err == nil || calls != 0 {
		t.Fatal("invalid level reached device creation")
	}
	_, _, _, err = createDeviceWithOptions(DeviceOptions{MinFeatureLevel: FEATURE_LEVEL_11_1}, create)
	if !errors.Is(err, com.HRESULT(-2147024809)) || !strings.Contains(err.Error(), "driver=5") || !strings.Contains(err.Error(), "flags=0x820") {
		t.Fatalf("missing attempt errors: %v", err)
	}
	if calls != 4 {
		t.Fatalf("made %d attempts, want both flag combinations on each driver", calls)
	}
}

func TestDeviceRetainsHardwareWithoutVideoSupport(t *testing.T) {
	var calls int
	create := func(driver, flags uint32, levels []uint32) (Device, Context, uint32, error) {
		calls++
		if driver != DRIVER_TYPE_HARDWARE {
			t.Fatal("used WARP before trying hardware without video support")
		}
		if levels[0] == FEATURE_LEVEL_11_1 {
			return Device{}, Context{}, 0, com.HRESULT(-2147024809)
		}
		if flags&CREATE_DEVICE_VIDEO_SUPPORT != 0 {
			return Device{}, Context{}, 0, com.HRESULT(-2005270524)
		}
		return Device{}, Context{}, FEATURE_LEVEL_11_0, nil
	}
	_, _, info, err := createDeviceWithOptions(DeviceOptions{MinFeatureLevel: FEATURE_LEVEL_10_1}, create)
	if err != nil || calls != 4 || info.Software || info.CreationFlags != CREATE_DEVICE_BGRA_SUPPORT || info.FallbackReason != "" {
		t.Fatalf("hardware retry: %+v, %v (%d calls)", info, err, calls)
	}
}

func TestWARPAdapterInfo(t *testing.T) {
	d, c, fl, err := createNativeDevice(DRIVER_TYPE_WARP, CREATE_DEVICE_BGRA_SUPPORT, []uint32{FEATURE_LEVEL_10_1})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Release()
	defer c.Release()
	info := DeviceInfo{DriverType: DRIVER_TYPE_WARP, FeatureLevel: fl}
	readAdapterInfo(d, &info)
	if !info.AdapterKnown || !info.Software || info.AdapterName == "" || info.FeatureLevel != FEATURE_LEVEL_10_1 {
		t.Fatalf("adapter info: %+v", info)
	}
}
