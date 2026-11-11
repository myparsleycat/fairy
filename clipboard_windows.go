package fairy

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

// SetClipboardRich puts text on the clipboard together with an HTML version
// of it, which word processors, mail and chat programs paste with its
// formatting. html is a fragment, such as "<p><b>Bold</b> text</p>".
func (w *Window) SetClipboardRich(text, html string) bool {
	if w.fakeClip != nil {
		*w.fakeClip = text
		return true
	}
	return win.SetClipboardData(w.hwnd, map[uint32][]byte{
		win.CF_UNICODETEXT: utf16z(text),
		cfHTMLFormat:       clipfmt.WrapHTML(html),
	})
}

// ClipboardHTML returns the HTML fragment on the clipboard, as copied from a
// browser or word processor. It is not sanitized.
func (w *Window) ClipboardHTML() (string, bool) {
	data, ok := win.ClipboardData(w.hwnd, cfHTMLFormat)
	if !ok {
		return "", false
	}
	return clipfmt.ExtractHTML(data)
}

// SetClipboardSecret puts a secret such as a password or API key on the
// clipboard, marked to be left out of clipboard history, cloud clipboard and
// clipboard monitors. After clearAfter, unless 0, it empties the clipboard
// if nothing else was copied since.
func (w *Window) SetClipboardSecret(text string, clearAfter time.Duration) bool {
	if w.fakeClip != nil {
		*w.fakeClip = text
		return true
	}
	zero := []byte{0, 0, 0, 0}
	ok := win.SetClipboardData(w.hwnd, map[uint32][]byte{
		win.CF_UNICODETEXT: utf16z(text),
		cfExcludeMonitor:   zero,
		cfHistory:          zero,
		cfCloud:            zero,
	})
	if ok && clearAfter > 0 {
		seq := win.ClipboardSequence()
		time.AfterFunc(clearAfter, func() {
			Go(func() {
				if win.ClipboardSequence() == seq {
					win.SetClipboardData(0, nil)
				}
			})
		})
	}
	return ok
}

// ClipboardImage returns the image on the clipboard. It prefers PNG data,
// which keeps transparency, and falls back to a device-independent bitmap.
func (w *Window) ClipboardImage() (image.Image, bool) {
	if data, ok := win.ClipboardData(w.hwnd, cfPNG); ok {
		if img, err := png.Decode(bytes.NewReader(data)); err == nil {
			return img, true
		}
	}
	for _, format := range []uint32{win.CF_DIBV5, win.CF_DIB} {
		if data, ok := win.ClipboardData(w.hwnd, format); ok {
			if img, err := clipfmt.DecodeDIB(data); err == nil {
				return img, true
			}
		}
	}
	return nil, false
}

// SetClipboardImage replaces the clipboard with img as PNG and as a bitmap,
// so that programs that read either format can paste it.
func (w *Window) SetClipboardImage(img image.Image) bool {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return false
	}
	return win.SetClipboardData(w.hwnd, map[uint32][]byte{cfPNG: buf.Bytes(), win.CF_DIB: clipfmt.EncodeDIB(img)})
}
