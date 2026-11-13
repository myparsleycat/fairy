package fairy

import (
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/myparsleycat/fairy/internal/darwin"
)

// privatePasteboard points the clipboard functions at a pasteboard of
// their own for the test, leaving the user's clipboard alone.
func privatePasteboard(t *testing.T) {
	var pb darwin.ID
	Do(func() { pb = darwin.Retain(darwin.SendClass("NSPasteboard", "pasteboardWithUniqueName")) })
	prev := pasteboard
	pasteboard = func() darwin.ID { return pb }
	t.Cleanup(func() {
		pasteboard = prev
		Do(func() {
			darwin.Send(pb, "releaseGlobally")
			darwin.Release(pb)
		})
	})
}

func TestDarwinClipboardFormats(t *testing.T) {
	privatePasteboard(t)
	w := newTestWindow(t, func() *El { return Box() })
	Do(func() {
		if !w.SetClipboardRich("bold text", "<b>bold</b> text") {
			t.Error("SetClipboardRich failed")
			return // not t.Fatal: it would end the UI loop inside Do
		}
		if s, ok := w.systemClipboardText(); !ok || s != "bold text" {
			t.Errorf("text = %q, %v", s, ok)
		}
		if h, ok := w.ClipboardHTML(); !ok || h != "<b>bold</b> text" {
			t.Errorf("HTML = %q, %v", h, ok)
		}

		img := image.NewNRGBA(image.Rect(0, 0, 3, 2))
		img.Set(1, 1, color.NRGBA{R: 255, G: 128, A: 128})
		if !w.SetClipboardImage(img) {
			t.Error("SetClipboardImage failed")
			return // not t.Fatal: it would end the UI loop inside Do
		}
		got, ok := w.ClipboardImage()
		if !ok || got.Bounds() != img.Bounds() {
			t.Errorf("image = %v, %v", got, ok)
			return // not t.Fatal: it would end the UI loop inside Do
		}
		if r, g, _, a := got.At(1, 1).RGBA(); a>>8 != 128 || r>>8 < 120 || g>>8 < 60 {
			t.Errorf("pixel = %v", got.At(1, 1))
		}
		// TIFF alone, as other apps copy images, reads too.
		if tiff, ok := pasteboardData(pbTIFF); !ok || !setPasteboard(map[string]any{pbTIFF: tiff}) {
			t.Error("no TIFF on the clipboard")
			return // not t.Fatal: it would end the UI loop inside Do
		}
		if got, ok := w.ClipboardImage(); !ok || got.Bounds() != img.Bounds() {
			t.Errorf("image from TIFF = %v, %v", got, ok)
		}
		if !w.SetClipboardSecret("hunter2", 50*time.Millisecond) {
			t.Error("SetClipboardSecret failed")
			return // not t.Fatal: it would end the UI loop inside Do
		}
		if _, ok := pasteboardData(pbConcealed); !ok {
			t.Error("the secret is not marked concealed")
		}
	})
	time.Sleep(300 * time.Millisecond)
	Do(func() {
		if s, ok := w.systemClipboardText(); ok {
			t.Errorf("the secret %q is still on the clipboard", s)
		}
	})
}
