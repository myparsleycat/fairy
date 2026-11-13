package fairy

import (
	"bytes"
	"image"
	"image/png"
	"time"

	"github.com/myparsleycat/fairy/internal/darwin"
)

// Pasteboard types beyond NSPasteboardTypeString.
const (
	pbHTML = "public.html"
	pbPNG  = "public.png"
	pbTIFF = "public.tiff"
	// pbConcealed and pbTransient mark secrets for clipboard managers, by
	// the convention of nspasteboard.org.
	pbConcealed = "org.nspasteboard.ConcealedType"
	pbTransient = "org.nspasteboard.TransientType"
)

// setPasteboard replaces the clipboard with the given types: a string
// value is set as text, a []byte as data.
func setPasteboard(items map[string]any) bool {
	ok := true
	darwin.WithPool(func() {
		pb := pasteboard()
		darwin.Send(pb, "clearContents")
		for typ, v := range items {
			switch v := v.(type) {
			case string:
				ok = darwin.Get[bool](pb, "setString:forType:", darwin.String(v), darwin.String(typ)) && ok
			case []byte:
				ok = darwin.Get[bool](pb, "setData:forType:", darwin.Data(v), darwin.String(typ)) && ok
			}
		}
	})
	return ok
}

// pasteboardData returns the clipboard's data of type typ.
func pasteboardData(typ string) ([]byte, bool) {
	var b []byte
	var ok bool
	darwin.WithPool(func() {
		d := darwin.Send(pasteboard(), "dataForType:", darwin.String(typ))
		b, ok = darwin.Bytes(d), d != 0
	})
	return b, ok
}

func pasteboardChanges() int64 { return darwin.Get[int64](pasteboard(), "changeCount") }

// SetClipboardRich puts text on the clipboard together with an HTML version
// of it, which word processors, mail and chat programs paste with its
// formatting. html is a fragment, such as "<p><b>Bold</b> text</p>".
func (w *Window) SetClipboardRich(text, html string) bool {
	if w.fakeClip != nil {
		*w.fakeClip = text
		return true
	}
	return setPasteboard(map[string]any{"public.utf8-plain-text": text, pbHTML: html})
}

// ClipboardHTML returns the HTML on the clipboard, as copied from a browser
// or word processor. It is not sanitized.
func (w *Window) ClipboardHTML() (string, bool) {
	b, ok := pasteboardData(pbHTML)
	if !ok {
		return "", false
	}
	return string(b), true
}

// SetClipboardSecret puts a secret such as a password or API key on the
// clipboard, marked to be left out of clipboard history and clipboard
// managers. After clearAfter, unless 0, it empties the clipboard if nothing
// else was copied since.
func (w *Window) SetClipboardSecret(text string, clearAfter time.Duration) bool {
	if w.fakeClip != nil {
		*w.fakeClip = text
		return true
	}
	ok := setPasteboard(map[string]any{"public.utf8-plain-text": text, pbConcealed: "", pbTransient: ""})
	if ok && clearAfter > 0 {
		n := pasteboardChanges()
		time.AfterFunc(clearAfter, func() {
			Go(func() {
				if pasteboardChanges() == n {
					darwin.Send(pasteboard(), "clearContents")
				}
			})
		})
	}
	return ok
}

// ClipboardImage returns the image on the clipboard, from PNG data, which
// keeps transparency, or else TIFF, as most macOS apps copy.
func (w *Window) ClipboardImage() (image.Image, bool) {
	if data, ok := pasteboardData(pbPNG); ok {
		if img, err := png.Decode(bytes.NewReader(data)); err == nil {
			return img, true
		}
	}
	if data, ok := pasteboardData(pbTIFF); ok {
		var p []byte
		darwin.WithPool(func() {
			rep := darwin.SendClass("NSBitmapImageRep", "imageRepWithData:", darwin.Data(data))
			if rep != 0 {
				p = darwin.Bytes(darwin.Send(rep, "representationUsingType:properties:", uint64(4), // NSBitmapImageFileTypePNG
					darwin.SendClass("NSDictionary", "dictionary")))
			}
		})
		if img, err := png.Decode(bytes.NewReader(p)); err == nil {
			return img, true
		}
	}
	return nil, false
}

// SetClipboardImage replaces the clipboard with img as PNG and as TIFF, so
// that programs that read either can paste it.
func (w *Window) SetClipboardImage(img image.Image) bool {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return false
	}
	var tiff []byte
	darwin.WithPool(func() {
		if rep := darwin.SendClass("NSBitmapImageRep", "imageRepWithData:", darwin.Data(buf.Bytes())); rep != 0 {
			tiff = darwin.Bytes(darwin.Send(rep, "TIFFRepresentation"))
		}
	})
	items := map[string]any{pbPNG: buf.Bytes()}
	if tiff != nil {
		items[pbTIFF] = tiff
	}
	return setPasteboard(items)
}
