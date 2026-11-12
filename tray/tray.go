// Package tray shows icons in the notification area: the Windows
// notification area, or the macOS menu bar.
package tray

import "github.com/myparsleycat/fairy"

// Options configures a notification area icon.
type Options struct {
	Icon    []byte // PNG
	Tooltip string
	// OnClick runs on a left click or keyboard selection.
	OnClick func()
	// Menu builds the right-click menu each time it opens.
	Menu func() []fairy.MenuItem
	// OnNotificationClick runs when the user clicks a notification shown as a
	// tray balloon. A toast shown under an AppID activates the app instead,
	// which launches it again.
	OnNotificationClick func()
	// AppID is the AppUserModelID NotifyID shows toasts under. Set it only
	// to an ID that is registered, by a Start menu shortcut carrying it (as
	// an installer makes) or an HKCU\Software\Classes\AppUserModelId\<ID>
	// key: Windows accepts toasts for unregistered IDs and silently drops
	// them. When empty, NotifyID uses the process's explicit AppUserModelID
	// if that has a registry key.
	AppID string
}

// NoticeKind selects the icon of a tray notification.
type NoticeKind uint8

const (
	NoticeInfo NoticeKind = iota
	NoticeWarning
	NoticeError
)
