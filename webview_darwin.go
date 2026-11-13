package fairy

// Web views on macOS are WKWebViews, subviews of the window's FairyView.
// The view is flipped, so a web view's frame in points is its element's
// box in logical pixels.
//
// Pages talk to Go through the object WebView2 gives them:
// window.chrome.webview, with postMessage and "message" events. A user
// script makes it at document start on top of a WKScriptMessageHandler.

import (
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/myparsleycat/fairy/internal/darwin"
)

// WebView is a WebKit browser shown as part of the window, by WebViewHost.
// It is created when WebViewHost first places it in a window. It is a
// native view above the window's drawing, so popovers and overlays never
// cover it, and it is not clipped by scroll containers. Pages that open
// windows, with window.open or a link with a target, get a plain window of
// their own, as with WebView2; closing the WebView closes them. Use it from
// the UI thread.
type WebView struct {
	opts     WebViewOptions
	host     *Window
	creating bool
	wk       darwin.ID // WKWebView
	delegate darwin.ID // FairyWebViewDelegate
	store    darwin.ID // WKWebsiteDataStore
	err      error
	frame    darwin.CGRect
	visible  bool
	pending  string // URL to navigate to once ready
	title    string
	closed   bool
	popups   []webPopup // windows the page opened
	// ephemeral keeps the web view's data in memory, for tests.
	ephemeral bool
}

// NewWebView returns a web view that is created when first shown.
func NewWebView(opts WebViewOptions) *WebView {
	return &WebView{opts: opts, pending: opts.URL}
}

var errWebViewClosed = errors.New("fairy: web view closed")

// byWebDelegate finds the WebView a WebKit callback is for.
var byWebDelegate = map[darwin.ID]*WebView{}

// webKit loads the WebKit framework.
var webKit = sync.OnceValues(func() (uintptr, error) {
	h, err := purego.Dlopen("/System/Library/Frameworks/WebKit.framework/WebKit", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return 0, fmt.Errorf("fairy: load WebKit: %w", err)
	}
	return h, nil
})

// messageHandler is the name of the script message handler the page's
// window.chrome.webview.postMessage sends to.
const messageHandler = "fairy"

// bridgeScript gives the page window.chrome.webview as WebView2 does.
// __fairyWebView delivers PostMessage's strings as "message" events.
const bridgeScript = `(function () {
  var handler = window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.` + messageHandler + `;
  if (!handler) return;
  var target = new EventTarget();
  Object.defineProperty(target, 'postMessage', {
    value: function (message) { handler.postMessage(message); }
  });
  if (!window.chrome) window.chrome = {};
  Object.defineProperty(window.chrome, 'webview', { value: target, enumerable: true });
  Object.defineProperty(window, '__fairyWebView', {
    value: function (data) { target.dispatchEvent(new MessageEvent('message', { data: data })); }
  });
})();`

// cspMarker is what the script wrapper returns when the page's content
// security policy forbids eval. It is never JSON.
const cspMarker = "!csp"

// scriptWrapper runs a script at global scope and returns its completion
// value as JSON, as WebView2's ExecuteScript does: "null" when it is
// undefined, cannot be encoded, or the script throws.
const scriptWrapper = `(function () {
  var r;
  try { r = (0, eval)(%s); } catch (e) {
    if (e instanceof EvalError && /unsafe-eval/.test(e.message)) return '` + cspMarker + `';
    return 'null';
  }
  try { var s = JSON.stringify(r); return s === undefined ? 'null' : s; } catch (e) { return 'null'; }
})()`

// dataStore returns the website data store for the web view's options.
func (v *WebView) dataStore() darwin.ID {
	cls := darwin.ID(darwin.Cls("WKWebsiteDataStore"))
	if v.ephemeral {
		return darwin.Send(cls, "nonPersistentDataStore")
	}
	folder := v.opts.UserDataFolder
	if folder == "" || !darwin.Get[bool](cls, "respondsToSelector:", darwin.Sel("dataStoreForIdentifier:")) {
		return darwin.Send(cls, "defaultDataStore")
	}
	// macOS 14 keeps separate stores by identifier: derive one from the
	// folder, so web views with the same folder share cookies and storage.
	if abs, err := filepath.Abs(folder); err == nil {
		folder = abs
	}
	h := sha1.Sum([]byte(filepath.Clean(folder)))
	h[6] = h[6]&0x0f | 0x50 // version 5
	h[8] = h[8]&0x3f | 0x80 // RFC 4122 variant
	s := fmt.Sprintf("%x-%x-%x-%x-%x", h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
	uuid := darwin.Send(darwin.SendClass("NSUUID", "alloc"), "initWithUUIDString:", darwin.String(s))
	defer darwin.Release(uuid)
	return darwin.Send(cls, "dataStoreForIdentifier:", uuid)
}

func (v *WebView) create(w *Window) {
	v.creating, v.host = true, w
	// WebView2 makes its browser asynchronously; so does this, outside the
	// frame being painted.
	darwin.Perform(func() {
		if v.closed || v.host == nil || v.host.closed {
			v.creating = false
			return
		}
		if _, err := webKit(); err != nil {
			v.fail(err)
			return
		}
		v.build()
	})
}

func (v *WebView) build() {
	webViewDelegateClass()
	cfg := darwin.New("WKWebViewConfiguration")
	defer darwin.Release(cfg)
	v.store = darwin.Retain(v.dataStore())
	darwin.Send(cfg, "setWebsiteDataStore:", v.store)
	prefs := darwin.Send(cfg, "preferences")
	// WebView2 opens a window for every window.open; WebKit by default only
	// for those a click starts.
	darwin.Send(prefs, "setJavaScriptCanOpenWindowsAutomatically:", true)
	if v.opts.DevTools {
		if darwin.Get[bool](prefs, "respondsToSelector:", darwin.Sel("_setDeveloperExtrasEnabled:")) {
			darwin.Send(prefs, "_setDeveloperExtrasEnabled:", true)
		}
	}
	v.delegate = darwin.New("FairyWebViewDelegate")
	byWebDelegate[v.delegate] = v
	ucc := darwin.Send(cfg, "userContentController")
	script := darwin.Send(darwin.SendClass("WKUserScript", "alloc"), "initWithSource:injectionTime:forMainFrameOnly:",
		darwin.String(bridgeScript), int64(0), true) // WKUserScriptInjectionTimeAtDocumentStart
	darwin.Send(ucc, "addUserScript:", script)
	darwin.Release(script)
	darwin.Send(ucc, "addScriptMessageHandler:name:", v.delegate, darwin.String(messageHandler))

	wk := darwin.Send(darwin.SendClass("WKWebView", "alloc"), "initWithFrame:configuration:", v.frame, cfg)
	if wk == 0 {
		delete(byWebDelegate, v.delegate)
		darwin.Release(v.delegate)
		darwin.Release(v.store)
		v.delegate, v.store = 0, 0
		v.fail(errors.New("fairy: create WKWebView failed"))
		return
	}
	v.creating = false
	v.wk = wk
	if v.opts.DevTools && darwin.Get[bool](wk, "respondsToSelector:", darwin.Sel("setInspectable:")) {
		darwin.Send(wk, "setInspectable:", true)
	}
	darwin.Send(wk, "setNavigationDelegate:", v.delegate)
	darwin.Send(wk, "setUIDelegate:", v.delegate)
	darwin.Send(wk, "addObserver:forKeyPath:options:context:", v.delegate, darwin.String("title"), uint64(1), uintptr(0))
	darwin.Send(wk, "setHidden:", !v.visible)
	darwin.Send(v.host.view, "addSubview:", wk)
	if v.pending != "" {
		v.Navigate(v.pending)
	}
	if v.opts.OnReady != nil {
		v.opts.OnReady()
	}
	v.update()
}

func (v *WebView) fail(err error) {
	v.creating, v.err = false, err
	if v.opts.OnError != nil {
		v.opts.OnError(err)
	}
	v.update()
}

func (v *WebView) update() {
	if v.host != nil {
		v.host.Update()
	}
}

// hasFocus reports whether the web view or a view inside it is the
// window's first responder.
func (v *WebView) hasFocus() bool {
	if v.wk == 0 || v.host == nil || v.host.closed {
		return false
	}
	fr := darwin.Send(v.host.nsWindow, "firstResponder")
	return fr != 0 && darwin.Get[bool](fr, "isKindOfClass:", darwin.ID(darwin.Cls("NSView"))) &&
		darwin.Get[bool](fr, "isDescendantOf:", v.wk)
}

// giveFocusBack makes the window's view the first responder when the web
// view has it, before the web view is hidden or removed.
func (v *WebView) giveFocusBack() {
	if v.hasFocus() {
		darwin.Send(v.host.nsWindow, "makeFirstResponder:", v.host.view)
	}
}

func (v *WebView) applyFrame() {
	if v.wk == 0 {
		return
	}
	if !v.visible {
		v.giveFocusBack()
	} else {
		darwin.Send(v.wk, "setFrame:", v.frame)
	}
	darwin.Send(v.wk, "setHidden:", !v.visible)
}

// placeWebView positions the web view over the element's content box.
func (w *Window) placeWebView(e *El, box rectF) {
	if !w.hasNativeWindow() {
		return
	}
	v := e.webview
	if v == nil || v.closed {
		return
	}
	w.webviews[v] = true
	if v.host != nil && v.host != w {
		if v.wk != 0 {
			v.giveFocusBack()
			darwin.Send(v.wk, "removeFromSuperview")
			darwin.Send(w.view, "addSubview:", v.wk)
		}
		delete(v.host.webviews, v)
		v.host = w
	}
	if v.wk == 0 && !v.creating && v.err == nil {
		v.create(w)
	}
	// Snap the edges to device pixels, as WebView2's bounds are.
	d := w.snap(box)
	s := float64(w.scale)
	f := darwin.Rect(float64(d[0])/s, float64(d[1])/s, float64(d[2])/s, float64(d[3])/s)
	if f != v.frame || !v.visible {
		v.frame, v.visible = f, true
		v.applyFrame()
	}
}

// sweepWebViews hides web views that were not placed this frame.
func (w *Window) sweepWebViews() {
	for v, placed := range w.webviews {
		if !placed {
			if v.visible {
				v.visible = false
				v.applyFrame()
			}
			delete(w.webviews, v)
			continue
		}
		w.webviews[v] = false
	}
}

func (w *Window) closeWebViews() {
	for v := range w.webviews {
		v.Close()
	}
}

// focusedWebView returns the web view that has the keyboard, or nil.
func (w *Window) focusedWebView() *WebView {
	for v := range w.webviews {
		if v.hasFocus() {
			return v
		}
	}
	return nil
}

// focusView takes the keyboard back from a web view, as a press on the
// window outside it does.
func (w *Window) focusView() {
	if w.focusedWebView() != nil {
		darwin.Send(w.nsWindow, "makeFirstResponder:", w.view)
	}
}

// overWebView reports whether the window point (x, y) is over a shown web
// view, whose events the web view gets.
func (w *Window) overWebView(x, y float32) bool {
	for v := range w.webviews {
		f := v.frame
		if v.wk != 0 && v.visible && float64(x) >= f.Origin.X && float64(y) >= f.Origin.Y &&
			float64(x) < f.Origin.X+f.Size.W && float64(y) < f.Origin.Y+f.Size.H {
			return true
		}
	}
	return false
}

// webViewEditActions are the edit commands of the shortcuts a page did not
// handle. The app has no Edit menu to send them, so the window does.
var webViewEditActions = map[string]string{
	"c": "copy:", "x": "cut:", "v": "paste:", "a": "selectAll:", "z": "undo:", "Z": "redo:",
}

// webViewKey handles a key a focused web view passed on: the page did not
// handle it, and WebKit sent it up the responder chain to the window's
// view. Keys typed into a web view do not reach the window's elements, as
// on Windows, but the edit shortcuts run their commands in the web view.
func (w *Window) webViewKey(ev darwin.ID) {
	flags := darwin.Get[uint64](ev, "modifierFlags")
	if flags&(nsCommand|nsControl|nsOption) != nsCommand {
		return
	}
	key := strings.ToLower(darwin.GoString(darwin.Send(ev, "charactersIgnoringModifiers")))
	if len(key) != 1 || key[0] < 'a' || key[0] > 'z' {
		// A layout without Latin letters, such as Korean: the key's place
		// on a US keyboard.
		key = map[uint16]string{0x00: "a", 0x06: "z", 0x07: "x", 0x08: "c", 0x09: "v"}[darwin.Get[uint16](ev, "keyCode")]
	}
	if flags&nsShift != 0 {
		key = strings.ToUpper(key)
	}
	if action, ok := webViewEditActions[key]; ok {
		// From the first responder up, which sendAction:to:from: does only
		// in the key window.
		fr := darwin.Send(w.nsWindow, "firstResponder")
		darwin.Send(fr, "tryToPerform:with:", darwin.Sel(action), w.view)
	}
}

// addFocusTracking makes the window's view report focus moving to a web
// view and back as the window losing and gaining focus, as Windows does
// when its child browser window takes the keyboard.
func addFocusTracking(c objc.Class) {
	type id = darwin.ID
	type sel = darwin.SEL
	win := func(self id) *Window {
		if w := byView[self]; w != nil && !w.closed {
			return w
		}
		return nil
	}
	add := func(name string, fn any) {
		if !c.AddMethod(darwin.Sel(name), objc.NewIMP(fn), "B@:") {
			panic("fairy: add method " + name)
		}
	}
	add("becomeFirstResponder", func(self id, _ sel) bool {
		if w := win(self); w != nil && !w.osFocus && darwin.Get[bool](w.nsWindow, "isKeyWindow") {
			w.onFocusChange(true)
			w.securePassword(w.focusedEl())
		}
		return true
	})
	add("resignFirstResponder", func(self id, _ sel) bool {
		if w := win(self); w != nil && w.osFocus {
			w.securePassword(nil)
			w.onFocusChange(false)
		}
		return true
	})
}

// Ready reports whether the browser has been created.
func (v *WebView) Ready() bool { return v.wk != 0 }

// Err returns the error that prevented the browser from being created.
func (v *WebView) Err() error { return v.err }

// Navigate loads url.
func (v *WebView) Navigate(url string) {
	if v.wk == 0 {
		v.pending = url
		return
	}
	v.pending = ""
	darwin.WithPool(func() {
		u := darwin.SendClass("NSURL", "URLWithString:", darwin.String(url))
		if u == 0 {
			return
		}
		if darwin.Get[bool](u, "isFileURL") {
			// WebView2 pages from files may read other files, as here.
			root := darwin.SendClass("NSURL", "fileURLWithPath:", darwin.String("/"))
			darwin.Send(v.wk, "loadFileURL:allowingReadAccessToURL:", u, root)
			return
		}
		darwin.Send(v.wk, "loadRequest:", darwin.SendClass("NSURLRequest", "requestWithURL:", u))
	})
}

// URL returns the address of the current page.
func (v *WebView) URL() string {
	if v.wk == 0 {
		return v.pending
	}
	var s string
	darwin.WithPool(func() {
		if u := darwin.Send(v.wk, "URL"); u != 0 {
			s = darwin.GoString(darwin.Send(u, "absoluteString"))
		}
	})
	return s
}

// Title returns the document title.
func (v *WebView) Title() string { return v.title }

// Reload reloads the page.
func (v *WebView) Reload() {
	if v.wk != 0 {
		darwin.Send(v.wk, "reload")
	}
}

// CanGoBack reports whether there is a previous page.
func (v *WebView) CanGoBack() bool { return v.wk != 0 && darwin.Get[bool](v.wk, "canGoBack") }

// GoBack goes to the previous page.
func (v *WebView) GoBack() {
	if v.wk != 0 {
		darwin.Send(v.wk, "goBack")
	}
}

// WKErrorDomain codes of evaluateJavaScript:.
const (
	wkErrorJavaScriptException   = 4
	wkErrorJavaScriptUnsupported = 5
)

// evaluate runs js in the page and calls done with the result object, which
// lives until done returns, and the NSError.
func (v *WebView) evaluate(js string, done func(result, nserr darwin.ID)) {
	b := objc.NewBlock(func(_ objc.Block, result, nserr darwin.ID) { done(result, nserr) })
	darwin.WithPool(func() {
		darwin.Send(v.wk, "evaluateJavaScript:completionHandler:", darwin.String(js), b)
	})
	b.Release()
}

// nsErr turns an NSError into an error.
func nsErr(op string, nserr darwin.ID) error {
	return fmt.Errorf("fairy: %s: %s", op, darwin.GoString(darwin.Send(nserr, "localizedDescription")))
}

// ExecuteScript runs JavaScript in the page; done, if set, receives the
// result as JSON. As with WebView2, the result is "null" when the script
// throws or its value cannot be encoded.
func (v *WebView) ExecuteScript(js string, done func(result string, err error)) {
	if done == nil {
		done = func(string, error) {}
	}
	if v.wk == 0 {
		done("", errWebViewClosed)
		return
	}
	src, _ := json.Marshal(js)
	v.evaluate(fmt.Sprintf(scriptWrapper, src), func(result, nserr darwin.ID) {
		if nserr != 0 {
			done("", nsErr("ExecuteScript", nserr))
			return
		}
		s := darwin.GoString(result)
		if s != cspMarker {
			done(s, nil)
			return
		}
		// The page forbids eval: run the script itself and encode the
		// object WebKit makes of its value.
		if v.wk == 0 {
			done("", errWebViewClosed)
			return
		}
		v.evaluate(js, func(result, nserr darwin.ID) {
			switch {
			case nserr == 0:
				done(jsonOf(result), nil)
			case darwin.Get[int64](nserr, "code") == wkErrorJavaScriptException,
				darwin.Get[int64](nserr, "code") == wkErrorJavaScriptUnsupported:
				done("null", nil)
			default:
				done("", nsErr("ExecuteScript", nserr))
			}
		})
	})
}

// jsonOf encodes a Foundation object as JSON, or "null".
func jsonOf(obj darwin.ID) string {
	if obj == 0 {
		return "null"
	}
	s := "null"
	darwin.WithPool(func() {
		arr := darwin.SendClass("NSArray", "arrayWithObject:", obj)
		if !darwin.Get[bool](darwin.ID(darwin.Cls("NSJSONSerialization")), "isValidJSONObject:", arr) {
			return
		}
		const withoutEscapingSlashes = 8
		data := darwin.SendClass("NSJSONSerialization", "dataWithJSONObject:options:error:", arr,
			uint64(withoutEscapingSlashes), darwin.ID(0))
		if b := darwin.Bytes(data); len(b) >= 2 {
			s = string(b[1 : len(b)-1])
		}
	})
	return s
}

// PostMessage sends a string to the page's window.chrome.webview "message" listeners.
func (v *WebView) PostMessage(msg string) {
	if v.wk == 0 {
		return
	}
	data, _ := json.Marshal(msg)
	v.evaluate("window.__fairyWebView && window.__fairyWebView("+string(data)+")", func(_, _ darwin.ID) {})
}

// cookieStore returns the web view's WKHTTPCookieStore, or 0.
func (v *WebView) cookieStore() darwin.ID {
	if v.wk == 0 || v.store == 0 {
		return 0
	}
	return darwin.Send(v.store, "httpCookieStore")
}

// allCookies calls done with every NSHTTPCookie of the web view's store.
func (v *WebView) allCookies(done func(cookies []darwin.ID)) bool {
	cs := v.cookieStore()
	if cs == 0 {
		return false
	}
	b := objc.NewBlock(func(_ objc.Block, arr darwin.ID) {
		n := uint64(0)
		if arr != 0 {
			n = darwin.Get[uint64](arr, "count")
		}
		list := make([]darwin.ID, 0, n)
		for i := range n {
			list = append(list, darwin.Send(arr, "objectAtIndex:", i))
		}
		done(list)
	})
	darwin.Send(cs, "getAllCookies:", b)
	b.Release()
	return true
}

func goCookie(c darwin.ID) Cookie {
	ck := Cookie{
		Name:     darwin.GoString(darwin.Send(c, "name")),
		Value:    darwin.GoString(darwin.Send(c, "value")),
		Domain:   darwin.GoString(darwin.Send(c, "domain")),
		Path:     darwin.GoString(darwin.Send(c, "path")),
		HTTPOnly: darwin.Get[bool](c, "isHTTPOnly"),
		Secure:   darwin.Get[bool](c, "isSecure"),
	}
	if d := darwin.Send(c, "expiresDate"); d != 0 {
		sec := darwin.Get[float64](d, "timeIntervalSince1970")
		whole, frac := math.Modf(sec)
		ck.Expires = time.Unix(int64(whole), int64(frac*1e9))
	}
	return ck
}

// cookieMatcher returns whether a cookie would be sent to uri. Every cookie
// matches an empty uri.
func cookieMatcher(uri string) (func(Cookie) bool, error) {
	if uri == "" {
		return func(Cookie) bool { return true }, nil
	}
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	host := strings.ToLower(u.Hostname())
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	secure := u.Scheme == "https" || u.Scheme == "wss"
	return func(c Cookie) bool {
		d := strings.ToLower(c.Domain)
		if strings.HasPrefix(d, ".") {
			if host != d[1:] && !strings.HasSuffix(host, d) {
				return false
			}
		} else if host != d {
			return false
		}
		p := c.Path
		if p == "" {
			p = "/"
		}
		if path != p && !(strings.HasPrefix(path, p) && (strings.HasSuffix(p, "/") || path[len(p)] == '/')) {
			return false
		}
		return !c.Secure || secure
	}, nil
}

// Cookies returns the cookies that would be sent to uri, or all cookies for
// an empty uri. done runs on the UI thread.
func (v *WebView) Cookies(uri string, done func([]Cookie, error)) {
	match, err := cookieMatcher(uri)
	if err != nil {
		done(nil, fmt.Errorf("fairy: Cookies: %w", err))
		return
	}
	if !v.allCookies(func(list []darwin.ID) {
		out := make([]Cookie, 0, len(list))
		for _, c := range list {
			if ck := goCookie(c); match(ck) {
				out = append(out, ck)
			}
		}
		done(out, nil)
	}) {
		done(nil, errWebViewClosed)
	}
}

// DeleteCookies deletes cookies called name that would be sent to uri.
func (v *WebView) DeleteCookies(name, uri string) {
	match, err := cookieMatcher(uri)
	if err != nil {
		return
	}
	v.allCookies(func(list []darwin.ID) {
		cs := v.cookieStore()
		if cs == 0 {
			return
		}
		for _, c := range list {
			if ck := goCookie(c); ck.Name == name && match(ck) {
				darwin.Send(cs, "deleteCookie:completionHandler:", c, darwin.ID(0))
			}
		}
	})
}

// DeleteAllCookies deletes every cookie of the web view's data store.
func (v *WebView) DeleteAllCookies() {
	if v.cookieStore() == 0 {
		return
	}
	lib, _ := webKit()
	darwin.WithPool(func() {
		types := darwin.SendClass("NSSet", "setWithObject:", darwin.SymbolID(lib, "WKWebsiteDataTypeCookies"))
		b := objc.NewBlock(func(objc.Block) {})
		darwin.Send(v.store, "removeDataOfTypes:modifiedSince:completionHandler:", types,
			darwin.SendClass("NSDate", "distantPast"), b)
		b.Release()
	})
}

// Close destroys the browser. The web view cannot be shown again.
func (v *WebView) Close() {
	if v.closed {
		return
	}
	v.closed = true
	if v.host != nil {
		delete(v.host.webviews, v)
	}
	for len(v.popups) > 0 {
		darwin.Send(v.popups[0].win, "close")
	}
	if v.wk != 0 {
		v.giveFocusBack()
		wk := v.wk
		v.wk = 0
		darwin.Send(wk, "removeObserver:forKeyPath:", v.delegate, darwin.String("title"))
		ucc := darwin.Send(darwin.Send(wk, "configuration"), "userContentController")
		darwin.Send(ucc, "removeScriptMessageHandlerForName:", darwin.String(messageHandler))
		darwin.Send(ucc, "removeAllUserScripts")
		darwin.Send(wk, "setNavigationDelegate:", darwin.ID(0))
		darwin.Send(wk, "setUIDelegate:", darwin.ID(0))
		darwin.Send(wk, "stopLoading")
		darwin.Send(wk, "removeFromSuperview")
		darwin.Release(wk)
	}
	if v.delegate != 0 {
		delete(byWebDelegate, v.delegate)
		darwin.Release(v.delegate)
		v.delegate = 0
	}
	darwin.Release(v.store)
	v.store = 0
}

// webPopup is a window a page opened with window.open or a link with a
// target, holding a web view that shares the opener's configuration.
type webPopup struct{ win, wk darwin.ID }

// openPopup opens a window for a page's window.open, as WebView2 does, sized
// as the page asks or 800x600 points, and returns its web view for WebKit
// to load the page into.
func (v *WebView) openPopup(cfg, features darwin.ID) darwin.ID {
	size := func(n darwin.ID, def float64) float64 {
		if n == 0 {
			return def
		}
		return min(max(darwin.Get[float64](n, "doubleValue"), 100), 10000)
	}
	w, h := size(darwin.Send(features, "width"), 800), size(darwin.Send(features, "height"), 600)
	rect := darwin.CGRect{Size: darwin.CGSize{W: w, H: h}}
	const style = 1 | 2 | 4 | 8 // titled, closable, miniaturizable, resizable
	win := darwin.Send(darwin.SendClass("NSWindow", "alloc"), "initWithContentRect:styleMask:backing:defer:",
		rect, uint64(style), uint64(2), false)
	if win == 0 {
		return 0
	}
	wk := darwin.Send(darwin.SendClass("WKWebView", "alloc"), "initWithFrame:configuration:", rect, cfg)
	if wk == 0 {
		darwin.Release(win)
		return 0
	}
	darwin.Send(win, "setReleasedWhenClosed:", false)
	darwin.Send(win, "setContentView:", wk)
	darwin.Send(win, "setDelegate:", v.delegate)
	darwin.Send(wk, "setUIDelegate:", v.delegate)
	darwin.Send(wk, "addObserver:forKeyPath:options:context:", v.delegate, darwin.String("title"), uint64(1), uintptr(0))
	if v.opts.DevTools && darwin.Get[bool](wk, "respondsToSelector:", darwin.Sel("setInspectable:")) {
		darwin.Send(wk, "setInspectable:", true)
	}
	v.popups = append(v.popups, webPopup{win: win, wk: wk})
	if v.host != nil && v.host.nsWindow != 0 {
		// Offset from the host window's top left, as cascading windows are.
		f := darwin.Get[darwin.CGRect](v.host.nsWindow, "frame")
		darwin.Send(win, "setFrameTopLeftPoint:", darwin.CGPoint{X: f.Origin.X + 24, Y: f.Origin.Y + f.Size.H - 24})
	} else {
		darwin.Send(win, "center")
	}
	darwin.Send(win, "makeKeyAndOrderFront:", darwin.ID(0))
	return wk
}

// popupOf returns the index of the popup with web view wk or window win, or
// -1.
func (v *WebView) popupOf(wk, win darwin.ID) int {
	for i, p := range v.popups {
		if wk != 0 && p.wk == wk || win != 0 && p.win == win {
			return i
		}
	}
	return -1
}

// closePopup frees popup i once its window closes.
func (v *WebView) closePopup(i int) {
	p := v.popups[i]
	v.popups = slices.Delete(v.popups, i, i+1)
	darwin.Send(p.wk, "removeObserver:forKeyPath:", v.delegate, darwin.String("title"))
	darwin.Send(p.wk, "setUIDelegate:", darwin.ID(0))
	darwin.Send(p.wk, "stopLoading")
	darwin.Send(p.win, "setDelegate:", darwin.ID(0))
	darwin.Release(p.wk)
	// The window is released after AppKit is done closing it.
	win := p.win
	darwin.Perform(func() { darwin.Release(win) })
}

// WebKit callbacks.

func (v *WebView) navigated(ok bool) {
	if v.opts.OnNavigated != nil {
		v.opts.OnNavigated(v.URL(), ok)
	}
	v.update()
}

func (v *WebView) titleChanged() {
	v.title = darwin.GoString(darwin.Send(v.wk, "title"))
	if v.opts.OnTitle != nil {
		v.opts.OnTitle(v.title)
	}
	v.update()
}

// message handles window.chrome.webview.postMessage. As WebView2's
// TryGetWebMessageAsString, it passes on strings and gives "" for other
// values; messages from frames other than the main one are dropped.
func (v *WebView) message(msg darwin.ID) {
	if v.opts.OnMessage == nil {
		return
	}
	if fi := darwin.Send(msg, "frameInfo"); fi == 0 || !darwin.Get[bool](fi, "isMainFrame") {
		return
	}
	body := darwin.Send(msg, "body")
	s := ""
	if body != 0 && darwin.Get[bool](body, "isKindOfClass:", darwin.ID(darwin.Cls("NSString"))) {
		s = darwin.GoString(body)
	}
	v.opts.OnMessage(s)
}

// callBlock calls an Objective-C block that takes integer or pointer
// arguments.
func callBlock(b darwin.ID, args ...uintptr) {
	invoke := *(*uintptr)(unsafe.Add(nil, uintptr(b)+16)) // Block_literal.invoke
	purego.SyscallN(invoke, append([]uintptr{uintptr(b)}, args...)...)
}

// panel shows a JavaScript alert, confirm or prompt dialog as a sheet on
// the window, and calls done with whether OK was chosen and the text. A
// hidden window shows none and answers Cancel at once, as WebKit does
// without a UI delegate.
func (v *WebView) panel(message string, buttons []string, input *string, done func(ok bool, text string)) {
	if v.host == nil || v.host.closed || !darwin.Get[bool](v.host.nsWindow, "isVisible") {
		done(false, "")
		return
	}
	alert := darwin.New("NSAlert")
	darwin.Send(alert, "setMessageText:", darwin.String(message))
	for _, b := range buttons {
		darwin.Send(alert, "addButtonWithTitle:", darwin.String(b))
	}
	var field darwin.ID
	if input != nil {
		field = darwin.Send(darwin.SendClass("NSTextField", "alloc"), "initWithFrame:", darwin.Rect(0, 0, 260, 24))
		darwin.Send(field, "setStringValue:", darwin.String(*input))
		darwin.Send(alert, "setAccessoryView:", field)
		darwin.Send(darwin.Send(alert, "window"), "setInitialFirstResponder:", field)
	}
	finish := func(resp int64) {
		text := ""
		if field != 0 {
			text = darwin.GoString(darwin.Send(field, "stringValue"))
			darwin.Release(field)
		}
		darwin.Release(alert)
		done(resp == 1000, text) // NSAlertFirstButtonReturn
	}
	b := objc.NewBlock(func(_ objc.Block, resp int64) { finish(resp) })
	darwin.Send(alert, "beginSheetModalForWindow:completionHandler:", v.host.nsWindow, b)
	b.Release()
}

var webViewDelegateClass = sync.OnceValue(func() darwin.Class {
	type id = darwin.ID
	type sel = darwin.SEL
	var protocols []*objc.Protocol
	for _, name := range []string{"WKNavigationDelegate", "WKUIDelegate", "WKScriptMessageHandler"} {
		if p := objc.GetProtocol(name); p != nil {
			protocols = append(protocols, p)
		}
	}
	web := func(self id) *WebView {
		if v := byWebDelegate[self]; v != nil && !v.closed && v.wk != 0 {
			return v
		}
		return nil
	}
	// held keeps a completion handler block until a dialog answers.
	held := func(handler id) id { return id(objc.Block(handler).Copy()) }
	c, err := objc.RegisterClass("FairyWebViewDelegate", darwin.Cls("NSObject"), protocols, nil, []objc.MethodDef{
		{Cmd: darwin.Sel("webView:didFinishNavigation:"), Fn: func(self id, _ sel, _, _ id) {
			if v := web(self); v != nil {
				v.navigated(true)
			}
		}},
		{Cmd: darwin.Sel("webView:didFailNavigation:withError:"), Fn: func(self id, _ sel, _, _, _ id) {
			if v := web(self); v != nil {
				v.navigated(false)
			}
		}},
		{Cmd: darwin.Sel("webView:didFailProvisionalNavigation:withError:"), Fn: func(self id, _ sel, _, _, _ id) {
			if v := web(self); v != nil {
				v.navigated(false)
			}
		}},
		{Cmd: darwin.Sel("userContentController:didReceiveScriptMessage:"), Fn: func(self id, _ sel, _, msg id) {
			if v := web(self); v != nil {
				v.message(msg)
			}
		}},
		{Cmd: darwin.Sel("observeValueForKeyPath:ofObject:change:context:"), Fn: func(self id, _ sel, _, obj, _ id, _ uintptr) {
			if v := web(self); v != nil {
				if obj == v.wk {
					v.titleChanged()
				} else if i := v.popupOf(obj, 0); i >= 0 {
					darwin.Send(v.popups[i].win, "setTitle:", darwin.Send(obj, "title"))
				}
			}
		}},
		{Cmd: darwin.Sel("webView:createWebViewWithConfiguration:forNavigationAction:windowFeatures:"),
			Fn: func(self id, _ sel, _, cfg, _, features id) id {
				if v := web(self); v != nil {
					return v.openPopup(cfg, features)
				}
				return 0
			}},
		{Cmd: darwin.Sel("webViewDidClose:"), Fn: func(self id, _ sel, wk id) {
			if v := web(self); v != nil {
				if i := v.popupOf(wk, 0); i >= 0 {
					darwin.Send(v.popups[i].win, "close")
				}
			}
		}},
		{Cmd: darwin.Sel("windowWillClose:"), Fn: func(self id, _ sel, note id) {
			if v := byWebDelegate[self]; v != nil {
				if i := v.popupOf(0, darwin.Send(note, "object")); i >= 0 {
					v.closePopup(i)
				}
			}
		}},
		{Cmd: darwin.Sel("webView:runJavaScriptAlertPanelWithMessage:initiatedByFrame:completionHandler:"),
			Fn: func(self id, _ sel, _, msg, _, handler id) {
				v := web(self)
				if v == nil {
					callBlock(handler)
					return
				}
				h := held(handler)
				v.panel(darwin.GoString(msg), []string{"OK"}, nil, func(bool, string) {
					callBlock(h)
					objc.Block(h).Release()
				})
			}},
		{Cmd: darwin.Sel("webView:runJavaScriptConfirmPanelWithMessage:initiatedByFrame:completionHandler:"),
			Fn: func(self id, _ sel, _, msg, _, handler id) {
				v := web(self)
				if v == nil {
					callBlock(handler, 0)
					return
				}
				h := held(handler)
				v.panel(darwin.GoString(msg), []string{"OK", "Cancel"}, nil, func(ok bool, _ string) {
					callBlock(h, boolWord(ok))
					objc.Block(h).Release()
				})
			}},
		{Cmd: darwin.Sel("webView:runJavaScriptTextInputPanelWithPrompt:defaultText:initiatedByFrame:completionHandler:"),
			Fn: func(self id, _ sel, _, prompt, def, _, handler id) {
				v := web(self)
				if v == nil {
					callBlock(handler, 0)
					return
				}
				h := held(handler)
				text := darwin.GoString(def)
				v.panel(darwin.GoString(prompt), []string{"OK", "Cancel"}, &text, func(ok bool, text string) {
					var s id
					if ok {
						s = darwin.String(text)
					}
					callBlock(h, uintptr(s))
					objc.Block(h).Release()
				})
			}},
	})
	if err != nil {
		panic(err)
	}
	return c
})

func boolWord(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}
