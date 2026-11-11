package fairy

import (
	"sync/atomic"
	"unsafe"

	xwin "golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var systemDark atomic.Bool

func init() { loadSystemDark() }

// SystemDark reports whether Windows is set to the dark mode for apps. When
// the user changes it, every window builds its view again, so a View that
// follows the system theme can read it on each build.
func SystemDark() bool { return systemDark.Load() }

func loadSystemDark() {
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		systemDark.Store(false)
		return
	}
	defer k.Close()
	light, _, err := k.GetIntegerValue("AppsUseLightTheme")
	systemDark.Store(err == nil && light == 0)
}

// isColorSetChange reports whether a WM_SETTINGCHANGE lParam announces a
// change of the app color mode.
func isColorSetChange(lp uintptr) bool {
	return lp != 0 && xwin.UTF16PtrToString((*uint16)(unsafe.Pointer(lp))) == "ImmersiveColorSet"
}
