// Package clipboard reads and writes the formats fairy puts on the Windows
// clipboard: text, HTML, secrets kept out of clipboard history, and images.
// owner is the window that opens the clipboard; writes need one.
package clipboard

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"time"
	"unicode/utf16"

	"github.com/myparsleycat/fairy/internal/clipfmt"
	"github.com/myparsleycat/fairy/internal/win"
)

var (
	cfPNG        = win.RegisterClipboardFormat("PNG")
	cfHTMLFormat = win.RegisterClipboardFormat("HTML Format")
	// Formats that keep a copy out of clipboard history, cloud clipboard and
	// clipboard monitors.
	cfExcludeMonitor = win.RegisterClipboardFormat("ExcludeClipboardContentFromMonitorProcessing")
	cfHistory        = win.RegisterClipboardFormat("CanIncludeInClipboardHistory")
	cfCloud          = win.RegisterClipboardFormat("CanUploadToCloudClipboard")
)

func utf16z(s string) []byte {
	u := utf16.Encode([]rune(s + "\x00"))
	b := make([]byte, len(u)*2)
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[i*2:], c)
	}
	return b
}

// Text returns the clipboard's text, and false when it holds none or could
// not be opened.
func Text(owner win.HWND) (string, bool) { return win.ClipboardText(owner) }

// HasText reports whether the clipboard holds text.
func HasText() bool { return win.ClipboardHasText() }

// SetText replaces the clipboard with s.
func SetText(owner win.HWND, s string) bool { return win.SetClipboardText(owner, s) }

// SetRich replaces the clipboard with text and an HTML fragment of it.
func SetRich(owner win.HWND, text, html string) bool {
	return win.SetClipboardData(owner, map[uint32][]byte{
		win.CF_UNICODETEXT: utf16z(text),
		cfHTMLFormat:       clipfmt.WrapHTML(html),
	})
}

// HTML returns the HTML fragment on the clipboard.
func HTML(owner win.HWND) (string, bool) {
	data, ok := win.ClipboardData(owner, cfHTMLFormat)
	if !ok {
		return "", false
	}
	return clipfmt.ExtractHTML(data)
}

// SetSecret replaces the clipboard with text, marked to be left out of
// clipboard history, cloud clipboard and clipboard monitors. After
// clearAfter, unless 0, run empties the clipboard if nothing else was copied
// since; run is given the function to call on the UI thread.
func SetSecret(owner win.HWND, text string, clearAfter time.Duration, run func(func())) bool {
	zero := []byte{0, 0, 0, 0}
	ok := win.SetClipboardData(owner, map[uint32][]byte{
		win.CF_UNICODETEXT: utf16z(text),
		cfExcludeMonitor:   zero,
		cfHistory:          zero,
		cfCloud:            zero,
	})
	if ok && clearAfter > 0 {
		seq := win.ClipboardSequence()
		time.AfterFunc(clearAfter, func() {
			run(func() {
				if win.ClipboardSequence() == seq {
					win.SetClipboardData(0, nil)
				}
			})
		})
	}
	return ok
}

// Image returns the image on the clipboard. It prefers PNG data, which keeps
// transparency, and falls back to a device-independent bitmap.
func Image(owner win.HWND) (image.Image, bool) {
	if data, ok := win.ClipboardData(owner, cfPNG); ok {
		if img, err := png.Decode(bytes.NewReader(data)); err == nil {
			return img, true
		}
	}
	for _, format := range []uint32{win.CF_DIBV5, win.CF_DIB} {
		if data, ok := win.ClipboardData(owner, format); ok {
			if img, err := clipfmt.DecodeDIB(data); err == nil {
				return img, true
			}
		}
	}
	return nil, false
}

// SetImage replaces the clipboard with img as PNG and as a bitmap.
func SetImage(owner win.HWND, img image.Image) bool {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return false
	}
	return win.SetClipboardData(owner, map[uint32][]byte{cfPNG: buf.Bytes(), win.CF_DIB: clipfmt.EncodeDIB(img)})
}
