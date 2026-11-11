// Command kit shows every kit component in light and dark themes, with the
// app's strings in English and Korean through the i18n package.
package main

import (
	"bytes"
	"cmp"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"log"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/i18n"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/shortcut"
)

var messages = map[string]string{
	"en": `{
		"title": "Component kit",
		"buttons": "Buttons", "forms": "Forms", "toggles": "Toggles", "navigation": "Navigation",
		"feedback": "Feedback", "table": "Table", "markdown": "Markdown", "outline": "Outline",
		"save": "Save", "cancel": "Cancel", "delete": "Delete", "more": "More",
		"name": "Name", "nameHint": "Shown in the document list.", "nameRequired": "Enter a name.",
		"notes": "Notes", "project": "Project", "pickProject": "Pick a project", "tag": "Tag", "released": "Released",
		"notify": "Notifications", "notifyDesc": "Show a toast when a download finishes.",
		"autoUpdate": "Update automatically", "volume": "Volume",
		"showToast": "Show toast", "showError": "Show error", "openDialog": "Open dialog",
		"saved": "Saved", "failed": "Download failed", "failedBody": "The server returned 503.",
		"dialogTitle": "Delete {{name}} permanently?",
		"dialogBody": "This removes {{count}} files from disk. It cannot be undone.",
		"rows_one": "{{count}} row", "rows_other": "{{count}} rows",
		"rightClick": "Right-click here",
		"accent": "Primary", "tint": "Tint",
		"composer": "Message", "composerHint": "Enter sends, Shift+Enter adds a line. Paste an image to attach it.",
		"images_one": "{{count}} image", "images_other": "{{count}} images",
		"media": "Media", "animated": "Animated GIF", "rendered": "RenderImage (offscreen)",
		"runTask": "Run task", "loading": "Downloading…", "done": "Download finished", "taskFailed": "Download failed",
		"mica": "Mica", "menubar": "Menu bar"
	}`,
	"ko": `{
		"title": "컴포넌트 키트",
		"buttons": "버튼", "forms": "입력", "toggles": "토글", "navigation": "탐색",
		"feedback": "피드백", "table": "표", "markdown": "마크다운", "outline": "개요",
		"save": "저장", "cancel": "취소", "delete": "삭제", "more": "더 보기",
		"name": "이름", "nameHint": "문서 목록에 표시됩니다.", "nameRequired": "이름을 입력하세요.",
		"notes": "메모", "project": "프로젝트", "pickProject": "프로젝트 선택", "tag": "태그", "released": "출시일",
		"notify": "알림", "notifyDesc": "다운로드가 끝나면 토스트를 표시합니다.",
		"autoUpdate": "자동 업데이트", "volume": "음량",
		"showToast": "토스트 표시", "showError": "오류 표시", "openDialog": "대화 상자 열기",
		"saved": "저장됨", "failed": "다운로드 실패", "failedBody": "서버가 503을 반환했습니다.",
		"dialogTitle": "{{name, josa(pair: 을/를)}} 영구 삭제할까요?",
		"dialogBody": "디스크에서 파일 {{count}}개를 삭제합니다. 되돌릴 수 없습니다.",
		"rows_other": "{{count}}행",
		"rightClick": "여기를 우클릭",
		"accent": "기본 색", "tint": "색조",
		"composer": "메시지", "composerHint": "Enter로 보내고 Shift+Enter로 줄을 바꿉니다. 이미지를 붙여 넣으면 첨부합니다.",
		"images_other": "이미지 {{count}}개",
		"media": "미디어", "animated": "움직이는 GIF", "rendered": "RenderImage (화면 밖)",
		"runTask": "작업 실행", "loading": "다운로드 중…", "done": "다운로드 완료", "taskFailed": "다운로드 실패",
		"mica": "Mica", "menubar": "메뉴 막대"
	}`,
}

const sample = "## Release notes\n" +
	"Version **2.4** adds *per-project* profiles and ~~removes~~ replaces the old `config.ini`.\n" +
	"See https://example.com/changelog or [the guide](https://example.com/guide).\n\n" +
	"- Faster scans\n- Profiles\n  - per project\n  - per folder\n- [x] Migrated settings\n- [ ] Cloud sync\n\n" +
	"> Back up your files before updating.\n\n" +
	"```go\n// Greet prints a greeting n times.\nfunc Greet(name string, n int) {\n\tfor i := range n {\n\t\tfmt.Printf(\"hello, %s #%d\\n\", name, i+1)\n\t}\n}\n```\n\n" +
	"![Generated gradient](kit:gradient \"Drawn in memory\")\n\n" +
	"![Missing screenshot](missing.png)\n\n" +
	"| Setting | Default | Notes |\n|:--|:-:|--:|\n| threads | 4 | `0` = auto |\n| cache | on | 512 MB |\n"

type row struct {
	name, project string
	size          int
}

type app struct {
	dark, korean              bool
	name, notes, project, tag string
	date                      time.Time
	notify, auto              bool
	radio, tab, seg           string
	volume                    float64
	dialog                    bool
	rows                      []row
	sortCol                   int
	sortDesc                  bool
	tableSel                  kit.TableState
	toaster                   kit.Toaster
	cat                       *i18n.Catalog
	accent                    fairy.Color
	progress                  float64
	tree                      *kit.TreeModel[string]
	advanced                  bool
	sections                  [3]bool
	shortcut                  keyboard.Chord
	tint                      fairy.Color
	draft                     string
	attached                  []*fairy.Image
	sent                      []string
	tasks                     int
	mica                      bool
	badge                     *fairy.Image
	badgeErr                  error
	badgeOpen                 bool
	bar                       viewerMenus
}

// viewerMenus is the state behind the menu bar section.
type viewerMenus struct {
	grid, wire, rim, specular bool
	camera, tone              string
	exposure                  float64
	exposureText              string
}

func main() {
	a := &app{radio: "b", tab: "general", seg: "grid", volume: 40, notify: true, sortCol: -1, tableSel: kit.TableState{Multi: true},
		shortcut: keyboard.MustChord("ctrl+shift+space"),
		bar:      viewerMenus{grid: true, camera: "perspective", tone: "neutral", exposure: 1, exposureText: "1.00"},
		tint:     fairy.HexA(0x38bdf8b3), cat: i18n.New("en"), progress: 0.62}
	for lang, js := range messages {
		if err := a.cat.LoadJSON(lang, []byte(js)); err != nil {
			panic(err)
		}
	}
	projects := []string{"Website", "Mobile app", "Reports", "Archive"}
	for i := range 1000 {
		a.rows = append(a.rows, row{fmt.Sprintf("Document %04d", i+1), projects[i%len(projects)], (i*7919)%5000 + 10})
	}
	a.tree = kit.NewTreeModel[string]().MultiSelect(true)
	for _, p := range projects {
		a.tree.Insert("", p, p)
		for _, kind := range []string{"Drafts", "Images", "Exports"} {
			a.tree.Insert(p, p+"/"+kind, kind)
			for j := range 3 {
				name := fmt.Sprintf("%s %d", strings.TrimSuffix(kind, "s"), j+1)
				a.tree.Insert(p+"/"+kind, p+"/"+kind+"/"+name, name)
			}
		}
	}
	a.tree.SetExpanded(projects[0], true)
	if err := fairy.Run(fairy.Options{Title: "Kit", Width: 1180, Height: 860, View: a.view}); err != nil {
		log.Fatal(err)
	}
}

func (a *app) t(key string, args ...any) string { return a.cat.T(key, args...) }

func (a *app) view(cx *fairy.Cx) *fairy.El {
	theme := kit.Light()
	if a.dark {
		theme = kit.Dark()
	}
	if a.accent.A > 0 {
		theme.Primary = a.accent
	}
	kit.Use(theme)
	kit.Messages = kit.EnglishStrings
	a.cat.SetLanguage("en")
	if a.korean {
		kit.Messages = kit.KoreanStrings
		a.cat.SetLanguage("ko")
	}
	t := kit.T()

	if a.dialog {
		kit.ShowDialog(cx, kit.DialogProps{
			Title:       a.t("dialogTitle", "name", map[bool]string{false: "Photo album", true: "사진 앨범"}[a.korean]),
			Description: a.t("dialogBody", "count", 12),
			Actions: []*fairy.El{
				kit.Button(a.t("cancel"), func() { a.dialog = false }, kit.Outline),
				kit.Button(a.t("delete"), func() { a.dialog = false; a.toaster.Success(a.t("saved")) }, kit.Destructive, kit.Icon(lucide.Trash)),
			},
			OnClose: func() { a.dialog = false },
		})
	}
	cx.Overlay(a.toaster.View(), nil)

	header := fairy.Row(
		fairy.Text(a.t("title")).FontSize(20).SemiBold(),
		fairy.Spacer(),
		kit.Segmented(map[bool]string{false: "en", true: "ko"}[a.korean], []kit.Item{{Value: "en", Label: "EN"}, {Value: "ko", Label: "한국어"}},
			func(v string) { a.korean = v == "ko" }),
		kit.Segmented(map[bool]string{false: "light", true: "dark"}[a.dark], []kit.Item{
			{Value: "light", Icon: lucide.Sun, Description: "Light"}, {Value: "dark", Icon: lucide.Moon, Description: "Dark"}}, func(v string) {
			a.dark = v == "dark"
			cx.Window().SetColors(a.dark, fairy.Color{}, fairy.Color{})
		}),
	).Gap(12).Align(fairy.Center).PadXY(24, 14).BorderEdges(0, 0, 1, 0, t.Border).Fill(t.Background)
	if fairy.BackdropSupported(fairy.BackdropMica) {
		header.Child(fairy.Row(kit.Switch(a.mica, func(on bool) {
			b := fairy.BackdropNone
			if on {
				b = fairy.BackdropMica
			}
			if err := cx.Window().SetBackdrop(b); err != nil {
				a.toaster.Error(a.t("mica"), err.Error())
				return
			}
			a.mica = on
		}).Key("mica"), fairy.Text(a.t("mica"))).Gap(8).Align(fairy.Center))
	}

	left := fairy.Col(a.buttons(), a.forms(cx), a.toggles(), a.menubar(), a.media(), a.outline()).Gap(16).Grow(1).Basis(0).MinW(0)
	right := fairy.Col(a.navigation(), a.feedback(), a.table(), a.markdown(cx)).Gap(16).Grow(1).Basis(0).MinW(0)
	body := fairy.Row(left, right).Gap(16).Pad(24).Align(fairy.Start)

	bg := t.Background
	if a.mica {
		// The backdrop shows through the translucent page.
		bg = bg.Alpha(0.5)
		header.Fill(t.Background.Alpha(0.6))
	}
	return fairy.Col(header, fairy.Col(body).ScrollY().Grow(1)).Fill(bg).Color(t.Foreground).FontSize(t.FontSize)
}

// media shows an animated GIF decoded in memory and an image drawn
// offscreen by RenderImage, as a texture baker would.
func (a *app) media() *fairy.El {
	t := kit.T()
	if a.badge == nil && a.badgeErr == nil {
		a.badge, a.badgeErr = renderBadge()
	}
	caption := func(s string) *fairy.El { return fairy.Text(s).FontSize(t.SmallSize).Color(t.MutedForeground) }
	// Clicking the badge moves it between the row and a full-size view below;
	// MatchID flies it from one place to the other.
	badge := func(scale float32, key string) *fairy.El {
		w, h := a.badge.Size()
		return fairy.ImageEl(a.badge).Size(float32(w)*scale, float32(h)*scale).Key(key).MatchID("badge").
			Cursor(fairy.CursorPointer).Role(fairy.RoleButton).Label("Quick menu").OnClick(func() { a.badgeOpen = !a.badgeOpen })
	}
	var rendered, large *fairy.El
	switch {
	case a.badge == nil:
		rendered = fairy.Text(a.badgeErr.Error()).Color(t.Destructive)
	case a.badgeOpen:
		w, h := a.badge.Size()
		rendered = fairy.Box().Size(float32(w)/2, float32(h)/2)
		large = badge(1, "badge-large")
	default:
		rendered = badge(0.5, "badge")
	}
	content := fairy.Col(fairy.Row(
		fairy.Col(fairy.ImageEl(spinnerGIF()).Size(64, 64).Radius(8), caption(a.t("animated"))).Gap(6),
		fairy.Col(rendered, caption(a.t("rendered"))).Gap(6),
	).Gap(24).Align(fairy.Start)).Gap(16)
	if large != nil {
		content.Child(large)
	}
	return section(a.t("media"), content)
}

// spinnerGIF encodes and decodes an animated GIF in memory, standing in for
// one an app loads.
var spinnerGIF = sync.OnceValue(func() *fairy.Image {
	const n, size = 12, 64
	g := &gif.GIF{LoopCount: 0}
	pal := color.Palette{color.Transparent, color.NRGBA{0x6d, 0x5d, 0xfc, 0xff}, color.NRGBA{0xf5, 0x9e, 0x0b, 0xff}}
	for i := range n {
		p := image.NewPaletted(image.Rect(0, 0, size, size), pal)
		a := float64(i) / n * 2 * math.Pi
		cx, cy := size/2+int(20*math.Cos(a)), size/2+int(20*math.Sin(a))
		for y := range size {
			for x := range size {
				dx, dy := x-cx, y-cy
				switch {
				case dx*dx+dy*dy <= 64:
					p.SetColorIndex(x, y, 2)
				case (x-size/2)*(x-size/2)+(y-size/2)*(y-size/2) <= 36:
					p.SetColorIndex(x, y, 1)
				}
			}
		}
		g.Image = append(g.Image, p)
		g.Delay = append(g.Delay, 8)
		g.Disposal = append(g.Disposal, gif.DisposalBackground)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		panic(err)
	}
	img, err := fairy.DecodeImage(buf.Bytes())
	if err != nil {
		panic(err)
	}
	return img
})

// renderBadge draws a menu button the way a texture baker does: an element
// tree rendered at twice the size into an image with a transparent
// background.
func renderBadge() (*fairy.Image, error) {
	el := fairy.Row(
		fairy.Icon(lucide.Sparkles).Size(22, 22).Color(fairy.Hex(0xfde68a)),
		fairy.Col(
			fairy.Text("Quick menu 🌸").FontSize(17).SemiBold().Color(fairy.Hex(0xffffff)),
			fairy.Text("F6 · 12 items").FontSize(12).Color(fairy.Hex(0xffffff).Alpha(0.75)),
		).Gap(2),
	).Gap(10).Align(fairy.Center).PadXY(14, 10).Size(220, 64).Radius(16).
		Gradient(90, fairy.Hex(0x6d5dfc), fairy.Hex(0xec4899)).Shadow(0, 4, 12, 0, fairy.HexA(0x00000055)).
		Margin(8)
	img, err := fairy.RenderImage(fairy.Box(el), 236, 80, 2)
	if err != nil {
		return nil, err
	}
	return fairy.NewImage(img), nil
}

func (a *app) outline() *fairy.El {
	text := func(s string) *fairy.El { return fairy.Text(s).Color(kit.T().MutedForeground).LineHeight(20) }
	return section(a.t("outline"),
		kit.Tree("projects", a.tree, kit.TreeProps[string]{
			Label: func(s string) string { return s },
			Row: func(s string, r kit.TreeRow) kit.TreeItem {
				it := kit.TreeItem{Label: s, Icon: lucide.File}
				if r.Expandable {
					it.Icon = lucide.Folder
					if r.Expanded {
						it.Icon = lucide.FolderOpen
					}
				}
				return it
			},
			Name:   a.t("outline"),
			Guides: true,
		}).H(220),
		kit.Collapsible("advanced", "Advanced", a.advanced, func(open bool) { a.advanced = open },
			text("Cache size, symbolic links and startup arguments.")),
		kit.Accordion([]kit.AccordionItem{
			{Title: "Install", Open: a.sections[0], OnToggle: func(o bool) { a.sections[0] = o }, Content: text("Drop files on the window to import them.")},
			{Title: "Update", Open: a.sections[1], OnToggle: func(o bool) { a.sections[1] = o }, Content: text("Synced folders check for changes daily.")},
			{Title: "Remove", Open: a.sections[2], Disabled: true},
		}),
	)
}

// menubar is an application menu bar with check and radio items, submenus,
// disabled items and a row that holds a number field.
func (a *app) menubar() *fairy.El {
	b := &a.bar
	setExposure := func(v float64) { b.exposure, b.exposureText = v, strconv.FormatFloat(v, 'f', 2, 64) }
	exposure := fairy.Col(
		fairy.Row(
			kit.Button("-0.1", func() { setExposure(b.exposure - 0.1) }, kit.Outline, kit.Small),
			kit.Button("Reset", func() { setExposure(1) }, kit.Outline, kit.Small),
			kit.Button("+0.1", func() { setExposure(b.exposure + 0.1) }, kit.Outline, kit.Small),
		).Gap(4),
		kit.TextField(b.exposureText, func(s string) {
			b.exposureText = s
			if v, err := strconv.ParseFloat(s, 64); err == nil {
				b.exposure = v
			}
		}, kit.Label("Exposure")).Key("exposure"),
	).Gap(8)
	radio := func(label, value string, cur *string) kit.MenuItem {
		return kit.MenuItem{Label: label, Radio: true, Checked: *cur == value, OnClick: func() { *cur = value }}
	}
	check := func(label string, on *bool) kit.MenuItem {
		return kit.MenuItem{Label: label, Checkbox: true, Checked: *on, OnClick: func() { *on = !*on }}
	}
	saved := func() { a.toaster.Success(a.t("saved")) }
	return section(a.t("menubar"), kit.Menubar("gallery", []kit.MenubarMenu{
		{Label: "File", Items: func() []kit.MenuItem {
			return []kit.MenuItem{
				{Label: "New", Shortcut: "Ctrl+N"},
				{Label: "Open…", Icon: lucide.FolderOpen, Shortcut: "Ctrl+O"},
				{Label: "Open recent", Submenu: []kit.MenuItem{{Label: "Report.pdf"}, {Label: "Notes.txt"}, {Label: "Budget.xlsx"}}},
				{Separator: true},
				{Label: "Save", Icon: lucide.Save, Shortcut: "Ctrl+S", OnClick: saved},
				{Label: "Export…", Disabled: true},
			}
		}},
		{Label: "View", Icon: lucide.Eye, Items: func() []kit.MenuItem {
			return []kit.MenuItem{
				{Heading: true, Label: "Show"},
				check("Grid", &b.grid),
				check("Wireframe", &b.wire),
				{Separator: true},
				{Heading: true, Label: "Camera"},
				radio("Perspective", "perspective", &b.camera),
				radio("Orthographic", "orthographic", &b.camera),
				{Separator: true},
				{Label: "Reset view", Icon: lucide.RotateCcw, Shortcut: "Home"},
			}
		}},
		{Label: "Rendering", Items: func() []kit.MenuItem {
			return []kit.MenuItem{
				{Heading: true, Label: "Tone mapping"},
				radio("Neutral", "neutral", &b.tone),
				radio("ACES Filmic", "aces", &b.tone),
				radio("None", "none", &b.tone),
				{Separator: true},
				{Heading: true, Label: "Exposure"},
				{Content: exposure},
				{Separator: true},
				{Label: "Toon style", Submenu: []kit.MenuItem{check("Rim light", &b.rim), check("Specular", &b.specular)}},
			}
		}},
		{Label: "Help", Items: func() []kit.MenuItem {
			return []kit.MenuItem{{Label: "About"}, {Separator: true}, {Label: "Check for updates", Disabled: true}}
		}},
	}).Key("menubar"))
}

func section(title string, children ...*fairy.El) *fairy.El {
	return kit.Card(append([]*fairy.El{fairy.Text(title).SemiBold().FontSize(15)}, children...)...)
}

func (a *app) buttons() *fairy.El {
	nop := func() {}
	return section(a.t("buttons"),
		fairy.Row(
			kit.Button(a.t("save"), func() { a.toaster.Success(a.t("saved")) }, kit.Icon(lucide.Check)),
			kit.Button("Secondary", nop, kit.Secondary),
			kit.Button("Outline", nop, kit.Outline),
			kit.Button("Ghost", nop, kit.Ghost),
			kit.Button(a.t("delete"), nop, kit.Destructive),
		).Gap(8).Wrap(),
		fairy.Row(
			kit.Button("Small", nop, kit.Small, kit.Outline),
			kit.Button("Large", nop, kit.Large),
			kit.Button("Loading", nop, kit.Loading(true)),
			kit.Button("Disabled", nop, kit.Disabled(true), kit.Outline),
			kit.IconButton(lucide.Settings, nop, kit.Tooltip("Settings")),
			kit.IconButton(lucide.Plus, nop, kit.Outline, kit.Tooltip("Add")),
			kit.DropdownMenu("more", kit.Button(a.t("more"), nil, kit.Outline, kit.IconRight(lucide.ChevronDown)), []kit.MenuItem{
				{Heading: true, Label: "Document"},
				{Label: "Open folder", Icon: lucide.FolderOpen, Shortcut: "Ctrl+O"},
				{Label: "Copy name", Icon: lucide.Copy, Shortcut: keyboard.MustChord("ctrl+c").Label()},
				{Label: "Sort by", Submenu: []kit.MenuItem{
					{Label: a.t("name"), Radio: true, Checked: a.sortCol == 0, OnClick: func() { a.sortCol = 0 }},
					{Label: a.t("project"), Radio: true, Checked: a.sortCol == 1, OnClick: func() { a.sortCol = 1 }},
					{Label: "KB", Radio: true, Checked: a.sortCol == 2, OnClick: func() { a.sortCol = 2 }},
				}},
				{Separator: true},
				{Label: a.t("delete"), Icon: lucide.Trash, Danger: true, OnClick: func() { a.dialog = true }},
			}),
		).Gap(8).Wrap().Align(fairy.Center),
	)
}

func (a *app) forms(cx *fairy.Cx) *fairy.El {
	projects := []kit.Item{
		{Value: "website", Label: "Website"}, {Value: "mobile", Label: "Mobile app"},
		{Value: "reports", Label: "Reports"}, {Value: "archive", Label: "Archive", Disabled: true},
	}
	tags := []kit.Item{}
	for _, s := range []string{"Draft", "Review", "Final", "Shared", "Private", "Urgent", "Template", "Archived"} {
		tags = append(tags, kit.Item{Value: s, Label: s})
	}
	nameOpts := []kit.Option{kit.Description(a.t("nameHint"))}
	if a.name == "" {
		nameOpts = []kit.Option{kit.Description(a.t("nameRequired")), kit.Invalid(true)}
	}
	return section(a.t("forms"),
		kit.Field(a.t("name"), kit.TextField(a.name, func(s string) { a.name = s }, kit.Placeholder("Document name"),
			kit.Invalid(a.name == "")), nameOpts...),
		fairy.Row(
			kit.Field(a.t("project"), kit.Select("project", a.project, projects, func(v string) { a.project = v }, kit.Placeholder(a.t("pickProject")))).Grow(1).Basis(0),
			kit.Field(a.t("tag"), kit.Combobox("tag", a.tag, tags, func(v string) { a.tag = v }, kit.Placeholder("—"))).Grow(1).Basis(0),
			kit.Field(a.t("released"), kit.DatePicker("released", a.date, func(d time.Time) { a.date = d })).Grow(1).Basis(0),
		).Gap(12),
		fairy.Row(
			kit.Field(a.t("accent"), kit.ColorPicker("accent", kit.T().Primary, func(c fairy.Color, final bool) { a.accent = c })),
			kit.Field(a.t("tint"), kit.ColorPicker("tint", a.tint, func(c fairy.Color, final bool) { a.tint = c }, kit.ColorAlpha)),
		).Gap(12),
		kit.Field(a.t("notes"), kit.TextArea(a.notes, func(s string) { a.notes = s }, kit.Placeholder("…"))),
		kit.TextField("", func(string) {}, kit.Icon(lucide.Search), kit.Placeholder(kit.Messages.Search)),
		a.composer(cx.Window()),
	)
}

// composer sends on Enter, adds a line on Shift+Enter and attaches a pasted
// image, as a chat input does.
func (a *app) composer(w *fairy.Window) *fairy.El {
	t := kit.T()
	col := fairy.Col(fairy.Text(a.t("composer")).FontSize(t.SmallSize + 1).Medium()).Gap(6)
	for _, m := range a.sent {
		col.Child(fairy.Text(m).Color(t.MutedForeground).LineHeight(20))
	}
	if len(a.attached) > 0 {
		thumbs := fairy.Row().Gap(6)
		for _, img := range a.attached {
			thumbs.Child(fairy.ImageEl(img).Size(48, 48).Fit(fairy.FitCover).Radius(6))
		}
		col.Child(thumbs)
	}
	paste := func() bool {
		// Text on the clipboard wins, as with a picture copied from a web page.
		if w.ClipboardText() != "" {
			return false
		}
		img, ok := w.ClipboardImage()
		if ok {
			a.attached = append(a.attached, fairy.NewImage(img))
		}
		return ok
	}
	send := func(text string) {
		text = strings.TrimSpace(text)
		if text == "" && len(a.attached) == 0 {
			return
		}
		if n := len(a.attached); n > 0 {
			text = strings.TrimSpace(text + " · " + a.t("images", "count", n))
		}
		a.sent = append(a.sent, text)
		if len(a.sent) > 3 {
			a.sent = a.sent[1:]
		}
		a.draft, a.attached = "", nil
	}
	return col.Child(kit.TextArea(a.draft, func(s string) { a.draft = s }, kit.Label(a.t("composer")),
		kit.Placeholder(a.t("composerHint")), kit.OnSubmit(send), kit.OnPaste(paste)).MinH(60).Key("composer"))
}

func (a *app) toggles() *fairy.El {
	return section(a.t("toggles"),
		kit.Switch(a.notify, func(v bool) { a.notify = v }, kit.Label(a.t("notify")), kit.Description(a.t("notifyDesc"))),
		kit.Checkbox(a.auto, func(v bool) { a.auto = v }, kit.Label(a.t("autoUpdate"))),
		kit.Checkbox(true, func(bool) {}, kit.Label("Disabled"), kit.Disabled(true)),
		kit.RadioGroup(a.radio, []kit.Item{
			{Value: "a", Label: "Copy", Description: "Keeps the original files."},
			{Value: "b", Label: "Move"},
			{Value: "c", Label: "Link", Disabled: true},
		}, func(v string) { a.radio = v }),
		fairy.Row(fairy.Text(a.t("volume")).W(80), kit.Slider(a.volume, 0, 100, 5, func(v float64) { a.volume = v }, kit.Label(a.t("volume"))).Grow(1),
			fairy.Text(fmt.Sprint(a.volume)).W(32).TextAlign(fairy.TextEnd)).Gap(12).Align(fairy.Center),
	)
}

func (a *app) navigation() *fairy.El {
	t := kit.T()
	target := fairy.Box(fairy.Text(a.t("rightClick")).Color(t.MutedForeground)).H(64).Center().Radius(t.Radius).
		Border(1, t.Input).Fill(t.Muted)
	return section(a.t("navigation"),
		kit.Tabs(a.tab, []kit.Item{
			{Value: "general", Label: "General", Icon: lucide.Settings},
			{Value: "files", Label: "Files"},
			{Value: "about", Label: "About"},
			{Value: "beta", Label: "Beta", Disabled: true},
		}, func(v string) { a.tab = v }),
		fairy.Row(
			kit.Segmented(a.seg, []kit.Item{{Value: "grid", Label: "Grid"}, {Value: "list", Label: "List"}, {Value: "table", Label: "Table"}},
				func(v string) { a.seg = v }),
			kit.Kbd("Ctrl+K"),
		).Gap(12).Align(fairy.Center),
		kit.ShortcutRecorder("show", a.shortcut, func(c keyboard.Chord) error {
			if c == keyboard.MustChord("ctrl+c") {
				return shortcut.ErrTaken // as shortcut.Register reports a chord another program holds
			}
			a.shortcut = c
			return nil
		}, kit.Label("Show window")),
		kit.ContextMenu("ctx", target, func() []kit.MenuItem {
			return []kit.MenuItem{
				{Label: "Enabled", Checked: a.auto, OnClick: func() { a.auto = !a.auto }},
				{Label: "Rename", Shortcut: "F2"},
				{Label: "Disabled item", Disabled: true},
				{Separator: true},
				{Label: a.t("delete"), Danger: true, OnClick: func() { a.dialog = true }},
			}
		}),
	)
}

func (a *app) feedback() *fairy.El {
	return section(a.t("feedback"),
		fairy.Row(
			kit.Badge("Default"), kit.Badge("New", kit.Secondary), kit.Badge("Active", kit.WithTone(kit.ToneSuccess)),
			kit.Badge("Outdated", kit.WithTone(kit.ToneWarning), kit.Icon(lucide.CircleAlert)),
			kit.Badge("Failed", kit.Destructive), kit.Badge("v2.4", kit.Outline),
		).Gap(6).Wrap(),
		fairy.Row(kit.Progress(a.progress).Grow(1), kit.ProgressCircle(a.progress, 18), kit.Spinner(18)).Gap(12).Align(fairy.Center),
		kit.Progress(1, kit.WithTone(kit.ToneSuccess)),
		fairy.Row(kit.Skeleton().Size(40, 40).Radius(20),
			fairy.Col(kit.Skeleton().H(12).WPct(0.7), kit.Skeleton().H(12).WPct(0.45)).Gap(8).Grow(1)).Gap(12).Align(fairy.Center),
		kit.Separator(),
		fairy.Row(
			kit.Button(a.t("showToast"), func() { a.toaster.Show(kit.ToastInfo, a.t("saved"), "settings/profile.json") }, kit.Outline),
			kit.Button(a.t("showError"), func() { a.toaster.Error(a.t("failed"), a.t("failedBody")) }, kit.Outline),
			kit.Button(a.t("openDialog"), func() { a.dialog = true }, kit.Secondary),
			kit.Button(a.t("runTask"), func() {
				a.tasks++
				fail := a.tasks%2 == 0
				a.toaster.Promise(kit.PromiseMessages{
					Loading: a.t("loading"),
					Success: a.t("done"),
					Error:   func(error) string { return a.t("taskFailed") },
				}, func() error {
					time.Sleep(1200 * time.Millisecond)
					if fail {
						return fmt.Errorf("HTTP 503")
					}
					return nil
				})
			}, kit.Outline).Key("run-task"),
		).Gap(8).Wrap(),
	)
}

func (a *app) table() *fairy.El {
	cols := []kit.Column{
		{Title: a.t("name"), Grow: 2, Sortable: true, Resizable: true, MinWidth: 80},
		{Title: a.t("project"), Grow: 2, Sortable: true, Resizable: true, MinWidth: 80},
		{Title: "KB", Width: 80, Align: kit.CellEnd, Sortable: true},
	}
	return section(fmt.Sprintf("%s · %s", a.t("table"), a.t("rows", "count", len(a.rows))),
		kit.Table(kit.TableProps{
			ID:      "documents",
			Columns: cols,
			Rows:    len(a.rows),
			Cell: func(r, c int) *fairy.El {
				x := a.rows[r]
				switch c {
				case 0:
					return fairy.Text(x.name).Ellipsis()
				case 1:
					return fairy.Text(x.project).Ellipsis().Color(kit.T().MutedForeground)
				}
				return fairy.Text(fmt.Sprint(x.size)).Font(kit.T().MonoFont)
			},
			State:      &a.tableSel,
			RowID:      func(r int) string { return a.rows[r].name },
			Zebra:      true,
			Label:      a.t("table"),
			SortColumn: a.sortCol,
			SortDesc:   a.sortDesc,
			OnSort: func(c int, desc bool) {
				a.sortCol, a.sortDesc = c, desc
				slices.SortStableFunc(a.rows, func(x, y row) int {
					var r int
					switch c {
					case 0:
						r = cmp.Compare(x.name, y.name)
					case 1:
						r = cmp.Compare(x.project, y.project)
					default:
						r = cmp.Compare(x.size, y.size)
					}
					if desc {
						r = -r
					}
					return r
				})
			},
		}).H(240),
	)
}

func (a *app) markdown(cx *fairy.Cx) *fairy.El {
	w := cx.Window()
	return section(a.t("markdown"), kit.Markdown(sample, kit.MarkdownOptions{
		Selectable: true,
		OnLink:     func(url string) { a.toaster.Info(url) },
		OnCopy:     func(code string) { w.SetClipboardText(code); a.toaster.Success(kit.Messages.Copied) },
		Images: func(src string) *fairy.Image {
			if src == "kit:gradient" {
				return gradient()
			}
			return fairy.Images.File(src, 0, 0)
		},
		ImageFrame: func(el *fairy.El, src string) *fairy.El {
			return el.Cursor(fairy.CursorPointer).OnClick(func() { a.toaster.Info(src) })
		},
	}))
}

// gradient is a picture made in memory, standing in for one an app loads.
var gradient = sync.OnceValue(func() *fairy.Image {
	const w, h = 360, 120
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{uint8(109 + x*120/w), uint8(93 + y*100/h), 252 - uint8(x*90/w), 255})
		}
	}
	return fairy.NewImage(img)
})
