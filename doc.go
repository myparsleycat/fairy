// Package fairy is a native GUI framework for Windows, written in Go without
// cgo. It renders with Direct3D 11, lays text out with DirectWrite and lays
// elements out with a flexbox engine.
//
// An app describes its window with a View function that builds a fresh
// element tree from the app's state:
//
//	s := &state{}
//	fairy.Run(fairy.Options{
//		Title: "Hello",
//		View: func(cx *fairy.Cx) *fairy.El {
//			return fairy.Col(
//				fairy.Text(fmt.Sprintf("Clicked %d times", s.count)),
//				fairy.Text("+1").Pad(8).OnClick(func() { s.count++ }),
//			)
//		},
//	})
//
// The view is built again after every event handler. Hover, scroll, focus and
// editing state is kept by element identity: an element's position in the
// tree and its Key.
//
// # Threads
//
// Handlers and views run on the UI thread and may change app state directly.
// Other goroutines hand work to the UI thread in one of three ways:
//   - Window.Post runs a function and then rebuilds that window's view.
//   - Go runs a function without waiting and then updates every window.
//   - Do runs a function and waits for it to return.
//
// Windows, tray icons and menus are created on the UI thread. Run opens one
// window and returns when it closes; Main runs until Quit or until no window
// and no tray icon is left.
//
// # Related packages
//
// The kit package provides themed components. The layout, motion, svg,
// keyboard and i18n packages hold the flexbox engine, animation curves, SVG
// vectors, keyboard values and a message catalog. The d3d package binds
// Direct3D 11 for apps that draw their own 3D content in a Surface. The
// dialog, tray and shortcut packages provide native file dialogs and message
// boxes, notification area icons and global keyboard shortcuts.
//
// # Diagnostics
//
// FAIRY_DEBUG_FRAMES=1 logs each frame's time by phase, FAIRY_DEBUG_GPU=1 adds
// GPU timing, and FAIRY_DEBUG_SLOW=8 logs work that holds the UI thread longer
// than 8 ms. Builds with -tags fairydebug also read FAIRY_SCRIPT, FAIRY_CLOCK,
// FAIRY_SNAPSHOT and FAIRY_SNAPSHOT_DELAY to drive and capture windows in tests.
package fairy
