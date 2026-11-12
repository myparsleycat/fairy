# Fairy guide

Detailed usage and development notes for [Fairy](../README.md). Run commands from the repository root.

## Features

- **Rendering**: Direct3D 11 with one instanced HLSL pipeline for rounded rectangles, per-side borders, gradients, Gaussian shadows, glyphs, images, arcs and wavy lines. Rounded clips (`Clip`), scroll edge fades (`ScrollEdgeFade`), offscreen layers for group opacity (`Isolate`), blur (`Blur`), frosted panels (`BackdropBlur`) and per-element affine transforms.
- **Text**: DirectWrite layout, shaping, font fallback and line breaking, grayscale antialiasing with DirectWrite gamma correction, color emoji. `RichText` mixes spans with their own font, size, weight, colors, decorations and click handlers in one paragraph.
- **Layout**: a flexbox engine in pure Go (`layout`) with wrap, grow, shrink, basis, min and max sizes, padding, margin, gap, alignment, absolute positioning and scroll containers.
- **Declarative API**: `View` builds a fresh element tree after every event. Hover, scroll, focus and editing state is kept by element identity. `Memo` reuses a subtree while its version is unchanged, and `Live` values redraw the elements bound to them without building the view again.
- **Input**: pointer, wheel and precision touchpad scrolling, middle-button autoscroll, drags with any button (`DragButtons`), keyboard focus and Tab order, focus groups with arrow-key and type-to-select navigation (`FocusGroup`), key chords (`keyboard.ParseChord`), global shortcuts (`shortcut.Register`).
- **Text input**: `Input` with selection, clipboard, undo, IME composition (Korean, Japanese, Chinese), spell checking with the Windows spell checker (`SpellCheck`), a native context menu, and handlers that run before it edits (`OnInputKey`, `OnPaste`). `Selectable` lets the user select and copy read-only text.
- **Windows**: per-monitor DPI v2, frameless windows with drag regions and native snap layouts (`MaximizeButton`), light and dark title bars (`SetColors`, `SystemDark`), Mica, Mica Alt and Acrylic backdrops on Windows 11 22H2 and later (`Options.Backdrop`, `BackdropSupported`).
- **Animation**: springs, ease-out and cubic-bezier curves (`motion`) for paint (`Animate`), position and size (`AnimateLayout`), shared-element transitions between views (`MatchID`), height changes (`AnimateHeight`, `AnimateRowHeights`), scale and rotation (`Scale`, `Rotate`, `PressScale`), repeating fades (`Pulse`), smooth scrolling and enter and exit transitions (`Enter`, `Exit`). Frames run only while something moves. With the system "Animation effects" setting off (`ReducedMotion`), movement finishes at once and transitions only fade.
- **Overlays**: popovers that flip to fit (`Popover`), tooltips (`SetTooltipStyle`), fading scroll bars (`SetScrollBarStyle`), modals and toasts (`Cx.Overlay`), Escape and outside-press dismissal, Tab confined to the top modal, and focus returned to the opener.
- **Virtualization**: `VirtualList`, `VirtualGrid` and `VirtualListMeasured` (rows of varying height) build only the rows in view. `Window.ScrollToRow`, `ScrollToRowTop`, `ScrollToRowInstant` and `ScrollToBottom` move to a row or the end, and `StickToBottom` follows growing content such as a log or chat.
- **Images**: PNG, JPEG, GIF, WebP and, with the Windows HEIF and AV1 extensions installed, still AVIF. `Images` decodes in the background at the requested size and keeps a byte-bounded LRU cache. Animated GIF and WebP play while drawn. `ImageCache.Blurred` loads blurred copies.
- **Icons and fonts**: SVG icons (`Icon`, `svg.Parse`), generated Lucide icons (`icons/lucide`), fonts registered from memory (`RegisterFont`).
- **Focus styles**: rings outside the border box (`Ring`, `HoverRing`) and styles shown only for keyboard focus, as CSS `:focus-visible` (`FocusRing`, `FocusVisibleBorder`, `FocusWithin`).
- **Component kit** (`kit`): themed controls with the sizes, colors and motion of shadcn/ui: buttons, switches, checkboxes, radios, sliders, text fields, selects, comboboxes, menus and a menu bar, a shortcut recorder, tabs, dialogs, toasts, a virtualized table, a split view with sidebar and inspector, a virtualized tree, a diff view (`kit/diff`), a date picker, a color picker, progress indicators and a Markdown renderer with syntax-highlighted code blocks.
- **Localization** (`i18n`): a message catalog that loads i18next JSON files, with interpolation, CLDR plural rules, Korean particles (`Josa`), number and list formatting, and matching of the system UI languages (`UseSystemLanguage`).
- **Native surfaces**: `Surface` elements drawn by the app with Direct3D 11, `VideoView` playing files and URLs through Media Foundation, and `WebViewHost` embedding Edge WebView2 without `WebView2Loader.dll`. All windows share one Direct3D device.
- **Offscreen rendering**: `RenderImage` draws an element tree into an `image.NRGBA` without a window. `SurfaceImage` and `NewTextureImage` turn rendered textures into `Image`s without a copy to memory.
- **Accessibility**: a UI Automation provider, so screen readers such as Narrator and UI test tools see the element tree. See [Accessibility](#accessibility).
- **Platform integration**: file dialogs, message boxes, file drop from Explorer (`OnFileDrop`), native popup menus, tray icons with notifications, single instance with launch forwarding, several windows, taskbar progress, clipboard images.

## Getting started

```go
s := &state{}
fairy.Run(fairy.Options{
	Title: "Hello",
	View: func(cx *fairy.Cx) *fairy.El {
		return fairy.Col(
			fairy.Text(fmt.Sprintf("Clicked %d times", s.count)).FontSize(20),
			fairy.Text("+1").PadXY(14, 6).Radius(6).
				Fill(fairy.Hex(0x3b82f6)).HoverFill(fairy.Hex(0x2563eb)).Color(fairy.Hex(0xffffff)).
				OnClick(func() { s.count++ }),
		).WFull().HFull().Center().Gap(12)
	},
})
```

Handlers run on the UI thread and may change state directly. The view is built again after each handler. From other goroutines, use `Window.Post(func(){...})`.

`Run` opens one window and returns when it closes. Programs with several windows or a tray icon (package `tray`) call `Main`, which runs until `Quit` or until no window and no tray icon is left:

```go
first, err := fairy.SingleInstance("com.example.app", onLaunch)
if err != nil {
	log.Fatal(err)
}
if !first {
	return // the running instance received this launch
}
err = fairy.Main(func() error {
	w, err := fairy.NewWindow(opts)
	if err != nil {
		return err
	}
	_, err = tray.New(tray.Options{Icon: png, OnClick: w.Show, Menu: trayMenu})
	return err
})
if err != nil {
	log.Fatal(err)
}
```

A further start waits up to 30 seconds while the first instance is starting up or shutting down, and runs as the first instance if that one exits meanwhile.

`OnSessionEnd` registers cleanup for sign-out, restart and shutdown. It runs on the UI thread, and Windows may end the process as soon as it returns, before `Main` returns.

Windows, trays and menus are created on the UI thread (inside `Main`, a handler, or `Do`). Dialogs and message boxes (package `dialog`) block their caller and may be called from any goroutine. `Go` and `Do` run a function on the UI thread without and with waiting.

Components that keep their own UI state, such as whether a menu is open, store it with `fairy.Local[T](key)`. `AutoFocus` focuses an element when it first appears, `OnBlur` runs when it loses focus, and `FocusReturn` hands focus to another element when the focused one leaves the tree.

## Layout rules

The engine follows CSS flexbox, with two differences:

- `Grow(g)` inside a parent with a definite main size uses basis 0, like CSS `flex: g`. In a content-sized parent it keeps its content size, so it never collapses to 0.
- Vertically, items do not shrink below their min-content height. Scroll containers can shrink to 0. Horizontally, items may shrink to 0, so `Text(...).Grow(1).Ellipsis()` in a row works without `MinW(0)`.

An explicit pixel size (`W`, `H`) is never shrunk. `WPct` and `HPct` take a fraction of the parent: `WPct(0.5)` is half its width.

## Accessibility

Most elements need no annotation. Plain containers are left out of the accessibility tree and their children take their place; text, inputs and clickable elements are exposed. A clickable element is a button named by the text inside it, unless it holds its own controls, in which case it is a group and they stay reachable. Annotate what cannot be derived:

```go
fairy.Box(fairy.Icon(lucide.Trash)).OnClick(remove).Label("Delete")       // icon-only button
fairy.Box(check).OnClick(toggle).Role(fairy.RoleCheckBox).Checked(on)     // custom check box
fairy.Row(items...).Role(fairy.RoleList)                                  // list of RoleListItem rows
fairy.Text("decoration").Role(fairy.RoleHidden)                           // left out with its subtree
row.Role(fairy.RoleNone)                                                // left out, children kept
```

`Disabled(true)` removes focus and input handlers from the element and its subtree and reports them as disabled; the look stays the caller's. In a popup whose keyboard focus stays on the trigger, mark the highlighted row with `ActiveDescendant`. While a modal overlay is open, only it and the layers above it are exposed. `Announce` reads a message aloud, and `Key` becomes the UI Automation `AutomationId`. The `kit` components set their roles and states themselves.

Elements are identified by their position in the tree and their `Key`. Give list items a `Key` so that state, layout animations and exit transitions follow the item, and key containers whose content is replaced wholesale (pages, tabs).

## Custom title bar input

Mark a custom title bar with `DragRegion().WindowChrome()` to keep window movement
available while an overlay is open. `WindowChrome` gives the subtree pointer input
priority without raising its paint order or changing modal keyboard focus. Mark
window control buttons with it too if they belong to a separate overlay.

Use `WindowChromeExclude()` on title bar actions that must stay blocked by modal
backdrops, such as activity badges or update notifications. The exclusion includes
descendants and takes precedence over nested `WindowChrome` markers. Excluded
areas use normal layer order; outside a modal they keep their usual behavior.
Clipping, transforms, rounded corners and `PassThrough` still apply. Wheel input,
file drops and accessibility retain normal layer order.

## Direct3D in the app

A `Surface` draws with the device and context in its `SurfaceFrame`; wrap them with `f.GPU()` and `f.RenderTarget()` to use the `d3d` package:

```go
fairy.Surface(func(f fairy.SurfaceFrame) bool {
	g := f.GPU() // borrowed: do not release
	_, rtv := f.RenderTarget()
	if g.Generation != r.gen { // the device was recreated: make resources again
		r.create(g.Device)
	}
	g.Context.OMSetRenderTargets([]d3d.RenderTargetView{rtv}, r.dsv)
	// ... draw ...
	return false
}).Retain(scene.Version()) // draw again only when the scene changes
```

Rules for the shared device:

- Use the immediate context only inside a Surface draw function or `UseDevice`. Both hold the device's multithread lock, which the window renderer and Media Foundation also take, so keep the work short and do not wait there for the UI thread. `UseDevice` may run on any goroutine.
- Every handle a `Create*` method returns is the caller's to `Release`. Handles from `SurfaceFrame` and `GPU` are borrowed.
- When the device is lost (driver update, GPU reset), windows make a new one with a higher `Generation`. Compare `GPU.Generation` or `SurfaceFrame.Generation` with the one your resources were made on and make them again. Retained surfaces draw again on the new device. Images from `SurfaceImage` and `NewTextureImage` stop drawing (`Image.Lost`); render them again.
- `NewTextureImage` keeps a reference to the app's texture and shows its contents live, or copies them with `Copy`. `Image.Release` frees the texture at once; otherwise it is freed when the `Image` is garbage collected.
- Drivers keep the system memory of released buffers and textures for reuse. After releasing many or large resources, such as when closing a 3D view, call `fairy.TrimDevice()` outside any Surface draw function or `UseDevice` to clear the pipeline state and return that memory (`IDXGIDevice3::Trim`). Fairy also trims when Windows reports low memory.

The shared device requires feature level 10.1 or later and falls back to WARP when no hardware device is available. A standalone `d3d.CreateDevice` accepts feature level 10.0; `d3d.CreateDeviceWithOptions` sets a higher minimum and reports the adapter in `DeviceInfo`.

## Examples

```bash
go run ./examples/hello
```

```bash
go run ./examples/kit
```

```bash
go run ./examples/demo
```

```bash
go run ./examples/shell
```

```bash
go run ./examples/media path/to/video.mp4
```

```bash
go run ./examples/viewport
```

`examples/kit` shows the component kit with English and Korean messages. `examples/demo` uses native features without `kit`: single instance, tray icon, file dialogs and drop, taskbar progress, virtual lists and a custom title bar. `examples/shell` is an app shell with a sidebar, `examples/media` plays video and hosts a WebView, and `examples/viewport` draws a depth-buffered cube with the `d3d` package into a retained surface.

## Diagnostics

- `FAIRY_DEBUG_FRAMES=1` logs each frame's time by phase (view, prepare, layout, rows, place, paint, record, present), its primitive and draw call counts, and why the view was built again. It also logs the Direct3D driver, feature level and adapter when the device is created.
- `FAIRY_DEBUG_GPU=1` adds GPU timing of the renderer's commands to the frame log. With a software device (WARP), the timing describes software execution.
- `FAIRY_DEBUG_SLOW=8` logs views, posted functions, `Go` calls and window messages that hold the UI thread longer than 8 ms.

## Automated testing

Builds with `-tags fairydebug` read these environment variables, so tests can drive an app without a person. Release builds ignore them.

- `FAIRY_SNAPSHOT=out.png` renders the first frame, writes it to a PNG and closes the window. `FAIRY_SNAPSHOT_DELAY=500ms` waits before the capture.
- `FAIRY_SCRIPT=file` runs commands from a file, one per line: `click X Y`, `rclick X Y ctrl`, `down X Y right shift`, `move X Y`, `up X Y right`, `type TEXT`, `key left shift`, `wheel X Y -3`, `hwheel X Y 0.5`, `dragfiles X Y PATH...`, `dropfiles X Y`, `resize W H`, `wait 100ms`, `snap out.png`, `tree out.json` and `quit`. In place of `X Y`, `@KEY` (or `@KEY+DX,DY`) targets the element with that `Key`. `tree` writes the element tree with keys, roles, names, text, rectangles and state.
- `clock 16ms` in a script, or `FAIRY_CLOCK=fixed`, switches to a virtual clock that only `step N` advances, so snapshots of animations match from run to run.

```bash
go run -tags fairydebug ./examples/kit
```

## Packages

| Import | Contents |
| --- | --- |
| `github.com/myparsleycat/fairy` | Elements, windows, input, animation, images and platform integration |
| `github.com/myparsleycat/fairy/kit` | Themed components, Markdown renderer; `kit/diff` diff view, `kit/syntax` highlighters |
| `github.com/myparsleycat/fairy/layout` | Flexbox engine (portable) |
| `github.com/myparsleycat/fairy/motion` | Springs, easing curves and animation tracks |
| `github.com/myparsleycat/fairy/svg` | SVG vectors for icons and masks |
| `github.com/myparsleycat/fairy/keyboard` | Keys, modifiers, key events and chords |
| `github.com/myparsleycat/fairy/i18n` | Message catalog for i18next JSON resources |
| `github.com/myparsleycat/fairy/icons/lucide` | Generated Lucide icons |
| `github.com/myparsleycat/fairy/icons/lucide/catalog` | Lucide icons looked up by name |
| `github.com/myparsleycat/fairy/d3d` | Direct3D 11 binding and `D3DCompile` for apps that draw 3D |
| `github.com/myparsleycat/fairy/dialog` | Native file dialogs and message boxes |
| `github.com/myparsleycat/fairy/tray` | Notification area icons and notifications |
| `github.com/myparsleycat/fairy/shortcut` | Global keyboard shortcuts |

The `internal` packages hold the Win32, COM, DirectWrite, renderer, Media Foundation, WebView2, UI Automation and image decoding code.

## Development

After editing `internal/render/shaders.hlsl`, run `go generate ./internal/render` to rebuild `shaders.bin`. A stale blob still works: the renderer then compiles the HLSL at startup.

`icons/lucide` holds every Lucide icon and alias; the linker keeps only the icons a program references. `icons/lucide/catalog` looks icons up by kebab-case name for names chosen at run time, and links all of them. To update to another lucide-react release, run:

```bash
go run ./cmd/genlucide -src path/to/node_modules/lucide-react
```

## Requirements

Go 1.27 or later and Windows 10 1703 or later on amd64.

## License

MIT; see [LICENSE](../LICENSE). Third-party notices for code adapted from Zed GPUI and Windows Terminal, and for the Lucide icons (ISC and MIT), are in [NOTICE](../NOTICE).
