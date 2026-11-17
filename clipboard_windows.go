package fairy

import "github.com/myparsleycat/fairy/internal/clipboard"

// The clipboard functions of a window use fakeClip in place of the system
// clipboard's text when a test sets it.

// clipboardText returns the clipboard's text, and false when the system
// clipboard holds none or could not be opened.
func (w *Window) clipboardText() (string, bool) {
	if w.fakeClip != nil {
		return *w.fakeClip, true
	}
	return clipboard.Text(w.hwnd)
}

func (w *Window) clipboardHasText() bool {
	if w.fakeClip != nil {
		return *w.fakeClip != ""
	}
	return clipboard.HasText()
}

func (w *Window) setClipboardText(s string) bool {
	if w.fakeClip != nil {
		*w.fakeClip = s
		return true
	}
	return clipboard.SetText(w.hwnd, s)
}

func (w *Window) setClipboardRich(text, html string) bool {
	if w.fakeClip != nil {
		*w.fakeClip = text
		return true
	}
	return clipboard.SetRich(w.hwnd, text, html)
}
