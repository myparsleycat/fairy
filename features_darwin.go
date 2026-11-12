package fairy

// Placeholders for the features the macOS backend does not have yet. Each
// keeps the public API of its Windows counterpart and does nothing, or
// reports errNoBackend.

import (
	"errors"
	"image"
	"time"

	"github.com/myparsleycat/fairy/internal/render"
)

// Backdrops.

var errBackdrop = errors.New("fairy: window backdrop not supported on this platform yet")

// BackdropSupported reports whether the system draws b behind a window.
func BackdropSupported(b Backdrop) bool { return b == BackdropNone }

func (w *Window) applyBackdrop(b Backdrop) {}

// Accessibility.

type a11yState struct{ focus uint64 }

func (w *Window) a11yAfterFrame() {}

// Announce asks screen readers to speak text.
func (w *Window) Announce(text string) {}

// Announce asks screen readers to speak text in the active window.
func Announce(text string) {}

// Clipboard formats beyond text.

// SetClipboardRich puts text and its HTML form on the clipboard.
func (w *Window) SetClipboardRich(text, html string) bool { return w.SetClipboardText(text) }

// ClipboardHTML returns the HTML on the clipboard.
func (w *Window) ClipboardHTML() (string, bool) { return "", false }

// SetClipboardSecret puts text on the clipboard, kept out of clipboard
// history, and clears it after clearAfter unless it changed.
func (w *Window) SetClipboardSecret(text string, clearAfter time.Duration) bool {
	return w.SetClipboardText(text)
}

// ClipboardImage returns the image on the clipboard.
func (w *Window) ClipboardImage() (image.Image, bool) { return nil, false }

// SetClipboardImage puts img on the clipboard.
func (w *Window) SetClipboardImage(img image.Image) bool { return false }

// Menus and the Dock.

// PopupMenu shows items as a native menu at window point (x, y).
func (w *Window) PopupMenu(x, y float32, items []MenuItem) error { return errNoBackend }

// ShowMenu shows items as a native menu at screen point (x, y).
func ShowMenu(x, y int, items []MenuItem) error { return errNoBackend }

// SetProgress shows progress on the window's Dock tile.
func (w *Window) SetProgress(state Progress, value float64) {}

// Surfaces and GPU images.

type surfaceEntry struct {
	target *render.Target
}

func (w *Window) paintSurface(e *El, box rectF, radii, clip [4]float32, opacity float32) {}
func (w *Window) sweepSurfaces()                                                         {}

// InvalidateSurface makes the Surface with key draw again on the next frame.
func (w *Window) InvalidateSurface(key string) {}

// gpuImage is an Image whose pixels live in a texture.
type gpuImage struct{ premul bool }

func (g *gpuImage) texture(gen uint64) *render.Texture { return nil }

func (g *gpuImage) readNRGBA() (*image.NRGBA, error) { return nil, errNoBackend }

// Release frees an Image's GPU texture. Other images need no release.
func (i *Image) Release() {}

// Lost reports whether the Image's texture was lost with its device.
func (i *Image) Lost() bool { return false }

// Video.

// Video is a playing video file or stream, drawn by VideoView.
type Video struct{}

// OpenVideo opens a video file or URL.
func OpenVideo(source string, opts VideoOptions) (*Video, error) { return nil, errNoBackend }

func (v *Video) Close()                  {}
func (v *Video) State() VideoState       { return VideoFailed }
func (v *Video) Err() error              { return errNoBackend }
func (v *Video) Play()                   {}
func (v *Video) Pause()                  {}
func (v *Video) Playing() bool           { return false }
func (v *Video) Seek(d time.Duration)    {}
func (v *Video) Position() time.Duration { return 0 }
func (v *Video) Duration() time.Duration { return 0 }
func (v *Video) Size() (int, int)        { return 0, 0 }
func (v *Video) SetMuted(m bool)         {}
func (v *Video) Muted() bool             { return false }
func (v *Video) SetVolume(vol float64)   {}
func (v *Video) Volume() float64         { return 0 }
func (v *Video) SetLoop(loop bool)       {}

func (w *Window) paintVideo(e *El, box rectF, radii, clip [4]float32, opacity float32) {}

// Web views.

// WebView is a browser shown as part of the window, by WebViewHost.
type WebView struct{ opts WebViewOptions }

// NewWebView makes a web view; it is created when WebViewHost first places it.
func NewWebView(opts WebViewOptions) *WebView { return &WebView{opts: opts} }

func (v *WebView) Ready() bool                                                  { return false }
func (v *WebView) Err() error                                                   { return errNoBackend }
func (v *WebView) Navigate(url string)                                          {}
func (v *WebView) URL() string                                                  { return "" }
func (v *WebView) Title() string                                                { return "" }
func (v *WebView) Reload()                                                      {}
func (v *WebView) CanGoBack() bool                                              { return false }
func (v *WebView) GoBack()                                                      {}
func (v *WebView) ExecuteScript(js string, done func(result string, err error)) {}
func (v *WebView) PostMessage(msg string)                                       {}
func (v *WebView) Cookies(uri string, done func([]Cookie, error))               {}
func (v *WebView) DeleteCookies(name, uri string)                               {}
func (v *WebView) DeleteAllCookies()                                            {}
func (v *WebView) Close()                                                       {}

func (w *Window) placeWebView(e *El, box rectF) {}
func (w *Window) sweepWebViews()                {}
