package fairy

import (
	"errors"
	"unsafe"

	syswin "golang.org/x/sys/windows"

	"github.com/myparsleycat/fairy/internal/win"
)

// Backdrop is a system material Windows draws behind a window's content.
type Backdrop uint8

const (
	BackdropNone    Backdrop = iota // the window clears to Options.Background
	BackdropMica                    // Mica, tinted by the desktop wallpaper
	BackdropMicaAlt                 // Mica Alt, for windows with tabs
	BackdropAcrylic                 // Acrylic, for transient windows
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

// SetBackdrop puts a system backdrop behind the window's content, or removes
// it with BackdropNone. While a backdrop is on, the window clears to
// transparent instead of Options.Background, so the backdrop shows wherever
// elements leave the window uncovered or translucent; give panels their own
// Fill. The swap chain then carries alpha through DirectComposition. The
// backdrop follows the dark mode of Options.Dark and SetColors, and Windows
// draws it only while the window is active unless the user's settings say
// otherwise.
//
// When b is not supported, or the transparent swap chain cannot be made, the
// window stays opaque with Options.Background and SetBackdrop returns an
// error. Call it on the UI thread.
func (w *Window) SetBackdrop(b Backdrop) error {
	if b == w.backdrop {
		return nil
	}
	if !BackdropSupported(b) {
		return errBackdrop
	}
	on := b != BackdropNone
	if w.r != nil && !w.opts.DynamicBackdrop {
		if err := w.r.SetTransparent(on); err != nil {
			w.applyBackdrop(BackdropNone)
			w.backdrop = BackdropNone
			w.Update()
			return err
		}
	}
	w.applyBackdrop(b)
	w.backdrop = b
	w.Update()
	return nil
}

// Backdrop returns the window's backdrop.
func (w *Window) Backdrop() Backdrop { return w.backdrop }

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

// clearColor is the color each frame starts from.
func (w *Window) clearColor() [4]float32 {
	if w.backdrop != BackdropNone && w.r != nil && w.r.Transparent() {
		return [4]float32{}
	}
	return w.opts.Background.arr(1)
}
