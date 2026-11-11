package kit

import "github.com/myparsleycat/fairy"

// Strings are the texts components show on their own. Replace them to
// translate the kit, for example with KoreanStrings, JapaneseStrings or
// ChineseStrings, or from an i18n catalog.
type Strings struct {
	Search    string
	NoResults string
	Close     string
	Copy      string
	Copied    string
	// Cut, Paste and SelectAll, with Copy, label the context menu of text
	// inputs (fairy.SetEditMenuText).
	Cut       string
	Paste     string
	SelectAll string
	// NoSuggestions, AddToDictionary and IgnoreWord label the spelling items
	// of that menu.
	NoSuggestions   string
	AddToDictionary string
	IgnoreWord      string
	// Weekdays starts on Sunday.
	Weekdays [7]string
	// MonthFormat formats the month title of a date picker (time.Format layout).
	MonthFormat string
	// DateFormat formats a picked date (time.Format layout).
	DateFormat string
	PickDate   string
	Today      string
	// ResizeColumn names the drag handle between table columns.
	ResizeColumn string
	// RecordShortcut and PressShortcut are a ShortcutRecorder's placeholder
	// at rest and while recording; ShortcutHelp describes it while recording.
	RecordShortcut string
	PressShortcut  string
	ShortcutHelp   string
	// ShortcutNeedsModifier refuses a chord without Ctrl, Alt or Win.
	// ShortcutTaken and ShortcutFailed are formats given the chord's label.
	ShortcutNeedsModifier string
	ShortcutTaken         string
	ShortcutFailed        string
	ShortcutClearFailed   string
	// Sidebar and Inspector name a SplitView's panes; the others label its
	// toggle button and resize handles.
	Sidebar         string
	Inspector       string
	ShowSidebar     string
	HideSidebar     string
	ResizeSidebar   string
	ResizeInspector string
	// Working, NeedsAttention and Unread describe a SidebarRow's status.
	Working        string
	NeedsAttention string
	Unread         string

	// Diff view texts. The Line formats take a line number and the line's
	// text; the counts take a number of lines.
	Diff              string
	DiffAddedLine     string
	DiffRemovedLine   string
	DiffUnchangedLine string
	DiffHiddenLine    string // exactly one unchanged line
	DiffHiddenLines   string
	DiffShowAll       string
	DiffShowAbove     string
	DiffShowBelow     string
	DiffCollapseFile  string
	DiffExpandFile    string
	DiffAdded         string
	DiffDeleted       string
	DiffRenamed       string
	DiffCopied        string
	DiffRenamedFrom   string // ", renamed from %s"
	DiffCounts        string // ", %d added, %d removed"
	DiffBinary        string
	DiffMoved         string
	DiffModeChanged   string
	DiffEmpty         string
	DiffNoChanges     string
	DiffNoNewline     string

	// ColorPicker texts. Color names the button when no Label is given;
	// SaturationBrightnessValue takes the two percentages.
	Color                     string
	SaturationBrightness      string
	SaturationBrightnessValue string
	Hue                       string
	Opacity                   string
	Hex                       string

	// Titles of Markdown alerts ("> [!NOTE]").
	CalloutNote      string
	CalloutTip       string
	CalloutImportant string
	CalloutWarning   string
	CalloutCaution   string

	// Remove names the button that takes a chip out of a MultiCombobox.
	Remove string
}

// EnglishStrings are the default texts.
var EnglishStrings = Strings{
	Search:          "Search…",
	NoResults:       "No results",
	Close:           "Close",
	Copy:            "Copy",
	Copied:          "Copied",
	Cut:             "Cut",
	Paste:           "Paste",
	SelectAll:       "Select all",
	NoSuggestions:   "No suggestions",
	AddToDictionary: "Add to dictionary",
	IgnoreWord:      "Ignore",
	Weekdays:        [7]string{"Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"},
	MonthFormat:     "January 2006",
	DateFormat:      "Jan 2, 2006",
	PickDate:        "Pick a date",
	Today:           "Today",
	ResizeColumn:    "Resize column",

	RecordShortcut:        "Record shortcut",
	PressShortcut:         "Press shortcut",
	ShortcutHelp:          "Press a shortcut. Escape cancels; Backspace clears.",
	ShortcutNeedsModifier: "Add Ctrl, Alt or Win.",
	ShortcutTaken:         "%s is already in use.",
	ShortcutFailed:        "%s couldn't be registered.",
	ShortcutClearFailed:   "The shortcut couldn't be cleared.",

	Sidebar:         "Sidebar",
	Inspector:       "Inspector",
	ShowSidebar:     "Show sidebar",
	HideSidebar:     "Hide sidebar",
	ResizeSidebar:   "Resize sidebar",
	ResizeInspector: "Resize inspector",
	Working:         "Working",
	NeedsAttention:  "Needs attention",
	Unread:          "Unread",

	Diff:              "Diff",
	DiffAddedLine:     "added line %d: %s",
	DiffRemovedLine:   "removed line %d: %s",
	DiffUnchangedLine: "unchanged line %d: %s",
	DiffHiddenLine:    "1 unchanged line",
	DiffHiddenLines:   "%d unchanged lines",
	DiffShowAll:       "Show all %d lines",
	DiffShowAbove:     "Show %d more lines above",
	DiffShowBelow:     "Show %d more lines below",
	DiffCollapseFile:  "Collapse file",
	DiffExpandFile:    "Expand file",
	DiffAdded:         "Added",
	DiffDeleted:       "Deleted",
	DiffRenamed:       "Renamed",
	DiffCopied:        "Copied",
	DiffRenamedFrom:   ", renamed from %s",
	DiffCounts:        ", %d added, %d removed",
	DiffBinary:        "Binary file not shown",
	DiffMoved:         "File moved without changes",
	DiffModeChanged:   "File mode changed",
	DiffEmpty:         "Empty file",
	DiffNoChanges:     "No changes",
	DiffNoNewline:     "No newline at end of file",

	Color:                     "Color",
	SaturationBrightness:      "Saturation and brightness",
	SaturationBrightnessValue: "Saturation %d%%, brightness %d%%",
	Hue:                       "Hue",
	Opacity:                   "Opacity",
	Hex:                       "Hex",

	CalloutNote:      "Note",
	CalloutTip:       "Tip",
	CalloutImportant: "Important",
	CalloutWarning:   "Warning",
	CalloutCaution:   "Caution",

	Remove: "Remove",
}

// KoreanStrings are Korean texts.
var KoreanStrings = Strings{
	Search:          "검색…",
	NoResults:       "결과 없음",
	Close:           "닫기",
	Copy:            "복사",
	Copied:          "복사됨",
	Cut:             "잘라내기",
	Paste:           "붙여넣기",
	SelectAll:       "모두 선택",
	NoSuggestions:   "추천 단어 없음",
	AddToDictionary: "사전에 추가",
	IgnoreWord:      "무시",
	Weekdays:        [7]string{"일", "월", "화", "수", "목", "금", "토"},
	MonthFormat:     "2006년 1월",
	DateFormat:      "2006년 1월 2일",
	PickDate:        "날짜 선택",
	Today:           "오늘",
	ResizeColumn:    "열 너비 조절",

	RecordShortcut:        "단축키 지정",
	PressShortcut:         "단축키를 누르세요",
	ShortcutHelp:          "단축키를 누르세요. Esc로 취소하고 Backspace로 지웁니다.",
	ShortcutNeedsModifier: "Ctrl, Alt, Win 중 하나를 함께 누르세요.",
	ShortcutTaken:         "%s 단축키는 이미 사용 중입니다.",
	ShortcutFailed:        "%s 단축키를 등록하지 못했습니다.",
	ShortcutClearFailed:   "단축키를 지우지 못했습니다.",

	Sidebar:         "사이드바",
	Inspector:       "속성 패널",
	ShowSidebar:     "사이드바 표시",
	HideSidebar:     "사이드바 숨기기",
	ResizeSidebar:   "사이드바 너비 조절",
	ResizeInspector: "속성 패널 너비 조절",
	Working:         "작업 중",
	NeedsAttention:  "확인 필요",
	Unread:          "읽지 않음",

	Diff:              "변경 내용",
	DiffAddedLine:     "추가된 줄 %d: %s",
	DiffRemovedLine:   "삭제된 줄 %d: %s",
	DiffUnchangedLine: "변경 없는 줄 %d: %s",
	DiffHiddenLine:    "변경 없는 줄 1개",
	DiffHiddenLines:   "변경 없는 줄 %d개",
	DiffShowAll:       "%d줄 모두 표시",
	DiffShowAbove:     "위로 %d줄 더 표시",
	DiffShowBelow:     "아래로 %d줄 더 표시",
	DiffCollapseFile:  "파일 접기",
	DiffExpandFile:    "파일 펼치기",
	DiffAdded:         "추가됨",
	DiffDeleted:       "삭제됨",
	DiffRenamed:       "이름 변경됨",
	DiffCopied:        "복사됨",
	DiffRenamedFrom:   ", 이전 이름 %s",
	DiffCounts:        ", %d줄 추가, %d줄 삭제",
	DiffBinary:        "바이너리 파일은 표시하지 않습니다",
	DiffMoved:         "내용 변경 없이 이동한 파일",
	DiffModeChanged:   "파일 권한 변경",
	DiffEmpty:         "빈 파일",
	DiffNoChanges:     "변경 없음",
	DiffNoNewline:     "파일 끝에 줄바꿈 없음",

	Color:                     "색",
	SaturationBrightness:      "채도와 명도",
	SaturationBrightnessValue: "채도 %d%%, 명도 %d%%",
	Hue:                       "색상",
	Opacity:                   "불투명도",
	Hex:                       "16진수",

	CalloutNote:      "참고",
	CalloutTip:       "팁",
	CalloutImportant: "중요",
	CalloutWarning:   "경고",
	CalloutCaution:   "주의",

	Remove: "제거",
}

// JapaneseStrings are Japanese texts.
var JapaneseStrings = Strings{
	Search:          "検索…",
	NoResults:       "結果なし",
	Close:           "閉じる",
	Copy:            "コピー",
	Copied:          "コピーしました",
	Cut:             "切り取り",
	Paste:           "貼り付け",
	SelectAll:       "すべて選択",
	NoSuggestions:   "候補なし",
	AddToDictionary: "辞書に追加",
	IgnoreWord:      "無視",
	Weekdays:        [7]string{"日", "月", "火", "水", "木", "金", "土"},
	MonthFormat:     "2006年1月",
	DateFormat:      "2006年1月2日",
	PickDate:        "日付を選択",
	Today:           "今日",
	ResizeColumn:    "列の幅を変更",

	RecordShortcut:        "ショートカットを設定",
	PressShortcut:         "ショートカットを押してください",
	ShortcutHelp:          "ショートカットを押してください。Esc で取り消し、Backspace で消去します。",
	ShortcutNeedsModifier: "Ctrl、Alt、Win のいずれかを一緒に押してください。",
	ShortcutTaken:         "%s はすでに使用されています。",
	ShortcutFailed:        "%s を登録できませんでした。",
	ShortcutClearFailed:   "ショートカットを消去できませんでした。",

	Sidebar:         "サイドバー",
	Inspector:       "インスペクター",
	ShowSidebar:     "サイドバーを表示",
	HideSidebar:     "サイドバーを隠す",
	ResizeSidebar:   "サイドバーの幅を変更",
	ResizeInspector: "インスペクターの幅を変更",
	Working:         "作業中",
	NeedsAttention:  "要確認",
	Unread:          "未読",

	Diff:              "差分",
	DiffAddedLine:     "追加された行 %d: %s",
	DiffRemovedLine:   "削除された行 %d: %s",
	DiffUnchangedLine: "変更のない行 %d: %s",
	DiffHiddenLine:    "変更のない行 1 行",
	DiffHiddenLines:   "変更のない行 %d 行",
	DiffShowAll:       "%d 行をすべて表示",
	DiffShowAbove:     "上に %d 行表示",
	DiffShowBelow:     "下に %d 行表示",
	DiffCollapseFile:  "ファイルを折りたたむ",
	DiffExpandFile:    "ファイルを展開",
	DiffAdded:         "追加",
	DiffDeleted:       "削除",
	DiffRenamed:       "名前変更",
	DiffCopied:        "コピー",
	DiffRenamedFrom:   "、旧名 %s",
	DiffCounts:        "、%d 行追加、%d 行削除",
	DiffBinary:        "バイナリファイルは表示しません",
	DiffMoved:         "内容を変えずに移動したファイル",
	DiffModeChanged:   "ファイルモードの変更",
	DiffEmpty:         "空のファイル",
	DiffNoChanges:     "変更なし",
	DiffNoNewline:     "ファイル末尾に改行なし",

	Color:                     "色",
	SaturationBrightness:      "彩度と明度",
	SaturationBrightnessValue: "彩度 %d%%、明度 %d%%",
	Hue:                       "色相",
	Opacity:                   "不透明度",
	Hex:                       "16 進数",

	CalloutNote:      "注記",
	CalloutTip:       "ヒント",
	CalloutImportant: "重要",
	CalloutWarning:   "警告",
	CalloutCaution:   "注意",

	Remove: "削除",
}

// ChineseStrings are Simplified Chinese texts.
var ChineseStrings = Strings{
	Search:          "搜索…",
	NoResults:       "无结果",
	Close:           "关闭",
	Copy:            "复制",
	Copied:          "已复制",
	Cut:             "剪切",
	Paste:           "粘贴",
	SelectAll:       "全选",
	NoSuggestions:   "无建议",
	AddToDictionary: "添加到词典",
	IgnoreWord:      "忽略",
	Weekdays:        [7]string{"日", "一", "二", "三", "四", "五", "六"},
	MonthFormat:     "2006年1月",
	DateFormat:      "2006年1月2日",
	PickDate:        "选择日期",
	Today:           "今天",
	ResizeColumn:    "调整列宽",

	RecordShortcut:        "设置快捷键",
	PressShortcut:         "请按下快捷键",
	ShortcutHelp:          "请按下快捷键。Esc 取消，Backspace 清除。",
	ShortcutNeedsModifier: "请同时按下 Ctrl、Alt 或 Win。",
	ShortcutTaken:         "%s 已被占用。",
	ShortcutFailed:        "无法注册 %s。",
	ShortcutClearFailed:   "无法清除快捷键。",

	Sidebar:         "侧边栏",
	Inspector:       "检查器",
	ShowSidebar:     "显示侧边栏",
	HideSidebar:     "隐藏侧边栏",
	ResizeSidebar:   "调整侧边栏宽度",
	ResizeInspector: "调整检查器宽度",
	Working:         "处理中",
	NeedsAttention:  "需要注意",
	Unread:          "未读",

	Diff:              "差异",
	DiffAddedLine:     "新增行 %d：%s",
	DiffRemovedLine:   "删除行 %d：%s",
	DiffUnchangedLine: "未更改行 %d：%s",
	DiffHiddenLine:    "1 行未更改",
	DiffHiddenLines:   "%d 行未更改",
	DiffShowAll:       "显示全部 %d 行",
	DiffShowAbove:     "向上显示 %d 行",
	DiffShowBelow:     "向下显示 %d 行",
	DiffCollapseFile:  "折叠文件",
	DiffExpandFile:    "展开文件",
	DiffAdded:         "已添加",
	DiffDeleted:       "已删除",
	DiffRenamed:       "已重命名",
	DiffCopied:        "已复制",
	DiffRenamedFrom:   "，原名 %s",
	DiffCounts:        "，新增 %d 行，删除 %d 行",
	DiffBinary:        "不显示二进制文件",
	DiffMoved:         "文件已移动，内容未更改",
	DiffModeChanged:   "文件模式已更改",
	DiffEmpty:         "空文件",
	DiffNoChanges:     "无更改",
	DiffNoNewline:     "文件末尾没有换行符",

	Color:                     "颜色",
	SaturationBrightness:      "饱和度和亮度",
	SaturationBrightnessValue: "饱和度 %d%%，亮度 %d%%",
	Hue:                       "色相",
	Opacity:                   "不透明度",
	Hex:                       "十六进制",

	CalloutNote:      "注意",
	CalloutTip:       "提示",
	CalloutImportant: "重要",
	CalloutWarning:   "警告",
	CalloutCaution:   "小心",

	Remove: "移除",
}

// Messages holds the strings components use.
var Messages = EnglishStrings

func init() {
	fairy.SetEditMenuText(func() fairy.EditMenuLabels {
		return fairy.EditMenuLabels{
			Cut: Messages.Cut, Copy: Messages.Copy, Paste: Messages.Paste, SelectAll: Messages.SelectAll,
			NoSuggestions: Messages.NoSuggestions, AddToDictionary: Messages.AddToDictionary, IgnoreWord: Messages.IgnoreWord,
		}
	})
}
