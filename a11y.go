package fairy

import "strings"

// Role is what an element is to assistive technology such as screen readers.
type Role uint8

const (
	// RoleAuto derives the role from the element: an Input is an edit field,
	// Text is text, an element with OnClick is a button, and a plain
	// container is left out while its children stay in the tree.
	RoleAuto Role = iota
	// RoleNone leaves the element out of the accessibility tree; its children stay in it.
	RoleNone
	// RoleHidden leaves the element and its subtree out of the accessibility
	// tree, as for decoration or a caption that a control already uses as its label.
	RoleHidden
	RoleGroup
	RoleButton
	RoleLink
	RoleCheckBox
	RoleSwitch
	RoleRadio
	RoleText
	RoleImage
	RoleEdit
	RoleComboBox
	RoleSlider
	RoleProgressBar
	RoleList
	RoleListItem
	RoleTree
	RoleTreeItem
	RoleMenu
	RoleMenuItem
	// RoleMenuBar is a row of menu items that each open a menu.
	RoleMenuBar
	RoleTabList
	RoleTab
	RoleToolBar
	RoleStatusBar
	RoleDialog
	RoleSeparator
	RoleDocument
	// RoleGrid is a table whose rows the keyboard moves through; RoleRow is one
	// of its rows (the header row too), RoleGridCell a body cell and
	// RoleColumnHeader a header cell.
	RoleGrid
	RoleRow
	RoleGridCell
	RoleColumnHeader
)

// leaf reports whether the role's subtree is presented as one element, whose
// name comes from the text inside it.
func (r Role) leaf() bool {
	switch r {
	case RoleButton, RoleLink, RoleCheckBox, RoleSwitch, RoleRadio, RoleText, RoleImage, RoleEdit,
		RoleComboBox, RoleSlider, RoleProgressBar, RoleMenuItem, RoleTab, RoleSeparator:
		return true
	}
	return false
}

// nameFromContent reports whether an unlabelled element of this role is named
// by the text inside it.
func (r Role) nameFromContent() bool {
	switch r {
	case RoleButton, RoleLink, RoleCheckBox, RoleSwitch, RoleRadio, RoleText, RoleComboBox,
		RoleMenuItem, RoleTab, RoleListItem, RoleTreeItem, RoleRow, RoleGridCell, RoleColumnHeader:
		return true
	}
	return false
}

type a11yProps struct {
	role      Role
	label     string
	checked   int8 // 0 unset, 1 off, 2 on, 3 mixed
	selected  int8 // 0 unset, 1 no, 2 yes
	expanded  int8 // 0 unset, 1 collapsed, 2 expanded
	heading   int8
	level     int32 // tree depth, 1-based; 0 unset
	pos, size int32 // position in set, 1-based, and set size; 0 unset
	disabled  bool
	clickable bool // had a click handler before Disabled removed it
	active    bool
	rng       *rangeProps
	valueText string
	live      LiveRegion
	liveSet   bool
}

// LiveRegion is how assistive technology reports changes to a live region.
type LiveRegion uint8

const (
	LiveOff LiveRegion = iota
	// LivePolite reads changes when the user is idle.
	LivePolite
	// LiveAssertive reads changes at once, interrupting.
	LiveAssertive
)

// LiveRegion makes the element a live region: when the text inside it changes,
// screen readers read it without the user moving focus there, as for a
// status line or a log. A RoleStatusBar is polite unless set otherwise.
func (e *El) LiveRegion(l LiveRegion) *El {
	a := e.acc()
	a.live, a.liveSet = l, true
	return e
}

func (e *El) liveSetting(r Role) LiveRegion {
	if e.a11y != nil && e.a11y.liveSet {
		return e.a11y.live
	}
	if r == RoleStatusBar {
		return LivePolite
	}
	return LiveOff
}

type rangeProps struct {
	value, min, max, step float64
	set                   func(float64)
}

func (e *El) acc() *a11yProps {
	if e.a11y == nil {
		e.a11y = &a11yProps{}
	}
	return e.a11y
}

// Role sets what the element is to assistive technology. Most elements need
// none: see RoleAuto.
func (e *El) Role(r Role) *El { e.acc().role = r; return e }

// Label names the element for assistive technology, replacing the name taken
// from its text or Tooltip. Give one to controls that show only an icon or
// whose caption is elsewhere.
func (e *El) Label(s string) *El { e.acc().label = s; return e }

// Checked sets the on/off state of a check box, switch or toggle button.
func (e *El) Checked(on bool) *El {
	e.acc().checked = 1
	if on {
		e.a11y.checked = 2
	}
	return e
}

// CheckedMixed marks a check box as partly checked.
func (e *El) CheckedMixed() *El { e.acc().checked = 3; return e }

// Selected sets whether a radio button, tab or list item is the selected one.
func (e *El) Selected(on bool) *El {
	e.acc().selected = 1
	if on {
		e.a11y.selected = 2
	}
	return e
}

// Expanded sets whether the menu, list or subtree that the element opens is shown.
func (e *El) Expanded(open bool) *El {
	e.acc().expanded = 1
	if open {
		e.a11y.expanded = 2
	}
	return e
}

// PositionInSet reports the element as item pos (1-based) of size items,
// for lists whose items are not all in the tree. Rows of virtual lists get
// their index and the list's count without it.
func (e *El) PositionInSet(pos, size int) *El {
	a := e.acc()
	a.pos, a.size = int32(max(pos, 0)), int32(max(size, 0))
	return e
}

// Level reports the depth of a tree item, 1 for top-level items.
func (e *El) Level(n int) *El { e.acc().level = int32(max(n, 0)); return e }

// Heading marks the element as a heading of level 1 to 9.
func (e *El) Heading(level int) *El { e.acc().heading = int8(min(max(level, 1), 9)); return e }

// RangeValue describes a slider (set non-nil) or a progress bar (set nil):
// its value between min and max, and the step that arrow keys move it by.
func (e *El) RangeValue(value, min, max, step float64, set func(float64)) *El {
	e.acc().rng = &rangeProps{value: value, min: min, max: max, step: step, set: set}
	return e
}

// ValueText gives assistive technology the element's value as text, read
// only, as "saturation 40%, brightness 80%" for an area that sets two values
// at once. A slider reports it beside its RangeValue.
func (e *El) ValueText(s string) *El { e.acc().valueText = s; return e }

// Disabled makes the element and its subtree unavailable: they take no focus,
// clicks, drags, key presses or edits, and assistive technology reports them
// as disabled. Their look is left to the caller.
func (e *El) Disabled(d bool) *El { e.acc().disabled = d; return e }

// ActiveDescendant marks the item that keyboard navigation has highlighted
// inside the focused control, such as the current row of an open list whose
// focus stays on its trigger. Assistive technology treats it as focused.
func (e *El) ActiveDescendant() *El { e.acc().active = true; return e }

// applyDisabled inherits Disabled from parent and strips the input handlers
// of a disabled element. It runs while the tree is prepared.
func (e *El) applyDisabled(parent *El) {
	if parent != nil && parent.a11y != nil && parent.a11y.disabled {
		e.acc().disabled = true
	}
	a := e.a11y
	if a == nil || !a.disabled {
		return
	}
	a.clickable = a.clickable || e.onClick != nil || e.onDoubleClick != nil
	e.onClick, e.onDoubleClick, e.onRightClick, e.onPointerDown = nil, nil, nil, nil
	e.onDrag, e.onKey, e.onFileDrop = nil, nil, nil
	e.onWheel, e.onPointerMove = nil, nil
	e.focusable, e.autoFocus = false, false
	if e.input != nil {
		e.input.onChange, e.input.onSubmit = nil, nil
		e.input.onKey, e.input.onPaste = nil, nil
	}
	if a.rng != nil {
		a.rng.set = nil
	}
}

func (e *El) disabled() bool { return e.a11y != nil && e.a11y.disabled }

func (e *El) clickable() bool {
	return e.onClick != nil || e.onDoubleClick != nil || e.a11y != nil && e.a11y.clickable
}

// a11yRole returns the role the element is exposed with, or RoleNone or
// RoleHidden when it is not exposed itself.
func (e *El) a11yRole() Role {
	a := e.a11y
	if a != nil && a.role != RoleAuto {
		return a.role
	}
	switch {
	case e.kind == kindInput:
		return RoleEdit
	case a != nil && a.rng != nil:
		if a.rng.set != nil || a.disabled {
			return RoleSlider
		}
		return RoleProgressBar
	case a != nil && a.checked != 0:
		return RoleCheckBox
	case e.clickable():
		// A clickable card holding its own controls keeps them reachable.
		for _, c := range e.children {
			if hasControl(c) {
				return RoleGroup
			}
		}
		return RoleButton
	case a != nil && a.heading > 0:
		return RoleText
	case e.kind == kindText:
		if e.text == "" {
			return RoleNone
		}
		return RoleText
	case e.focusable, a != nil && a.liveSet && a.live != LiveOff:
		return RoleGroup
	case a != nil && a.label != "":
		switch e.kind {
		case kindImage, kindIcon, kindVideo, kindSurface:
			return RoleImage
		}
		return RoleGroup
	}
	return RoleNone
}

// hasControl reports whether e or a descendant is something the user operates.
func hasControl(e *El) bool {
	if e.style.Hidden || e.a11y != nil && e.a11y.role == RoleHidden {
		return false
	}
	if e.clickable() || e.focusable || e.kind == kindInput {
		return true
	}
	for _, c := range e.children {
		if hasControl(c) {
			return true
		}
	}
	return false
}

// a11yName returns the accessible name of an element exposed with role r.
func (e *El) a11yName(r Role) string {
	if e.a11y != nil && e.a11y.label != "" {
		return e.a11y.label
	}
	if r == RoleEdit && e.input != nil {
		return e.input.placeholder
	}
	if r.nameFromContent() {
		var b strings.Builder
		textContent(e, &b)
		if b.Len() > 0 {
			return b.String()
		}
	}
	return e.tooltip
}

// textContent appends the visible text of e's subtree, separated by spaces.
func textContent(e *El, b *strings.Builder) {
	if e.style.Hidden {
		return
	}
	add := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(s)
		}
	}
	if a := e.a11y; a != nil {
		if a.role == RoleHidden {
			return
		}
		if a.label != "" {
			add(a.label)
			return
		}
	}
	if e.kind == kindText {
		add(e.text)
		return
	}
	for _, c := range e.children {
		textContent(c, b)
	}
}

// a11yTree is the accessibility view of a window: the exposed elements and
// their exposed parents. ID 0 is the window.
type a11yTree struct {
	parent map[uint64]uint64
	kids   map[uint64][]uint64
	el     map[uint64]*El
	role   map[uint64]Role
	active uint64 // an element marked ActiveDescendant
}

func buildA11yTree(roots []*El) *a11yTree {
	t := &a11yTree{
		parent: map[uint64]uint64{},
		kids:   map[uint64][]uint64{},
		el:     map[uint64]*El{},
		role:   map[uint64]Role{},
	}
	for _, r := range roots {
		t.walk(r, 0)
	}
	return t
}

func (t *a11yTree) walk(e *El, parent uint64) {
	if e.style.Hidden {
		return
	}
	r := e.a11yRole()
	if r == RoleHidden {
		return
	}
	if r != RoleNone {
		t.parent[e.id] = parent
		t.kids[parent] = append(t.kids[parent], e.id)
		t.el[e.id] = e
		t.role[e.id] = r
		if e.a11y != nil && e.a11y.active {
			t.active = e.id
		}
		if r.leaf() {
			return
		}
		parent = e.id
	}
	for _, c := range e.children {
		t.walk(c, parent)
	}
}

// sibling returns the exposed sibling of id at offset d, or 0.
func (t *a11yTree) sibling(id uint64, d int) uint64 {
	kids := t.kids[t.parent[id]]
	for i, k := range kids {
		if k == id {
			if j := i + d; j >= 0 && j < len(kids) {
				return kids[j]
			}
			return 0
		}
	}
	return 0
}

// exposedAncestor returns the nearest exposed element at or above e, or 0.
func (t *a11yTree) exposedAncestor(e *El) uint64 {
	for ; e != nil; e = e.parent {
		if _, ok := t.el[e.id]; ok {
			return e.id
		}
	}
	return 0
}

// a11yRoots returns the trees assistive technology sees: the view and its
// layers, or only the topmost modal overlay and the layers above it.
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

// a11yFocus returns the element assistive technology should treat as
// focused, or 0.
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

// a11yValue returns the value of an element exposed with role r as text:
// its ValueText, the shown choice of a combo box, or the text of an edit
// field, which is empty for a password.
func (e *El) a11yValue(r Role) string {
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

// steps returns the amounts a range moves by for a small step, such as an
// arrow key, and for a large one, such as Page Up.
func (g *rangeProps) steps() (small, large float64) {
	small = g.step
	if small <= 0 {
		small = (g.max - g.min) / 100
	}
	return small, max((g.max-g.min)/10, small)
}
