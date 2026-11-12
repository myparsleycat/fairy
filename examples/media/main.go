//go:build windows

// Command media shows the native surfaces: a Direct3D surface, video
// playback and an embedded WebView2 browser.
//
//	go run ./examples/media [video file or URL]
package main

import (
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"
)

// clearView fills the surface target, standing in for an app's own Direct3D
// renderer.
func clearView(f fairy.SurfaceFrame, rgba [4]float32) {
	_, rtv := f.RenderTarget()
	f.GPU().Context.ClearRenderTargetView(rtv, rgba)
}

const page = `<!doctype html><html><head><title>Embedded page</title></head>
<body style="font-family:sans-serif;background:#f4f4f5">
<h2>WebView2</h2><p id="t">Loaded.</p>
<button onclick="chrome.webview.postMessage('clicked at ' + new Date().toLocaleTimeString())">Send message</button>
<script>
chrome.webview.addEventListener('message', e => document.getElementById('t').textContent = 'From Go: ' + e.data);
chrome.webview.postMessage('hello from the page');
addEventListener('resize', () => chrome.webview.postMessage('viewport ' + innerWidth + 'x' + innerHeight + ' @' + devicePixelRatio));
</script></body></html>`

func main() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, page)
	}))
	url := "http://" + ln.Addr().String() + "/"

	var video *fairy.Video
	var status, lastMsg string
	web := fairy.NewWebView(fairy.WebViewOptions{
		URL:       url,
		OnMessage: func(m string) { lastMsg = m },
		OnError:   func(err error) { status = err.Error() },
	})

	err = fairy.Main(func() error {
		if len(os.Args) > 1 {
			video, err = fairy.OpenVideo(os.Args[1], fairy.VideoOptions{AutoPlay: true, Loop: true, Muted: true,
				OnError: func(err error) { status = err.Error(); log.Print(err) }})
			if err != nil {
				return err
			}
		}
		_, err := fairy.NewWindow(fairy.Options{Title: "Media", Width: 1100, Height: 760, View: func(cx *fairy.Cx) *fairy.El {
			kit.Use(kit.Light())
			t := kit.T()
			surface := fairy.Surface(func(f fairy.SurfaceFrame) bool {
				s := f.Time.Seconds()
				clearView(f, [4]float32{
					float32(0.5 + 0.5*math.Sin(s)), float32(0.5 + 0.5*math.Sin(s+2)), float32(0.5 + 0.5*math.Sin(s+4)), 1,
				})
				return true
			}).Grow(1).Radius(12)

			var player *fairy.El
			if video != nil {
				pos, dur := video.Position().Truncate(time.Second/10), video.Duration().Truncate(time.Second/10)
				icon := lucide.Play
				if video.Playing() {
					icon = lucide.Pause
				}
				w, h := video.Size()
				player = fairy.Col(
					fairy.VideoView(video).Grow(1).Radius(12).Fill(fairy.Hex(0x000000)),
					fairy.Row(
						kit.IconButton(icon, func() {
							if video.Playing() {
								video.Pause()
							} else {
								video.Play()
							}
						}, kit.Outline),
						fairy.Text(fmt.Sprintf("%v / %v  ·  %dx%d", pos, dur, w, h)).Color(t.MutedForeground),
					).Gap(10).Align(fairy.Center),
				).Gap(8).Grow(1)
			} else {
				player = fairy.Box(fairy.Text("Pass a video path to play it.").Color(t.MutedForeground)).Grow(1).Center().
					Radius(12).Border(1, t.Border)
			}

			return fairy.Col(
				fairy.Row(
					fairy.Col(fairy.Text("Surface").SemiBold(), surface).Gap(8).Grow(1).Basis(0),
					fairy.Col(fairy.Text("Video").SemiBold(), player).Gap(8).Grow(1).Basis(0),
				).Gap(16).H(300),
				fairy.Row(
					fairy.Text("WebView2").SemiBold(), fairy.Spacer(),
					fairy.Text(lastMsg).Color(t.MutedForeground),
					kit.Button("Post message", func() { web.PostMessage("ping " + time.Now().Format("15:04:05")) }, kit.Outline, kit.Small),
					kit.Button("Reload", web.Reload, kit.Outline, kit.Small),
				).Gap(10).Align(fairy.Center),
				fairy.Text(status).Color(t.Destructive),
				fairy.Box(fairy.WebViewHost(web).Grow(1)).Grow(1).Border(1, t.Border).Radius(8).Pad(1),
			).Gap(12).Pad(20).Fill(t.Background).Color(t.Foreground)
		}})
		return err
	})
	if err != nil {
		log.Fatal(err)
	}
}
