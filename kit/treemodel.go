package kit

import (
	"sort"
	"strings"
	"time"

	"github.com/myparsleycat/fairy/keyboard"
)

// TreeModel holds an outline's nodes, open folders, selection and keyboard
// cursor. The app keeps it in its state and Tree draws it. Nodes are keyed by
// string ids unique across the tree (a file tree uses paths). The visible rows
// are worked out on the next read after a change.
type TreeModel[T any] struct {
	nodes   []treeNode[T]
	index   map[string]int
	roots   []int
	cursor  int // -1 when none
	anchor  int // -1 when none
	multi   bool
	filter  *treeFilter
	keep    func(T) bool // the filter's test, for nodes inserted while it holds
	visible *treeVisible
	typed   string
	typedAt time.Time

	viewport  float32 // the tree's visible height, for Page Up and Page Down
	rowHeight float32
	pressMods keyboard.Mods // modifiers of the press that started the click being handled
}

type treeNode[T any] struct {
	id       string
	data     T
	parent   int // -1 at the top level
	children []int
	depth    int
	open     bool
	selected bool
}

type treeShown uint8

const (
	shownHidden treeShown = iota
	shownPath             // shown to reach a match
	shownMatch
)

type treeFilter struct {
	shown  []treeShown
	closed []bool // folders the user closed while filtering; the rest show open
}

type treeVisible struct {
	rows  []int // node of each visible row, top to bottom
	rowOf []int // row of each node, -1 when hidden
	pos   []int // 1-based position among shown siblings, indexed by node
	size  []int // number of shown siblings, indexed by node
}

// typePause is how long a pause in typing starts a new name, as AppKit's type select.
const typePause = time.Second

// NewTreeModel returns an empty tree.
func NewTreeModel[T any]() *TreeModel[T] {
	return &TreeModel[T]{index: map[string]int{}, cursor: -1, anchor: -1, rowHeight: 24}
}

// MultiSelect lets several rows be selected: Shift with the arrows or a
// click extends the selection, Ctrl with a click adds or removes a row, and
// Ctrl+A selects every row.
func (m *TreeModel[T]) MultiSelect(on bool) *TreeModel[T] { m.multi = on; return m }

func (m *TreeModel[T]) dirty() { m.visible = nil }

// Insert adds id under parent ("" for the top level), after its siblings. It
// returns false and adds nothing when id is already in the tree or parent isn't.
func (m *TreeModel[T]) Insert(parent, id string, data T) bool {
	if _, ok := m.index[id]; ok {
		return false
	}
	p := -1
	if parent != "" {
		var ok bool
		if p, ok = m.index[parent]; !ok {
			return false
		}
	}
	i := len(m.nodes)
	depth := 0
	if p >= 0 {
		depth = m.nodes[p].depth + 1
		m.nodes[p].children = append(m.nodes[p].children, i)
	} else {
		m.roots = append(m.roots, i)
	}
	m.index[id] = i
	m.nodes = append(m.nodes, treeNode[T]{id: id, data: data, parent: p, depth: depth})
	if f := m.filter; f != nil {
		f.shown = append(f.shown, shownHidden)
		f.closed = append(f.closed, false)
		if m.keep(data) {
			f.shown[i] = shownMatch
			for q := p; q >= 0 && f.shown[q] == shownHidden; q = m.nodes[q].parent {
				f.shown[q] = shownPath
			}
		}
	}
	m.dirty()
	return true
}

// Clear removes every node, the selection and the filter.
func (m *TreeModel[T]) Clear() {
	*m = TreeModel[T]{index: map[string]int{}, cursor: -1, anchor: -1, multi: m.multi, viewport: m.viewport, rowHeight: m.rowHeight}
}

// Len returns the number of nodes, shown or not.
func (m *TreeModel[T]) Len() int { return len(m.nodes) }

// Contains reports whether the tree has a node id.
func (m *TreeModel[T]) Contains(id string) bool { _, ok := m.index[id]; return ok }

// HasChildren reports whether id is a folder with at least one child.
func (m *TreeModel[T]) HasChildren(id string) bool {
	i, ok := m.index[id]
	return ok && len(m.nodes[i].children) > 0
}

// Get returns the data of id.
func (m *TreeModel[T]) Get(id string) (T, bool) {
	if i, ok := m.index[id]; ok {
		return m.nodes[i].data, true
	}
	var zero T
	return zero, false
}

// Set replaces the data of id.
func (m *TreeModel[T]) Set(id string, data T) {
	if i, ok := m.index[id]; ok {
		m.nodes[i].data = data
	}
}

// Parent returns the id of id's parent, or "" at the top level.
func (m *TreeModel[T]) Parent(id string) string {
	if i, ok := m.index[id]; ok && m.nodes[i].parent >= 0 {
		return m.nodes[m.nodes[i].parent].id
	}
	return ""
}

// Children returns id's children in order, or the top-level nodes for "".
func (m *TreeModel[T]) Children(id string) []string {
	list := m.roots
	if id != "" {
		i, ok := m.index[id]
		if !ok {
			return nil
		}
		list = m.nodes[i].children
	}
	out := make([]string, len(list))
	for k, c := range list {
		out[k] = m.nodes[c].id
	}
	return out
}

// Depth returns id's depth, 0 at the top level, or -1 when id is not in the tree.
func (m *TreeModel[T]) Depth(id string) int {
	if i, ok := m.index[id]; ok {
		return m.nodes[i].depth
	}
	return -1
}

// SortBy sorts every node's children, and the top level, by less.
func (m *TreeModel[T]) SortBy(less func(a, b T) bool) {
	order := func(list []int) {
		sort.SliceStable(list, func(x, y int) bool { return less(m.nodes[list[x]].data, m.nodes[list[y]].data) })
	}
	for i := range m.nodes {
		order(m.nodes[i].children)
	}
	order(m.roots)
	m.dirty()
}

// IsExpanded reports whether id shows its children. Under a filter, folders
// leading to a match show open unless closed since.
func (m *TreeModel[T]) IsExpanded(id string) bool {
	i, ok := m.index[id]
	return ok && m.open(i)
}

func (m *TreeModel[T]) open(i int) bool {
	if m.filter != nil {
		return !m.filter.closed[i]
	}
	return m.nodes[i].open
}

func (m *TreeModel[T]) setOpen(i int, open bool) {
	if m.filter != nil {
		m.filter.closed[i] = !open
	} else {
		m.nodes[i].open = open
	}
	// A cursor or anchor inside a closing folder moves up to it.
	if !open {
		if c := m.cursor; c >= 0 && m.isUnder(c, i) {
			if !m.multi && m.nodes[c].selected {
				m.selectOnly(i)
			} else {
				m.cursor = i
			}
		}
		if a := m.anchor; a >= 0 && m.isUnder(a, i) {
			m.anchor = i
		}
	}
	m.dirty()
}

func (m *TreeModel[T]) isUnder(node, ancestor int) bool {
	for p := m.nodes[node].parent; p >= 0; p = m.nodes[p].parent {
		if p == ancestor {
			return true
		}
	}
	return false
}

// SetExpanded opens or closes folder id. Closing it moves a cursor inside it
// up to the folder. While a filter is set, it changes the filtered view only.
func (m *TreeModel[T]) SetExpanded(id string, open bool) {
	if i, ok := m.index[id]; ok {
		m.setOpen(i, open)
	}
}

// Toggle opens folder id if it is closed and closes it if it is open, as
// SetExpanded does.
func (m *TreeModel[T]) Toggle(id string) {
	if i, ok := m.index[id]; ok {
		m.setOpen(i, !m.open(i))
	}
}

// ExpandAll opens every folder.
func (m *TreeModel[T]) ExpandAll() {
	for i := range m.nodes {
		if len(m.nodes[i].children) > 0 {
			m.setOpen(i, true)
		}
	}
}

// CollapseAll closes every folder and moves the cursor up to its top-level
// node.
func (m *TreeModel[T]) CollapseAll() {
	if c := m.cursor; c >= 0 {
		root := c
		for m.nodes[root].parent >= 0 {
			root = m.nodes[root].parent
		}
		if root != c && !m.multi && m.nodes[c].selected {
			m.selectOnly(root)
		} else {
			m.cursor = root
		}
	}
	for m.anchor >= 0 && m.nodes[m.anchor].parent >= 0 {
		m.anchor = m.nodes[m.anchor].parent
	}
	for i := range m.nodes {
		if m.filter != nil {
			m.filter.closed[i] = true
		} else {
			m.nodes[i].open = false
		}
	}
	m.dirty()
}

// ExpandTo opens every folder above id, so its row shows.
func (m *TreeModel[T]) ExpandTo(id string) {
	i, ok := m.index[id]
	if !ok {
		return
	}
	if f := m.filter; f != nil {
		if f.shown[i] == shownHidden {
			f.shown[i] = shownMatch
		}
		for p := m.nodes[i].parent; p >= 0; p = m.nodes[p].parent {
			if f.shown[p] == shownHidden {
				f.shown[p] = shownPath
			}
		}
		m.dirty()
	}
	for p := m.nodes[i].parent; p >= 0; p = m.nodes[p].parent {
		m.nodes[p].open = true
		m.setOpen(p, true)
	}
}

// Selected returns the selected ids in tree order.
func (m *TreeModel[T]) Selected() []string {
	var out []string
	stack := reversed(m.roots)
	for len(stack) > 0 {
		i := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if m.nodes[i].selected {
			out = append(out, m.nodes[i].id)
		}
		stack = append(stack, reversed(m.nodes[i].children)...)
	}
	return out
}

func reversed(s []int) []int {
	out := make([]int, len(s))
	for k, v := range s {
		out[len(s)-1-k] = v
	}
	return out
}

// IsSelected reports whether id is selected.
func (m *TreeModel[T]) IsSelected(id string) bool {
	i, ok := m.index[id]
	return ok && m.nodes[i].selected
}

// Cursor returns the keyboard cursor's row id, where the arrows move from, or "".
func (m *TreeModel[T]) Cursor() string {
	if m.cursor < 0 {
		return ""
	}
	return m.nodes[m.cursor].id
}

// ClearSelection deselects every node. The cursor stays.
func (m *TreeModel[T]) ClearSelection() {
	for i := range m.nodes {
		m.nodes[i].selected = false
	}
}

// Select selects id alone and puts the cursor on it.
func (m *TreeModel[T]) Select(id string) {
	if i, ok := m.index[id]; ok {
		m.selectOnly(i)
	}
}

func (m *TreeModel[T]) selectOnly(i int) {
	m.ClearSelection()
	m.nodes[i].selected = true
	m.cursor, m.anchor = i, i
}

// SetSelected adds id to the selection or takes it out in a multi-select
// tree; otherwise it selects id alone or clears it.
func (m *TreeModel[T]) SetSelected(id string, on bool) {
	i, ok := m.index[id]
	if !ok {
		return
	}
	if !m.multi {
		m.ClearSelection()
	}
	m.nodes[i].selected = on
	m.cursor, m.anchor = i, i
}

// SelectRange selects the visible rows from the anchor (the last row clicked
// or moved to without Shift) to id, as Shift-click does.
func (m *TreeModel[T]) SelectRange(id string) {
	i, ok := m.index[id]
	if !ok {
		return
	}
	if !m.multi {
		m.selectOnly(i)
		return
	}
	anchor := m.anchor
	if anchor < 0 {
		anchor = i
	}
	v := m.rows()
	a, b := v.rowOf[anchor], v.rowOf[i]
	if a < 0 || b < 0 {
		m.selectOnly(i)
		return
	}
	m.ClearSelection()
	for _, j := range v.rows[min(a, b) : max(a, b)+1] {
		m.nodes[j].selected = true
	}
	m.cursor = i
}

// Click applies a click on id's row with mods held: it selects the row alone,
// or in a multi-select tree extends the selection to it with Shift and adds
// or removes it with Ctrl. It reports whether it was a plain click.
func (m *TreeModel[T]) Click(id string, mods keyboard.Mods) bool {
	i, ok := m.index[id]
	if !ok {
		return false
	}
	if m.multi && mods&keyboard.ModShift != 0 {
		m.SelectRange(id)
		return false
	}
	if m.multi && mods&keyboard.ModPrimary != 0 {
		m.nodes[i].selected = !m.nodes[i].selected
		m.cursor, m.anchor = i, i
		return false
	}
	m.selectOnly(i)
	return true
}

// Filter shows only the nodes keep matches, with the folders that lead to
// them, open, as a file tree's search does. A folder that matches shows
// without the children that don't.
func (m *TreeModel[T]) Filter(keep func(T) bool) {
	n := len(m.nodes)
	shown := make([]treeShown, n)
	for i := range m.nodes {
		if !keep(m.nodes[i].data) {
			continue
		}
		shown[i] = shownMatch
		for p := m.nodes[i].parent; p >= 0 && shown[p] == shownHidden; p = m.nodes[p].parent {
			shown[p] = shownPath
		}
	}
	m.filter, m.keep = &treeFilter{shown: shown, closed: make([]bool, n)}, keep
	m.dirty()
	v := m.rows()
	if m.cursor >= 0 && v.rowOf[m.cursor] < 0 {
		m.cursor = -1
	}
	if m.anchor >= 0 && v.rowOf[m.anchor] < 0 {
		m.anchor = m.cursor
	}
}

// ClearFilter removes the filter set by Filter and shows every node again.
func (m *TreeModel[T]) ClearFilter() {
	if m.filter != nil {
		m.filter, m.keep = nil, nil
		m.dirty()
	}
}

// IsFiltering reports whether a filter is set.
func (m *TreeModel[T]) IsFiltering() bool { return m.filter != nil }

// IsMatch reports whether the filter matched id itself, not just a folder
// leading to a match; true for every node without a filter.
func (m *TreeModel[T]) IsMatch(id string) bool {
	i, ok := m.index[id]
	return ok && (m.filter == nil || m.filter.shown[i] == shownMatch)
}

func (m *TreeModel[T]) rows() *treeVisible {
	if m.visible != nil {
		return m.visible
	}
	v := &treeVisible{
		rowOf: make([]int, len(m.nodes)),
		pos:   make([]int, len(m.nodes)),
		size:  make([]int, len(m.nodes)),
	}
	for i := range v.rowOf {
		v.rowOf[i] = -1
	}
	var stack []int
	push := func(siblings []int) {
		count := 0
		for _, i := range siblings {
			if m.shown(i) {
				count++
			}
		}
		pos := count
		for k := len(siblings) - 1; k >= 0; k-- {
			i := siblings[k]
			if m.shown(i) {
				v.pos[i], v.size[i] = pos, count
				pos--
				stack = append(stack, i)
			}
		}
	}
	push(m.roots)
	for len(stack) > 0 {
		i := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		v.rowOf[i] = len(v.rows)
		v.rows = append(v.rows, i)
		if m.open(i) {
			push(m.nodes[i].children)
		}
	}
	m.visible = v
	return v
}

func (m *TreeModel[T]) shown(i int) bool {
	return m.filter == nil || m.filter.shown[i] != shownHidden
}

// openable reports whether node i has children the filter shows.
func (m *TreeModel[T]) openable(i int) bool {
	for _, c := range m.nodes[i].children {
		if m.shown(c) {
			return true
		}
	}
	return false
}

// VisibleLen returns how many rows show.
func (m *TreeModel[T]) VisibleLen() int { return len(m.rows().rows) }

// VisibleID returns the id of visible row row, or "".
func (m *TreeModel[T]) VisibleID(row int) string {
	v := m.rows()
	if row < 0 || row >= len(v.rows) {
		return ""
	}
	return m.nodes[v.rows[row]].id
}

// VisibleRow returns the row id shows at, and false when it does not show.
func (m *TreeModel[T]) VisibleRow(id string) (int, bool) {
	i, ok := m.index[id]
	if !ok {
		return 0, false
	}
	r := m.rows().rowOf[i]
	return r, r >= 0
}

// TreeRow is a row's state as a tree draws it.
type TreeRow struct {
	ID    string
	Depth int // 0 at the top level
	// Expandable is set on a row with children (shown ones, under a filter);
	// Expanded then reports whether they show.
	Expandable, Expanded bool
	Selected             bool
	Cursor               bool // the keyboard cursor is on it
	Matched              bool // a filter matched it; false without a filter
	multi                bool
	pos, size            int // 1-based place among the siblings that show, and their count
}

// Row returns visible row row's data and state, for drawing it.
func (m *TreeModel[T]) Row(row int) (T, TreeRow, bool) {
	v := m.rows()
	if row < 0 || row >= len(v.rows) {
		var zero T
		return zero, TreeRow{}, false
	}
	i := v.rows[row]
	n := &m.nodes[i]
	r := TreeRow{
		ID: n.id, Depth: n.depth, Selected: n.selected, Cursor: m.cursor == i,
		Matched: m.filter != nil && m.filter.shown[i] == shownMatch, multi: m.multi,
		pos: v.pos[i], size: v.size[i],
	}
	if m.openable(i) {
		r.Expandable, r.Expanded = true, m.open(i)
	}
	return n.data, r, true
}

// TreeKeyResult is what a key did to a TreeModel.
type TreeKeyResult uint8

const (
	// TreeKeyIgnored: not a tree key; it goes on to the window.
	TreeKeyIgnored TreeKeyResult = iota
	// TreeKeyHandled: it moved the cursor, the selection or a folder.
	TreeKeyHandled
	// TreeKeyOpen: Enter or Space on a row without children; the app opens Cursor().
	TreeKeyOpen
)

func (m *TreeModel[T]) moveTo(i int, extend bool) {
	if extend && m.multi {
		m.SelectRange(m.nodes[i].id)
	} else {
		m.selectOnly(i)
	}
}

// HandleKey applies a key: Up and Down move, Right opens a folder or goes to
// its first child, Left closes it or goes to the parent, Home and End go to
// the ends, Page Up and Page Down move by a screen, Enter and Space open or
// toggle, and typing a name jumps to it. label gives a node's name for that.
// Tree calls it from its key handler and scrolls the cursor's row into view.
func (m *TreeModel[T]) HandleKey(ev keyboard.KeyEvent, label func(T) string) TreeKeyResult {
	mods := ev.Mods
	if mods&^(keyboard.ModPrimary|keyboard.ModShift) != 0 {
		return TreeKeyIgnored
	}
	n := m.VisibleLen()
	if n == 0 {
		return TreeKeyIgnored
	}
	if ev.Is(keyboard.KeyA, keyboard.ModPrimary) && m.multi {
		for _, i := range m.rows().rows {
			m.nodes[i].selected = true
		}
		return TreeKeyHandled
	}
	if mods&keyboard.ModPrimary != 0 {
		return TreeKeyIgnored
	}
	at := -1
	if m.cursor >= 0 {
		at = m.rows().rowOf[m.cursor]
	}
	page := max(int(m.viewport/max(m.rowHeight, 1))-1, 1)
	shift := mods&keyboard.ModShift != 0
	target := -1
	switch ev.Key {
	case keyboard.KeyUp:
		target = n - 1
		if at >= 0 {
			target = max(at-1, 0)
		}
	case keyboard.KeyDown:
		target = 0
		if at >= 0 {
			target = min(at+1, n-1)
		}
	case keyboard.KeyHome:
		target = 0
	case keyboard.KeyEnd:
		target = n - 1
	case keyboard.KeyPageUp:
		target = 0
		if at >= 0 {
			target = max(at-page, 0)
		}
	case keyboard.KeyPageDown:
		target = 0
		if at >= 0 {
			target = min(at+page, n-1)
		}
	case keyboard.KeyRight, keyboard.KeyLeft:
		if shift {
			return TreeKeyIgnored
		}
		if at < 0 {
			return m.goTo(0, false)
		}
		i := m.rows().rows[at]
		if ev.Key == keyboard.KeyRight {
			if !m.openable(i) {
				return TreeKeyHandled
			}
			if !m.open(i) {
				m.setOpen(i, true)
				return TreeKeyHandled
			}
			target = min(at+1, n-1)
		} else {
			if m.openable(i) && m.open(i) {
				m.setOpen(i, false)
				return TreeKeyHandled
			}
			p := m.nodes[i].parent
			if p < 0 {
				return TreeKeyHandled
			}
			target = m.rows().rowOf[p]
		}
	case keyboard.KeyEnter, keyboard.KeySpace:
		if shift {
			return TreeKeyIgnored
		}
		return m.activate(at, ev.Repeat)
	default:
		typed := strings.TrimSpace(ev.Text)
		if typed == "" || ev.Mods&^keyboard.ModShift != 0 {
			return TreeKeyIgnored
		}
		now := time.Now()
		pause := m.typedAt.IsZero() || now.Sub(m.typedAt) > typePause
		m.typedAt = now
		if r := m.typeSelect(typed, pause, label); r >= 0 {
			return m.goTo(r, false)
		}
		return TreeKeyHandled
	}
	return m.goTo(target, shift)
}

func (m *TreeModel[T]) activate(at int, repeat bool) TreeKeyResult {
	if at < 0 {
		return TreeKeyIgnored
	}
	if repeat {
		return TreeKeyHandled
	}
	i := m.rows().rows[at]
	if m.openable(i) {
		m.setOpen(i, !m.open(i))
		return TreeKeyHandled
	}
	return TreeKeyOpen
}

func (m *TreeModel[T]) goTo(row int, extend bool) TreeKeyResult {
	m.moveTo(m.rows().rows[row], extend)
	return TreeKeyHandled
}

// typeSelect returns the row typing jumps to: the next whose name starts
// with everything typed since the last pause, while the same letter again
// cycles through names starting with it. It returns -1 when none matches.
func (m *TreeModel[T]) typeSelect(typed string, pause bool, label func(T) string) int {
	if pause {
		m.typed = ""
	}
	m.typed += strings.ToLower(typed)
	first := []rune(m.typed)[0]
	cycle := strings.Trim(m.typed, string(first)) == ""
	prefix := m.typed
	if cycle {
		prefix = string(first)
	}
	v := m.rows()
	n := len(v.rows)
	start := 0
	if m.cursor >= 0 && v.rowOf[m.cursor] >= 0 {
		start = v.rowOf[m.cursor]
		if cycle {
			start++
		}
	}
	for k := 0; k < n; k++ {
		r := (start + k) % n
		if strings.HasPrefix(strings.ToLower(label(m.nodes[v.rows[r]].data)), prefix) {
			return r
		}
	}
	return -1
}
