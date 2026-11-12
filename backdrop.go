package fairy

// Backdrop is a system material Windows draws behind a window's content.
type Backdrop uint8

const (
	BackdropNone    Backdrop = iota // the window clears to Options.Background
	BackdropMica                    // Mica, tinted by the desktop wallpaper
	BackdropMicaAlt                 // Mica Alt, for windows with tabs
	BackdropAcrylic                 // Acrylic, for transient windows
)

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

// clearColor is the color each frame starts from.
func (w *Window) clearColor() [4]float32 {
	if w.backdrop != BackdropNone && w.r != nil && w.r.Transparent() {
		return [4]float32{}
	}
	return w.opts.Background.arr(1)
}
