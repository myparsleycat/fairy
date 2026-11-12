package tray

import "errors"

// errNoBackend is returned until macOS has menu bar icons.
var errNoBackend = errors.New("tray: not implemented on macOS yet")

// Tray is an icon in the notification area. While a tray icon exists,
// fairy.Main keeps running after the last window closes.
type Tray struct{ opts Options }

// New adds an icon to the notification area. Call it on the UI thread.
func New(opts Options) (*Tray, error) { return nil, errNoBackend }

func (t *Tray) SetTooltip(s string)                                 {}
func (t *Tray) SetIcon(png []byte)                                  {}
func (t *Tray) Notify(title, message string, kind NoticeKind)       {}
func (t *Tray) NotifyID(id, title, message string, kind NoticeKind) {}
func (t *Tray) Remove()                                             {}
