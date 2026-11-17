package fairy

import (
	"errors"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"syscall"
	"time"
	"unicode/utf16"

	"github.com/myparsleycat/fairy/d3d"
	"github.com/myparsleycat/fairy/internal/frameprof"
	"github.com/myparsleycat/fairy/internal/render"
	"github.com/myparsleycat/fairy/internal/text"
	"github.com/myparsleycat/fairy/internal/win"
	"github.com/myparsleycat/fairy/keyboard"
)

// Frame is a frame drawn by an Options.Headless window.
type Frame struct {
	// Handle is the legacy shared handle (IDXGIResource::GetSharedHandle, not
	// an NT handle) of a DXGI_FORMAT_R8G8B8A8_UNORM texture with premultiplied
	// alpha. Open it with ID3D11Device::OpenSharedResource on a device of the
	// adapter AdapterLUID names, and hold its IDXGIKeyedMutex with key 0
	// (AcquireSync(0)/ReleaseSync(0)) around every use. The texture is
	// replaced, with a new handle, when the size changes, the device is
	// recreated, or a device released it while holding its keyed mutex. Handle is 0 when the texture could not be made; read the
	// pixels with Window.ReadFrame then.
	Handle uintptr
	// Width and Height are the texture size in physical pixels.
	Width, Height int
	// Seq numbers the frames of the window, starting at 1.
	Seq uint64
	// AdapterLUID identifies the adapter of Fairy's device. A program on
	// another adapter cannot open Handle and reads the pixels with ReadFrame.
	AdapterLUID d3d.LUID
}

// WheelAxis is the direction of an injected wheel step.
type WheelAxis uint8

const (
	WheelVertical WheelAxis = iota
	WheelHorizontal
)

const (
	msgPaint      = win.WM_APP + 2 // draws a frame of a headless window that needs one
	msgInvalidate = win.WM_APP + 3 // Live.Set from any goroutine
	msgInput      = win.WM_APP + 4 // runs injected input
	timerFrame    = 6
	// headlessFrameInterval is the shortest time between frames of a
	// headless window. The system timer ticks every 15.6 ms by default, so a
	// shorter wait still lands on the next tick.
	headlessFrameInterval = 15 * time.Millisecond
	// sharedWait is how long a frame waits for the program reading the
	// shared texture to release its keyed mutex. Waiting would hold the
	// device lock all windows share; a busy copy is retried by timer instead.
	sharedWait = 0
)

// headlessState is what only Options.Headless windows use.
type headlessState struct {
	drawnAt time.Time // start of the last frame, for pacing
	shared  *render.SharedTexture
	// failed is the size and device generation for which making or filling
	// the shared texture failed; it is not tried again for them.
	failed   sharedKey
	seq      uint64
	mods     keyboard.Mods
	keysDown [256]bool
	// dropChar: the last injected key down was handled without typing, so
	// the character the program translated from it is dropped, as
	// dropTranslatedChars does for a real window. A key up ends it.
	dropChar bool
	cursor   Cursor // last reported to OnCursor
	// copyPending: the target holds a drawn frame that could not be copied
	// to the shared texture yet.
	copyPending bool
}

type sharedKey struct {
	w, h int
	gen  uint64
}

type injectKind uint8

const (
	injectMove injectKind = iota
	injectLeave
	injectButton
	injectWheel
	injectKey
	injectChar
	injectFocus
	injectSize
)

type injected struct {
	kind    injectKind
	x, y    float32
	delta   float32
	button  int
	down    bool
	axis    WheelAxis
	key     keyboard.Key
	mods    keyboard.Mods
	r       rune
	text    string
	w, h    int
	dpi     uint32
	focused bool
}

func (w *Window) initHeadless() error {
	w.hl = &headlessState{}
	w.tracking = true // a message-only window gets no WM_MOUSELEAVE
	w.width, w.height = int(w.opts.Width*w.scale), int(w.opts.Height*w.scale)
	title, _ := syscall.UTF16PtrFromString(w.opts.Title)
	hwnd, err := win.CreateWindowEx(0, className, title, 0, 0, 0, 0, 0, win.HWND(win.HWND_MESSAGE), 0, win.GetModuleHandle(), 0)
	if err != nil {
		return fmt.Errorf("fairy: create window: %w", err)
	}
	w.hwnd = hwnd
	windows[hwnd] = w
	if w.r, err = w.newRenderer(); err != nil {
		win.DestroyWindow(hwnd)
		return fmt.Errorf("fairy: renderer: %w", err)
	}
	if w.ts, err = text.NewSystem(w.r.Atlas); err != nil {
		win.DestroyWindow(hwnd)
		return fmt.Errorf("fairy: text: %w", err)
	}
	// The first frame waits for the message loop, so OnFrame never runs
	// before NewWindow has returned the window.
	w.invalidate()
	w.Show()
	liveMu.Lock()
	live[w] = struct{}{}
	liveMu.Unlock()
	watchMemoryPressure()
	return nil
}

// scheduleFrame asks for a frame of a headless window, at most one per
// headlessFrameInterval. There is no WM_PAINT or vblank wait to pace it.
func (w *Window) scheduleFrame() {
	if wait := headlessFrameInterval - time.Since(w.hl.drawnAt); wait > 0 {
		w.setTimer(timerFrame, uint32(max(wait.Milliseconds(), 1)))
		return
	}
	win.PostMessage(w.hwnd, msgPaint, 0, 0)
}

// publishFrame copies the frame just drawn into the shared texture and
// reports it to OnFrame.
func (w *Window) publishFrame() {
	hl := w.hl
	hl.seq++
	hl.copyPending = false
	if w.opts.OnFrame == nil {
		return
	}
	dev := w.r.Device()
	key := sharedKey{w.width, w.height, dev.Gen}
	if s := hl.shared; s != nil && (sharedKey{s.Width, s.Height, s.Gen}) != key {
		w.releaseShared()
	}
	if hl.shared == nil && hl.failed != key {
		s, err := render.NewSharedTexture(dev, w.width, w.height)
		if err != nil {
			log.Printf("fairy: shared frame texture %dx%d: %v", w.width, w.height, err)
			hl.failed = key
		}
		hl.shared = s
	}
	f := Frame{Width: w.width, Height: w.height, Seq: hl.seq, AdapterLUID: dev.Info.AdapterLUID}
	if s := hl.shared; s != nil {
		err := w.r.CopyToShared(s, sharedWait)
		if errors.Is(err, render.ErrSharedAbandoned) {
			// The retry below copies into a new texture with a new handle.
			w.releaseShared()
			err = render.ErrSharedBusy
		}
		switch {
		case errors.Is(err, render.ErrSharedBusy):
			// The target keeps this frame until the next draw, so only the
			// copy is tried again.
			hl.seq--
			hl.copyPending = true
			if !w.needPaint {
				// A frame already scheduled publishes anyway, at its pace.
				w.setTimer(timerFrame, 1)
			}
			return
		case errors.Is(err, render.ErrDeviceLost):
			w.recreateRenderer()
			return
		case err != nil:
			log.Printf("fairy: copy frame to shared texture: %v", err)
			w.releaseShared()
			hl.failed = key
		default:
			f.Handle = s.Handle
		}
	}
	w.opts.OnFrame(f)
}

func (w *Window) releaseShared() {
	if w.hl == nil {
		return
	}
	w.hl.shared.Release()
	w.hl.shared = nil
}

// ReadFrame copies the frame just passed to OnFrame of an Options.Headless
// window into dst as rows of Width*4 bytes of RGBA with premultiplied alpha,
// the same pixels as Frame.Handle holds, for a program on another adapter.
// dst must hold Width*Height*4 bytes. Call it from OnFrame: at other times the
// window may hold a newer frame that OnFrame has not received.
func (w *Window) ReadFrame(dst []byte) error {
	if w.hl == nil {
		return errors.New("fairy: ReadFrame needs a Headless window")
	}
	if w.r == nil {
		return errors.New("fairy: ReadFrame: no renderer while the GPU device is being recreated")
	}
	return w.r.ReadPixels(dst)
}

// SetHeadlessSize sizes an Options.Headless window to width x height
// physical pixels at dpi (96 is 100%), as the program showing it is. A dpi
// of 0 keeps the current scale. It is safe from any goroutine.
func (w *Window) SetHeadlessSize(width, height int, dpi uint32) {
	w.inject(injected{kind: injectSize, w: width, h: height, dpi: dpi})
}

// The Inject methods feed input to an Options.Headless window, in physical
// pixels of its surface, as a program showing it forwards its own input.
// They are safe from any goroutine; events run in order on the UI thread and,
// unlike Post, update the view only when a handler changes something. They do
// nothing on other windows.

// InjectPointerMove moves the pointer to (x, y).
func (w *Window) InjectPointerMove(x, y float32) {
	w.inject(injected{kind: injectMove, x: x, y: y})
}

// InjectPointerLeave reports that the pointer left the surface.
func (w *Window) InjectPointerLeave() { w.inject(injected{kind: injectLeave}) }

// InjectButton presses or releases a mouse button at (x, y): 0 left, 1
// right, 2 middle, 3 back (X1) and 4 forward (X2). The back and forward
// buttons act on release, as KeyBrowserBack and KeyBrowserForward.
func (w *Window) InjectButton(button int, down bool, x, y float32) {
	w.inject(injected{kind: injectButton, button: button, down: down, x: x, y: y})
}

// InjectWheel scrolls by delta notches at (x, y), with the signs of
// WM_MOUSEWHEEL and WM_MOUSEHWHEEL: positive is away from the user, or to
// the right. Fractions of a notch come from precision touchpads.
func (w *Window) InjectWheel(axis WheelAxis, delta, x, y float32) {
	w.inject(injected{kind: injectWheel, axis: axis, delta: delta, x: x, y: y})
}

// InjectKey presses or releases the virtual key vk with mods held. text is
// the printable character the key types, if any, as KeyEvent.Text reports
// it; type-ahead in lists reads it. It types nothing: typed text arrives
// separately through InjectChar, as WM_CHAR follows WM_KEYDOWN, and a
// character that follows a key a handler used is dropped.
func (w *Window) InjectKey(vk keyboard.Key, down bool, mods keyboard.Mods, text string) {
	w.inject(injected{kind: injectKey, key: vk, down: down, mods: mods, text: text})
}

// InjectChar types r into the focused input.
func (w *Window) InjectChar(r rune) { w.inject(injected{kind: injectChar, r: r}) }

// SetFocused gives an Options.Headless window keyboard focus or takes it
// away, as WM_SETFOCUS and WM_KILLFOCUS do for a window on screen: it runs
// OnFocus, shows or hides the caret, and taking focus away ends presses and
// releases held keys. A headless window starts without focus.
func (w *Window) SetFocused(focused bool) {
	w.inject(injected{kind: injectFocus, focused: focused})
}

func (w *Window) inject(e injected) {
	if !w.opts.Headless || !finite(e.x) || !finite(e.y) || !finite(e.delta) {
		return
	}
	w.mu.Lock()
	q := &w.injectQueue
	// Moves between other events collapse into the latest, as Windows
	// coalesces WM_MOUSEMOVE.
	if n := len(*q); e.kind == injectMove && n > 0 && (*q)[n-1].kind == injectMove {
		(*q)[n-1] = e
		w.mu.Unlock()
		return
	}
	*q = append(*q, e)
	first := len(*q) == 1
	w.mu.Unlock()
	if first && !win.PostMessage(w.hwnd, msgInput, 0, 0) {
		w.mu.Lock()
		*q = nil
		w.mu.Unlock()
	}
}

func (w *Window) runInjected() {
	w.mu.Lock()
	q := w.injectQueue
	w.injectQueue = nil
	w.mu.Unlock()
	for _, e := range q {
		if w.hl == nil {
			return
		}
		w.applyInjected(e)
		w.reportCursor()
	}
}

func (w *Window) applyInjected(e injected) {
	hl := w.hl
	x, y := e.x/w.scale, e.y/w.scale
	switch e.kind {
	case injectMove:
		w.onMouseMove(x, y)
	case injectLeave:
		w.onMouseLeave()
	case injectButton:
		switch e.button {
		case 0, 1, 2:
		case 3, 4:
			if !e.down {
				k := keyboard.KeyBrowserBack
				if e.button == 4 {
					k = keyboard.KeyBrowserForward
				}
				w.onKeyDown(k, false, "")
			}
			return
		default:
			return
		}
		w.onMouseMove(x, y)
		if e.down {
			w.onButtonDown(x, y, e.button)
		} else {
			w.onButtonUp(x, y, e.button)
		}
	case injectWheel:
		w.wheel(x, y, e.delta, e.delta != float32(int(e.delta)), e.axis == WheelHorizontal)
	case injectKey:
		hl.mods = e.mods
		i := int(e.key) & 0xff
		if !e.down {
			hl.keysDown[i] = false
			hl.dropChar = false
			return
		}
		repeat := hl.keysDown[i]
		hl.keysDown[i] = true
		w.wantChar = false
		hl.dropChar = w.onKeyDown(e.key, repeat, e.text) && !w.wantChar
	case injectChar:
		if hl.dropChar {
			hl.dropChar = false
			return
		}
		var buf [2]uint16
		for _, u := range utf16.AppendRune(buf[:0], e.r) {
			w.onChar(u)
		}
	case injectFocus:
		w.setHeadlessFocus(e.focused)
	case injectSize:
		w.resizeHeadless(e.w, e.h, e.dpi)
	}
}

func (w *Window) setHeadlessFocus(focused bool) {
	if focused == w.osFocus {
		return
	}
	if w.opts.OnFocus != nil {
		w.opts.OnFocus(focused)
		w.Update()
	}
	w.osFocus = focused
	if focused {
		w.resetBlink()
	} else {
		w.hl.mods, w.hl.keysDown, w.hl.dropChar = 0, [256]bool{}, false
		w.commitComposition()
		w.cancelPress()
		w.stopAutoScroll()
	}
	w.invalidate()
}

func (w *Window) resizeHeadless(width, height int, dpi uint32) {
	width, height = max(width, 0), max(height, 0)
	if dpi != 0 {
		if s := float32(dpi) / 96; s != w.scale {
			w.scale = s
			InvalidateMemos()
			w.markDirty(frameprof.CauseResize)
		}
	}
	if width != w.width || height != w.height {
		w.width, w.height = width, height
		if w.r != nil && width > 0 && height > 0 {
			w.resizeRenderer()
		}
		w.markDirty(frameprof.CauseResize)
	}
	// Input queued behind the resize hit-tests the new layout, not the one the
	// next frame would replace.
	if w.dirtyBy&frameprof.CauseResize != 0 && w.r != nil && w.width > 0 && w.height > 0 {
		w.layout(0)
	}
	w.invalidate()
}

// reportCursor tells OnCursor the pointer shape a headless window wants
// when it changed.
func (w *Window) reportCursor() {
	if w.hl == nil || w.opts.OnCursor == nil {
		return
	}
	if c := w.wantedCursor(); c != w.hl.cursor {
		w.hl.cursor = c
		w.opts.OnCursor(c)
	}
}

func (w *Window) setCapture() {
	if w.hl == nil {
		win.SetCapture(w.hwnd)
	}
}

// releaseCapture releases the mouse capture, which belongs to the thread: a
// headless window never took it and leaves another window's alone.
func (w *Window) releaseCapture() {
	if w.hl == nil {
		win.ReleaseCapture()
	}
}

// SaveSnapshot writes the next rendered frame to a PNG file and calls done with the result.
func (w *Window) SaveSnapshot(path string, done func(error)) {
	if w.r == nil {
		if done != nil {
			w.Post(func() { done(errors.New("fairy: snapshot: no renderer while the GPU device is being recreated")) })
		}
		return
	}
	w.r.RequestSnapshot(func(img *image.NRGBA) {
		err := writePNG(path, img)
		if done != nil {
			w.Post(func() { done(err) })
		}
	})
	w.invalidate()
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
