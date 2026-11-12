package fairy

import (
	"errors"
	"unsafe"

	syswin "golang.org/x/sys/windows"

	"github.com/myparsleycat/fairy/internal/win"
)

// backdropMinBuild is the first Windows 11 build (22H2) with
// DWMWA_SYSTEMBACKDROP_TYPE.
const backdropMinBuild = 22621

// BackdropSupported reports whether this version of Windows draws b. Mica
// and Acrylic need Windows 11 22H2 (build 22621) or later; BackdropNone is
// always supported.
func BackdropSupported(b Backdrop) bool {
	if b == BackdropNone {
		return true
	}
	if b > BackdropAcrylic {
		return false
	}
	return syswin.RtlGetVersion().BuildNumber >= backdropMinBuild
}

// errBackdrop reports a backdrop that this Windows does not draw.
var errBackdrop = errors.New("fairy: window backdrop not supported on this version of Windows")

func (w *Window) applyBackdrop(b Backdrop) {
	m := win.MARGINS{}
	if w.opts.Frameless {
		// System backdrops cover the whole window. Extending the full frame
		// would also draw native caption buttons over the custom title bar.
		m.Top = 1
	} else if b != BackdropNone {
		m = win.MARGINS{Left: -1, Right: -1, Top: -1, Bottom: -1}
	}
	win.DwmExtendFrameIntoClientArea(w.hwnd, &m)
	if !BackdropSupported(BackdropMica) {
		return
	}
	v := [...]int32{1, 2, 4, 3}[b] // DWMSBT_NONE, MAINWINDOW, TABBEDWINDOW, TRANSIENTWINDOW
	win.DwmSetWindowAttribute(w.hwnd, win.DWMWA_SYSTEMBACKDROP_TYPE, unsafe.Pointer(&v), 4)
}
