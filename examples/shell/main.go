// Command shell shows the kit's app shell: a frameless window with a
// SplitView, a Sidebar with sections and rows, and an inspector pane.
// Ctrl+Shift+B (⇧⌘B on macOS) shows and hides the sidebar; F5 switches the theme.
package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/kit/diff"
	"github.com/myparsleycat/fairy/svg"
)

type item struct {
	id, name, detail string
	icon             *svg.Vector
	status           kit.RowStatus
	badge            string
}

type app struct {
	sidebar, inspector kit.PaneState
	selected           string
	collapsed          map[string]bool
	dark               bool
	diff               *diff.State
}

var sections = []struct {
	title string
	items []item
}{
	{"Library", []item{
		{id: "documents", name: "Documents", icon: lucide.FileText, badge: "128"},
		{id: "downloads", name: "Downloads", icon: lucide.Download, status: kit.StatusBusy},
		{id: "uploads", name: "Uploads", icon: lucide.CloudUpload, status: kit.StatusUnread},
	}},
	{"Projects", []item{
		{id: "website", name: "Website", icon: lucide.Folder, detail: "2h"},
		{id: "mobile", name: "Mobile app", icon: lucide.Folder, status: kit.StatusAttention},
		{id: "reports", name: "Reports", icon: lucide.Folder},
	}},
}

func main() {
	a := &app{
		sidebar:   kit.NewPane(248),
		inspector: kit.NewPane(280).Range(220, 420).Collapsed(),
		selected:  "documents",
		collapsed: map[string]bool{},
		diff:      diff.NewState(sampleDiff()),
	}
	err := fairy.Run(fairy.Options{
		Title: "Shell", Width: 1100, Height: 720, MinWidth: 640, MinHeight: 420, Frameless: true,
		View: a.view,
		OnKey: func(k keyboard.KeyEvent) bool {
			switch {
			case kit.IsSidebarToggle(k):
				a.sidebar.Toggle()
				return true
			case k.Is(keyboard.KeyF5, 0):
				a.dark = !a.dark
				return true
			}
			return false
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}

func (a *app) view(cx *fairy.Cx) *fairy.El {
	theme := kit.Light()
	if a.dark {
		theme = kit.Dark()
	}
	kit.Use(theme)
	t := kit.T()
	w := cx.Window()

	var items []*fairy.El
	for _, s := range sections {
		var rows []*fairy.El
		for _, it := range s.items {
			id := it.id
			rows = append(rows, kit.SidebarRow(kit.SidebarRowProps{
				Label: it.name, Icon: it.icon, Detail: it.detail, Badge: it.badge, Status: it.status,
				Selected: a.selected == id, OnSelect: func() { a.selected = id },
			}).Key(id))
		}
		title := s.title
		items = append(items, kit.SidebarSection(title, len(s.items), a.collapsed[title],
			func() { a.collapsed[title] = !a.collapsed[title] }, rows...).Key(title))
	}
	sidebar := kit.Sidebar(kit.SidebarProps{
		Items:  items,
		Footer: []*fairy.El{kit.Button("Settings", nil, kit.Ghost, kit.Icon(lucide.Settings))},
	})

	caption := func(glyph string, danger bool, f func()) *fairy.El {
		hover := t.Muted
		if danger {
			hover = fairy.Hex(0xe81123)
		}
		return fairy.Text(glyph).Font("Segoe MDL2 Assets").FontSize(10).TextAlign(fairy.TextCenter).
			W(46).HFull().Justify(fairy.JustifyCenter).HoverFill(hover).OnClick(f)
	}
	inspectorLabel := "Show inspector"
	if a.inspector.Open() {
		inspectorLabel = "Hide inspector"
	}
	toolbar := fairy.Row(
		fairy.Text(a.title()).Medium(),
		fairy.Spacer(),
		kit.IconButton(lucide.Info, a.inspector.Toggle, kit.Small, kit.Tooltip(inspectorLabel)).
			Label(inspectorLabel).Expanded(a.inspector.Open()),
	).HFull().Gap(4).Align(fairy.Center)
	if !fairy.SystemWindowControls {
		toolbar.Child(fairy.Box().W(8),
			// Glyphs of Segoe MDL2 Assets: ChromeMinimize, ChromeMaximize and ChromeClose.
			caption("\uE921", false, w.Minimize),
			caption("\uE922", false, w.ToggleMaximize).MaximizeButton(),
			caption("\uE8BB", true, w.Close))
	}

	main := fairy.Col(
		fairy.Text(a.title()).FontSize(20).SemiBold(),
		fairy.Text(fmt.Sprintf("Sidebar %v px, open %v. Drag its edge, double-click it to reset, or press %s.",
			a.sidebar.Width(), a.sidebar.Open(), keyboard.MustChord("mod+shift+b").Label())).Color(t.MutedForeground),
		fairy.Row(kit.ProgressCircle(0.25, 24), kit.ProgressCircle(0.5, 24), kit.ProgressCircle(0.75, 24), kit.ProgressCircle(1, 24),
			fairy.Spacer(),
			kit.Segmented(a.diffMode(), []kit.Item{{Value: "unified", Label: "Unified"}, {Value: "split", Label: "Split"}},
				func(v string) {
					if v == "split" {
						a.diff.SetMode(diff.Split)
					} else {
						a.diff.SetMode(diff.Unified)
					}
				}),
		).Gap(12).Align(fairy.Center),
		diff.View("sample", a.diff, diff.ViewProps{}),
	).Pad(24).Gap(12).Grow(1).MinH(0)

	inspector := fairy.Col(
		fairy.Text("Inspector").SemiBold().H(kit.TitleBarHeight).Justify(fairy.JustifyCenter),
		fairy.Text("Details of "+a.title()).Color(t.MutedForeground),
	).PadXY(12, 0)

	return kit.SplitView(kit.SplitProps{
		Main:    main,
		Sidebar: &a.sidebar, SidebarContent: sidebar,
		Inspector: &a.inspector, InspectorContent: inspector,
		Toolbar: toolbar,
	}).Color(t.Foreground)
}

func (a *app) diffMode() string {
	if a.diff.Mode() == diff.Split {
		return "split"
	}
	return "unified"
}

func sampleDiff() []diff.FileDiff {
	var old strings.Builder
	for i := range 120 {
		fmt.Fprintf(&old, "\tfmt.Println(\"line %d\")\n", i+1)
	}
	src := "package main\n\nimport \"fmt\"\n\nfunc main() {\n" + old.String() + "}\n"
	changed := strings.Replace(src, "\"line 10\"", "\"line ten\"", 1)
	changed = strings.Replace(changed, "\tfmt.Println(\"line 60\")\n", "", 1)
	changed = strings.Replace(changed, "\tfmt.Println(\"line 100\")\n", "\tfmt.Println(\"line 100\")\n\tfmt.Println(\"inserted\")\n", 1)
	return append([]diff.FileDiff{diff.FromTexts("main.go", src, changed)},
		diff.Parse("diff --git a/old.txt b/new.txt\nsimilarity index 80%\nrename from old.txt\nrename to new.txt\n"+
			"--- a/old.txt\n+++ b/new.txt\n@@ -1,2 +1,2 @@ intro\n keep\n-the old price * count\n+the old price * amount\n")...)
}

func (a *app) title() string {
	for _, s := range sections {
		for _, it := range s.items {
			if it.id == a.selected {
				return it.name
			}
		}
	}
	return ""
}
