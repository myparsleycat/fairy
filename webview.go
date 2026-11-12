package fairy

import "time"

// WebViewOptions configures NewWebView.
type WebViewOptions struct {
	// URL is loaded once the browser is ready.
	URL string
	// UserDataFolder keeps cookies, storage and cache. Empty means
	// %LOCALAPPDATA%\<executable name>\WebView2. Web views with the same
	// folder share one browser process and must use the same BrowserArgs.
	UserDataFolder string
	// BrowserArgs are extra Chromium switches, such as "--proxy-server=host:port".
	BrowserArgs string
	DevTools    bool
	// OnNavigated runs when a top-level navigation finishes.
	OnNavigated func(url string, ok bool)
	// OnTitle runs when the document title changes.
	OnTitle func(title string)
	// OnMessage receives strings the page sends with window.chrome.webview.postMessage.
	OnMessage func(msg string)
	// OnReady runs once the browser is created.
	OnReady func()
	// OnError runs if the browser cannot be created.
	OnError func(error)
}

// Cookie is a browser cookie.
type Cookie struct {
	Name, Value, Domain, Path string
	Expires                   time.Time // zero for session cookies
	HTTPOnly, Secure          bool
}

// WebViewHost reserves the area v is shown in. Size it like a box.
func WebViewHost(v *WebView) *El {
	e := newEl(kindWebView)
	e.webview = v
	return e
}
