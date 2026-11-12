//go:build windows

package apphost

import "github.com/myparsleycat/fairy/internal/win"

// MsgTray is the callback message of tray icons.
const MsgTray = win.WM_APP + 11

// HWND returns the app window, or 0 outside Main.
var HWND = func() win.HWND { return 0 }
