package tray

import (
	"errors"
	"sync"

	"github.com/ebitengine/purego/objc"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/internal/apphost"
	"github.com/myparsleycat/fairy/internal/darwin"
)

// Tray is an icon in the macOS menu bar, an NSStatusItem. While a tray icon
// exists, fairy.Main keeps running after the last window closes.
type Tray struct {
	opts   Options
	item   darwin.ID // NSStatusItem
	button darwin.ID // its NSStatusBarButton
}

// trays finds the Tray of a status bar button's action.
var trays = map[darwin.ID]*Tray{}

// iconHeight is the height of the icon in points: that of the menu bar's
// own icons.
const iconHeight = 18

func init() {
	apphost.OnShutdown(func() {
		for _, t := range trays {
			t.Remove()
		}
	})
}

var targetClass = sync.OnceValue(func() darwin.Class {
	c, err := objc.RegisterClass("FairyTrayTarget", darwin.Cls("NSObject"), nil, nil, []objc.MethodDef{
		{
			Cmd: darwin.Sel("fairyTrayClick:"),
			Fn: func(_ darwin.ID, _ darwin.SEL, sender darwin.ID) {
				if t := trays[sender]; t != nil {
					t.clicked()
				}
			},
		},
	})
	if err != nil {
		panic(err)
	}
	return c
})

// target receives the actions of every status bar button.
var target = sync.OnceValue(func() darwin.ID {
	targetClass()
	return darwin.New("FairyTrayTarget")
})

// NSEvent types and modifier flags.
const (
	eventLeftMouseUp  = 2
	eventRightMouseUp = 4
	maskLeftMouseUp   = 1 << eventLeftMouseUp
	maskRightMouseUp  = 1 << eventRightMouseUp
	modControl        = 1 << 18
)

// New adds an icon to the menu bar. Call it on the UI thread.
func New(opts Options) (*Tray, error) {
	if !apphost.OnUIThread() {
		return nil, errors.New("tray: New must run on the UI thread (inside fairy.Main, a handler or fairy.Do)")
	}
	t := &Tray{opts: opts}
	var err error
	darwin.WithPool(func() {
		bar := darwin.SendClass("NSStatusBar", "systemStatusBar")
		item := darwin.Send(bar, "statusItemWithLength:", float64(-1)) // NSVariableStatusItemLength
		if item == 0 {
			err = errors.New("tray: statusItemWithLength failed")
			return
		}
		t.item = darwin.Retain(item)
		t.button = darwin.Send(item, "button")
		darwin.Send(t.button, "setTarget:", target())
		darwin.Send(t.button, "setAction:", darwin.Sel("fairyTrayClick:"))
		darwin.Send(t.button, "sendActionOn:", uint64(maskLeftMouseUp|maskRightMouseUp))
		t.setIcon(opts.Icon)
		t.setTooltip(opts.Tooltip)
	})
	if err != nil {
		return nil, err
	}
	trays[t.button] = t
	apphost.Hold()
	return t, nil
}

// clicked runs OnClick for a left click, or shows the menu for a right or
// Control click.
func (t *Tray) clicked() {
	ev := darwin.Send(darwin.SendClass("NSApplication", "sharedApplication"), "currentEvent")
	menu := false
	if ev != 0 {
		typ := darwin.Get[uint64](ev, "type")
		flags := darwin.Get[uint64](ev, "modifierFlags")
		menu = typ == eventRightMouseUp || flags&modControl != 0
	}
	if !menu {
		if t.opts.OnClick != nil {
			t.opts.OnClick()
			apphost.UpdateWindows()
		}
		return
	}
	if t.opts.Menu == nil {
		return
	}
	items := t.opts.Menu()
	if len(items) == 0 {
		return
	}
	// Open the menu below the icon, as the menu bar's own menus do, and keep
	// the icon highlighted while it is open.
	x, y, ok := t.menuPoint()
	if !ok {
		return
	}
	darwin.Send(t.button, "highlight:", true)
	defer func() {
		if trays[t.button] == t {
			darwin.Send(t.button, "highlight:", false)
		}
	}()
	fairy.ShowMenu(x, y, items)
}

// menuPoint is the bottom left corner of the icon in the screen points
// fairy.ShowMenu takes.
func (t *Tray) menuPoint() (x, y int, ok bool) {
	win := darwin.Send(t.button, "window")
	screen := darwin.SendClass("NSScreen", "mainScreen")
	if win == 0 || screen == 0 {
		return 0, 0, false
	}
	b := darwin.Get[darwin.CGRect](t.button, "bounds")
	r := darwin.Get[darwin.CGRect](t.button, "convertRect:toView:", b, darwin.ID(0))
	r = darwin.Get[darwin.CGRect](win, "convertRectToScreen:", r)
	h := darwin.Get[darwin.CGRect](screen, "frame").Size.H
	return int(r.Origin.X), int(h - r.Origin.Y), true
}

func (t *Tray) setTooltip(s string) {
	darwin.Send(t.button, "setToolTip:", darwin.String(s))
}

func (t *Tray) setIcon(png []byte) {
	var img darwin.ID
	if len(png) > 0 {
		img = darwin.Send(darwin.SendClass("NSImage", "alloc"), "initWithData:", darwin.Data(png))
	}
	if img != 0 {
		s := darwin.Get[darwin.CGSize](img, "size")
		if s.H > 0 {
			darwin.Send(img, "setSize:", darwin.CGSize{W: s.W * iconHeight / s.H, H: iconHeight})
		}
		darwin.Send(img, "setTemplate:", false)
	}
	darwin.Send(t.button, "setImage:", img)
	darwin.Release(img)
}

// SetTooltip changes the text shown on hover. It may be called from any
// goroutine.
func (t *Tray) SetTooltip(s string) {
	apphost.Do(func() {
		t.opts.Tooltip = s
		if trays[t.button] == t {
			darwin.WithPool(func() { t.setTooltip(s) })
		}
	})
}

// SetIcon replaces the icon with a PNG. It may be called from any goroutine.
func (t *Tray) SetIcon(png []byte) {
	apphost.Do(func() {
		t.opts.Icon = png
		if trays[t.button] == t {
			darwin.WithPool(func() { t.setIcon(png) })
		}
	})
}

// Notify would show a notification from the tray icon. On macOS it does
// nothing: notifications go through UNUserNotificationCenter, which only
// serves an app bundle with a bundle identifier and the user's permission.
func (t *Tray) Notify(title, message string, kind NoticeKind) {}

// NotifyID would show a notification like Notify that replaces the one shown
// earlier with the same id. Like Notify, it does nothing on macOS.
func (t *Tray) NotifyID(id, title, message string, kind NoticeKind) {}

// Remove deletes the icon. fairy.Main ends if no window is left either.
func (t *Tray) Remove() {
	apphost.Do(func() {
		if trays[t.button] != t {
			return
		}
		delete(trays, t.button)
		darwin.WithPool(func() {
			darwin.Send(darwin.SendClass("NSStatusBar", "systemStatusBar"), "removeStatusItem:", t.item)
		})
		darwin.Release(t.item)
		t.item, t.button = 0, 0
		apphost.Release()
	})
}
