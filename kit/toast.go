package kit

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/motion"
)

// ToastKind picks the icon and color of a toast.
type ToastKind uint8

const (
	ToastInfo ToastKind = iota
	ToastSuccess
	ToastWarning
	ToastError
)

// ToastLoading shows a spinner and stays until the toast is updated to
// another kind or dismissed.
const ToastLoading ToastKind = ToastError + 1

// ToastID identifies a toast for Update and Dismiss.
type ToastID uint64

type toast struct {
	id          ToastID
	kind        ToastKind
	title, body string
	timed       bool
	deadline    time.Time     // when the toast leaves, while the timer runs
	remaining   time.Duration // the time left, while the timer is held
	gen         int           // invalidates timers armed before a pause
}

// Toaster shows short notifications in the bottom-right corner. Its methods
// may be called from any goroutine. Draw it with View. While the pointer is
// over the toasts their timers hold, and they start again with the time that
// was left when it leaves.
type Toaster struct {
	// Duration is how long a toast stays; 4 seconds when zero, forever when negative.
	Duration time.Duration
	items    []toast
	seq      atomic.Uint64
	paused   bool
	heights  map[ToastID]float32 // natural heights, measured as toasts are laid out
}

// maxToasts is how many toasts show at once; older ones leave first.
const maxToasts = 5

// Show adds a toast and returns its id. body may be empty.
func (ts *Toaster) Show(kind ToastKind, title, body string) ToastID {
	id := ToastID(ts.seq.Add(1))
	fairy.Go(func() { ts.put(id, kind, title, body) })
	return id
}

// Loading shows a toast with a spinner that stays until Update gives it
// another kind or it is dismissed.
func (ts *Toaster) Loading(title, body string) ToastID { return ts.Show(ToastLoading, title, body) }

// Update changes the kind, title and body of the toast id. Its timer starts
// over for the new kind, so a loading toast updated to success leaves after
// Duration. A toast that already left is shown again.
func (ts *Toaster) Update(id ToastID, kind ToastKind, title, body string) {
	fairy.Go(func() { ts.put(id, kind, title, body) })
}

// Dismiss removes the toast id.
func (ts *Toaster) Dismiss(id ToastID) { fairy.Go(func() { ts.dismiss(id) }) }

// PromiseMessages are the texts Promise shows.
type PromiseMessages struct {
	Loading string // title while the work runs
	Success string // title when it succeeds
	// Error returns the title when the work fails; nil uses the error text.
	// The error text is the body either way when Error is set. It runs on
	// the UI thread.
	Error func(error) string
}

// Promise shows a loading toast, runs work on a new goroutine and turns the
// toast into a success or error toast when work returns, as sonner's
// toast.promise does.
func (ts *Toaster) Promise(m PromiseMessages, work func() error) ToastID {
	id := ts.Loading(m.Loading, "")
	go func() {
		err := work()
		fairy.Go(func() {
			switch {
			case err == nil:
				ts.put(id, ToastSuccess, m.Success, "")
			case m.Error != nil:
				ts.put(id, ToastError, m.Error(err), err.Error())
			default:
				ts.put(id, ToastError, err.Error(), "")
			}
		})
	}()
	return id
}

// put adds the toast id or replaces its content, on the UI thread.
func (ts *Toaster) put(id ToastID, kind ToastKind, title, body string) {
	if body != "" {
		fairy.Announce(title + ". " + body)
	} else {
		fairy.Announce(title)
	}
	it := ts.find(id)
	if it == nil {
		ts.items = append(ts.items, toast{id: id})
		if len(ts.items) > maxToasts {
			for _, old := range ts.items[:len(ts.items)-maxToasts] {
				delete(ts.heights, old.id)
			}
			ts.items = ts.items[len(ts.items)-maxToasts:]
		}
		it = &ts.items[len(ts.items)-1]
	}
	it.kind, it.title, it.body = kind, title, body
	it.gen++ // drop a timer armed for the previous content
	d := ts.Duration
	if d == 0 {
		d = 4 * time.Second
	}
	it.timed = d > 0 && kind != ToastLoading
	if it.timed {
		it.remaining = d
		if !ts.paused {
			ts.arm(it)
		}
	}
}

func (ts *Toaster) find(id ToastID) *toast {
	for i := range ts.items {
		if ts.items[i].id == id {
			return &ts.items[i]
		}
	}
	return nil
}

// arm starts the toast's timer with the time it has left.
func (ts *Toaster) arm(it *toast) {
	it.gen++
	it.deadline = time.Now().Add(it.remaining)
	id, gen := it.id, it.gen
	time.AfterFunc(it.remaining, func() {
		fairy.Go(func() {
			if it := ts.find(id); it != nil && it.gen == gen && !ts.paused {
				ts.dismiss(id)
			}
		})
	})
}

// hold stops the timers while the pointer is over the toasts and restarts
// them with the time that was left when it leaves.
func (ts *Toaster) hold(on bool) {
	if on == ts.paused {
		return
	}
	ts.paused = on
	now := time.Now()
	for i := range ts.items {
		it := &ts.items[i]
		if !it.timed {
			continue
		}
		if on {
			it.gen++
			it.remaining = max(it.deadline.Sub(now), 0)
		} else {
			ts.arm(it)
		}
	}
}

// Info shows an informational toast.
func (ts *Toaster) Info(title string) ToastID { return ts.Show(ToastInfo, title, "") }

// Success shows a success toast.
func (ts *Toaster) Success(title string) ToastID { return ts.Show(ToastSuccess, title, "") }

// Error shows an error toast.
func (ts *Toaster) Error(title, body string) ToastID { return ts.Show(ToastError, title, body) }

func (ts *Toaster) dismiss(id ToastID) {
	for i, it := range ts.items {
		if it.id == id {
			ts.items = append(ts.items[:i:i], ts.items[i+1:]...)
			delete(ts.heights, id)
			return
		}
	}
}

// toastColors are a toast's background, border and text, as sonner's rich colors.
type toastColors struct{ bg, border, text fairy.Color }

func richColors(kind ToastKind) toastColors {
	t := &current
	pick := func(light, dark [3]uint32) toastColors {
		c := light
		if t.Dark {
			c = dark
		}
		return toastColors{fairy.Hex(c[0]), fairy.Hex(c[1]), fairy.Hex(c[2])}
	}
	switch kind {
	case ToastSuccess:
		return pick([3]uint32{0xecfdf3, 0xbffcd9, 0x008a2e}, [3]uint32{0x001f0f, 0x003d1c, 0x59f3a6})
	case ToastInfo:
		return pick([3]uint32{0xf0f8ff, 0xdde7fd, 0x0973dc}, [3]uint32{0x000d1f, 0x19233e, 0x5896f3})
	case ToastWarning:
		return pick([3]uint32{0xfffcf0, 0xfbeeb1, 0xdc7609}, [3]uint32{0x1d1f00, 0x2e2e00, 0xf3cf58})
	case ToastError:
		return pick([3]uint32{0xfff0f0, 0xffe0e1, 0xe60000}, [3]uint32{0x2d0607, 0x4d0408, 0xff9ea1})
	}
	return toastColors{t.Popover, t.Border, t.PopoverForeground}
}

const (
	toastGap     = 14 // between expanded toasts, and how far each one behind peeks out
	toastVisible = 3  // toasts shown at once; older ones wait hidden behind
)

// toastMotion is sonner's: 400ms of the CSS ease curve.
var toastMotion = motion.Bezier(0.4, 0.25, 0.1, 0.25, 1)

// View returns the toast layer. Pass it to fairy.Cx.Overlay on every build,
// after any conditional overlays:
//
//	cx.Overlay(toaster.View(), nil)
//
// Toasts stack in the bottom-right corner as sonner's do: the newest in
// front, up to two more peeking out behind it, all spread out while the
// pointer is over them.
func (ts *Toaster) View() *fairy.El {
	if ts.heights == nil {
		ts.heights = map[ToastID]float32{}
	}
	n := len(ts.items)
	var frontH float32
	if n > 0 {
		frontH = ts.heights[ts.items[n-1].id]
	}
	expanded := ts.paused
	stack := fairy.Box().Right(24).Bottom(24).W(356).OnHover(ts.hold)
	var total float32
	for i := range n {
		it := ts.items[i]
		depth := n - 1 - i // 0 for the newest, in front
		h := ts.heights[it.id]
		var offset float32
		for j := i + 1; j < n; j++ {
			offset += ts.heights[ts.items[j].id] + toastGap
		}
		if depth < toastVisible {
			total = max(total, offset+h)
		}
		stack.Child(ts.toastView(it, depth, h, frontH, offset, expanded))
	}
	if !expanded && n > 0 {
		total = frontH + toastGap*float32(min(n, toastVisible)-1)
	}
	// The stack covers the gaps between spread toasts, so the pointer
	// crossing them keeps the stack spread.
	stack.H(total)
	return fairy.Box(stack).Key("kit.toasts").PassThrough()
}

func (ts *Toaster) toastView(it toast, depth int, h, frontH, offset float32, expanded bool) *fairy.El {
	t := &current
	c := richColors(it.kind)
	var mark *fairy.El
	switch it.kind {
	case ToastLoading:
		mark = fairy.Spinner().Size(16, 16)
	case ToastSuccess:
		mark = fairy.Icon(lucide.CircleCheck).Size(16, 16)
	case ToastWarning:
		mark = fairy.Icon(lucide.TriangleAlert).Size(16, 16)
	case ToastError:
		mark = fairy.Icon(lucide.OctagonX).Size(16, 16)
	default:
		mark = fairy.Icon(lucide.Info).Size(16, 16)
	}
	text := fairy.Col(fairy.Text(it.title).Medium().LineHeight(19.5)).Gap(2).Grow(1).MinW(0)
	if it.body != "" {
		text.Child(fairy.Text(it.body).LineHeight(18.2))
	}
	id := it.id
	content := fairy.Row(mark.MarginEdges(0, 4, 0, -3), text).Gap(6).Align(fairy.Center).Pad(16).Shrink(0).
		OnLayout(func(_, h float32) { ts.heights[id] = h }).Animate(toastMotion)
	front := depth == 0
	toastEl := fairy.Box(content).Key(fmt.Sprint(id)).Left(0).Right(0).Radius(t.RadiusLg())
	switch {
	case front && !expanded && len(ts.items) > 1:
		// The collapsed toasts behind would show through a translucent front
		// one. It keeps the blur under its opaque fill, so expanding the stack
		// only fades the fill.
		t.surface(toastEl, c.bg, 1)
	case it.kind == ToastSuccess || it.kind == ToastInfo || it.kind == ToastWarning || it.kind == ToastError:
		t.surface(toastEl, c.bg, max(t.SurfaceAlpha, 0.8))
	default:
		t.Surface(toastEl, c.bg)
	}
	toastEl.Border(1, c.border).Color(c.text).FontSize(13).
		Shadow(0, 4, 12, 0, fairy.RGBA(0, 0, 0, 0.1)).
		AnimateLayout(toastMotion).ScaleMotion(toastMotion).Animate(toastMotion).
		Enter(fairy.Transition{Y: max(h, 56), Motion: toastMotion})
	if front {
		toastEl.Exit(fairy.Transition{Y: max(h, 56), Motion: toastMotion})
	} else {
		toastEl.Exit(fairy.Transition{Y: max(h, 56) * 0.4, Motion: toastMotion})
	}
	switch {
	case depth >= toastVisible:
		toastEl.Bottom(0).Opacity(0).PassThrough()
		if frontH > 0 {
			toastEl.H(frontH)
		}
		content.Opacity(0)
	case expanded || front:
		toastEl.Bottom(offset)
		if h > 0 {
			toastEl.H(h)
		}
	default:
		// Collapsed behind the front toast: lifted, smaller, and as tall as
		// it, with the content hidden.
		toastEl.Bottom(toastGap * float32(depth)).Scale(1 - 0.05*float32(depth)).Clip()
		if frontH > 0 {
			toastEl.H(frontH)
		}
		content.Opacity(0)
	}
	if it.kind != ToastLoading {
		closeFill, closeBorder := c.bg, c.border
		x := fairy.Box(fairy.Icon(lucide.X).Size(12, 12)).Size(20, 20).Left(-7).Top(-7).Center().Radius(10).
			Fill(closeFill).Border(1, closeBorder).Animate(motion.Fast).Cursor(fairy.CursorPointer).
			Role(fairy.RoleButton).Label(Messages.Close).Tooltip(Messages.Close).
			OnClick(func() { ts.dismiss(id) })
		if !t.Dark && closeFill == t.Popover {
			x.HoverFill(fairy.Hex(0xf8f8f8)).HoverBorder(fairy.Hex(0xe8e8e8))
		}
		if !front && !expanded {
			x.Opacity(0).PassThrough()
		}
		toastEl.Child(x)
	}
	return toastEl
}
