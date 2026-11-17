// Package webview embeds Edge WebView2 browsers in fairy windows, without
// WebView2Loader.dll.
package webview

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/internal/com"
	"github.com/myparsleycat/fairy/internal/embed"
	"github.com/myparsleycat/fairy/internal/webview2"
	"github.com/myparsleycat/fairy/internal/win"
)

// Options configures New.
type Options struct {
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

// WebView is an Edge WebView2 browser shown as a child window. It is created
// when Host first places it in a window. Native windows cannot be
// drawn under other elements, so popovers and overlays never cover it, and
// it is not clipped by scroll containers. Use it from the UI thread.
type WebView struct {
	opts     Options
	host     *fairy.Window
	hwnd     win.HWND
	creating bool
	ctl      unsafe.Pointer // ICoreWebView2Controller
	view     unsafe.Pointer // ICoreWebView2
	err      error
	bounds   [4]int32
	visible  bool
	pending  string // URL to navigate to once ready
	title    string
	closed   bool
}

// New returns a web view that is created when first shown.
func New(opts Options) *WebView {
	return &WebView{opts: opts, pending: opts.URL}
}

// Host reserves the area v is shown in. Size it like a box. A nil v shows
// nothing.
func Host(v *WebView) *fairy.El {
	var x embed.Element
	if v != nil {
		x = (*element)(v)
	}
	return embed.NewEl(x).(*fairy.El)
}

var errClosed = errors.New("fairy: web view closed")

type envKey struct{ folder, args string }

type envEntry struct {
	env     unsafe.Pointer
	err     error
	waiting []func(unsafe.Pointer, error)
}

var environments = map[envKey]*envEntry{}

func defaultUserDataFolder() string {
	exe, _ := os.Executable()
	name := strings.TrimSuffix(filepath.Base(exe), filepath.Ext(exe))
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, name, "WebView2")
}

func environment(folder, args string, done func(unsafe.Pointer, error)) {
	k := envKey{folder, args}
	if e := environments[k]; e != nil {
		if e.env != nil || e.err != nil {
			done(e.env, e.err)
		} else {
			e.waiting = append(e.waiting, done)
		}
		return
	}
	e := &envEntry{waiting: []func(unsafe.Pointer, error){done}}
	environments[k] = e
	finish := func(env unsafe.Pointer, err error) {
		e.env, e.err = env, err
		if err != nil {
			delete(environments, k) // allow a retry later
		}
		for _, f := range e.waiting {
			f(env, err)
		}
		e.waiting = nil
	}
	if err := os.MkdirAll(folder, 0o755); err != nil {
		finish(nil, err)
		return
	}
	if err := webview2.CreateEnvironment(webview2.Options{UserDataFolder: folder, BrowserArgs: args}, finish); err != nil {
		finish(nil, err)
	}
}

func (v *WebView) create() {
	v.creating = true
	folder := v.opts.UserDataFolder
	if folder == "" {
		folder = defaultUserDataFolder()
	}
	environment(folder, v.opts.BrowserArgs, func(env unsafe.Pointer, err error) {
		if err != nil {
			v.fail(err)
			return
		}
		if v.closed {
			return
		}
		h := com.NewHandler(com.GUID{}, func(a, b, _ uintptr) uintptr {
			if hr := com.HRESULT(int32(a)); hr.Failed() || b == 0 {
				v.fail(hr.Err("fairy: create web view"))
				return 0
			}
			ctl := com.Ptr(b)
			com.AddRef(ctl)
			v.ready(ctl)
			return 0
		})
		defer com.Release(h)
		if hr := com.HR(env, 3, uintptr(v.hwnd), uintptr(h)); hr.Failed() {
			v.fail(hr.Err("fairy: CreateCoreWebView2Controller"))
		}
	})
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

func (v *WebView) ready(ctl unsafe.Pointer) {
	v.creating = false
	if v.closed {
		com.Call(ctl, 24) // Close
		com.Release(ctl)
		return
	}
	v.ctl = ctl
	v.view = webview2.Object(ctl, 25)              // get_CoreWebView2
	if s := webview2.Object(v.view, 3); s != nil { // get_Settings
		com.HR(s, 12, boolArg(v.opts.DevTools)) // put_AreDevToolsEnabled
		com.HR(s, 10, 0)                        // put_IsStatusBarEnabled
		com.Release(s)
	}
	v.on(15, func(args unsafe.Pointer) { // add_NavigationCompleted
		ok := webview2.Bool(args, 3)
		if v.opts.OnNavigated != nil {
			v.opts.OnNavigated(v.URL(), ok)
		}
		v.update()
	})
	v.on(46, func(unsafe.Pointer) { // add_DocumentTitleChanged
		v.title = webview2.String(v.view, 48)
		if v.opts.OnTitle != nil {
			v.opts.OnTitle(v.title)
		}
		v.update()
	})
	v.on(34, func(args unsafe.Pointer) { // add_WebMessageReceived
		if v.opts.OnMessage != nil {
			v.opts.OnMessage(webview2.String(args, 5)) // TryGetWebMessageAsString
		}
	})
	v.applyBounds()
	if v.pending != "" {
		v.Navigate(v.pending)
	}
	if v.opts.OnReady != nil {
		v.opts.OnReady()
	}
	v.update()
}

// on subscribes to the ICoreWebView2 event whose add_ method is at slot idx.
func (v *WebView) on(idx int, f func(args unsafe.Pointer)) {
	h := com.NewHandler(com.GUID{}, func(_, args, _ uintptr) uintptr {
		f(com.Ptr(args))
		return 0
	})
	var token int64
	com.HR(v.view, idx, uintptr(h), uintptr(unsafe.Pointer(&token)))
	com.Release(h)
}

func boolArg(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

func (v *WebView) applyBounds() {
	if v.ctl == nil {
		return
	}
	com.HR(v.ctl, 4, boolArg(v.visible)) // put_IsVisible
	if v.visible {
		r := v.bounds
		com.HR(v.ctl, 6, uintptr(unsafe.Pointer(&r))) // put_Bounds (RECT is passed by reference)
	}
}

// element is the embed.Native of a WebView, kept off the WebView's methods.
type element WebView

// Paint positions the browser over the element's content box.
func (e *element) Paint(c *embed.Context) {
	v := (*WebView)(e)
	if v.closed {
		return
	}
	if host := c.Window.(*fairy.Window); host != v.host {
		if v.ctl != nil {
			com.HR(v.ctl, 22, uintptr(c.HWND)) // put_ParentWindow
		}
		v.host, v.hwnd = host, win.HWND(c.HWND)
	}
	if v.ctl == nil && !v.creating && v.err == nil {
		v.create()
	}
	s, box := c.Scale, c.Box
	b := [4]int32{
		int32(math.Round(float64(box.X * s))), int32(math.Round(float64(box.Y * s))),
		int32(math.Round(float64((box.X + box.W) * s))), int32(math.Round(float64((box.Y + box.H) * s))),
	}
	if b != v.bounds || !v.visible {
		v.bounds, v.visible = b, true
		v.applyBounds()
	}
}

// Unplaced hides the browser when the window showing it painted a frame
// without it.
func (e *element) Unplaced(host any) {
	v := (*WebView)(e)
	if host == any(v.host) && v.visible {
		v.visible = false
		v.applyBounds()
	}
}

// Moved tells the browser that its window moved, so its popups (select
// lists, autofill) open in the right place.
func (e *element) Moved(host any) {
	v := (*WebView)(e)
	if host == any(v.host) && v.ctl != nil {
		com.HR(v.ctl, 23) // NotifyParentWindowPositionChanged
	}
}

// HostClosed closes the browser when the window showing it is destroyed.
func (e *element) HostClosed(host any) {
	v := (*WebView)(e)
	if host == any(v.host) {
		v.Close()
	}
}

// Ready reports whether the browser has been created.
func (v *WebView) Ready() bool { return v.view != nil }

// Err returns the error that prevented the browser from being created.
func (v *WebView) Err() error { return v.err }

// Navigate loads url.
func (v *WebView) Navigate(url string) {
	if v.view == nil {
		v.pending = url
		return
	}
	v.pending = ""
	p := webview2.WStr(url)
	com.HR(v.view, 5, uintptr(unsafe.Pointer(p)))
}

// URL returns the address of the current page.
func (v *WebView) URL() string {
	if v.view == nil {
		return v.pending
	}
	return webview2.String(v.view, 4)
}

// Title returns the document title.
func (v *WebView) Title() string { return v.title }

// Reload reloads the page.
func (v *WebView) Reload() {
	if v.view != nil {
		com.HR(v.view, 31)
	}
}

// CanGoBack reports whether there is a previous page.
func (v *WebView) CanGoBack() bool { return v.view != nil && webview2.Bool(v.view, 38) }

// GoBack goes to the previous page.
func (v *WebView) GoBack() {
	if v.view != nil {
		com.HR(v.view, 40)
	}
}

// ExecuteScript runs JavaScript in the page; done, if set, receives the
// result as JSON.
func (v *WebView) ExecuteScript(js string, done func(result string, err error)) {
	if v.view == nil {
		if done != nil {
			done("", errClosed)
		}
		return
	}
	h := com.NewHandler(com.GUID{}, func(a, b, _ uintptr) uintptr {
		if done != nil {
			if hr := com.HRESULT(int32(a)); hr.Failed() {
				done("", hr.Err("fairy: ExecuteScript"))
			} else {
				done(webview2.Borrowed(b), nil)
			}
		}
		return 0
	})
	defer com.Release(h)
	p := webview2.WStr(js)
	com.HR(v.view, 29, uintptr(unsafe.Pointer(p)), uintptr(h))
}

// PostMessage sends a string to the page's window.chrome.webview "message" listeners.
func (v *WebView) PostMessage(msg string) {
	if v.view != nil {
		p := webview2.WStr(msg)
		com.HR(v.view, 33, uintptr(unsafe.Pointer(p)))
	}
}

func (v *WebView) cookieManager() unsafe.Pointer {
	if v.view == nil {
		return nil
	}
	v2, err := com.QueryInterface(v.view, &webview2.IID_ICoreWebView2_2)
	if err != nil {
		return nil
	}
	defer com.Release(v2)
	return webview2.Object(v2, 66) // get_CookieManager
}

// Cookies returns the cookies that would be sent to uri, or all cookies for
// an empty uri. done runs on the UI thread.
func (v *WebView) Cookies(uri string, done func([]Cookie, error)) {
	cm := v.cookieManager()
	if cm == nil {
		done(nil, errClosed)
		return
	}
	defer com.Release(cm)
	h := com.NewHandler(com.GUID{}, func(a, b, _ uintptr) uintptr {
		if hr := com.HRESULT(int32(a)); hr.Failed() || b == 0 {
			done(nil, hr.Err("fairy: GetCookies"))
			return 0
		}
		list := com.Ptr(b)
		var n uint32
		com.HR(list, 3, uintptr(unsafe.Pointer(&n)))
		out := make([]Cookie, 0, n)
		for i := range n {
			var c unsafe.Pointer
			if com.HR(list, 4, uintptr(i), uintptr(unsafe.Pointer(&c))).Failed() || c == nil {
				continue
			}
			ck := Cookie{
				Name:     webview2.String(c, 3),
				Value:    webview2.String(c, 4),
				Domain:   webview2.String(c, 6),
				Path:     webview2.String(c, 7),
				HTTPOnly: webview2.Bool(c, 10),
				Secure:   webview2.Bool(c, 14),
			}
			var exp float64
			if com.HR(c, 8, uintptr(unsafe.Pointer(&exp))) == 0 && exp > 0 {
				ck.Expires = time.Unix(0, int64(exp*float64(time.Second)))
			}
			com.Release(c)
			out = append(out, ck)
		}
		done(out, nil)
		return 0
	})
	defer com.Release(h)
	p := webview2.WStr(uri)
	if hr := com.HR(cm, 5, uintptr(unsafe.Pointer(p)), uintptr(h)); hr.Failed() {
		done(nil, hr.Err("fairy: GetCookies"))
	}
}

// DeleteCookies deletes cookies called name that would be sent to uri.
func (v *WebView) DeleteCookies(name, uri string) {
	if cm := v.cookieManager(); cm != nil {
		n, u := webview2.WStr(name), webview2.WStr(uri)
		com.HR(cm, 8, uintptr(unsafe.Pointer(n)), uintptr(unsafe.Pointer(u)))
		com.Release(cm)
	}
}

// DeleteAllCookies deletes every cookie of the web view's profile.
func (v *WebView) DeleteAllCookies() {
	if cm := v.cookieManager(); cm != nil {
		com.HR(cm, 10)
		com.Release(cm)
	}
}

// Close destroys the browser. The web view cannot be shown again.
func (v *WebView) Close() {
	if v.closed {
		return
	}
	v.closed = true
	if v.ctl != nil {
		com.Call(v.ctl, 24) // Close
		com.Release(v.view)
		com.Release(v.ctl)
		v.ctl, v.view = nil, nil
	}
}
