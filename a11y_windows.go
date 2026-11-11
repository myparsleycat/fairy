package fairy

import (
	"slices"
	"strings"

	"github.com/myparsleycat/fairy/internal/uia"
	"github.com/myparsleycat/fairy/internal/win"
)

func init() { uia.Dispatch = doQuiet }

// a11yState is the window's UI Automation side. It exists once a client has
// asked for the window's provider.
type a11yState struct {
	root   *uia.Provider
	nodes  map[uint64]*a11yNode
	tree   *a11yTree
	ver    int       // treeVer the tree was built from
	evTree *a11yTree // tree of the last change-event pass
	focus  uint64
}

// a11yNode is the provider of one exposed element, kept by element identity.
type a11yNode struct {
	w    *Window
	id   uint64
	prov *uia.Provider
	seen int // treeVer when last in the tree
	last a11ySnap
	kids []uint64
}

// a11ySnap holds the properties whose changes are raised as events.
type a11ySnap struct {
	name     string
	toggle   int32
	selected bool
	expand   int32
	value    string
	rng      float64
	enabled  bool
	live     string // the text of a live region
}

func (w *Window) a11yGetObject(wp, lp uintptr) uintptr {
	if !uia.Available() {
		return win.DefWindowProc(w.hwnd, win.WM_GETOBJECT, wp, lp)
	}
	if w.acc == nil {
		w.acc = &a11yState{nodes: map[uint64]*a11yNode{}}
		w.acc.root = uia.NewRoot(&a11yRootEl{w})
	}
	return uia.ReturnRawElementProvider(uintptr(w.hwnd), wp, lp, w.acc.root)
}

func (w *Window) a11yShutdown() {
	a := w.acc
	if a == nil {
		return
	}
	uia.ReturnRawElementProvider(uintptr(w.hwnd), 0, 0, nil)
	for _, n := range a.nodes {
		n.prov.Disconnect()
		n.prov.Release()
	}
	a.root.Disconnect()
	a.root.Release()
	w.acc = nil
}

// a11yRoots returns the trees UI Automation sees: the view and its layers, or
// only the topmost modal overlay and the layers above it.
func (w *Window) a11yRoots() []*El {
	start := -1
	for i, l := range w.layers {
		if l.modal && !l.hidden {
			start = i
		}
	}
	var roots []*El
	if start < 0 && w.root != nil {
		roots = append(roots, w.root)
	}
	for _, l := range w.layers[max(start, 0):] {
		if !l.hidden {
			roots = append(roots, l.root)
		}
	}
	return roots
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
				n.prov.Disconnect()
				n.prov.Release()
				delete(a.nodes, id)
			}
		}
	}
	return a.tree
}

// a11yProvider returns the provider of an exposed element, or the root for 0.
func (w *Window) a11yProvider(id uint64) *uia.Provider {
	if id == 0 {
		return w.acc.root
	}
	n := w.acc.nodes[id]
	if n == nil {
		n = &a11yNode{w: w, id: id, seen: w.acc.ver}
		n.prov = uia.NewProvider(n, w.acc.root)
		w.acc.nodes[id] = n
	}
	return n.prov
}

// a11yFocus returns the element UI Automation should treat as focused, or 0.
func (w *Window) a11yFocus() uint64 {
	f := w.focusedEl()
	if f == nil {
		return 0
	}
	t := w.a11yTree()
	if a := t.el[t.active]; a != nil && w.activeFor(a, f) {
		return t.active
	}
	return t.exposedAncestor(f)
}

// activeFor reports whether the ActiveDescendant a belongs to the focused
// element f: it is inside f, in the same popover, or in a popover anchored at
// f or one of f's ancestors.
func (w *Window) activeFor(a, f *El) bool {
	ra, rf := a, f
	for ; ra.parent != nil; ra = ra.parent {
		if ra == f {
			return true
		}
	}
	for rf.parent != nil {
		rf = rf.parent
	}
	for _, l := range w.layers {
		if l.root != ra || l.pop == nil {
			continue
		}
		if ra == rf {
			return true
		}
		anchor := w.byID[l.anchor]
		for e := f; e != nil; e = e.parent {
			if e == anchor {
				return true
			}
		}
	}
	return false
}

// a11yAfterFrame raises focus, structure and property change events.
func (w *Window) a11yAfterFrame() {
	a := w.acc
	if a == nil || !uia.ClientsAreListening() {
		return
	}
	t := w.a11yTree()
	if prev := a.evTree; prev != t {
		a.evTree = t
		if prev != nil {
			for id, r := range t.role {
				if _, was := prev.el[id]; was {
					continue
				}
				switch r {
				case RoleMenu:
					w.a11yProvider(id).RaiseEvent(uia.EventMenuOpened)
				case RoleDialog:
					w.a11yProvider(id).RaiseEvent(uia.EventWindowOpened)
				}
			}
		}
		if kids := t.kids[0]; prev != nil && !slices.Equal(prev.kids[0], kids) {
			a.root.RaiseChildrenInvalidated()
		}
		for id, n := range a.nodes {
			if _, ok := t.el[id]; !ok {
				continue
			}
			if kids := t.kids[id]; n.kids != nil && !slices.Equal(n.kids, kids) {
				n.prov.RaiseChildrenInvalidated()
			}
			n.kids = append(n.kids[:0], t.kids[id]...)
			if n.kids == nil {
				n.kids = []uint64{}
			}
			n.raiseChanges()
		}
	}
	focus := uint64(0)
	if w.osFocus {
		focus = w.a11yFocus()
	}
	if focus != a.focus {
		a.focus = focus
		if focus != 0 {
			w.a11yProvider(focus).RaiseEvent(uia.EventFocusChanged)
		}
	}
}

// Announce asks screen readers to read text, as for a finished background
// task or a toast. It does nothing when no screen reader is running. It may
// be called from any goroutine.
func (w *Window) Announce(text string) {
	if !onUIThread() {
		Go(func() { w.Announce(text) })
		return
	}
	if w.acc != nil && text != "" && uia.ClientsAreListening() {
		w.acc.root.RaiseNotification(text)
	}
}

// screenRect converts a window rectangle in logical pixels to screen pixels.
func (w *Window) screenRect(r rectF) uia.Rect {
	var p win.POINT
	win.ClientToScreen(w.hwnd, &p)
	s := float64(w.scale)
	return uia.Rect{
		Left:   float64(p.X) + float64(r.x)*s,
		Top:    float64(p.Y) + float64(r.y)*s,
		Width:  float64(r.w) * s,
		Height: float64(r.h) * s,
	}
}

// visibleRect returns the part of e that is on screen, or false when it is
// scrolled out, culled or inside a culled ancestor.
func visibleRect(e *El) (rectF, bool) {
	for a := e; a != nil; a = a.parent {
		if a.culled {
			return rectF{}, false
		}
	}
	r := rectF{e.absX, e.absY, e.w, e.h}.intersect(e.clipR)
	return r, !r.empty()
}

// scrollIntoView scrolls e's scroll container ancestors, innermost first, so
// that e is visible.
func (w *Window) scrollIntoView(e *El) {
	for a := e.parent; a != nil; a = a.parent {
		if !(a.style.ScrollX || a.style.ScrollY) || a.state == nil {
			continue
		}
		var x, y float32 // e's position in a's content
		for c := e; c != a; c = c.parent {
			x += c.node.X
			y += c.node.Y
			if p := c.parent; p != a && p.state != nil && (p.style.ScrollX || p.style.ScrollY) {
				x -= p.state.scrollX
				y -= p.state.scrollY
			}
		}
		s, n := a.state, &a.node
		sx, sy := s.scrollX, s.scrollY
		if a.style.ScrollY {
			if y < sy {
				sy = y
			} else if y+e.node.H > sy+n.H {
				sy = y + e.node.H - n.H
			}
		}
		if a.style.ScrollX {
			if x < sx {
				sx = x
			} else if x+e.node.W > sx+n.W {
				sx = x + e.node.W - n.W
			}
		}
		w.setScroll(a, sx, sy)
	}
}

var controlTypes = map[Role]int32{
	RoleGroup:        uia.ControlGroup,
	RoleButton:       uia.ControlButton,
	RoleLink:         uia.ControlHyperlink,
	RoleCheckBox:     uia.ControlCheckBox,
	RoleSwitch:       uia.ControlButton,
	RoleRadio:        uia.ControlRadioButton,
	RoleText:         uia.ControlText,
	RoleImage:        uia.ControlImage,
	RoleEdit:         uia.ControlEdit,
	RoleComboBox:     uia.ControlComboBox,
	RoleSlider:       uia.ControlSlider,
	RoleProgressBar:  uia.ControlProgressBar,
	RoleList:         uia.ControlList,
	RoleListItem:     uia.ControlListItem,
	RoleTree:         uia.ControlTree,
	RoleTreeItem:     uia.ControlTreeItem,
	RoleMenu:         uia.ControlMenu,
	RoleMenuItem:     uia.ControlMenuItem,
	RoleMenuBar:      uia.ControlMenuBar,
	RoleTabList:      uia.ControlTab,
	RoleTab:          uia.ControlTabItem,
	RoleToolBar:      uia.ControlToolBar,
	RoleStatusBar:    uia.ControlStatusBar,
	RoleDialog:       uia.ControlPane,
	RoleSeparator:    uia.ControlSeparator,
	RoleDocument:     uia.ControlDocument,
	RoleGrid:         uia.ControlDataGrid,
	RoleRow:          uia.ControlDataItem,
	RoleGridCell:     uia.ControlText,
	RoleColumnHeader: uia.ControlHeaderItem,
}

// a11yRootEl is the window's fragment root.
type a11yRootEl struct{ w *Window }

func (r *a11yRootEl) Alive() bool { return r.w.acc != nil && windows[r.w.hwnd] == r.w }

func (r *a11yRootEl) Navigate(d uia.Direction) *uia.Provider {
	kids := r.w.a11yTree().kids[0]
	if len(kids) == 0 {
		return nil
	}
	switch d {
	case uia.FirstChild:
		return r.w.a11yProvider(kids[0])
	case uia.LastChild:
		return r.w.a11yProvider(kids[len(kids)-1])
	}
	return nil
}

func (r *a11yRootEl) Property(id int32) any {
	if id == uia.PropFrameworkID {
		return "fairy"
	}
	return nil
}

func (r *a11yRootEl) Pattern(int32) any  { return nil }
func (r *a11yRootEl) Bounds() uia.Rect   { return uia.Rect{} }
func (r *a11yRootEl) SetFocus()          {}
func (r *a11yRootEl) RuntimeID() []int32 { return nil }
func (r *a11yRootEl) HWND() uintptr      { return uintptr(r.w.hwnd) }

func (r *a11yRootEl) FromPoint(x, y float64) *uia.Provider {
	w := r.w
	p := win.POINT{X: int32(x), Y: int32(y)}
	win.ScreenToClient(w.hwnd, &p)
	e := w.hitTest(float32(p.X)/w.scale, float32(p.Y)/w.scale)
	if id := w.a11yTree().exposedAncestor(e); id != 0 {
		return w.a11yProvider(id)
	}
	return nil
}

func (r *a11yRootEl) Focus() *uia.Provider {
	if id := r.w.a11yFocus(); id != 0 {
		return r.w.a11yProvider(id)
	}
	return nil
}

func (n *a11yNode) el() (*El, Role) {
	t := n.w.a11yTree()
	return t.el[n.id], t.role[n.id]
}

func (n *a11yNode) Alive() bool {
	if n.w.acc == nil || windows[n.w.hwnd] != n.w {
		return false
	}
	e, _ := n.el()
	return e != nil
}

func (n *a11yNode) RuntimeID() []int32 {
	return []int32{3 /* UiaAppendRuntimeId */, int32(n.id >> 32), int32(n.id)}
}

func (n *a11yNode) Navigate(d uia.Direction) *uia.Provider {
	w, t := n.w, n.w.a11yTree()
	var id uint64
	switch d {
	case uia.Parent:
		return w.a11yProvider(t.parent[n.id])
	case uia.NextSibling:
		id = t.sibling(n.id, 1)
	case uia.PreviousSibling:
		id = t.sibling(n.id, -1)
	case uia.FirstChild:
		if k := t.kids[n.id]; len(k) > 0 {
			id = k[0]
		}
	case uia.LastChild:
		if k := t.kids[n.id]; len(k) > 0 {
			id = k[len(k)-1]
		}
	}
	if id == 0 {
		return nil
	}
	return w.a11yProvider(id)
}

func (n *a11yNode) Property(id int32) any {
	e, r := n.el()
	switch id {
	case uia.PropControlType:
		return controlTypes[r]
	case uia.PropName:
		return e.a11yName(r)
	case uia.PropHasKeyboardFocus:
		return n.w.osFocus && n.w.a11yFocus() == n.id
	case uia.PropIsKeyboardFocusable:
		return e.focusable
	case uia.PropIsEnabled:
		return !e.disabled()
	case uia.PropAutomationID:
		if e.key != "" {
			return e.key
		}
	case uia.PropHelpText:
		if e.tooltip != "" && e.tooltip != e.a11yName(r) {
			return e.tooltip
		}
	case uia.PropIsControlElement, uia.PropIsContentElement:
		return true
	case uia.PropIsPassword:
		return e.input != nil && e.input.password
	case uia.PropIsOffscreen:
		_, ok := visibleRect(e)
		return !ok
	case uia.PropFrameworkID:
		return "fairy"
	case uia.PropHeadingLevel:
		if e.a11y != nil && e.a11y.heading > 0 {
			return uia.Heading1 - 1 + int32(e.a11y.heading)
		}
		return uia.HeadingNone
	case uia.PropIsDialog:
		return r == RoleDialog
	case uia.PropPositionInSet, uia.PropSizeOfSet:
		if pos, size := setPosition(e); size > 0 {
			if id == uia.PropPositionInSet {
				return pos
			}
			return size
		}
	case uia.PropLevel:
		if e.a11y != nil && e.a11y.level > 0 {
			return e.a11y.level
		}
	case uia.PropLiveSetting:
		if l := e.liveSetting(r); l != LiveOff {
			return int32(l)
		}
	}
	return nil
}

// setPosition returns the element's 1-based position in its set and the set's
// size: as set with PositionInSet, or for a virtual list row, its index among
// all rows of the list, built or not.
func setPosition(e *El) (int32, int32) {
	if a := e.a11y; a != nil && a.size > 0 {
		return a.pos, a.size
	}
	if e.rowIndex > 0 && e.parent != nil && e.parent.virtual != nil {
		return e.rowIndex, int32(e.parent.virtual.count)
	}
	return 0, 0
}

func (n *a11yNode) Pattern(id int32) any {
	e, r := n.el()
	a := e.a11y
	if a == nil {
		a = &a11yProps{}
	}
	switch id {
	case uia.PatternInvoke:
		if e.onClick == nil || a.checked != 0 {
			return nil
		}
		switch r {
		case RoleCheckBox, RoleSwitch, RoleRadio, RoleTab, RoleComboBox, RoleEdit, RoleSlider, RoleDialog:
			return nil
		}
		return n
	case uia.PatternToggle:
		if a.checked != 0 || r == RoleCheckBox || r == RoleSwitch {
			return n
		}
	case uia.PatternSelectionItem:
		if a.selected != 0 || r == RoleRadio || r == RoleTab {
			return n
		}
	case uia.PatternExpandCollapse:
		if a.expanded != 0 {
			return n
		}
	case uia.PatternValue:
		if r == RoleEdit || r == RoleComboBox || a.valueText != "" {
			return n
		}
	case uia.PatternRangeValue:
		if a.rng != nil {
			return n
		}
	case uia.PatternScrollItem:
		return n
	}
	return nil
}

func (n *a11yNode) Bounds() uia.Rect {
	e, _ := n.el()
	r, ok := visibleRect(e)
	if !ok {
		return uia.Rect{}
	}
	return n.w.screenRect(r)
}

func (n *a11yNode) SetFocus() {
	e, _ := n.el()
	if e.focusable {
		n.w.setFocus(e)
		n.w.Update()
	}
}

// post runs f with the node's current element after the call returns, so
// that handlers which open dialogs do not block the UI Automation client.
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

func (n *a11yNode) Invoke() { n.click() }

func (n *a11yNode) ToggleState() int32 {
	e, _ := n.el()
	if e.a11y == nil {
		return uia.ToggleOff
	}
	switch e.a11y.checked {
	case 2:
		return uia.ToggleOn
	case 3:
		return uia.ToggleIndeterminate
	}
	return uia.ToggleOff
}

func (n *a11yNode) Toggle() { n.click() }

func (n *a11yNode) IsSelected() bool {
	e, _ := n.el()
	return e.a11y != nil && e.a11y.selected == 2
}

func (n *a11yNode) Select() {
	if !n.IsSelected() {
		n.click()
	}
}

func (n *a11yNode) Container() *uia.Provider {
	t := n.w.a11yTree()
	for id := t.parent[n.id]; id != 0; id = t.parent[id] {
		switch t.role[id] {
		case RoleList, RoleTabList, RoleTree, RoleMenu, RoleGroup, RoleGrid:
			return n.w.a11yProvider(id)
		}
	}
	return nil
}

func (n *a11yNode) ExpandCollapseState() int32 {
	e, _ := n.el()
	if e.a11y != nil && e.a11y.expanded == 2 {
		return uia.Expanded
	}
	return uia.Collapsed
}

func (n *a11yNode) Expand() {
	if n.ExpandCollapseState() == uia.Collapsed {
		n.click()
	}
}

func (n *a11yNode) Collapse() {
	if n.ExpandCollapseState() == uia.Expanded {
		n.click()
	}
}

func (n *a11yNode) Value() string {
	e, r := n.el()
	if e.a11y != nil && e.a11y.valueText != "" {
		return e.a11y.valueText
	}
	if r == RoleComboBox {
		// The shown choice, not the label that names the combo box.
		var b strings.Builder
		for _, c := range e.children {
			textContent(c, &b)
		}
		return b.String()
	}
	if e.input == nil {
		return e.text // a read-only field drawn as Text
	}
	if e.input.password {
		return ""
	}
	return e.input.value
}

func (n *a11yNode) ReadOnly() bool {
	e, r := n.el()
	return r != RoleEdit || e.input == nil || e.input.onChange == nil
}

func (n *a11yNode) RangeReadOnly() bool {
	e, _ := n.el()
	return e.a11y.rng.set == nil
}

func (n *a11yNode) SetValue(s string) {
	w := n.w
	n.post(func(e *El) {
		if e.input == nil || e.input.onChange == nil {
			return
		}
		st := w.editOf(e)
		st.Anchor, st.Caret = 0, len(st.Value)
		w.insertText(e, s)
	})
}

func (n *a11yNode) Range() (value, lo, hi, small, large float64) {
	e, _ := n.el()
	g := e.a11y.rng
	small = g.step
	if small <= 0 {
		small = (g.max - g.min) / 100
	}
	return g.value, g.min, g.max, small, max((g.max-g.min)/10, small)
}

func (n *a11yNode) SetRangeValue(v float64) {
	n.post(func(e *El) {
		if e.a11y != nil && e.a11y.rng != nil && e.a11y.rng.set != nil {
			e.a11y.rng.set(v)
		}
	})
}

func (n *a11yNode) ScrollIntoView() {
	e, _ := n.el()
	n.w.scrollIntoView(e)
}

// raiseChanges raises property change events against the last snapshot.
func (n *a11yNode) raiseChanges() {
	e, r := n.el()
	s := a11ySnap{name: e.a11yName(r), enabled: !e.disabled()}
	a := e.a11y
	if a != nil {
		s.toggle = n.ToggleState()
		s.selected = a.selected == 2
		s.expand = n.ExpandCollapseState()
		if a.rng != nil {
			s.rng = a.rng.value
		}
	}
	if r == RoleEdit || r == RoleComboBox || a != nil && a.valueText != "" {
		s.value = n.Value()
	}
	if e.liveSetting(r) != LiveOff {
		var b strings.Builder
		textContent(e, &b)
		s.live = b.String()
	}
	old := n.last
	n.last = s
	if old == (a11ySnap{}) {
		return // first look: nothing to compare with
	}
	p := n.prov
	if s.name != old.name {
		p.RaisePropertyChanged(uia.PropName, old.name, s.name)
	}
	if s.enabled != old.enabled {
		p.RaisePropertyChanged(uia.PropIsEnabled, old.enabled, s.enabled)
	}
	if s.toggle != old.toggle {
		p.RaisePropertyChanged(uia.PropToggleState, old.toggle, s.toggle)
	}
	if s.selected != old.selected {
		p.RaisePropertyChanged(uia.PropSelectionIsSelected, old.selected, s.selected)
	}
	if s.expand != old.expand {
		p.RaisePropertyChanged(uia.PropExpandCollapseState, old.expand, s.expand)
	}
	if s.value != old.value {
		p.RaisePropertyChanged(uia.PropValueValue, old.value, s.value)
	}
	if s.rng != old.rng {
		p.RaisePropertyChanged(uia.PropRangeValueValue, old.rng, s.rng)
	}
	if s.live != old.live && s.live != "" {
		p.RaiseEvent(uia.EventLiveRegionChanged)
	}
}

// Announce asks screen readers to read text in the focused window, or in
// another window when none of them has focus. It may be called from any goroutine.
func Announce(text string) {
	if !onUIThread() {
		Go(func() { Announce(text) })
		return
	}
	var target *Window
	for _, w := range windows {
		if w.acc != nil && (target == nil || w.osFocus) {
			target = w
		}
	}
	if target != nil {
		target.Announce(text)
	}
}
