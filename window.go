//go:build windows

package fairy

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"
	"weak"

	"github.com/myparsleycat/fairy/internal/embed"
	"github.com/myparsleycat/fairy/internal/frameprof"
	"github.com/myparsleycat/fairy/internal/geom"
	"github.com/myparsleycat/fairy/internal/gesture"
	"github.com/myparsleycat/fairy/internal/render"
	"github.com/myparsleycat/fairy/internal/scroll"
	"github.com/myparsleycat/fairy/internal/text"
	"github.com/myparsleycat/fairy/internal/tooltip"
	"github.com/myparsleycat/fairy/keyboard"
)

// Options configures the application window.
type Options struct {
	Title               string
	Width, Height       float32 // logical pixels; 0 picks 960x640
	MinWidth, MinHeight float32
	// Frameless removes the system title bar and borders. Mark title-bar
	// elements with DragRegion and build your own window buttons.
	Frameless bool
	// Dark asks for a dark system title bar and window frame.
	Dark       bool
	Background Color
	// Icon is a PNG used for the window and taskbar icon.
	Icon []byte
	// Default text style.
	Font      string
	FontSize  float32
	TextColor Color
	// StartHidden creates the window without showing it; call Window.Show.
	StartHidden bool
	// Backdrop puts a system material such as Mica behind the content where
	// Windows supports it; see Window.SetBackdrop.
	Backdrop Backdrop
	// DynamicBackdrop is for windows that switch the backdrop on and off at
	// run time with SetBackdrop: the window keeps its composition swap chain
	// when the backdrop is turned off, so the old frame does not stay visible.
	// It has no effect where backdrops are not supported.
	DynamicBackdrop bool
	// Headless makes a window that is not on screen, to draw the UI inside
	// another program such as a game. It renders into an offscreen texture
	// whenever the view changes, at most about 60 frames per second, and
	// hands each frame to OnFrame. Size it with SetHeadlessSize, drive it with
	// the Inject methods and SetFocused, and show the pointer shape OnCursor
	// reports. Width and Height give the first size at the system DPI. It
	// clears to transparent instead of Background. Title bar, frame, icon,
	// backdrop, minimum size, file drop, IME and native menu options do not
	// apply, and the methods that move, show or activate a window do nothing.
	// Like a window on screen, it keeps Main running until it is closed.
	Headless bool
	// OnFrame receives each frame of a Headless window on the UI thread,
	// right after it is drawn.
	OnFrame func(Frame)
	// OnCursor receives the pointer shape that the element under a Headless
	// window's injected pointer asks for, whenever it changes.
	OnCursor func(Cursor)
	// SpellLanguage is the BCP 47 tag, such as "en-US", of the dictionary
	// that checks spelling in Inputs. Empty uses the user's locale.
	SpellLanguage string

	// View builds the UI. It runs on the UI thread after every handler and Post.
	View func(cx *Cx) *El
	// OnKey receives key presses that no focused element handled.
	OnKey func(keyboard.KeyEvent) bool
	// OnClose runs when the user closes the window; return false to keep it open.
	OnClose func() bool
	// OnFocus runs when the window gains or loses keyboard focus.
	OnFocus func(focused bool)
	// OnReady runs once after the first frame is on screen.
	OnReady func(w *Window)
}

// Cx is passed to View.
type Cx struct{ w *Window }

// Size returns the window's client size in logical pixels.
func (cx *Cx) Size() (float32, float32) {
	return float32(cx.w.width) / cx.w.scale, float32(cx.w.height) / cx.w.scale
}

// Overlay draws content above the view, sized to the window. Overlays block
// input to the view beneath unless content is PassThrough. onDismiss, if set,
// runs on Escape. Call it from View; it lasts for that build. Overlays are
// identified by call order, so give content a Key when an earlier overlay is
// shown conditionally.
func (cx *Cx) Overlay(content *El, onDismiss func()) {
	if content == nil {
		return
	}
	cx.w.layers = append(cx.w.layers, &layer{root: content, modal: !content.passThrough, onDismiss: onDismiss})
}

// Window returns the window being built.
func (cx *Cx) Window() *Window { return cx.w }

// Focused reports whether the element with key has keyboard focus.
func (cx *Cx) Focused(key string) bool { return cx.w.Focused(key) }

// Focused reports whether the element with key has keyboard focus.
func (w *Window) Focused(key string) bool {
	return w.focused != 0 && w.focusKey != "" && w.focusKey == key
}

const (
	timerCaret   = 1
	timerTip     = 2
	timerAnim    = 3
	timerRecover = 4
	timerTouch   = 5
	caretBlinkMs = 530
	// Animation frame interval inside the modal move/size loop. Painting back
	// to back there blocks in Present on every vblank and starves the loop's
	// mouse handling, so the window lags behind the cursor.
	sizeMoveFrameMs = 16
	// Animation frame interval while the window is occluded. Present does not
	// wait for vblank then, so invalidating back to back would spin.
	hiddenFrameMs = 100
	// Interval between attempts to resize the swap chain after resizing failed.
	resizeRetryMs = 250
)

// Window is a top-level native window.
type Window struct {
	windowPlat // the native window

	opts          Options
	dirtyBy       frameprof.Cause
	growing       bool // an element in the view has AnimateHeight
	ft            frameprof.Times
	clock         *scriptClock
	r             *render.Renderer
	ts            *text.System
	scene         render.Scene
	scale         float32
	width, height int

	root      *El
	states    map[uint64]*elState
	frame     int
	dirty     bool
	needPaint bool
	ready     bool
	building  bool

	collecting  bool // popovers are gathered into layers
	autoFocused bool // the first AutoFocus in a tree or layer wins

	gen       int // view build generation
	prevByID  map[uint64]*El
	layers    []*layer
	ghosts    []*ghost
	ghostIn   map[uint64]bool // ids of the elements ghosts are drawn in, as of the last build
	dt        float32         // seconds since the previous frame while animating
	lastFrame time.Time
	animating bool
	wakeAt    time.Time // earliest time a later frame was requested for; zero when none
	sizeMove  bool      // inside the modal move/size loop
	recoverMs int       // delay before the next renderer recreation attempt; 0 when none failed
	resizeErr bool      // the last swap chain resize failed; logged once per failure streak

	// growStale is set when the view was laid out since the last growPass,
	// and grew when that pass moved anything; otherwise the pass has
	// nothing to undo or apply.
	growStale, grew bool
	// exits counts the elements with an Exit transition indexed since the
	// last build began, and prevExits those of the build before.
	exits, prevExits int

	preciseWheelAt time.Time // last wheel message with a fraction of a notch
	wheelLatch     scroll.Latch
	// scriptWheelLines replaces the system's wheel scroll lines setting, so
	// tests scroll the same distance on every machine.
	scriptWheelLines *uint32

	hits       []hit
	chromeHits []int
	// chromeTop is the bottom of the WindowChrome band along the top edge.
	// Popovers and tooltips stay below it, since chrome takes the pointer
	// before overlays and would swallow presses on anything drawn over it.
	chromeTop float32
	// layerChromeTop is chromeTop measured from overlay layers on the last
	// placement. Popovers are placed among those layers, before later ones
	// are, so they use the previous frame's value.
	layerChromeTop float32
	bars           []scrollBar
	focusables     []*El
	byID           map[uint64]*El

	hoverPath      []*El
	hoverScratch   []*El // updateHover builds the next path here
	hoverBusy      bool  // flushPointerMove is ranging over hoverPath
	hoverIDs       map[uint64]bool
	mouseX, mouseY float32
	mouseIn        bool

	press      *pressState
	autoScroll *autoScroll    // middle-button autoscroll in progress
	touch      *gesture.Touch // the touch contact being followed
	touchPress bool           // onButtonDown is handling a touch
	focused    uint64
	focusKey   string
	focusWant  string        // key Focus asked for before its element was built
	returns    []focusReturn // open modal overlays and popovers, bottom first
	// keyFocus: the keyboard moved focus last, so FocusHighlight rows show it.
	// hoverFocus: the pointer focused the FocusHighlight row under it.
	keyFocus   bool
	hoverFocus bool
	typeahead  typeahead
	imeOn      bool
	wantChar   bool // the key being handled was typed into an input; keep its WM_CHAR

	// A ScrollToRow request, applied when the list is next placed.
	revealKey      string
	revealRow      int
	revealInstant  bool
	revealTop      bool                // a ScrollToRowTop request
	bottomKey      string              // a ScrollToBottom request
	scrollKeys     map[string]*elState // keyed scroll containers of the last build
	intoViewKey    string              // a ScrollIntoView request
	scrollStepReq  *scrollStepRequest
	tickers        []ticker
	tickRun        []ticker // scratch for runTicks
	memos          map[string]*memoEntry
	layerRanges    [][2]int // scratch for transformPrims
	matched        map[string]matchRec
	matchedPrune   int               // len(matched) over which placeAll drops stale records
	matchClaims    map[string]uint64 // match ids of the elements in the view being built
	placePass      int
	flights        []flight
	paintingFlight bool
	memoPath       string    // the key of the Memo being built, which prefixes those inside it
	memoRec        *[]string // collects the Local keys the Memo being built touches
	keeping        bool      // prepare is inside a reused Memo subtree that lays out as before

	lastClickAt uint32
	lastClickX  float32
	lastClickY  float32
	clickCount  int

	caretOn    bool
	scriptMods *keyboard.Mods
	scripted   bool // FAIRY_SCRIPT drives input; real pointer moves, wheel and touch are ignored
	hoverMoves bool // an element on the hover path has OnPointerMove
	moveDue    bool // the pointer moved since OnPointerMove handlers last ran
	osFocus    bool
	tip        tooltip.State

	textures        map[uint64]*texEntry
	uploaded        int64 // image upload, update and mip writes in this frame
	imageWorkEpoch  uint64
	imageWorkSeq    uint64
	imageWork       map[imageWorkKey]imageWorkPending
	capture         *offscreenCapture
	surfaces        map[uint64]*surfaceEntry
	invalidSurfaces map[string]bool // InvalidateSurface keys waiting for the next paint
	canvases        map[uint64]*canvasEntry
	pathTextures    map[uint64]*pathTexture // by content; see Painter.drawPath
	natives         map[embed.Native]bool   // child windows of embedded elements, and whether this frame placed them
	embedCtx        embed.Context
	iconTiles       map[iconKey]render.Tile
	iconGen         int

	drag   *fileDrag
	locals map[string]*localEntry

	acc     *a11yState
	treeVer int // bumped whenever the element tree may have changed shape

	fakeClip *string // tests use it in place of the system clipboard

	sel *textSelection // selection of read-only text; nil when none

	backdrop Backdrop

	hl *headlessState // set for Options.Headless windows

	mu          sync.Mutex
	posted      []postedCall
	injectQueue []injected
	quit        bool
	closed      atomic.Bool // destroyed; read by Live.Set on any goroutine
}

type texEntry struct {
	tex    *render.Texture
	img    weak.Pointer[Image]
	frame  int // window frame that last drew it
	shown  int // animation frame the texture holds
	shared bool
	gen    uint64 // device generation of a shared texture
}

type hit struct {
	el         *El
	x, y, w, h float32
	clip       geom.Rect
	radii      [4]float32
	inv        xform // window to layout space, when turned
	turned     bool
	clipInv    xform // window to the space clip is in, when clipTurned
	clipTurned bool
}

// Run opens one window and runs the message loop until it closes. Programs
// with several windows or a tray icon use Main instead.
func Run(opts Options) error {
	return Main(func() error {
		_, err := NewWindow(opts)
		return err
	})
}

var (
	liveMu sync.Mutex
	live   = map[*Window]struct{}{}
)

// wakeWindows updates every open window, for state that changes off the UI
// thread such as finished image loads.
func wakeWindows() {
	liveMu.Lock()
	defer liveMu.Unlock()
	for w := range live {
		w.Post(func() {})
	}
}

// Bounds is a window rectangle in workspace coordinates: device pixels like
// screen coordinates, but offset by the taskbar and other app bars docked at the
// top or left of the monitor.
type Bounds struct{ X, Y, W, H int }

// SetColors switches the window between light and dark as Options.Dark,
// Background and TextColor do at creation: the system frame and title bar,
// tooltips, the color behind the view and the default text color. A zero
// color keeps the current one. Call it on the UI thread.
func (w *Window) SetColors(dark bool, background, text Color) {
	if dark != w.opts.Dark {
		w.opts.Dark = dark
		w.setDarkFrame(dark)
	}
	if background.isSet() {
		w.opts.Background = background
	}
	if text.isSet() {
		w.opts.TextColor = text
	}
	w.Update()
}

// Scale returns the device pixels per logical pixel.
func (w *Window) Scale() float32 { return w.scale }

// Update marks the view as stale so View runs again before the next frame.
// Call it on the UI thread; from other goroutines use Post.
func (w *Window) Update() {
	w.markDirty(frameprof.CauseUpdate)
	w.invalidate()
}

// Post runs f on the UI thread and then rebuilds the view. It is safe from any goroutine.
// Calls posted to a closed window are discarded.
func (w *Window) Post(f func()) { w.post(f, false) }

// PostQuiet runs f on the UI thread without rebuilding the view, for work
// that changes nothing the view shows or that redraws through Live values.
// f calls Update when it does change the view. It is safe from any goroutine
// and keeps its order among calls to Post.
func (w *Window) PostQuiet(f func()) { w.post(f, true) }

type postedCall struct {
	f     func()
	quiet bool
}

func (w *Window) post(f func(), quiet bool) {
	w.mu.Lock()
	w.posted = append(w.posted, postedCall{f, quiet})
	n := len(w.posted)
	w.mu.Unlock()
	if n == 1 && !w.wake() {
		// Nothing will run the queue, and a later Post wakes the window
		// only when the queue is empty.
		w.mu.Lock()
		w.posted = nil
		w.mu.Unlock()
	}
}

// Focus moves keyboard focus to the first element with key, or clears it for "".
// When no such element is built yet, focus moves to it once a build makes it.
func (w *Window) Focus(key string) {
	w.focusWant = ""
	if key == "" {
		w.setFocus(nil)
		return
	}
	for _, e := range w.focusables {
		if e.key == key {
			w.setFocus(e)
			return
		}
	}
	w.focusWant = key
}

// ScrollToRow scrolls the virtual list with key the least distance that shows
// row i (cell i of a VirtualGrid). It applies when the list is next placed, so
// rows added by the same handler can be shown. In a VirtualListMeasured, rows
// whose height is not known yet are measured on the way.
//
// The view glides there: the rows on the way are measured first and the glide
// starts at most two views away from the row.
func (w *Window) ScrollToRow(key string, i int) {
	w.revealKey, w.revealRow, w.revealInstant, w.revealTop = key, i, false, false
	w.invalidate()
}

// ScrollToRowTop glides the virtual list with key until row i is at the top
// of the view, or as near as the content allows.
func (w *Window) ScrollToRowTop(key string, i int) {
	w.revealKey, w.revealRow, w.revealInstant, w.revealTop = key, i, false, true
	w.invalidate()
}

// ScrollToRowInstant is ScrollToRow without the glide.
func (w *Window) ScrollToRowInstant(key string, i int) {
	w.revealKey, w.revealRow, w.revealInstant, w.revealTop = key, i, true, false
	w.invalidate()
}

// ScrollToBottom glides the vertical scroll container with key to its end,
// following content that grows on the way. In a VirtualListMeasured the last
// rows are measured first, so the glide ends where the content does. A
// container with StickToBottom keeps following once there.
func (w *Window) ScrollToBottom(key string) {
	w.bottomKey = key
	w.invalidate()
}

// ScrollIntoView scrolls the scroll containers around the element with key,
// innermost first, by the least distance that shows it, gliding vertically.
// The element must be in the view, though it may be scrolled out of sight;
// for rows a virtual list has not built, use ScrollToRow.
func (w *Window) ScrollIntoView(key string) {
	w.intoViewKey = key
	w.invalidate()
}

// ScrollOffset returns the scroll offset of the scroll container with key as
// it is now, and whether the last build had such a container. Called from
// View, it finds the containers of the build before. It reads the offset
// without an OnScroll handler, which would build the view again as it
// scrolls. Call it on the UI thread.
func (w *Window) ScrollOffset(key string) (x, y float32, ok bool) {
	s := w.scrollKeys[key]
	if s == nil {
		return 0, 0, false
	}
	return s.ScrollX, s.ScrollY, true
}

// ScrollStep scrolls the innermost vertical scroll container around the
// element with key by one of its children, at once, when the view is next
// placed: up shows the child before the first one shown whole, down the one
// after the last one shown whole. Children with RoleSeparator are passed
// over. The child shown lines up with the container's ClipInset on that
// edge, as under a scroll arrow laid over it with PinToView. It reports
// whether an element with key is in the view; when it is not, nothing
// scrolls.
func (w *Window) ScrollStep(key string, up bool) bool {
	if w.findKey(key) == nil {
		return false
	}
	w.scrollStepReq = &scrollStepRequest{key, up}
	w.invalidate()
	return true
}

func (w *Window) invalidate() {
	if !w.needPaint {
		w.needPaint = true
		w.requestPaint()
	}
}

func (w *Window) runPosted() {
	w.mu.Lock()
	calls := w.posted
	w.posted = nil
	w.mu.Unlock()
	rebuild := false
	for _, c := range calls {
		t := time.Now()
		c.f()
		if frameprof.Slow > 0 {
			frameprof.LogSlow("posted function", t)
		}
		rebuild = rebuild || !c.quiet
	}
	if rebuild {
		w.markDirty(frameprof.CausePost)
		w.invalidate()
	}
}

// Mods returns the modifier keys held down now, such as Ctrl during a click.
func (w *Window) Mods() keyboard.Mods { return w.mods() }

func (w *Window) mods() keyboard.Mods {
	if w.scriptMods != nil {
		return *w.scriptMods
	}
	if w.hl != nil {
		return w.hl.mods
	}
	return w.keyMods()
}

// newWindowState checks opts, fills in its defaults and makes the window's
// state, before the native window exists.
func newWindowState(opts Options) (*Window, error) {
	opts.DynamicBackdrop = opts.DynamicBackdrop && BackdropSupported(BackdropMica)
	if opts.View == nil {
		return nil, errors.New("fairy: Options.View is required")
	}
	if !onUIThread() {
		return nil, errors.New("fairy: NewWindow must run on the UI thread (inside Main, a handler or Do)")
	}
	if opts.Width <= 0 {
		opts.Width = 960
	}
	if opts.Height <= 0 {
		opts.Height = 640
	}
	if opts.FontSize <= 0 {
		opts.FontSize = 14
	}
	if opts.Font == "" {
		opts.Font = defaultFont
	}
	if !opts.Background.isSet() {
		if opts.Dark {
			opts.Background = Hex(0x1e1e1e)
		} else {
			opts.Background = Hex(0xffffff)
		}
	}
	if !opts.TextColor.isSet() {
		if opts.Dark {
			opts.TextColor = Hex(0xe6e6e6)
		} else {
			opts.TextColor = Hex(0x1a1a1a)
		}
	}

	w := &Window{
		opts:      opts,
		states:    map[uint64]*elState{},
		hoverIDs:  map[uint64]bool{},
		textures:  map[uint64]*texEntry{},
		surfaces:  map[uint64]*surfaceEntry{},
		natives:   map[embed.Native]bool{},
		iconTiles: map[iconKey]render.Tile{},
		byID:      map[uint64]*El{},
		prevByID:  map[uint64]*El{},
		locals:    map[string]*localEntry{},
		dirty:     true,
	}
	return w, nil
}

// onResize handles a new client size in device pixels.
func (w *Window) onResize(width, height int) {
	if width == w.width && height == w.height {
		return
	}
	w.width, w.height = width, height
	if w.r != nil {
		w.resizeRenderer()
	}
	w.markDirty(frameprof.CauseResize)
	// Draw now so a live resize shows the new size immediately.
	w.renderFrame()
}

// onFocusChange handles the window gaining or losing keyboard focus.
func (w *Window) onFocusChange(focused bool) {
	if w.opts.OnFocus != nil {
		w.opts.OnFocus(focused)
		w.Update()
	}
	w.osFocus = focused
	if focused {
		w.resetBlink()
	} else {
		if w.acc != nil {
			w.acc.focus = 0
		}
		w.commitComposition()
		w.cancelPress()
		w.stopAutoScroll()
	}
	w.invalidate()
}

// onTimer handles the window timer id set with setTimer.
func (w *Window) onTimer(id uintptr) {
	switch id {
	case timerCaret:
		w.caretOn = !w.caretOn || w.clock != nil
		w.invalidate()
	case timerTip:
		w.killTimer(timerTip)
		w.showTooltip()
	case timerAnim:
		w.killTimer(timerAnim)
		w.invalidate()
	case timerFrame:
		w.killTimer(timerFrame)
		switch {
		case w.needPaint:
			w.renderFrame()
		case w.hl != nil && w.hl.copyPending && w.r != nil:
			w.publishFrame()
		}
	case timerTouch:
		w.killTimer(timerTouch)
		w.onTouchLongPress()
	case timerRecover:
		w.killTimer(timerRecover)
		if w.r == nil {
			w.recreateRenderer()
		} else {
			w.invalidate() // renderFrame retries a failed resize
		}
	}
}

// release frees what the closed window holds and lets its trees and
// element state go.
func (w *Window) release() {
	w.tickers = nil
	liveMu.Lock()
	delete(live, w)
	liveMu.Unlock()
	w.releaseTextures()
	w.textures = nil
	w.releaseShared()
	for _, se := range w.surfaces {
		se.target.Release()
	}
	w.surfaces = nil
	w.releasePathTextures()
	if w.ts != nil {
		w.ts.Release()
		w.ts = nil
	}
	if w.r != nil {
		w.r.Release()
		w.r = nil
	}
	w.closed.Store(true)
	// The app or a Live may still hold w; let the trees and element state go.
	// Maps are emptied rather than dropped, as methods called on the closed
	// window may still write to them.
	w.root, w.sel, w.press = nil, nil, nil
	w.layers, w.hits, w.focusables, w.hoverPath, w.hoverScratch = nil, nil, nil, nil, nil
	w.ghosts, w.flights = nil, nil
	w.scene = render.Scene{}
	clear(w.states)
	clear(w.byID)
	clear(w.prevByID)
	clear(w.memos)
	clear(w.locals)
	clear(w.matched)
	clear(w.hoverIDs)
	clear(w.ghostIn)
	clear(w.matchClaims)
	clear(w.iconTiles)
}
