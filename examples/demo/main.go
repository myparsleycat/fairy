// Command demo builds an app from Fairy elements alone, without the kit
// package: a frameless window with a custom title bar, an animated card grid
// with a filter menu, a confirmation dialog and toasts, VirtualList and
// VirtualGrid pages, icons and styling. It also shows the native features: a
// single instance, a tray icon, file dialogs and file drop, a context menu and
// taskbar progress. F5 switches the theme.
//
// Two environment variables supply local assets:
//
//	FAIRY_DEMO_FONTS   glob of font files to register; the first family becomes the UI font
//	FAIRY_DEMO_IMAGES  glob of images shown on the cards
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/dialog"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/motion"
	"github.com/myparsleycat/fairy/svg"
	"github.com/myparsleycat/fairy/tray"
)

type theme struct {
	bg, panel, sidebar, border, text, muted, accent, accentHover, hover, selected fairy.Color
}

var (
	light = theme{
		bg: fairy.Hex(0xf7f7f8), panel: fairy.Hex(0xffffff), sidebar: fairy.Hex(0xefeff1), border: fairy.Hex(0xdedee3),
		text: fairy.Hex(0x18181b), muted: fairy.Hex(0x71717a), accent: fairy.Hex(0x6d5dfc), accentHover: fairy.Hex(0x5b4bea),
		hover: fairy.RGBA(0, 0, 0, 0.05), selected: fairy.RGBA(109, 93, 252, 0.14),
	}
	dark = theme{
		bg: fairy.Hex(0x131316), panel: fairy.Hex(0x1c1c21), sidebar: fairy.Hex(0x18181c), border: fairy.Hex(0x2c2c33),
		text: fairy.Hex(0xececf1), muted: fairy.Hex(0x9a9aa6), accent: fairy.Hex(0x8b7dff), accentHover: fairy.Hex(0x9d91ff),
		hover: fairy.RGBA(255, 255, 255, 0.06), selected: fairy.RGBA(139, 125, 255, 0.2),
	}
)

type project struct {
	name     string
	category string
	active   bool
	hue      uint32
	image    string
}

type app struct {
	page        int
	query       string
	category    string
	menuOpen    bool
	projects    []project
	selected    int // selected card
	selectedRow int // selected row of the virtual list
	deleting    int
	toast       string
	toastSeq    int
	dark        bool
	notes       string
	added       int // rows added to the measured list
	win         *fairy.Window
}

var categories = []string{"Design", "Research", "Engineering", "Marketing"}

func main() {
	a := &app{selected: -1, selectedRow: -1, deleting: -1}
	hues := []uint32{0xf97316, 0x22c55e, 0x3b82f6, 0xec4899, 0xa855f7, 0x14b8a6}
	for i := range 48 {
		a.projects = append(a.projects, project{
			name:     fmt.Sprintf("Project %02d %s", i+1, []string{"Website", "Brochure", "Survey", "Prototype"}[i%4]),
			category: categories[i%len(categories)],
			active:   i%3 == 0,
			hue:      hues[i%len(hues)],
		})
	}
	font := ""
	if pattern := os.Getenv("FAIRY_DEMO_FONTS"); pattern != "" {
		paths, _ := filepath.Glob(pattern)
		for _, p := range paths {
			data, err := os.ReadFile(p)
			if err != nil {
				log.Fatal(err)
			}
			names, err := fairy.RegisterFont(data)
			if err != nil {
				log.Fatalf("%s: %v", p, err)
			}
			if font == "" {
				font = names[0]
			}
		}
	}
	// The card images load in the background.
	if pattern := os.Getenv("FAIRY_DEMO_IMAGES"); pattern != "" {
		images, _ := filepath.Glob(pattern)
		for i := range a.projects {
			if len(images) > 0 {
				a.projects[i].image = images[i%len(images)]
			}
		}
	}
	first, err := fairy.SingleInstance("nahida.fairy.demo", func(fairy.Launch) {
		if a.win != nil {
			a.win.Show()
			a.showToast("Brought the running window to the front")
		}
	})
	if err != nil {
		log.Fatal(err)
	}
	if !first {
		return
	}
	icon := appIcon()
	err = fairy.Main(func() error {
		w, err := fairy.NewWindow(fairy.Options{
			Title:     "Fairy Demo",
			Icon:      icon,
			Font:      font,
			Width:     1100,
			Height:    720,
			MinWidth:  640,
			MinHeight: 420,
			Frameless: true,
			View:      a.view,
			OnKey: func(k keyboard.KeyEvent) bool {
				if k.Is(keyboard.KeyF5, 0) {
					a.dark = !a.dark
					return true
				}
				return false
			},
			// The tray icon keeps Main running, so closing the window quits explicitly.
			OnClose: func() bool { fairy.Quit(); return true },
		})
		if err != nil {
			return err
		}
		a.win = w
		_, err = tray.New(tray.Options{
			Icon:    icon,
			Tooltip: "Fairy Demo",
			OnClick: w.Show,
			Menu: func() []fairy.MenuItem {
				return []fairy.MenuItem{
					{Label: "Show window", OnClick: w.Show},
					{Label: "Dark mode", Checked: a.dark, OnClick: func() { a.dark = !a.dark }},
					{Separator: true},
					{Label: "Quit", OnClick: fairy.Quit},
				}
			},
		})
		return err
	})
	if err != nil {
		log.Fatal(err)
	}
}

// appIcon draws the window and tray icon.
func appIcon() []byte {
	const size = 64
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	from, to := fairy.Hex(0x6d5dfc), fairy.Hex(0xec4899)
	for y := range size {
		for x := range size {
			// Rounded square with a diagonal gradient.
			dx, dy := max(12-x, x-(size-13), 0), max(12-y, y-(size-13), 0)
			if dx*dx+dy*dy > 144 {
				continue
			}
			c := from.Mix(to, float32(x+y)/(2*size))
			img.SetNRGBA(x, y, color.NRGBA{uint8(c.R * 255), uint8(c.G * 255), uint8(c.B * 255), 255})
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

// addFiles adds a project for each path and shows taskbar progress while it "imports".
func (a *app) addFiles(paths []string) {
	for _, p := range paths {
		name := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		a.projects = append(a.projects, project{name: name, category: categories[len(a.projects)%len(categories)], hue: 0x6d5dfc})
	}
	a.showToast("Added " + files(len(paths)))
	w := a.win
	go func() {
		for i := 1; i <= 20; i++ {
			w.SetProgress(fairy.ProgressNormal, float64(i)/20)
			time.Sleep(50 * time.Millisecond)
		}
		w.SetProgress(fairy.ProgressNone, 0)
	}()
}

func (a *app) openFiles() {
	paths, err := dialog.OpenFiles(a.win, dialog.FileOptions{
		Title:    "Add files",
		Multiple: true,
		Role:     "demo-files",
		Filters: []dialog.Filter{
			{Name: "Documents", Patterns: []string{"*.pdf", "*.docx", "*.txt", "*.md"}},
			{Name: "All files", Patterns: []string{"*.*"}},
		},
	})
	if err == nil {
		a.addFiles(paths)
	}
}

func (a *app) dropZone(t theme, paths []string) *fairy.El {
	zone := fairy.Col(
		fairy.Icon(lucide.Upload).Size(40, 40).Color(t.accent),
		fairy.Text("Drop to add projects").FontSize(18).SemiBold(),
		fairy.Text(files(len(paths))).Color(t.muted),
	).Gap(10).Center().Grow(1).Radius(16).
		Fill(t.panel.Alpha(0.92)).Border(2, t.border).DropBorder(t.accent).DropFill(t.selected.Over(t.panel)).
		OnFileDrop(func(d fairy.FileDrop) { a.addFiles(d.Paths) })
	return fairy.Col(zone).Key("dropzone").Pad(24).Fill(fairy.RGBA(0, 0, 0, 0.15)).
		Enter(fairy.Fade().With(motion.Snappy))
}

func (a *app) theme() theme {
	if a.dark {
		return dark
	}
	return light
}

func (a *app) view(cx *fairy.Cx) *fairy.El {
	t := a.theme()
	pages := []func(theme) *fairy.El{a.projectsPage, a.listPage, a.iconsPage, a.stylePage}
	if a.deleting >= 0 {
		cx.Overlay(a.confirmDialog(t), func() { a.deleting = -1 })
	}
	if paths := cx.FileDrag(); paths != nil {
		cx.Overlay(a.dropZone(t, paths), nil)
	}
	cx.Overlay(a.toastView(t), nil)
	return fairy.Col(
		a.titleBar(cx, t),
		fairy.Row(
			a.sidebar(t),
			pages[a.page](t).Key(fmt.Sprint("page", a.page)).Grow(1).MinW(0),
		).Grow(1).MinH(0),
	).WFull().HFull().Fill(t.bg).Color(t.text)
}

func (a *app) titleBar(cx *fairy.Cx, t theme) *fairy.El {
	w := cx.Window()
	btn := func(label string, danger bool, f func()) *fairy.El {
		hover := t.hover
		if danger {
			hover = fairy.Hex(0xe81123)
		}
		b := fairy.Text(label).Font("Segoe MDL2 Assets").FontSize(10).TextAlign(fairy.TextCenter).
			W(46).HFull().Justify(fairy.JustifyCenter).HoverFill(hover).
			OnClick(f)
		if danger {
			b.HoverColor(fairy.Hex(0xffffff))
		}
		return b
	}
	// Glyphs of Segoe MDL2 Assets: ChromeMaximize, ChromeRestore, ChromeMinimize
	// and ChromeClose.
	maxLabel := "\uE922"
	if w.IsMaximized() {
		maxLabel = "\uE923"
	}
	return fairy.Row(
		fairy.Box().Size(14, 14).Radius(4).Gradient(135, t.accent, fairy.Hex(0xec4899)),
		fairy.Text("Fairy Demo").FontSize(12).Color(t.muted),
		fairy.Spacer(),
		btn("\uE921", false, w.Minimize),
		btn(maxLabel, false, w.ToggleMaximize).MaximizeButton(),
		btn("\uE8BB", true, w.Close),
	).H(36).PadEdges(0, 0, 0, 12).Gap(8).Align(fairy.Center).
		Fill(t.sidebar).BorderEdges(0, 0, 1, 0, t.border).DragRegion()
}

func (a *app) sidebar(t theme) *fairy.El {
	item := func(i int, icon *svg.Vector, label string) *fairy.El {
		e := fairy.Row(
			fairy.Icon(icon).Size(16, 16),
			fairy.Text(label),
		).Gap(10).PadXY(10, 8).Radius(8).Align(fairy.Center).
			HoverFill(t.hover).Cursor(fairy.CursorPointer).Animate(motion.Snappy).
			OnClick(func() { a.page = i })
		if a.page == i {
			e.Fill(t.selected).Color(t.accent).Medium()
		}
		return e
	}
	themeIcon, themeLabel := lucide.Moon, "Dark mode"
	if a.dark {
		themeIcon, themeLabel = lucide.Sun, "Light mode"
	}
	return fairy.Col(
		fairy.Text("WORKSPACE").FontSize(11).Color(t.muted).SemiBold().PadXY(10, 4),
		item(0, lucide.Package, "Projects"),
		item(1, lucide.List, "Virtual list"),
		item(2, lucide.Sparkles, "Icons"),
		item(3, lucide.Palette, "Style"),
		fairy.Spacer(),
		fairy.Row(
			fairy.Icon(themeIcon).Size(14, 14),
			fairy.Text(themeLabel+"  (F5)"),
		).Gap(8).Align(fairy.Center).FontSize(12).Color(t.muted).PadXY(10, 8).Radius(8).
			HoverFill(t.hover).Cursor(fairy.CursorPointer).OnClick(func() { a.dark = !a.dark }),
	).W(210).Pad(10).Gap(2).Fill(t.sidebar).BorderEdges(0, 1, 0, 0, t.border)
}

func (a *app) showToast(msg string) {
	a.toast = msg
	a.toastSeq++
	seq := a.toastSeq
	time.AfterFunc(2500*time.Millisecond, func() {
		a.win.Post(func() {
			if a.toastSeq == seq {
				a.toast = ""
			}
		})
	})
}

// toastView is always present so that a toast can animate out after it is cleared.
func (a *app) toastView(t theme) *fairy.El {
	layer := fairy.Box().Key("toasts").PassThrough()
	if a.toast == "" {
		return layer
	}
	return layer.Child(
		fairy.Row(
			fairy.Icon(lucide.CircleCheck).Size(16, 16).Color(fairy.Hex(0x22c55e)),
			fairy.Text(a.toast),
		).Key(fmt.Sprint("toast", a.toastSeq)).Gap(10).Align(fairy.Center).PadXY(16, 12).Radius(10).
			Fill(t.panel).Border(1, t.border).Shadow(0, 8, 24, 0, fairy.RGBA(0, 0, 0, 0.18)).
			Enter(fairy.SlideY(16).With(motion.Bouncy)).Exit(fairy.SlideY(16)).
			Right(24).Bottom(24),
	)
}

func (a *app) confirmDialog(t theme) *fairy.El {
	p := a.projects[a.deleting]
	cancel := func() { a.deleting = -1 }
	remove := func() {
		a.showToast("Deleted " + p.name)
		a.projects = slices.Delete(a.projects, a.deleting, a.deleting+1)
		a.deleting, a.selected = -1, -1
	}
	button := func(label string, fill, text, hover fairy.Color, f func()) *fairy.El {
		return fairy.Text(label).Medium().PadXY(14, 8).Radius(8).Fill(fill).Color(text).HoverFill(hover).
			Cursor(fairy.CursorPointer).Animate(motion.Snappy).OnClick(f)
	}
	dialog := fairy.Col(
		fairy.Row(
			fairy.Box(fairy.Icon(lucide.Trash).Size(18, 18).Color(fairy.Hex(0xef4444))).
				Size(36, 36).Radius(18).Fill(fairy.RGBA(239, 68, 68, 0.12)).Center(),
			fairy.Text("Delete project").FontSize(17).SemiBold(),
		).Gap(12).Align(fairy.Center),
		fairy.Text(fmt.Sprintf("%s will be deleted. This cannot be undone.", p.name)).Color(t.muted).LineHeight(22),
		fairy.Row(
			fairy.Spacer(),
			button("Cancel", t.hover, t.text, t.border, cancel),
			button("Delete", fairy.Hex(0xef4444), fairy.Hex(0xffffff), fairy.Hex(0xdc2626), remove),
		).Gap(8),
	).W(380).Pad(22).Gap(16).Radius(14).Fill(t.panel).Border(1, t.border).
		Shadow(0, 20, 50, 0, fairy.RGBA(0, 0, 0, 0.3)).
		Enter(fairy.ScaleFade(0.94)).
		OnClick(func() {}) // keeps clicks inside the dialog from reaching the scrim
	return fairy.Box(dialog).Key("confirm").Center().Fill(fairy.RGBA(0, 0, 0, 0.4)).
		Enter(fairy.Fade()).Exit(fairy.Fade().With(motion.Snappy)).OnClick(cancel)
}

func (a *app) categoryMenu(t theme) *fairy.El {
	item := func(label, value string) *fairy.El {
		check := fairy.Box().Size(14, 14)
		if a.category == value {
			check = fairy.Icon(lucide.Check).Size(14, 14).Color(t.accent)
		}
		return fairy.Row(check, fairy.Text(label)).Gap(8).Align(fairy.Center).PadXY(10, 7).Radius(6).
			HoverFill(t.hover).Cursor(fairy.CursorPointer).
			OnClick(func() { a.category, a.menuOpen = value, false })
	}
	items := []*fairy.El{item("All categories", "")}
	for _, c := range categories {
		items = append(items, item(c, c))
	}
	return fairy.Col(items...).W(220).Pad(4).Radius(10).Fill(t.panel).Border(1, t.border).
		Shadow(0, 8, 24, 0, fairy.RGBA(0, 0, 0, 0.16)).
		Enter(fairy.SlideY(-6).With(motion.Snappy))
}

func (a *app) projectsPage(t theme) *fairy.El {
	q := strings.ToLower(strings.TrimSpace(a.query))
	var cards []*fairy.El
	active := 0
	for i := range a.projects {
		p := &a.projects[i]
		if p.active {
			active++
		}
		if a.category != "" && p.category != a.category {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(p.name+" "+p.category), q) {
			continue
		}
		cards = append(cards, a.card(t, i, p))
	}
	search := fairy.Row(
		fairy.Icon(lucide.Search).Size(15, 15).Color(t.muted),
		fairy.Input(a.query, func(v string) { a.query = v }).Placeholder("Search projects…").Grow(1),
	).W(260).Gap(8).Align(fairy.Center).PadXY(12, 8).Radius(8).Fill(t.panel).Border(1, t.border)

	categoryLabel := "All categories"
	if a.category != "" {
		categoryLabel = a.category
	}
	filter := fairy.Row(
		fairy.Icon(lucide.Funnel).Size(14, 14).Color(t.muted),
		fairy.Text(categoryLabel),
		fairy.Icon(lucide.ChevronDown).Size(14, 14).Color(t.muted).Rotate(chevronTurn(a.menuOpen)).ScaleMotion(motion.Snappy),
	).Gap(8).Align(fairy.Center).PadXY(12, 8).Radius(8).Fill(t.panel).Border(1, t.border).
		HoverBorder(t.accent).Cursor(fairy.CursorPointer).Animate(motion.Snappy).
		OnClick(func() { a.menuOpen = !a.menuOpen })
	if a.menuOpen {
		filter.Popover(a.categoryMenu(t), fairy.PopoverOptions{Align: fairy.AlignEnd, OnDismiss: func() { a.menuOpen = false }})
	}

	add := fairy.Row(
		fairy.Icon(lucide.Plus).Size(14, 14),
		fairy.Text("Add"),
	).Gap(6).Align(fairy.Center).PadXY(12, 8).Radius(8).Fill(t.accent).Color(fairy.Hex(0xffffff)).
		HoverFill(t.accentHover).Cursor(fairy.CursorPointer).Animate(motion.Snappy).
		Tooltip("Choose files, or drop them on the window").OnClick(a.openFiles)

	header := fairy.Row(
		fairy.Col(
			fairy.Text("Projects").FontSize(22).SemiBold(),
			fairy.Text(fmt.Sprintf("%d projects · %d active · %d shown", len(a.projects), active, len(cards))).Color(t.muted).FontSize(13),
		).Gap(4),
		fairy.Spacer(),
		add,
		filter,
		search,
	).Align(fairy.Center).Gap(10)

	grid := fairy.Row(cards...).Wrap().Gap(14)
	if len(cards) == 0 {
		grid = fairy.Text("No matching projects.").Color(t.muted).PadXY(0, 40)
	}
	return fairy.Col(
		header.PadEdges(24, 28, 16, 28),
		fairy.Col(grid).ScrollY().Grow(1).MinH(0).PadEdges(4, 28, 28, 28),
	)
}

func (a *app) card(t theme, i int, p *project) *fairy.El {
	hue := fairy.Hex(p.hue)
	toggleFill, knobX := t.border, float32(2)
	if p.active {
		toggleFill, knobX = t.accent, 18
	}
	toggle := fairy.Box(
		fairy.Box().Size(16, 16).Radius(8).Fill(fairy.Hex(0xffffff)).Shadow(0, 1, 2, 0, fairy.RGBA(0, 0, 0, 0.25)).
			Left(knobX).Top(2).AnimateLayout(motion.Snappy),
	).Size(36, 20).Radius(10).Fill(toggleFill).Cursor(fairy.CursorPointer).Animate(motion.Snappy).
		OnClick(func() { p.active = !p.active })

	del := fairy.Box(fairy.Icon(lucide.Trash).Size(14, 14)).Size(26, 26).Radius(6).Center().
		Color(fairy.Hex(0xffffff)).Fill(fairy.RGBA(0, 0, 0, 0.35)).HoverFill(fairy.Hex(0xef4444)).
		Cursor(fairy.CursorPointer).Animate(motion.Snappy).Tooltip("Delete").
		OnClick(func() { a.deleting = i }).Right(10).Top(10)

	border := t.border
	if a.selected == i {
		border = t.accent
	}
	cover := fairy.Box().H(110).Radii(10, 10, 0, 0).Gradient(160, hue, hue.Mix(fairy.Hex(0x111111), 0.55))
	if p.image != "" {
		if img := fairy.Images.File(p.image, 440, 220); img != nil {
			cover.Child(fairy.ImageEl(img).Inset(0).Radii(10, 10, 0, 0).Enter(fairy.Fade()))
		}
	}
	cover.Child(
		fairy.Text(p.category).FontSize(11).Medium().Color(fairy.Hex(0xffffff)).
			PadXY(8, 3).Radius(6).Fill(fairy.RGBA(0, 0, 0, 0.35)).Left(10).Top(10),
		del,
	)
	return fairy.Col(
		cover,
		fairy.Row(
			fairy.Col(
				fairy.Text(p.name).Medium().Ellipsis(),
				fairy.Text(p.category).FontSize(12).Color(t.muted).Ellipsis(),
			).Gap(2).Grow(1).MinW(0),
			toggle,
		).Pad(12).Gap(8).Align(fairy.Center),
	).Key(p.name).W(220).Radius(10).Fill(t.panel).Border(1, border).HoverBorder(t.accent).
		Shadow(0, 2, 8, 0, fairy.RGBA(0, 0, 0, 0.06)).
		Animate(motion.Snappy).AnimateLayout(motion.Default).
		Enter(fairy.SlideY(10)).Exit(fairy.ScaleFade(0.9).With(motion.Snappy)).PressScale(0.97).
		OnClick(func() { a.selected = i }).
		OnDoubleClick(func() { p.active = !p.active }).
		OnRightClick(func(pe fairy.PointerEvent) {
			a.selected = i
			var move []fairy.MenuItem
			for _, c := range categories {
				move = append(move, fairy.MenuItem{Label: c, Checked: p.category == c, OnClick: func() { p.category = c }})
			}
			a.win.PopupMenu(pe.X, pe.Y, []fairy.MenuItem{
				{Label: "Active", Checked: p.active, OnClick: func() { p.active = !p.active }},
				{Label: "Category", Items: move},
				{Separator: true},
				{Label: "Delete…", OnClick: func() { a.deleting = i }},
			})
		})
}

func (a *app) listPage(t theme) *fairy.El {
	const count = 100_000
	list := fairy.VirtualList(count, 36, func(i int) *fairy.El {
		row := fairy.Row(
			fairy.Text(fmt.Sprintf("%06d", i+1)).Color(t.muted).W(64),
			fairy.Text(fmt.Sprintf("Item %d. Only the visible rows are built.", i+1)).Grow(1).Ellipsis(),
			fairy.Text(fmt.Sprintf("%d KB", (i*37)%900+12)).Color(t.muted).FontSize(12),
		).H(36).PadXY(14, 0).Align(fairy.Center).Radius(6).
			HoverFill(t.hover).OnClick(func() { a.selectedRow = i })
		if a.selectedRow == i {
			row.Fill(t.selected)
		}
		return row
	})
	notes := 10_000 + a.added
	words := strings.Fields("A list with rows of varying height builds only the visible rows, measures each row when it is built and remembers the height by key")
	measured := fairy.VirtualListMeasured(notes, 60, strconv.Itoa, func(i int) *fairy.El {
		n := 4 + (i*7919)%40
		var b strings.Builder
		for j := 0; j < n; j++ {
			if j > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(words[(i+j)%len(words)])
		}
		card := fairy.Col(
			fairy.Text(fmt.Sprintf("#%d", i+1)).FontSize(12).Color(t.muted),
			fairy.Text(b.String()),
		).Gap(4).PadXY(14, 10).Radius(8).Fill(t.panel).Border(1, t.border)
		return fairy.Box(card).PadEdges(0, 0, 8, 0)
	}).StickToBottom()
	button := func(label string, f func()) *fairy.El {
		return fairy.Text(label).PadXY(12, 6).Radius(8).Fill(t.panel).Border(1, t.border).HoverFill(t.hover).
			Cursor(fairy.CursorPointer).OnClick(f)
	}
	return fairy.Col(
		fairy.Row(
			fairy.Text("Virtual list").FontSize(22).SemiBold(),
			fairy.Text(fmt.Sprintf("%d rows · %d rows of varying height", count, notes)).Color(t.muted).Grow(1),
			button("Add row", func() { a.added++ }),
			button("Go to #5000", func() { a.win.ScrollToRow("measured", 4999) }),
		).Gap(10).Align(fairy.Center).PadEdges(24, 28, 12, 28),
		fairy.Row(
			list.Grow(1).MinW(0),
			measured.Key("measured").Grow(1).MinW(0),
		).Gap(16).Grow(1).MinH(0).PadEdges(0, 20, 20, 20),
	)
}

var iconSet = []*svg.Vector{
	lucide.House, lucide.Search, lucide.Settings, lucide.Bell, lucide.Folder, lucide.FolderOpen,
	lucide.Download, lucide.Upload, lucide.Trash, lucide.Copy, lucide.Check, lucide.X, lucide.Plus,
	lucide.Minus, lucide.Info, lucide.Sparkles, lucide.Moon, lucide.Sun, lucide.Package, lucide.Palette,
	lucide.ArrowDown, lucide.RefreshCw, lucide.Star, lucide.Heart, lucide.Image, lucide.Play,
}

func (a *app) iconsPage(t theme) *fairy.El {
	const count = 20_000
	grid := fairy.VirtualGrid(count, 96, 88, 10, func(i int) *fairy.El {
		hue := fairy.Hex([]uint32{0x6d5dfc, 0xf97316, 0x22c55e, 0x3b82f6, 0xec4899, 0x14b8a6}[i%6])
		return fairy.Col(
			fairy.Icon(iconSet[i%len(iconSet)]).Size(24, 24).Color(hue),
			fairy.Text(fmt.Sprint(i+1)).FontSize(11).Color(t.muted),
		).Gap(8).Center().Radius(10).Fill(t.panel).Border(1, t.border).HoverBorder(hue).
			Animate(motion.Snappy)
	})
	return fairy.Col(
		fairy.Row(
			fairy.Text("Icons").FontSize(22).SemiBold(),
			fairy.Text(fmt.Sprintf("VirtualGrid · %d cells", count)).Color(t.muted),
		).Gap(10).Align(fairy.Center).PadEdges(24, 28, 12, 28),
		grid.Grow(1).MinH(0).PadEdges(0, 28, 20, 28),
	)
}

func (a *app) stylePage(t theme) *fairy.El {
	swatch := func(label string, e *fairy.El) *fairy.El {
		return fairy.Col(e.Size(160, 100), fairy.Text(label).FontSize(12).Color(t.muted)).Gap(8).Align(fairy.Center)
	}
	return fairy.Col(
		fairy.Text("Style").FontSize(22).SemiBold(),
		fairy.Row(
			swatch("radius + shadow", fairy.Box().Radius(16).Fill(t.panel).Shadow(0, 8, 24, 0, fairy.RGBA(0, 0, 0, 0.18))),
			swatch("gradient", fairy.Box().Radius(16).Gradient(90, fairy.Hex(0x06b6d4), fairy.Hex(0x8b5cf6))),
			swatch("border 2px", fairy.Box().Radius(16).Border(2, t.accent).Fill(t.selected)),
			swatch("mixed radii", fairy.Box().Radii(28, 4, 28, 4).Fill(fairy.Hex(0xf59e0b))),
			swatch("inset shadow", fairy.Box().Radius(16).Fill(t.panel).InsetShadow(0, 2, 12, 0, fairy.RGBA(0, 0, 0, 0.35))),
			swatch("opacity 50%", fairy.Box().Radius(16).Fill(fairy.Hex(0xef4444)).Opacity(0.5)),
		).Wrap().Gap(24),
		fairy.Text("Multiline input").SemiBold().MarginEdges(12, 0, 0, 0),
		fairy.Input(a.notes, func(v string) { a.notes = v }).Multiline().
			Placeholder("Type a note. Enter starts a new line.").
			WFull().MaxW(560).MinH(90).Pad(12).Radius(8).Fill(t.panel).Border(1, t.border).FocusBorder(t.accent),
		fairy.Text("The quick brown fox jumps over the lazy dog. 다람쥐 헌 쳇바퀴에 타고파. いろはにほへと 色は匂へど. 🦊🐶😀🎉👍🏽❤️").
			FontSize(16).MaxW(560).LineHeight(26),
	).Pad(28).Gap(16).ScrollY()
}

func files(n int) string {
	if n == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", n)
}

// chevronTurn turns a menu's chevron upside down while the menu is open.
func chevronTurn(open bool) float32 {
	if open {
		return math.Pi
	}
	return 0
}
