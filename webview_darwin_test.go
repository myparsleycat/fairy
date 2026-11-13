package fairy

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/myparsleycat/fairy/internal/darwin"
)

// waitFor polls cond on the UI thread until it holds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		ok := false
		Do(func() { ok = cond() })
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// runScript runs js in v and waits for its result.
func runScript(t *testing.T, v *WebView, js string) (string, error) {
	t.Helper()
	type result struct {
		s   string
		err error
	}
	ch := make(chan result, 1)
	Do(func() { v.ExecuteScript(js, func(s string, err error) { ch <- result{s, err} }) })
	select {
	case r := <-ch:
		return r.s, r.err
	case <-time.After(15 * time.Second):
		t.Fatalf("ExecuteScript(%q) did not finish", js)
		return "", nil
	}
}

// getCookies reads v's cookies for uri.
func getCookies(t *testing.T, v *WebView, uri string) []Cookie {
	t.Helper()
	ch := make(chan []Cookie, 1)
	Do(func() {
		v.Cookies(uri, func(c []Cookie, err error) {
			if err != nil {
				t.Error(err)
			}
			ch <- c
		})
	})
	select {
	case c := <-ch:
		return c
	case <-time.After(15 * time.Second):
		t.Fatal("Cookies did not finish")
		return nil
	}
}

func cookieNames(cs []Cookie) []string {
	var names []string
	for _, c := range cs {
		names = append(names, c.Name)
	}
	slices.Sort(names)
	return names
}

const testPage = `<!doctype html><html><head><title>%s</title></head><body>
<input id="in" value="hello">
<script>
chrome.webview.addEventListener('message', e => chrome.webview.postMessage('echo:' + e.data));
chrome.webview.postMessage('hello from ' + document.title);
chrome.webview.postMessage({not: 'a string'});
document.cookie = 'js=1; path=/';
</script></body></html>`

func testServer(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/":
			http.SetCookie(rw, &http.Cookie{Name: "srv", Value: "2", Path: "/", HttpOnly: true})
			fmt.Fprintf(rw, testPage, "First")
		case "/second":
			fmt.Fprintf(rw, testPage, "Second")
		case "/csp":
			rw.Header().Set("Content-Security-Policy", "script-src 'none'")
			fmt.Fprint(rw, `<!doctype html><title>CSP</title><p>no scripts</p>`)
		default:
			http.NotFound(rw, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A WKWebView loads pages, runs scripts with JSON results, passes messages
// both ways through window.chrome.webview, keeps cookies, follows its
// element's box, and hides when the element leaves the tree.
func TestDarwinWebView(t *testing.T) {
	srv := testServer(t)
	var (
		msgs      []string
		titles    []string
		navigated []string
		ready     bool
		shown     = true
		value     = ""
	)
	v := NewWebView(WebViewOptions{
		URL:         srv.URL + "/",
		OnReady:     func() { ready = true },
		OnMessage:   func(m string) { msgs = append(msgs, m) },
		OnTitle:     func(s string) { titles = append(titles, s) },
		OnNavigated: func(u string, ok bool) { navigated = append(navigated, fmt.Sprint(u, " ", ok)) },
		OnError:     func(err error) { t.Error(err) },
	})
	v.ephemeral = true
	w := newTestWindow(t, func() *El {
		col := Col(Input(value, func(s string) { value = s }).Key("in").Size(100, 24))
		if shown {
			col.Child(WebViewHost(v).Key("web").Size(200, 120))
		}
		return col.Pad(8)
	})
	waitFor(t, "ready and loaded", func() bool { return ready && v.Title() == "First" && len(navigated) > 0 })
	Do(func() {
		if u := v.URL(); u != srv.URL+"/" {
			t.Errorf("URL %q", u)
		}
		if navigated[0] != srv.URL+"/ true" {
			t.Errorf("OnNavigated %q", navigated)
		}
		if !slices.Contains(titles, "First") {
			t.Errorf("OnTitle %q", titles)
		}
		if v.Err() != nil {
			t.Error(v.Err())
		}
		// The frame is the element's box, in points of the flipped view.
		e := w.findKey("web")
		f := darwin.Get[darwin.CGRect](v.wk, "frame")
		want := darwin.Rect(float64(e.absX), float64(e.absY), float64(e.w), float64(e.h))
		if f != want || want.Size.W != 200 || want.Size.H != 120 {
			t.Errorf("frame %v, want %v", f, want)
		}
		if darwin.Send(v.wk, "superview") != w.view || darwin.Get[bool](v.wk, "isHidden") {
			t.Error("the web view is not shown in the window's view")
		}
	})

	// Messages from the page, and back.
	waitFor(t, "messages from the page", func() bool { return len(msgs) >= 2 })
	Do(func() {
		if msgs[0] != "hello from First" || msgs[1] != "" {
			t.Errorf("messages %q", msgs)
		}
		v.PostMessage(`ping "quoted" ` + " ")
	})
	waitFor(t, "echo", func() bool { return len(msgs) >= 3 })
	Do(func() {
		if msgs[2] != `echo:ping "quoted" `+" " {
			t.Errorf("echo %q", msgs[2])
		}
	})

	// Script results are JSON, as WebView2's.
	for js, want := range map[string]string{
		"1 + 2":                          "3",
		"'a' + 'b'":                      `"ab"`,
		"({a: 1, b: [true, null, 'x']})": `{"a":1,"b":[true,null,"x"]}`,
		"var x = 5; x * 2":               "10",
		"undefined":                      "null",
		"throw new Error('boom')":        "null",
		"syntax error here (":            "null",
		"document.title":                 `"First"`,
		"(function () {})":               "null",
	} {
		got, err := runScript(t, v, js)
		if err != nil || got != want {
			t.Errorf("ExecuteScript(%q) = %q, %v; want %q", js, got, err, want)
		}
	}
	if got, _ := runScript(t, v, "x"); got != "5" {
		t.Errorf("a var of an earlier script is %q", got)
	}

	// Cookies from the server and from the page.
	var names []string
	for range 200 {
		if names = cookieNames(getCookies(t, v, srv.URL+"/")); len(names) == 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !slices.Equal(names, []string{"js", "srv"}) {
		t.Fatalf("cookies %q", names)
	}
	for _, c := range getCookies(t, v, "") {
		if c.Name == "srv" && (!c.HTTPOnly || c.Value != "2" || c.Path != "/" || c.Domain != "127.0.0.1" || !c.Expires.IsZero()) {
			t.Errorf("server cookie %+v", c)
		}
	}
	if cs := getCookies(t, v, "http://other.example/"); len(cs) != 0 {
		t.Errorf("cookies for another host %+v", cs)
	}
	Do(func() { v.DeleteCookies("js", srv.URL+"/") })
	for range 200 {
		if names = cookieNames(getCookies(t, v, srv.URL+"/")); len(names) == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !slices.Equal(names, []string{"srv"}) {
		t.Errorf("after DeleteCookies: %q", names)
	}
	Do(v.DeleteAllCookies)
	for range 200 {
		if names = cookieNames(getCookies(t, v, "")); len(names) == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(names) != 0 {
		t.Errorf("after DeleteAllCookies: %q", names)
	}

	// History.
	Do(func() { v.Navigate(srv.URL + "/second") })
	waitFor(t, "the second page", func() bool { return v.Title() == "Second" })
	Do(func() {
		if !v.CanGoBack() || v.URL() != srv.URL+"/second" {
			t.Errorf("CanGoBack %v, URL %q", v.CanGoBack(), v.URL())
		}
		v.GoBack()
	})
	waitFor(t, "going back", func() bool { return v.Title() == "First" })
	n := 0
	Do(func() { n = len(navigated); v.Reload() })
	waitFor(t, "reload", func() bool { return len(navigated) > n })

	// A page whose content security policy forbids eval.
	Do(func() { v.Navigate(srv.URL + "/csp") })
	waitFor(t, "the CSP page", func() bool { return v.Title() == "CSP" })
	for js, want := range map[string]string{
		"document.title":    `"CSP"`,
		"({a: [1, 'x/y']})": `{"a":[1,"x/y"]}`,
		"throw 1":           "null",
	} {
		got, err := runScript(t, v, js)
		if err != nil || got != want {
			t.Errorf("CSP page: ExecuteScript(%q) = %q, %v; want %q", js, got, err, want)
		}
	}

	// Removing the element hides the web view; placing it shows it again.
	Do(func() {
		shown = false
		w.Update()
		w.renderFrame()
		if !darwin.Get[bool](v.wk, "isHidden") || v.visible {
			t.Error("the web view stays shown without its element")
		}
		shown = true
		w.Update()
		w.renderFrame()
		if darwin.Get[bool](v.wk, "isHidden") {
			t.Error("the web view stays hidden with its element back")
		}
	})

	// Close releases it.
	Do(func() {
		v.Close()
		if v.Ready() || v.wk != 0 {
			t.Error("ready after Close")
		}
		if n := darwin.Get[uint64](darwin.Send(w.view, "subviews"), "count"); n != 0 {
			t.Errorf("%d subviews after Close", n)
		}
		if len(byWebDelegate) != 0 {
			t.Error("the delegate is still registered")
		}
		w.renderFrame()
		if v.wk != 0 || v.creating {
			t.Error("a closed web view was made again")
		}
	})
	if _, err := runScript(t, v, "1"); !errors.Is(err, errWebViewClosed) {
		t.Errorf("ExecuteScript after Close: %v", err)
	}
}

// Keys go to a focused web view and not to the window's elements; a press
// outside it gives the window's view the keyboard back. The edit shortcuts
// the page does not take run their commands in the web view.
func TestDarwinWebViewFocus(t *testing.T) {
	srv := testServer(t)
	value := ""
	v := NewWebView(WebViewOptions{URL: srv.URL + "/"})
	v.ephemeral = true
	w := newTestWindow(t, func() *El {
		return Col(
			Input(value, func(s string) { value = s }).Key("in").Size(100, 24),
			WebViewHost(v).Size(200, 120),
		)
	})
	Do(func() {
		w.Focus("in")
		w.renderFrame()
	})
	waitFor(t, "loaded", func() bool { return v.Title() == "First" })
	Do(func() {
		if darwin.Send(w.nsWindow, "firstResponder") != w.view {
			t.Fatal("the web view took the keyboard while loading")
		}
		w.osFocus = true // as in the key window
	})
	press := func(code uint16, ch rune, flags uint64) {
		Do(func() { darwin.WithPool(func() { w.keyDown(keyEvent(code, ch, flags)) }) })
	}
	// Focusing an element of the page gives the web view the keyboard, and
	// takes focus from the window's elements.
	if s, err := runScript(t, v, "var i = document.getElementById('in'); i.focus(); i.setSelectionRange(1, 1); i.selectionEnd"); err != nil || s != "1" {
		t.Fatal(s, err)
	}
	waitFor(t, "the web view taking the keyboard", func() bool { return w.focusedWebView() == v })
	Do(func() {
		if w.osFocus {
			t.Error("the window's elements kept focus when the web view took the keyboard")
		}
	})
	press(0x07, 'x', 0)
	if value != "" {
		t.Errorf("a key for the web view typed %q into the window's Input", value)
	}
	press(0x00, 'a', nsCommand)
	sel := ""
	for range 200 {
		sel, _ = runScript(t, v, "var i = document.getElementById('in'); i.selectionStart + ',' + i.selectionEnd")
		if sel == `"0,5"` {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if sel != `"0,5"` {
		t.Errorf("⌘A selected %s in the page's input", sel)
	}

	// A press on the window outside the web view takes the keyboard back.
	Do(func() {
		darwin.WithPool(func() {
			h := darwin.Get[darwin.CGRect](w.view, "bounds").Size.H
			ev := darwin.SendClass("NSEvent",
				"mouseEventWithType:location:modifierFlags:timestamp:windowNumber:context:eventNumber:clickCount:pressure:",
				uint64(1), darwin.CGPoint{X: 300, Y: h - 10}, uint64(0), 0.0,
				darwin.Get[int64](w.nsWindow, "windowNumber"), darwin.ID(0), int64(0), int64(1), float32(1))
			w.mouseDown(ev, 0)
			w.mouseUp(ev, 0)
		})
		if w.focusedWebView() != nil || darwin.Send(w.nsWindow, "firstResponder") != w.view {
			t.Error("a press outside the web view left it the keyboard")
		}
		w.Focus("in")
	})
	press(0x07, 'x', 0)
	if value != "x" {
		t.Errorf("the window's Input got %q after taking the keyboard back", value)
	}
}

func TestCookieMatcher(t *testing.T) {
	c := func(domain, path string, secure bool) Cookie {
		return Cookie{Domain: domain, Path: path, Secure: secure}
	}
	for _, tc := range []struct {
		uri  string
		c    Cookie
		want bool
	}{
		{"http://a.example/x", c("a.example", "/", false), true},
		{"http://b.a.example/x", c("a.example", "/", false), false},
		{"http://b.a.example/x", c(".a.example", "/", false), true},
		{"http://a.example/x", c(".a.example", "/", false), true},
		{"http://ba.example/x", c(".a.example", "/", false), false},
		{"http://a.example/docs/1", c("a.example", "/docs", false), true},
		{"http://a.example/docsx", c("a.example", "/docs", false), false},
		{"http://a.example/docs", c("a.example", "/docs/", false), false},
		{"http://a.example/", c("a.example", "/", true), false},
		{"https://a.example/", c("a.example", "/", true), true},
		{"", c("anything", "/x", true), true},
	} {
		m, err := cookieMatcher(tc.uri)
		if err != nil {
			t.Fatal(err)
		}
		if got := m(tc.c); got != tc.want {
			t.Errorf("%s with %+v: %v", tc.uri, tc.c, got)
		}
	}
	if !strings.Contains(bridgeScript, messageHandler) {
		t.Error("the bridge script does not use the message handler")
	}
}

// File pages load with the files next to them; a failed navigation reports
// ok false; web views with the same UserDataFolder share a data store.
func TestDarwinWebViewFilesAndStores(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "page.html"), []byte(`<title>File</title><script src="a.js"></script>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.js"), []byte(`document.title += ' with script'`), 0o644); err != nil {
		t.Fatal(err)
	}
	var navs []bool
	v := NewWebView(WebViewOptions{
		URL:         (&url.URL{Scheme: "file", Path: filepath.Join(dir, "page.html")}).String(),
		OnNavigated: func(_ string, ok bool) { navs = append(navs, ok) },
	})
	v.ephemeral = true
	newTestWindow(t, func() *El { return WebViewHost(v).Size(200, 120) })
	waitFor(t, "the file page", func() bool { return v.Title() == "File with script" })

	// Nothing listens on port 1.
	Do(func() { navs = nil; v.Navigate("http://127.0.0.1:1/") })
	waitFor(t, "a failed navigation", func() bool { return len(navs) > 0 })
	Do(func() {
		if navs[0] {
			t.Error("a failed navigation reported ok")
		}
		// Same folder, same store; another folder, another store.
		a := &WebView{opts: WebViewOptions{UserDataFolder: "fairy-test-profile"}}
		b := &WebView{opts: WebViewOptions{UserDataFolder: "./fairy-test-profile"}}
		c := &WebView{opts: WebViewOptions{UserDataFolder: "fairy-test-profile-2"}}
		sa, sb, sc := a.dataStore(), b.dataStore(), c.dataStore()
		if !darwin.Get[bool](darwin.ID(darwin.Cls("WKWebsiteDataStore")), "respondsToSelector:", darwin.Sel("dataStoreForIdentifier:")) {
			return
		}
		ida, idb, idc := darwin.Send(sa, "identifier"), darwin.Send(sb, "identifier"), darwin.Send(sc, "identifier")
		if !darwin.Get[bool](ida, "isEqual:", idb) || darwin.Get[bool](ida, "isEqual:", idc) {
			t.Errorf("store identifiers %s %s %s", darwin.GoString(darwin.Send(ida, "UUIDString")),
				darwin.GoString(darwin.Send(idb, "UUIDString")), darwin.GoString(darwin.Send(idc, "UUIDString")))
		}
		if !darwin.Get[bool](sa, "isPersistent") {
			t.Error("a UserDataFolder store is not persistent")
		}
	})
}

// alert, confirm and prompt answer Cancel at once in a hidden window.
func TestDarwinWebViewDialogsHidden(t *testing.T) {
	v := NewWebView(WebViewOptions{URL: "about:blank"})
	v.ephemeral = true
	newTestWindow(t, func() *El { return WebViewHost(v).Size(200, 120) })
	waitFor(t, "ready", v.Ready)
	for js, want := range map[string]string{
		"alert('hi'); 1":       "1",
		"confirm('sure?')":     "false",
		"prompt('name?', 'x')": "null",
	} {
		if got, err := runScript(t, v, js); err != nil || got != want {
			t.Errorf("%s = %q, %v; want %q", js, got, err, want)
		}
	}
}

// alert, confirm and prompt show a sheet whose answer goes back to the
// page. The sheet shows the window, so the test runs only with
// FAIRY_TEST_SHEETS=1.
func TestDarwinWebViewDialogs(t *testing.T) {
	if os.Getenv("FAIRY_TEST_SHEETS") != "1" {
		t.Skip("set FAIRY_TEST_SHEETS=1 to show sheets")
	}
	v := NewWebView(WebViewOptions{URL: "about:blank"})
	v.ephemeral = true
	w := newTestWindow(t, func() *El { return WebViewHost(v).Size(200, 120) })
	waitFor(t, "ready", v.Ready)
	Do(func() { darwin.Send(w.nsWindow, "orderBack:", darwin.ID(0)) })
	answer := func(js string, code int64, text string) string {
		t.Helper()
		ch := make(chan string, 1)
		Do(func() { v.ExecuteScript(js, func(s string, err error) { ch <- fmt.Sprint(s, err) }) })
		var sheet darwin.ID
		waitFor(t, "the sheet of "+js, func() bool {
			sheet = darwin.Send(w.nsWindow, "attachedSheet")
			return sheet != 0
		})
		Do(func() {
			if text != "" {
				field := darwin.Send(sheet, "initialFirstResponder")
				darwin.Send(field, "setStringValue:", darwin.String(text))
			}
			darwin.Send(w.nsWindow, "endSheet:returnCode:", sheet, code)
		})
		select {
		case s := <-ch:
			return s
		case <-time.After(15 * time.Second):
			t.Fatalf("%s did not finish", js)
			return ""
		}
	}
	if got := answer("alert('hi'); 1", 1000, ""); got != "1<nil>" {
		t.Errorf("alert: %q", got)
	}
	if got := answer("confirm('sure?')", 1000, ""); got != "true<nil>" {
		t.Errorf("confirm OK: %q", got)
	}
	if got := answer("confirm('sure?')", 1001, ""); got != "false<nil>" {
		t.Errorf("confirm Cancel: %q", got)
	}
	if got := answer("prompt('name?', 'x')", 1000, "fairy"); got != `"fairy"<nil>` {
		t.Errorf("prompt OK: %q", got)
	}
	if got := answer("prompt('name?', 'x')", 1001, "fairy"); got != "null<nil>" {
		t.Errorf("prompt Cancel: %q", got)
	}
}

// window.open opens a window with a web view sharing the opener's
// configuration, as WebView2 does; the page can close it, closing it frees
// it, and closing the opener closes what it opened.
func TestDarwinWebViewPopups(t *testing.T) {
	srv := testServer(t)
	ready := false
	v := NewWebView(WebViewOptions{URL: srv.URL + "/", OnReady: func() { ready = true }, OnError: func(err error) { t.Error(err) }})
	v.ephemeral = true
	newTestWindow(t, func() *El { return WebViewHost(v).Size(200, 120) })
	waitFor(t, "loaded", func() bool { return ready && v.Title() == "First" })

	if _, err := runScript(t, v, `window.p = window.open('/second', '_blank', 'width=320,height=240'); 1`); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "a popup", func() bool { return len(v.popups) == 1 })
	var popup webPopup
	Do(func() { popup = v.popups[0] })
	waitFor(t, "the popup's page", func() bool { return darwin.GoString(darwin.Send(popup.win, "title")) == "Second" })
	Do(func() {
		r := darwin.Get[darwin.CGRect](popup.wk, "frame")
		if r.Size.W != 320 || r.Size.H != 240 {
			t.Errorf("popup web view %v, want 320x240", r.Size)
		}
		if !darwin.Get[bool](popup.win, "isVisible") {
			t.Error("popup window not shown")
		}
	})
	// The opener reaches the popup's page, which shares its cookies.
	if got, err := runScript(t, v, `window.p.document.title`); err != nil || got != `"Second"` {
		t.Errorf("opener sees popup title %s, %v", got, err)
	}
	// The page closes it.
	if _, err := runScript(t, v, `window.p.close(); 1`); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the page to close the popup", func() bool { return len(v.popups) == 0 })

	// The user closes one.
	runScript(t, v, `window.open('/second'); 1`)
	waitFor(t, "a second popup", func() bool { return len(v.popups) == 1 })
	Do(func() { darwin.Send(v.popups[0].win, "performClose:", darwin.ID(0)) })
	waitFor(t, "the user to close the popup", func() bool { return len(v.popups) == 0 })

	// Closing the opener closes the rest.
	runScript(t, v, `window.open('/second'); window.open('/second'); 1`)
	waitFor(t, "two popups", func() bool { return len(v.popups) == 2 })
	var wins []darwin.ID
	Do(func() {
		for _, p := range v.popups {
			wins = append(wins, p.win)
		}
		v.Close()
		if len(v.popups) != 0 {
			t.Errorf("%d popups after Close", len(v.popups))
		}
		for _, w := range wins {
			if darwin.Get[bool](w, "isVisible") {
				t.Error("a popup is still shown after Close")
			}
		}
	})
}

func TestDarwinWebViewLocalReadScope(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "page")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"><rect width="1" height="1"/></svg>`)
	for _, path := range []string{filepath.Join(dir, "inside.svg"), filepath.Join(root, "outside.svg")} {
		if err := os.WriteFile(path, svg, 0600); err != nil {
			t.Fatal(err)
		}
	}
	html := `<html><head><title>Loading</title></head><body><script>
 window.results = {};
 for (const [name, src] of [['inside','inside.svg'],['outside','../outside.svg']]) {
  const img=new Image();
  img.onload=()=>window.results[name]=true;
  img.onerror=()=>window.results[name]=false;
  img.src=src;
 }
 </script></body></html>`
	path := filepath.Join(dir, "index.html")
	if err := os.WriteFile(path, []byte(html), 0600); err != nil {
		t.Fatal(err)
	}
	v := NewWebView(WebViewOptions{URL: (&url.URL{Scheme: "file", Path: path}).String()})
	v.ephemeral = true
	newTestWindow(t, func() *El { return WebViewHost(v).Size(200, 120) })
	waitFor(t, "local page", func() bool { return v.Ready() && v.URL() != "" })
	deadline := time.Now().Add(10 * time.Second)
	for {
		got, err := runScript(t, v, `JSON.stringify(window.results)`)
		if err == nil && strings.Contains(got, "inside") && strings.Contains(got, "outside") {
			if got != `"{\"inside\":true,\"outside\":false}"` && got != `"{\"outside\":false,\"inside\":true}"` {
				t.Fatalf("read scope: %s", got)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("local resources did not finish: %s %v", got, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestDarwinWebViewPopupMessages(t *testing.T) {
	srv := testServer(t)
	var messages []string
	v := NewWebView(WebViewOptions{URL: srv.URL + "/", OnMessage: func(s string) { messages = append(messages, s) }})
	v.ephemeral = true
	newTestWindow(t, func() *El { return WebViewHost(v).Size(200, 120) })
	waitFor(t, "opener", func() bool { return v.Title() == "First" })
	if _, err := runScript(t, v, `window.p=window.open('/second'); 1`); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "popup loaded", func() bool {
		return len(v.popups) == 1 && darwin.GoString(darwin.Send(v.popups[0].wk, "title")) == "Second"
	})
	if _, err := runScript(t, v, `window.p.chrome.webview.postMessage('popup-only'); chrome.webview.postMessage('parent\u0000message'); 1`); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "parent message", func() bool { return slices.Contains(messages, "parent\x00message") })
	Do(func() {
		if slices.Contains(messages, "popup-only") {
			t.Error("popup reached opener OnMessage")
		}
	})
}
