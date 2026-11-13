package fairy

// Placeholder until the macOS backend has web views: it keeps the public API of
// the Windows backend and does nothing.

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
