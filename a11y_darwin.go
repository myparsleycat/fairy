package fairy

// Accessibility on macOS: each exposed element of the window's accessibility
// tree (see a11y.go) is a FairyAccessibilityElement, a subclass of
// NSAccessibilityElement that answers AppKit's accessibility getters from
// the element tree when they are asked, as the UI Automation providers do on
// Windows. The FairyView is the root: it is not an element itself, and its
// accessibility children are the top-level exposed elements.
//
// Nothing is built until an assistive client such as VoiceOver asks the view
// for its children, its focused element or the element at a point, so
// windows pay nothing without one. From then on, the objects stay the same
// per element identity, and a11yAfterFrame posts notifications for focus,
// value, title, selection and structure changes.
//
// Getters that return NSRect or NSRange go through forwardInvocation:, as in
// ime_darwin.go, since purego callbacks cannot return those structs.

import (
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego/objc"

	"github.com/myparsleycat/fairy/internal/darwin"
)

// a11yState is the window's NSAccessibility side. It exists once an
// assistive client has asked the view about its contents.
type a11yState struct {
	nodes  map[uint64]*a11yNode
	tree   *a11yTree
	ver    int       // treeVer the tree was built from
	evTree *a11yTree // tree of the last change-notification pass
	focus  uint64
	sel    darwin.NSRange    // selected text range of the focused edit field
	live   map[uint64]string // the text of each live region
}

// a11yNode is the accessibility element of one exposed element, kept by
// element identity.
type a11yNode struct {
	w    *Window
	id   uint64
	obj  darwin.ID // FairyAccessibilityElement
	seen int       // treeVer when last in the tree
	last a11ySnap

	// The value last set on a range, which the tree built at rngVer does
	// not show yet.
	rngTo  float64
	rngVer int
	rngSet bool
}

// a11ySnap holds the properties whose changes are posted as notifications.
type a11ySnap struct {
	name     string
	value    string
	enabled  bool
	selected bool
	expanded int8
	seen     bool
}

// byElement finds the node an accessibility element is for.
var byElement = map[darwin.ID]*a11yNode{}

// a11yStart creates the window's accessibility state when an assistive
// client first asks for it. It reports false for a closed window.
func (w *Window) a11yStart() bool {
	if w.closed {
		return false
	}
	if w.acc == nil {
		w.acc = &a11yState{nodes: map[uint64]*a11yNode{}, live: map[uint64]string{}}
	}
	return true
}

// a11yShutdown releases the accessibility elements of a closing window.
func (w *Window) a11yShutdown() {
	a := w.acc
	if a == nil {
		return
	}
	for _, n := range a.nodes {
		n.destroy()
	}
	w.acc = nil
}

// destroy tells clients the element is gone and lets it go. It may run
// inside a call to the element itself, which is why the element is released
// when the autorelease pool drains, after the call.
func (n *a11yNode) destroy() {
	axPost(n.obj, axNames().destroyed)
	delete(byElement, n.obj)
	darwin.Send(n.obj, "autorelease")
	n.obj = 0
}

func (w *Window) a11yTree() *a11yTree {
	a := w.acc
	if a.tree == nil || a.ver != w.treeVer {
		a.tree = buildA11yTree(w.a11yRoots())
		a.ver = w.treeVer
		for id, n := range a.nodes {
			if _, ok := a.tree.el[id]; ok {
				n.seen = a.ver
			} else if a.ver-n.seen > stateTTL {
				n.destroy()
				delete(a.nodes, id)
			}
		}
	}
	return a.tree
}

// a11yElement returns the accessibility element of an exposed element, or
// the view for 0.
func (w *Window) a11yElement(id uint64) darwin.ID {
	if id == 0 {
		return w.view
	}
	n := w.acc.nodes[id]
	if n == nil {
		n = &a11yNode{w: w, id: id, seen: w.acc.ver}
		n.obj = darwin.Send(darwin.Send(darwin.ID(elementClass()), "alloc"), "init")
		w.acc.nodes[id] = n
		byElement[n.obj] = n
	}
	return n.obj
}

// a11yArray returns an NSArray of the elements of ids.
func (w *Window) a11yArray(ids []uint64) darwin.ID {
	arr := darwin.SendClass("NSMutableArray", "arrayWithCapacity:", uint64(len(ids)))
	for _, id := range ids {
		darwin.Send(arr, "addObject:", w.a11yElement(id))
	}
	return arr
}

// axNode returns the node of an accessibility element and its element and
// role, or nil when the element left the tree or the window closed.
func axNode(self darwin.ID) (*a11yNode, *El, Role) {
	n := byElement[self]
	if n == nil || n.w.closed || n.w.acc == nil {
		return nil, nil, 0
	}
	t := n.w.a11yTree()
	e := t.el[n.id]
	if e == nil {
		return nil, nil, 0
	}
	return n, e, t.role[n.id]
}

// a11yAfterFrame posts focus, structure and property change notifications.
func (w *Window) a11yAfterFrame() {
	a := w.acc
	if a == nil || w.closed {
		return
	}
	names := axNames()
	t := w.a11yTree()
	if prev := a.evTree; prev != t {
		a.evTree = t
		if prev != nil {
			changed := len(prev.kids) != len(t.kids)
			for id, kids := range t.kids {
				if !changed && !slices.Equal(prev.kids[id], kids) {
					changed = true
				}
			}
			// A new menu may have no exposed children, so has no entry in kids.
			for id, r := range t.role {
				if _, was := prev.el[id]; id != 0 && !was && r == RoleMenu {
					axPost(w.a11yElement(id), names.menuOpened)
				}
			}
			if changed {
				axPost(w.nsWindow, names.layoutChanged)
			}
		}
		for id, n := range a.nodes {
			if e := t.el[id]; e != nil {
				n.postChanges(e, t.role[id])
			}
		}
		w.a11yLiveRegions(t)
	}
	focus := uint64(0)
	if w.osFocus {
		focus = w.a11yFocus()
	}
	if focus != a.focus {
		a.focus = focus
		a.sel = darwin.NSRange{Location: darwin.NSNotFound}
		if focus != 0 {
			axPost(w.a11yElement(focus), names.focusChanged)
		}
	}
	if e := t.el[focus]; e != nil && t.role[focus] == RoleEdit {
		if r := w.a11ySelectedRange(e, RoleEdit); r != a.sel {
			first := a.sel.Location == darwin.NSNotFound
			a.sel = r
			if !first {
				axPost(w.a11yElement(focus), names.selectedTextChanged)
			}
		}
	}
}

// postChanges posts notifications for the properties that changed since
// the last look.
func (n *a11yNode) postChanges(e *El, r Role) {
	s := a11ySnap{name: e.a11yName(r), enabled: !e.disabled(), seen: true}
	s.value = n.w.axValueText(e, r)
	if a := e.a11y; a != nil {
		s.selected = a.selected == 2
		s.expanded = a.expanded
	}
	old := n.last
	n.last = s
	if !old.seen {
		return // first look: nothing to compare with
	}
	names := axNames()
	if s.name != old.name {
		axPost(n.obj, names.titleChanged)
	}
	if s.value != old.value || s.enabled != old.enabled {
		axPost(n.obj, names.valueChanged)
	}
	if s.selected != old.selected {
		if p := n.w.a11yTree().parent[n.id]; p != 0 {
			axPost(n.w.a11yElement(p), names.selectedChildrenChanged)
		}
	}
	if s.expanded != old.expanded {
		switch {
		case r != RoleTreeItem:
			axPost(n.obj, names.expandedChanged)
		case s.expanded == 2:
			axPost(n.obj, names.rowExpanded)
		default:
			axPost(n.obj, names.rowCollapsed)
		}
	}
}

// a11yLiveRegions announces the new text of live regions, which VoiceOver
// does not watch by itself.
func (w *Window) a11yLiveRegions(t *a11yTree) {
	a := w.acc
	for id, e := range t.el {
		l := e.liveSetting(t.role[id])
		if l == LiveOff {
			delete(a.live, id)
			continue
		}
		var b strings.Builder
		textContent(e, &b)
		s := b.String()
		old, seen := a.live[id]
		a.live[id] = s
		if seen && s != old && s != "" {
			priority := int64(axPriorityMedium)
			if l == LiveAssertive {
				priority = axPriorityHigh
			}
			w.announce(s, priority)
		}
	}
	for id := range a.live {
		if _, ok := t.el[id]; !ok {
			delete(a.live, id)
		}
	}
}

// NSAccessibilityPriorityLevel values.
const (
	axPriorityMedium = 50
	axPriorityHigh   = 90
)

// Announce asks screen readers to read text, as for a finished background
// task or a toast. It may be called from any goroutine.
func (w *Window) Announce(text string) {
	if !onUIThread() {
		Go(func() { w.Announce(text) })
		return
	}
	if text != "" {
		w.announce(text, axPriorityHigh)
	}
}

func (w *Window) announce(text string, priority int64) {
	if !w.hasNativeWindow() {
		return
	}
	names := axNames()
	info := darwin.SendClass("NSMutableDictionary", "dictionary")
	darwin.Send(info, "setObject:forKey:", darwin.String(text), names.announcementKey)
	darwin.Send(info, "setObject:forKey:", darwin.SendClass("NSNumber", "numberWithLongLong:", priority), names.priorityKey)
	axPostInfo(w.nsWindow, names.announcement, info)
}

// Announce asks screen readers to read text in the focused window, or in
// another window when none of them has focus. It may be called from any goroutine.
func Announce(text string) {
	if !onUIThread() {
		Go(func() { Announce(text) })
		return
	}
	// The focused window first, then one an assistive client has asked about.
	rank := func(w *Window) int {
		switch {
		case w.osFocus:
			return 2
		case w.acc != nil:
			return 1
		}
		return 0
	}
	var target *Window
	for _, w := range openWindows {
		if !w.closed && (target == nil || rank(w) > rank(target)) {
			target = w
		}
	}
	if target != nil {
		target.Announce(text)
	}
}

// Notifications.

var (
	axPostNotification         func(element, name darwin.ID)
	axPostNotificationWithInfo func(element, name, info darwin.ID)
)

type axNotificationNames struct {
	focusChanged, valueChanged, titleChanged, layoutChanged, destroyed,
	selectedTextChanged, selectedChildrenChanged, rowExpanded, rowCollapsed,
	expandedChanged, menuOpened, announcement, announcementKey, priorityKey darwin.ID
}

var axNames = sync.OnceValue(func() *axNotificationNames {
	darwin.Func(&axPostNotification, darwin.AppKit, "NSAccessibilityPostNotification")
	darwin.Func(&axPostNotificationWithInfo, darwin.AppKit, "NSAccessibilityPostNotificationWithUserInfo")
	sym := func(name string) darwin.ID { return darwin.SymbolID(darwin.AppKit, name) }
	// Constants AppKit does not export are made once and kept.
	str := func(s string) darwin.ID { return darwin.Retain(darwin.String(s)) }
	return &axNotificationNames{
		focusChanged:            sym("NSAccessibilityFocusedUIElementChangedNotification"),
		valueChanged:            sym("NSAccessibilityValueChangedNotification"),
		titleChanged:            sym("NSAccessibilityTitleChangedNotification"),
		layoutChanged:           sym("NSAccessibilityLayoutChangedNotification"),
		destroyed:               sym("NSAccessibilityUIElementDestroyedNotification"),
		selectedTextChanged:     sym("NSAccessibilitySelectedTextChangedNotification"),
		selectedChildrenChanged: sym("NSAccessibilitySelectedChildrenChangedNotification"),
		rowExpanded:             sym("NSAccessibilityRowExpandedNotification"),
		rowCollapsed:            sym("NSAccessibilityRowCollapsedNotification"),
		expandedChanged:         str("AXExpandedChanged"),
		menuOpened:              str("AXMenuOpened"),
		announcement:            sym("NSAccessibilityAnnouncementRequestedNotification"),
		announcementKey:         sym("NSAccessibilityAnnouncementKey"),
		priorityKey:             sym("NSAccessibilityPriorityKey"),
	}
})

func axPost(element, name darwin.ID) {
	if element != 0 {
		axPostNotification(element, name)
	}
}

func axPostInfo(element, name, info darwin.ID) {
	if element != 0 {
		axPostNotificationWithInfo(element, name, info)
	}
}

// Roles and values.

// axRoles are the NSAccessibility role and subrole of each Role.
var axRoles = map[Role][2]string{
	RoleGroup:        {"AXGroup"},
	RoleButton:       {"AXButton"},
	RoleLink:         {"AXLink"},
	RoleCheckBox:     {"AXCheckBox"},
	RoleSwitch:       {"AXCheckBox", "AXSwitch"},
	RoleRadio:        {"AXRadioButton"},
	RoleText:         {"AXStaticText"},
	RoleImage:        {"AXImage"},
	RoleEdit:         {"AXTextField"},
	RoleComboBox:     {"AXPopUpButton"},
	RoleSlider:       {"AXSlider"},
	RoleProgressBar:  {"AXProgressIndicator"},
	RoleList:         {"AXList"},
	RoleListItem:     {"AXGroup"},
	RoleTree:         {"AXOutline"},
	RoleTreeItem:     {"AXRow", "AXOutlineRow"},
	RoleMenu:         {"AXMenu"},
	RoleMenuItem:     {"AXMenuItem"},
	RoleMenuBar:      {"AXMenuBar"},
	RoleTabList:      {"AXTabGroup"},
	RoleTab:          {"AXRadioButton", "AXTabButton"},
	RoleToolBar:      {"AXToolbar"},
	RoleStatusBar:    {"AXGroup", "AXApplicationStatus"},
	RoleDialog:       {"AXGroup", "AXApplicationDialog"},
	RoleSeparator:    {"AXSplitter"},
	RoleDocument:     {"AXGroup", "AXDocument"},
	RoleGrid:         {"AXTable"},
	RoleRow:          {"AXRow", "AXTableRow"},
	RoleGridCell:     {"AXCell"},
	RoleColumnHeader: {"AXCell"},
}

// axRole returns the NSAccessibility role and subrole of an element exposed
// with role r.
func axRole(e *El, r Role) (role, subrole string) {
	a := e.a11y
	switch {
	case r == RoleText && a != nil && a.heading > 0:
		return "AXHeading", ""
	case r == RoleButton && a != nil && a.checked != 0:
		return "AXCheckBox", "AXToggle"
	case r == RoleEdit && e.input != nil && e.input.password:
		return "AXTextField", "AXSecureTextField"
	case r == RoleEdit && e.input != nil && e.input.multiline:
		return "AXTextArea", ""
	}
	rs := axRoles[r]
	if rs[0] == "" {
		rs[0] = "AXGroup"
	}
	return rs[0], rs[1]
}

// axRoleDescriptions name the roles and subroles that AppKit has no
// description of.
var axRoleDescriptions = map[string]string{
	"AXHeading":           "heading",
	"AXApplicationStatus": "status",
	"AXApplicationDialog": "dialog",
	"AXDocument":          "document",
}

// axValueKind is what an element's AXValue holds.
type axValueKind uint8

const (
	axNoValue      axValueKind = iota
	axCheckValue               // NSNumber 0, 1 or 2 for mixed
	axSelectValue              // NSNumber 1 when selected
	axRangeValue               // NSNumber of the range
	axHeadingValue             // NSNumber of the heading level
	axNameValue                // the name, for static text
	axEditValue                // the text of an edit field
	axTextValue                // ValueText or a combo box's choice
)

func axValueKindOf(e *El, r Role) axValueKind {
	a := e.a11y
	switch {
	case r == RoleEdit:
		return axEditValue
	case a != nil && a.checked != 0, r == RoleCheckBox, r == RoleSwitch:
		return axCheckValue
	case r == RoleRadio, r == RoleTab:
		return axSelectValue
	case a != nil && a.rng != nil:
		return axRangeValue
	case r == RoleText && a != nil && a.heading > 0:
		return axHeadingValue
	case r == RoleText:
		return axNameValue
	case r == RoleComboBox, a != nil && a.valueText != "":
		return axTextValue
	}
	return axNoValue
}

// axValueOf returns the element's AXValue as text or as a number, as its
// kind says.
func (w *Window) axValueOf(e *El, r Role) (text string, num float64, k axValueKind) {
	k = axValueKindOf(e, r)
	a := e.a11y
	switch k {
	case axCheckValue:
		if a != nil && a.checked > 1 {
			num = float64(a.checked - 1)
		}
	case axSelectValue:
		if a != nil && a.selected == 2 {
			num = 1
		}
	case axRangeValue:
		num = a.rng.value
	case axHeadingValue:
		num = float64(a.heading)
	case axNameValue:
		text = e.a11yName(r)
	case axEditValue:
		text, _, _ = w.a11yEditText(e, r)
	case axTextValue:
		text = e.a11yValue(r)
	}
	return text, num, k
}

// axValueText returns the element's AXValue as text, to tell when it changes.
func (w *Window) axValueText(e *El, r Role) string {
	s, num, k := w.axValueOf(e, r)
	switch k {
	case axCheckValue, axSelectValue, axRangeValue, axHeadingValue:
		return strconv.FormatFloat(num, 'g', -1, 64)
	}
	return s
}

// axValue returns the element's AXValue: an NSNumber, an NSString or nil.
func (w *Window) axValue(e *El, r Role) darwin.ID {
	s, num, k := w.axValueOf(e, r)
	switch k {
	case axCheckValue, axSelectValue, axHeadingValue:
		return darwin.SendClass("NSNumber", "numberWithLongLong:", int64(num))
	case axRangeValue:
		return darwin.Number(num)
	case axNameValue, axEditValue, axTextValue:
		return darwin.String(s)
	}
	return 0
}

// Text fields.

// a11yEditText returns the text of an edit field as assistive technology
// reads it, which is empty for a password, and its selection in bytes.
func (w *Window) a11yEditText(e *El, r Role) (text string, a, b int) {
	if e.input == nil {
		s := e.a11yValue(r)
		return s, len(s), len(s)
	}
	if e.input.password {
		return "", 0, 0
	}
	if s := w.states[e.id]; s != nil && s.edit != nil {
		st := s.edit
		a, b := st.Selection()
		a, b = min(max(a, 0), len(st.Value)), min(max(b, 0), len(st.Value))
		return st.Value, a, b
	}
	v := e.input.value
	return v, len(v), len(v)
}

// a11yCaret returns an edit field's text and the caret's byte offset in it,
// which is the selection's start after a selection made backward.
func (w *Window) a11yCaret(e *El, r Role) (string, int) {
	s, _, b := w.a11yEditText(e, r)
	if e.input != nil && !e.input.password {
		if st := w.states[e.id]; st != nil && st.edit != nil && st.edit.Value == s {
			return s, min(max(st.edit.Caret, 0), len(s))
		}
	}
	return s, b
}

// a11ySelectedRange is the selection of an edit field in UTF-16 units.
func (w *Window) a11ySelectedRange(e *El, r Role) darwin.NSRange {
	s, a, b := w.a11yEditText(e, r)
	return darwin.NSRange{Location: uint64(utf16Len(s[:a])), Length: uint64(utf16Len(s[a:b]))}
}

// utf16Slice returns the part of s in a UTF-16 range, clamped to s.
func utf16Slice(s string, loc, n uint64) string {
	total := uint64(utf16Len(s))
	loc = min(loc, total)
	end := min(loc+min(n, total), total)
	return s[utf16ToByteOffset(s, int(loc)):utf16ToByteOffset(s, int(end))]
}

// lineOf returns the 0-based line of the UTF-16 index i in s.
func lineOf(s string, i uint64) int64 {
	return int64(strings.Count(utf16Slice(s, 0, i), "\n"))
}

// lineRange returns the UTF-16 range of line n of s, with its line break.
func lineRange(s string, n int64) darwin.NSRange {
	var at uint64
	for i, line := range strings.SplitAfter(s, "\n") {
		l := uint64(utf16Len(line))
		if int64(i) == n {
			return darwin.NSRange{Location: at, Length: l}
		}
		at += l
	}
	return darwin.NSRange{Location: darwin.NSNotFound}
}

// Geometry.

// axScreenRect returns the visible part of an element in screen
// coordinates, or an empty rectangle when it is not on screen.
func (w *Window) axScreenRect(e *El) darwin.CGRect {
	r, ok := visibleRect(e)
	if !ok || !w.hasNativeWindow() {
		return darwin.CGRect{}
	}
	cr := darwin.Rect(float64(r.x), float64(r.y), float64(r.w), float64(r.h))
	cr = darwin.Get[darwin.CGRect](w.view, "convertRect:toView:", cr, darwin.ID(0))
	return darwin.Get[darwin.CGRect](w.nsWindow, "convertRectToScreen:", cr)
}

// a11yHitTest returns the deepest exposed element at a point in screen
// coordinates, or 0.
func (w *Window) a11yHitTest(p darwin.CGPoint) uint64 {
	p = darwin.Get[darwin.CGPoint](w.nsWindow, "convertPointFromScreen:", p)
	x, y := w.viewPoint(p)
	t := w.a11yTree()
	id := t.exposedAncestor(w.hitTest(x, y))
	// Hits are recorded for the elements that take input; static text
	// inside them is found by its frame.
	for {
		next := uint64(0)
		for _, k := range t.kids[id] {
			if r, ok := visibleRect(t.el[k]); ok && r.contains(x, y) {
				next = k
			}
		}
		if next == 0 {
			return id
		}
		id = next
	}
}

// Actions.

// axScrollToVisible is NSAccessibilityScrollToVisibleAction, exported only
// from macOS 26.
const axScrollToVisible = "AXScrollToVisible"

// axActions are the protocol's action methods elements implement, by
// action name.
var axActions = []struct {
	name string
	sel  darwin.SEL
}{
	{"AXPress", darwin.Sel("accessibilityPerformPress")},
	{"AXIncrement", darwin.Sel("accessibilityPerformIncrement")},
	{"AXDecrement", darwin.Sel("accessibilityPerformDecrement")},
}

// inScroller reports whether e is inside a scroll container, which can
// scroll it into view.
func inScroller(e *El) bool {
	for a := e.parent; a != nil; a = a.parent {
		if (a.style.ScrollX || a.style.ScrollY) && a.state != nil {
			return true
		}
	}
	return false
}

// post runs f with the node's current element after the call returns, so
// that handlers which open dialogs do not block the assistive client.
func (n *a11yNode) post(f func(e *El)) {
	w, id := n.w, n.id
	w.Post(func() {
		if e := w.byID[id]; e != nil {
			f(e)
		}
	})
}

func (n *a11yNode) click() {
	n.post(func(e *El) {
		if e.onClick != nil {
			e.onClick()
		}
	})
}

// setRange sets a range's value, and remembers it as the value steps start
// from until the tree is rebuilt with it.
func (n *a11yNode) setRange(v float64) {
	n.rngTo, n.rngVer, n.rngSet = v, n.w.treeVer, true
	n.post(func(e *El) {
		if g := e.a11y; g != nil && g.rng != nil && g.rng.set != nil {
			g.rng.set(min(max(v, g.rng.min), g.rng.max))
		}
	})
}

// step moves a range by its small step. Two steps before a rebuild move it
// two steps, not one from the value the tree was built with.
func (n *a11yNode) step(e *El, dir float64) bool {
	g := e.a11y.rng
	small, _ := g.steps()
	from := g.value
	if n.rngSet && n.rngVer == n.w.treeVer {
		from = min(max(n.rngTo, g.min), g.max)
	}
	n.setRange(from + dir*small)
	return true
}

// setText replaces the whole text of an edit field, or with sel only its
// selection.
func (n *a11yNode) setText(s string, sel bool) {
	w := n.w
	n.post(func(e *El) {
		if e.input == nil || e.input.onChange == nil {
			return
		}
		if !sel {
			st := w.editOf(e)
			st.Anchor, st.Caret = 0, len(st.Value)
		}
		w.insertText(e, s)
	})
}

// The element class.

var (
	selAXFrame         = darwin.Sel("accessibilityFrame")
	selAXFrameForRange = darwin.Sel("accessibilityFrameForRange:")
	selAXSelectedRange = darwin.Sel("accessibilitySelectedTextRange")
	selAXVisibleRange  = darwin.Sel("accessibilityVisibleCharacterRange")
	selAXRangeForLine  = darwin.Sel("accessibilityRangeForLine:")
	selAXRoleDesc      = darwin.Sel("accessibilityRoleDescription")
	selAXAllowed       = darwin.Sel("isAccessibilitySelectorAllowed:")
)

// axGate says which elements answer a selector. Selectors not in it are
// left to NSAccessibilityElement.
var axGate = sync.OnceValue(func() map[darwin.SEL]func(e *El, r Role) bool {
	isEdit := func(e *El, r Role) bool { return r == RoleEdit }
	editable := func(e *El, r Role) bool { return r == RoleEdit && e.input != nil && e.input.onChange != nil }
	ranged := func(e *El, r Role) bool { return e.a11y != nil && e.a11y.rng != nil }
	settable := func(e *El, r Role) bool { return ranged(e, r) && e.a11y.rng.set != nil }
	selectable := func(e *El, r Role) bool { return e.a11y != nil && e.a11y.selected != 0 }
	expandable := func(e *El, r Role) bool { return e.a11y != nil && e.a11y.expanded != 0 && r != RoleTreeItem }
	g := map[string]func(e *El, r Role) bool{
		"accessibilityPerformPress": func(e *El, r Role) bool {
			return e.onClick != nil && r != RoleEdit && r != RoleSlider
		},
		"accessibilityPerformIncrement": settable,
		"accessibilityPerformDecrement": settable,
		"setAccessibilityValue:":        func(e *El, r Role) bool { return editable(e, r) || settable(e, r) },
		"accessibilityValue":            func(e *El, r Role) bool { return axValueKindOf(e, r) != axNoValue },
		"accessibilityValueDescription": func(e *El, r Role) bool { return ranged(e, r) && e.a11y.valueText != "" },
		"accessibilityMinValue":         ranged,
		"accessibilityMaxValue":         ranged,
		"setAccessibilityFocused:":      func(e *El, r Role) bool { return e.focusable },
		"isAccessibilitySelected":       selectable,
		"setAccessibilitySelected:":     func(e *El, r Role) bool { return selectable(e, r) && e.onClick != nil },
		"isAccessibilityExpanded":       expandable,
		"setAccessibilityExpanded:":     func(e *El, r Role) bool { return expandable(e, r) && e.onClick != nil },
		"isAccessibilityDisclosed":      func(e *El, r Role) bool { return r == RoleTreeItem },
		"accessibilityDisclosureLevel":  func(e *El, r Role) bool { return r == RoleTreeItem },
		"setAccessibilityDisclosed:": func(e *El, r Role) bool {
			return r == RoleTreeItem && e.a11y != nil && e.a11y.expanded != 0 && e.onClick != nil
		},
		"accessibilitySelectedChildren": func(e *El, r Role) bool {
			return r == RoleList || r == RoleTabList || r == RoleTree || r == RoleGrid
		},
		"accessibilityRows": func(e *El, r Role) bool { return r == RoleTree || r == RoleGrid },
		"accessibilityPlaceholderValue": func(e *El, r Role) bool {
			return r == RoleEdit && e.input != nil && e.input.placeholder != "" && e.a11yName(r) != e.input.placeholder
		},
		"accessibilityHelp":       func(e *El, r Role) bool { return e.tooltip != "" && e.tooltip != e.a11yName(r) },
		"accessibilityIdentifier": func(e *El, r Role) bool { return e.key != "" },
		"accessibilityLabel": func(e *El, r Role) bool {
			return axValueKindOf(e, r) != axNameValue && e.a11yName(r) != ""
		},
		"accessibilityNumberOfCharacters":       isEdit,
		"accessibilitySelectedText":             isEdit,
		"setAccessibilitySelectedText:":         editable,
		"accessibilitySelectedTextRange":        isEdit,
		"setAccessibilitySelectedTextRange:":    func(e *El, r Role) bool { return r == RoleEdit && e.input != nil },
		"accessibilityVisibleCharacterRange":    isEdit,
		"accessibilityInsertionPointLineNumber": isEdit,
		"accessibilityLineForIndex:":            isEdit,
		"accessibilityRangeForLine:":            isEdit,
		"accessibilityStringForRange:":          isEdit,
		"accessibilityFrameForRange:":           isEdit,
	}
	m := make(map[darwin.SEL]func(e *El, r Role) bool, len(g))
	for name, f := range g {
		m[darwin.Sel(name)] = f
	}
	return m
})

// Type encodings of the methods with struct arguments or results.
const (
	encAXRect         = "{CGRect={CGPoint=dd}{CGSize=dd}}"
	encAXRange        = "{_NSRange=QQ}"
	encAXPoint        = "{CGPoint=dd}"
	encAXFrame        = encAXRect + "@:"
	encAXFrameOfRange = encAXRect + "@:" + encAXRange
	encAXRangeFn      = encAXRange + "@:"
	encAXRangeOfLine  = encAXRange + "@:q"
)

var (
	elementClassOnce sync.Once
	axElementClass   darwin.Class
)

// elementClass returns the FairyAccessibilityElement class.
func elementClass() darwin.Class {
	elementClassOnce.Do(func() { axElementClass = registerElementClass() })
	return axElementClass
}

func registerElementClass() darwin.Class {
	type id = darwin.ID
	type sel = darwin.SEL
	str := func(s string) id {
		if s == "" {
			return 0
		}
		return darwin.String(s)
	}
	// getter makes a method that answers from the node's element, or with
	// zero once the element is gone.
	type getter[T any] func(n *a11yNode, e *El, r Role) T
	obj := func(f getter[id]) func(id, sel) id {
		return func(self id, _ sel) id {
			if n, e, r := axNode(self); n != nil {
				return f(n, e, r)
			}
			return 0
		}
	}
	boolean := func(f getter[bool]) func(id, sel) bool {
		return func(self id, _ sel) bool {
			if n, e, r := axNode(self); n != nil {
				return f(n, e, r)
			}
			return false
		}
	}
	integer := func(f getter[int64]) func(id, sel) int64 {
		return func(self id, _ sel) int64 {
			if n, e, r := axNode(self); n != nil {
				return f(n, e, r)
			}
			return 0
		}
	}
	setBool := func(f func(n *a11yNode, e *El, r Role, on bool)) func(id, sel, bool) {
		return func(self id, _ sel, on bool) {
			if n, e, r := axNode(self); n != nil {
				f(n, e, r, on)
			}
		}
	}
	setObj := func(f func(n *a11yNode, e *El, r Role, v id)) func(id, sel, id) {
		return func(self id, _ sel, v id) {
			if n, e, r := axNode(self); n != nil {
				f(n, e, r, v)
			}
		}
	}
	editText := func(n *a11yNode, e *El, r Role) string {
		s, _, _ := n.w.a11yEditText(e, r)
		return s
	}
	methods := []objc.MethodDef{
		{Cmd: darwin.Sel("isAccessibilityElement"), Fn: func(id, sel) bool { return true }},
		{Cmd: darwin.Sel("accessibilityNotifiesWhenDestroyed"), Fn: func(id, sel) bool { return true }},
		{Cmd: darwin.Sel("accessibilityRole"), Fn: func(self id, _ sel) id {
			if n, e, r := axNode(self); n != nil {
				role, _ := axRole(e, r)
				return darwin.String(role)
			}
			return darwin.String("AXUnknown")
		}},
		{Cmd: darwin.Sel("accessibilitySubrole"), Fn: obj(func(n *a11yNode, e *El, r Role) id {
			_, sub := axRole(e, r)
			return str(sub)
		})},
		{Cmd: selAXRoleDesc, Fn: func(self id, cmd sel) id {
			if n, e, r := axNode(self); n != nil {
				role, sub := axRole(e, r)
				if d := axRoleDescriptions[sub]; d != "" {
					return darwin.String(d)
				}
				if d := axRoleDescriptions[role]; d != "" {
					return darwin.String(d)
				}
			}
			return darwin.SendSuper(self, axElementClass, cmd)
		}},
		{Cmd: darwin.Sel("accessibilityLabel"), Fn: obj(func(n *a11yNode, e *El, r Role) id {
			if axValueKindOf(e, r) == axNameValue {
				return 0 // static text reads its value
			}
			return str(e.a11yName(r))
		})},
		{Cmd: darwin.Sel("accessibilityValue"), Fn: obj(func(n *a11yNode, e *El, r Role) id {
			return n.w.axValue(e, r)
		})},
		{Cmd: darwin.Sel("accessibilityValueDescription"), Fn: obj(func(n *a11yNode, e *El, r Role) id {
			if e.a11y != nil && e.a11y.rng != nil {
				return str(e.a11y.valueText)
			}
			return 0
		})},
		{Cmd: darwin.Sel("accessibilityMinValue"), Fn: obj(func(n *a11yNode, e *El, r Role) id {
			if e.a11y != nil && e.a11y.rng != nil {
				return darwin.Number(e.a11y.rng.min)
			}
			return 0
		})},
		{Cmd: darwin.Sel("accessibilityMaxValue"), Fn: obj(func(n *a11yNode, e *El, r Role) id {
			if e.a11y != nil && e.a11y.rng != nil {
				return darwin.Number(e.a11y.rng.max)
			}
			return 0
		})},
		{Cmd: darwin.Sel("accessibilityHelp"), Fn: obj(func(n *a11yNode, e *El, r Role) id {
			if e.tooltip != e.a11yName(r) {
				return str(e.tooltip)
			}
			return 0
		})},
		{Cmd: darwin.Sel("accessibilityIdentifier"), Fn: obj(func(n *a11yNode, e *El, r Role) id { return str(e.key) })},
		{Cmd: darwin.Sel("accessibilityPlaceholderValue"), Fn: obj(func(n *a11yNode, e *El, r Role) id {
			if e.input != nil && e.input.placeholder != e.a11yName(r) {
				return str(e.input.placeholder)
			}
			return 0
		})},
		{Cmd: darwin.Sel("accessibilityParent"), Fn: func(self id, _ sel) id {
			n := byElement[self]
			if n == nil || n.w.closed || n.w.acc == nil {
				return 0
			}
			return n.w.a11yElement(n.w.a11yTree().parent[n.id])
		}},
		{Cmd: darwin.Sel("accessibilityChildren"), Fn: obj(func(n *a11yNode, e *El, r Role) id {
			return n.w.a11yArray(n.w.a11yTree().kids[n.id])
		})},
		{Cmd: darwin.Sel("accessibilityWindow"), Fn: func(self id, _ sel) id {
			if n := byElement[self]; n != nil && !n.w.closed {
				return n.w.nsWindow
			}
			return 0
		}},
		{Cmd: darwin.Sel("accessibilityTopLevelUIElement"), Fn: func(self id, _ sel) id {
			if n := byElement[self]; n != nil && !n.w.closed {
				return n.w.nsWindow
			}
			return 0
		}},
		{Cmd: darwin.Sel("isAccessibilityEnabled"), Fn: boolean(func(n *a11yNode, e *El, r Role) bool { return !e.disabled() })},
		{Cmd: darwin.Sel("isAccessibilityFocused"), Fn: boolean(func(n *a11yNode, e *El, r Role) bool {
			return n.w.osFocus && n.w.a11yFocus() == n.id
		})},
		{Cmd: darwin.Sel("setAccessibilityFocused:"), Fn: setBool(func(n *a11yNode, e *El, r Role, on bool) {
			if on && e.focusable {
				n.w.setFocus(e)
				n.w.Update()
			}
		})},
		{Cmd: darwin.Sel("isAccessibilitySelected"), Fn: boolean(func(n *a11yNode, e *El, r Role) bool {
			return e.a11y != nil && e.a11y.selected == 2
		})},
		{Cmd: darwin.Sel("setAccessibilitySelected:"), Fn: setBool(func(n *a11yNode, e *El, r Role, on bool) {
			if on && (e.a11y == nil || e.a11y.selected != 2) {
				n.click()
			}
		})},
		{Cmd: darwin.Sel("isAccessibilityExpanded"), Fn: boolean(func(n *a11yNode, e *El, r Role) bool {
			return e.a11y != nil && e.a11y.expanded == 2
		})},
		{Cmd: darwin.Sel("setAccessibilityExpanded:"), Fn: setBool(func(n *a11yNode, e *El, r Role, on bool) {
			if on != (e.a11y != nil && e.a11y.expanded == 2) {
				n.click()
			}
		})},
		{Cmd: darwin.Sel("isAccessibilityDisclosed"), Fn: boolean(func(n *a11yNode, e *El, r Role) bool {
			return e.a11y != nil && e.a11y.expanded == 2
		})},
		{Cmd: darwin.Sel("setAccessibilityDisclosed:"), Fn: setBool(func(n *a11yNode, e *El, r Role, on bool) {
			if on != (e.a11y != nil && e.a11y.expanded == 2) {
				n.click()
			}
		})},
		{Cmd: darwin.Sel("accessibilityDisclosureLevel"), Fn: integer(func(n *a11yNode, e *El, r Role) int64 {
			if e.a11y != nil && e.a11y.level > 0 {
				return int64(e.a11y.level - 1)
			}
			return 0
		})},
		{Cmd: darwin.Sel("accessibilitySelectedChildren"), Fn: obj(func(n *a11yNode, e *El, r Role) id {
			t := n.w.a11yTree()
			var ids []uint64
			for _, k := range t.kids[n.id] {
				if a := t.el[k].a11y; a != nil && a.selected == 2 {
					ids = append(ids, k)
				}
			}
			return n.w.a11yArray(ids)
		})},
		{Cmd: darwin.Sel("accessibilityRows"), Fn: obj(func(n *a11yNode, e *El, r Role) id {
			t := n.w.a11yTree()
			var ids []uint64
			var walk func(id uint64)
			walk = func(id uint64) {
				for _, k := range t.kids[id] {
					switch t.role[k] {
					case RoleRow, RoleTreeItem:
						ids = append(ids, k)
					}
					switch t.role[k] {
					case RoleGrid, RoleTree:
					default:
						walk(k)
					}
				}
			}
			walk(n.id)
			return n.w.a11yArray(ids)
		})},
		// AppKit has no protocol method for AXScrollToVisible, which
		// VoiceOver sends to bring an element it moves to into view, so
		// the element answers the older action methods, listing the
		// protocol's actions it allows along with it.
		{Cmd: darwin.Sel("accessibilityActionNames"), Fn: func(self id, _ sel) id {
			arr := darwin.SendClass("NSMutableArray", "array")
			_, e, _ := axNode(self)
			if e == nil {
				return arr
			}
			for _, a := range axActions {
				if darwin.Get[bool](self, "isAccessibilitySelectorAllowed:", a.sel) {
					darwin.Send(arr, "addObject:", darwin.String(a.name))
				}
			}
			if inScroller(e) {
				darwin.Send(arr, "addObject:", darwin.String(axScrollToVisible))
			}
			return arr
		}},
		{Cmd: darwin.Sel("accessibilityPerformAction:"), Fn: func(self id, _ sel, action id) {
			n, e, _ := axNode(self)
			if n == nil {
				return
			}
			name := darwin.GoString(action)
			if name == axScrollToVisible {
				n.w.scrollIntoView(e)
				return
			}
			for _, a := range axActions {
				if a.name == name && darwin.Get[bool](self, "isAccessibilitySelectorAllowed:", a.sel) {
					objc.Send[bool](self, a.sel)
				}
			}
		}},
		{Cmd: darwin.Sel("accessibilityPerformPress"), Fn: boolean(func(n *a11yNode, e *El, r Role) bool {
			if e.onClick == nil {
				return false
			}
			n.click()
			return true
		})},
		{Cmd: darwin.Sel("accessibilityPerformIncrement"), Fn: boolean(func(n *a11yNode, e *El, r Role) bool {
			return e.a11y != nil && e.a11y.rng != nil && e.a11y.rng.set != nil && n.step(e, 1)
		})},
		{Cmd: darwin.Sel("accessibilityPerformDecrement"), Fn: boolean(func(n *a11yNode, e *El, r Role) bool {
			return e.a11y != nil && e.a11y.rng != nil && e.a11y.rng.set != nil && n.step(e, -1)
		})},
		{Cmd: darwin.Sel("setAccessibilityValue:"), Fn: setObj(func(n *a11yNode, e *El, r Role, v id) {
			switch {
			case v == 0:
			case r == RoleEdit:
				if s, ok := nsText(v); ok {
					n.setText(s, false)
				}
			case e.a11y != nil && e.a11y.rng != nil && e.a11y.rng.set != nil:
				if darwin.Get[bool](v, "respondsToSelector:", darwin.Sel("doubleValue")) {
					n.setRange(darwin.Get[float64](v, "doubleValue"))
				}
			}
		})},
		{Cmd: darwin.Sel("accessibilityNumberOfCharacters"), Fn: integer(func(n *a11yNode, e *El, r Role) int64 {
			return int64(utf16Len(editText(n, e, r)))
		})},
		{Cmd: darwin.Sel("accessibilitySelectedText"), Fn: obj(func(n *a11yNode, e *El, r Role) id {
			s, a, b := n.w.a11yEditText(e, r)
			return darwin.String(s[a:b])
		})},
		{Cmd: darwin.Sel("setAccessibilitySelectedText:"), Fn: setObj(func(n *a11yNode, e *El, r Role, v id) {
			if s, ok := nsText(v); ok && r == RoleEdit {
				n.setText(s, true)
			}
		})},
		{Cmd: darwin.Sel("accessibilityInsertionPointLineNumber"), Fn: integer(func(n *a11yNode, e *El, r Role) int64 {
			s, caret := n.w.a11yCaret(e, r)
			return int64(strings.Count(s[:caret], "\n"))
		})},
		{Cmd: darwin.Sel("accessibilityLineForIndex:"), Fn: func(self id, _ sel, i int64) int64 {
			if n, e, r := axNode(self); n != nil && i >= 0 {
				return lineOf(editText(n, e, r), uint64(i))
			}
			return 0
		}},
		{Cmd: selAXAllowed, Fn: func(self id, cmd sel, s sel) bool {
			if gate := axGate()[s]; gate != nil {
				n, e, r := axNode(self)
				return n != nil && gate(e, r)
			}
			return darwin.GetSuper[bool](self, axElementClass, cmd, s)
		}},
		{Cmd: darwin.Sel("forwardInvocation:"), Fn: func(self id, cmd sel, inv id) {
			if !axInvocation(self, inv) {
				darwin.SendSuper(self, axElementClass, cmd, inv)
			}
		}},
	}
	c, err := objc.RegisterClass("FairyAccessibilityElement", darwin.Cls("NSAccessibilityElement"), nil, nil, methods)
	if err != nil {
		panic(err)
	}
	add := func(name, types string, fn any) {
		if !c.AddMethod(darwin.Sel(name), objc.NewIMP(fn), types) {
			panic("fairy: add method " + name)
		}
	}
	add("accessibilityStringForRange:", "@@:"+encAXRange, func(self id, _ sel, loc, n uint64) id {
		if node, e, r := axNode(self); node != nil {
			return darwin.String(utf16Slice(editText(node, e, r), loc, n))
		}
		return 0
	})
	add("setAccessibilitySelectedTextRange:", "v@:"+encAXRange, func(self id, _ sel, loc, n uint64) {
		node, e, r := axNode(self)
		if node == nil || r != RoleEdit || e.input == nil || e.input.password {
			return
		}
		w := node.w
		st := w.editOf(e)
		total := uint64(utf16Len(st.Value))
		loc = min(loc, total)
		w.selectUTF16(st, loc, min(n, total-loc))
		w.invalidate()
	})
	fwd := darwin.Symbol(darwin.Objc, "_objc_msgForward")
	rectFwd := fwd
	if runtime.GOARCH == "amd64" {
		// A CGRect is returned through memory on x86-64.
		rectFwd = darwin.Symbol(darwin.Objc, "_objc_msgForward_stret")
	}
	c.AddMethod(selAXFrame, objc.IMP(rectFwd), encAXFrame)
	c.AddMethod(selAXFrameForRange, objc.IMP(rectFwd), encAXFrameOfRange)
	c.AddMethod(selAXSelectedRange, objc.IMP(fwd), encAXRangeFn)
	c.AddMethod(selAXVisibleRange, objc.IMP(fwd), encAXRangeFn)
	c.AddMethod(selAXRangeForLine, objc.IMP(fwd), encAXRangeOfLine)
	return c
}

// axInvocation answers a forwarded call to an element's getter that returns
// a struct. It reports false for a selector it does not answer.
func axInvocation(self darwin.ID, inv darwin.ID) bool {
	n, e, r := axNode(self)
	switch darwin.Get[darwin.SEL](inv, "selector") {
	case selAXFrame, selAXFrameForRange:
		// The frame of a range is the field's: text positions are not
		// exposed.
		rect := new(darwin.CGRect)
		if n != nil {
			*rect = n.w.axScreenRect(e)
		}
		darwin.Send(inv, "setReturnValue:", unsafe.Pointer(rect))
	case selAXSelectedRange:
		rng := &darwin.NSRange{Location: darwin.NSNotFound}
		if n != nil && r == RoleEdit {
			*rng = n.w.a11ySelectedRange(e, r)
		}
		darwin.Send(inv, "setReturnValue:", unsafe.Pointer(rng))
	case selAXVisibleRange:
		rng := &darwin.NSRange{Location: darwin.NSNotFound}
		if n != nil && r == RoleEdit {
			s, _, _ := n.w.a11yEditText(e, r)
			*rng = darwin.NSRange{Length: uint64(utf16Len(s))}
		}
		darwin.Send(inv, "setReturnValue:", unsafe.Pointer(rng))
	case selAXRangeForLine:
		line := new(int64)
		darwin.Send(inv, "getArgument:atIndex:", unsafe.Pointer(line), int64(2))
		rng := &darwin.NSRange{Location: darwin.NSNotFound}
		if n != nil && r == RoleEdit {
			s, _, _ := n.w.a11yEditText(e, r)
			*rng = lineRange(s, *line)
		}
		darwin.Send(inv, "setReturnValue:", unsafe.Pointer(rng))
	default:
		return false
	}
	return true
}

// addAccessibility makes the view the root of the window's accessibility
// tree.
func addAccessibility(c objc.Class) {
	type id = darwin.ID
	type sel = darwin.SEL
	win := func(self id) *Window {
		if w := viewWindow(self); w != nil && w.a11yStart() {
			return w
		}
		return nil
	}
	add := func(name, types string, fn any) {
		if !c.AddMethod(darwin.Sel(name), objc.NewIMP(fn), types) {
			panic("fairy: add method " + name)
		}
	}
	add("isAccessibilityElement", "B@:", func(id, sel) bool { return false })
	add("accessibilityRole", "@@:", func(id, sel) id { return darwin.String("AXGroup") })
	add("accessibilityChildren", "@@:", func(self id, cmd sel) id {
		w := win(self)
		if w == nil {
			return darwin.SendSuper(self, c, cmd)
		}
		arr := w.a11yArray(w.a11yTree().kids[0])
		// Native views placed in the window, such as a web view.
		if sub := darwin.SendSuper(self, c, cmd); sub != 0 {
			darwin.Send(arr, "addObjectsFromArray:", sub)
		}
		return arr
	})
	add("accessibilityFocusedUIElement", "@@:", func(self id, cmd sel) id {
		if w := win(self); w != nil {
			if f := w.a11yFocus(); f != 0 {
				return w.a11yElement(f)
			}
		}
		return darwin.SendSuper(self, c, cmd)
	})
	add("accessibilityHitTest:", "@@:"+encAXPoint, func(self id, cmd sel, x, y float64) id {
		if w := win(self); w != nil {
			if hit := w.a11yHitTest(darwin.CGPoint{X: x, Y: y}); hit != 0 {
				return w.a11yElement(hit)
			}
		}
		return darwin.SendSuper(self, c, cmd, darwin.CGPoint{X: x, Y: y})
	})
}
