// Package clipboard reads and writes the Windows clipboard. Writing needs
// fairy.Main to be running.
package clipboard

import (
	"image"
	"time"

	"github.com/myparsleycat/fairy/internal/apphost"
	clip "github.com/myparsleycat/fairy/internal/clipboard"
)

// Text returns the clipboard's text.
func Text() string {
	s, _ := clip.Text(apphost.HWND())
	return s
}

// SetText puts s on the clipboard and reports whether it got there; another
// program holding the clipboard open makes it fail.
func SetText(s string) bool { return clip.SetText(apphost.HWND(), s) }

// SetRich puts text on the clipboard together with an HTML version of it,
// which word processors, mail and chat programs paste with its formatting.
// html is a fragment, such as "<p><b>Bold</b> text</p>".
func SetRich(text, html string) bool { return clip.SetRich(apphost.HWND(), text, html) }

// HTML returns the HTML fragment on the clipboard, as copied from a browser
// or word processor. It is not sanitized.
func HTML() (string, bool) { return clip.HTML(apphost.HWND()) }

// SetSecret puts a secret such as a password or API key on the clipboard,
// marked to be left out of clipboard history, cloud clipboard and clipboard
// monitors. After clearAfter, unless 0, it empties the clipboard if nothing
// else was copied since.
func SetSecret(text string, clearAfter time.Duration) bool {
	return clip.SetSecret(apphost.HWND(), text, clearAfter, func(f func()) { apphost.Go(f) })
}

// Image returns the image on the clipboard. It prefers PNG data, which keeps
// transparency, and falls back to a device-independent bitmap.
func Image() (image.Image, bool) { return clip.Image(apphost.HWND()) }

// SetImage replaces the clipboard with img as PNG and as a bitmap, so that
// programs that read either format can paste it.
func SetImage(img image.Image) bool { return clip.SetImage(apphost.HWND(), img) }
