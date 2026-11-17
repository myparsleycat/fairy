package tray

import (
	"errors"
	"fmt"
	"hash/fnv"
	"syscall"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/internal/apphost"
	"github.com/myparsleycat/fairy/internal/win"
)

// Tray is an icon in the notification area. While a tray icon exists,
// fairy.Main keeps running after the last window closes.
type Tray struct {
	id         uint32
	opts       Options
	icon       win.HANDLE
	balloonTag string // NotifyID id of the balloon shown last
}

var (
	trays   = map[uint32]*Tray{}
	traySeq uint32
)

func init() {
	apphost.Handle(apphost.MsgTray, func(wp, lp uintptr) uintptr {
		if t := trays[uint32(win.HIWORD(lp))]; t != nil {
			t.notify(uint32(win.LOWORD(lp)), wp)
		}
		return 0
	})
	// Explorer restarted: its new taskbar has none of the icons.
	apphost.Handle(win.RegisterWindowMessage("TaskbarCreated"), func(_, _ uintptr) uintptr {
		for _, t := range trays {
			t.add()
		}
		return 0
	})
	apphost.OnShutdown(func() {
		for _, t := range trays {
			t.Remove()
		}
	})
}

// New adds an icon to the notification area. Call it on the UI thread.
func New(opts Options) (*Tray, error) {
	if !apphost.OnUIThread() {
		return nil, errors.New("tray: New must run on the UI thread (inside fairy.Main, a handler or fairy.Do)")
	}
	traySeq++
	t := &Tray{id: traySeq, opts: opts}
	t.icon = trayIcon(opts.Icon)
	if !t.add() {
		t.destroyIcon()
		return nil, errors.New("tray: Shell_NotifyIcon failed")
	}
	trays[t.id] = t
	apphost.Hold()
	return t, nil
}

func trayIcon(png []byte) win.HANDLE {
	size := win.GetSystemMetricsForDpi(win.SM_CXSMICON, win.GetDpiForSystem())
	return win.CreateIconFromPNG(png, size)
}

func (t *Tray) data(flags uint32) win.NOTIFYICONDATAW {
	d := win.NOTIFYICONDATAW{Wnd: apphost.HWND(), ID: t.id, Flags: flags}
	if flags&win.NIF_MESSAGE != 0 {
		d.CallbackMessage = apphost.MsgTray
	}
	if flags&win.NIF_ICON != 0 {
		d.Icon = t.icon
	}
	if flags&win.NIF_TIP != 0 {
		copyUTF16(d.Tip[:], t.opts.Tooltip)
	}
	return d
}

func (t *Tray) add() bool {
	d := t.data(win.NIF_MESSAGE | win.NIF_ICON | win.NIF_TIP | win.NIF_SHOWTIP)
	if !win.ShellNotifyIcon(win.NIM_ADD, &d) {
		return false
	}
	d.Version = win.NOTIFYICON_VERSION_4
	win.ShellNotifyIcon(win.NIM_SETVERSION, &d)
	return true
}

func (t *Tray) notify(event uint32, wp uintptr) {
	switch event {
	case win.NIN_SELECT, win.NIN_KEYSELECT:
		if t.opts.OnClick != nil {
			t.opts.OnClick()
			apphost.UpdateWindows()
		}
	case win.NIN_BALLOONUSERCLICK:
		if t.opts.OnNotificationClick != nil {
			t.opts.OnNotificationClick()
			apphost.UpdateWindows()
		}
	case win.WM_CONTEXTMENU:
		if t.opts.Menu == nil {
			return
		}
		x, y := win.PointFromLParam(wp)
		// The owner must be in the foreground, and must receive a message
		// afterwards, or the menu does not close when clicking elsewhere.
		hwnd := apphost.HWND()
		win.SetForegroundWindow(hwnd)
		fairy.ShowMenu(int(x), int(y), t.opts.Menu())
		win.PostMessage(hwnd, win.WM_NULL, 0, 0)
	}
}

// SetTooltip changes the text shown on hover.
func (t *Tray) SetTooltip(s string) {
	apphost.Do(func() {
		t.opts.Tooltip = s
		d := t.data(win.NIF_TIP | win.NIF_SHOWTIP)
		win.ShellNotifyIcon(win.NIM_MODIFY, &d)
	})
}

// SetIcon replaces the icon with a PNG.
func (t *Tray) SetIcon(png []byte) {
	apphost.Do(func() {
		old := t.icon
		t.icon = trayIcon(png)
		d := t.data(win.NIF_ICON)
		win.ShellNotifyIcon(win.NIM_MODIFY, &d)
		if old != 0 {
			win.DestroyIcon(old)
		}
	})
}

// Notify shows a notification from the tray icon. Windows 10 and later show
// it as a toast in the notification center.
func (t *Tray) Notify(title, message string, kind NoticeKind) {
	apphost.Do(func() { t.showBalloon("", title, message, kind) })
}

func (t *Tray) showBalloon(tag, title, message string, kind NoticeKind) {
	d := t.data(win.NIF_INFO)
	copyUTF16(d.InfoTitle[:], title)
	copyUTF16(d.Info[:], message)
	d.InfoFlags = [...]uint32{win.NIIF_INFO, win.NIIF_WARNING, win.NIIF_ERROR}[kind]
	win.ShellNotifyIcon(win.NIM_MODIFY, &d)
	t.balloonTag = tag
}

// NotifyID shows a notification like Notify that replaces the notification
// shown earlier with the same id, both on screen and in the notification
// center, so repeated status messages (an update check) do not pile up.
//
// It shows a toast under Options.AppID (see there). Without one, or when the
// toast fails or toasts are turned off for the app, it falls back to the tray
// balloon of Notify: that hides a balloon of the same id still on screen, but
// Windows keeps earlier ones in the notification center. The kind picks the
// balloon icon; toasts show the app icon.
func (t *Tray) NotifyID(id, title, message string, kind NoticeKind) {
	apphost.Do(func() {
		appID := t.opts.AppID
		if appID == "" {
			if id := win.ProcessAppID(); id != "" && win.AppIDRegistered(id) {
				appID = id
			}
		}
		if appID != "" && win.ShowToast(appID, toastTag(id), "fairy.tray", title, message) == nil {
			return
		}
		if t.balloonTag == id {
			hide := t.data(win.NIF_INFO)
			win.ShellNotifyIcon(win.NIM_MODIFY, &hide)
		}
		t.showBalloon(id, title, message, kind)
	})
}

// toastTag fits id into the 64 characters a toast tag allows.
func toastTag(id string) string {
	if len([]rune(id)) <= 64 {
		return id
	}
	h := fnv.New64a()
	h.Write([]byte(id))
	return fmt.Sprintf("%x", h.Sum64())
}

// Remove deletes the icon. fairy.Main ends if no window is left either.
func (t *Tray) Remove() {
	apphost.Do(func() {
		if trays[t.id] == nil {
			return
		}
		d := t.data(0)
		win.ShellNotifyIcon(win.NIM_DELETE, &d)
		t.destroyIcon()
		delete(trays, t.id)
		apphost.Release()
	})
}

func (t *Tray) destroyIcon() {
	if t.icon != 0 {
		win.DestroyIcon(t.icon)
		t.icon = 0
	}
}

// copyUTF16 writes s into a fixed buffer, truncated and NUL-terminated.
func copyUTF16(dst []uint16, s string) {
	u, _ := syscall.UTF16FromString(s)
	if len(u) > len(dst) {
		u = u[:len(dst)]
		u[len(u)-1] = 0
	}
	copy(dst, u)
}
