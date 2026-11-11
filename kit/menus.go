package kit

import (
	"fmt"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/motion"
	"github.com/myparsleycat/fairy/svg"
)

// MenuItem is an entry of a DropdownMenu, ContextMenu or Menubar.
type MenuItem struct {
	Label string
	Icon  *svg.Vector
	// Image is a picture shown in place of Icon, such as an application's
	// logo, in the same 16 pixel square.
	Image    *fairy.Image
	Shortcut string // shown on the right, such as "Ctrl+C"
	Checked  bool
	Danger   bool
	Disabled bool
	// Loading shows a spinner in place of the icon while the item's action
	// runs; pair it with Disabled to keep the action from starting twice.
	Loading   bool
	Separator bool // draws a line; other fields are ignored
	Heading   bool // a non-interactive section title
	// Radio shows Checked as a dot, for one choice of a group of items.
	Radio bool
	// Checkbox makes the item a check item: its row keeps the room of the
	// check while unchecked and reports the unchecked state to assistive
	// technology.
	Checkbox bool
	// Submenu opens these items to the right when the pointer rests on the
	// item or it is clicked; OnClick is not used.
	Submenu []MenuItem
	// SubmenuWidth fixes the width of Submenu when set, as a w class on
	// shadcn/ui's sub content.
	SubmenuWidth float32
	OnClick      func()
	// Content is shown in place of a row, with a row's padding, for controls
	// such as a number field. It is not highlighted, and the menu's arrow keys
	// and type-select pass over it: its controls are a focus group of their
	// own. Using them leaves the menu open. Other fields are ignored.
	Content *fairy.El
}

// takesFocus reports whether it is a row that the menu's keys move to.
func (it MenuItem) takesFocus() bool {
	return !it.Separator && !it.Heading && !it.Disabled && it.Content == nil
}

type popupState struct {
	open      bool
	highlight int
	query     string
	x, y      float32
	sub       []int // the open submenu at each depth, by item index
	subByKey  bool  // the deepest submenu was opened from the keyboard
	anchorW   float32
	hovered   string // the Select row whose RowAction shows
	byPointer bool   // the pointer, not keys or typing, last moved highlight
	chip      string // the MultiCombobox chip under the pointer
	// openSent and highlightSent are what OnOpenChange and OnHighlight
	// last received.
	openSent      bool
	highlightSent string
}

// report runs o's OnOpenChange and OnHighlight when the list's open state
// or highlighted value differs from what they last received. They run
// through fairy.Go, after this build, because they usually change what other
// parts of the view show.
func (st *popupState) report(o options, highlighted string) {
	if f := o.onOpenChange; f != nil && st.open != st.openSent {
		open := st.open
		st.openSent = open
		fairy.Go(func() { f(open) })
	}
	if f := o.onHighlight; f != nil && highlighted != st.highlightSent {
		st.highlightSent = highlighted
		fairy.Go(func() { f(highlighted) })
	}
}

// popupEnter and popupExit are how menus, selects and popovers open and
// close: they fade, grow from 95% and come 8 pixels from the anchor's side.
var (
	popupEnter = fairy.Transition{Scale: 0.95, Slide: 8, Motion: motion.Popup}
	popupExit  = fairy.Transition{Scale: 0.95, Motion: motion.Popup}
)

// popupSurface styles e as the surface of a popover: a hairline ring, a
// medium shadow (large for submenus), and the open and close motion.
func popupSurface(e *fairy.El, large bool) *fairy.El {
	t := &current
	e.Radius(t.RadiusLg()).Fill(t.Popover).Color(t.PopoverForeground).Ring(1, alpha(t.Foreground, 0.1)).
		Key("kit.popup").Enter(popupEnter).Exit(popupExit)
	if large {
		return shadowLg(e)
	}
	return shadowMd(e)
}

// popupPanel is the scrolling list of a menu or select. It is as tall as its
// rows; the popover cuts it to the room beside its anchor.
func popupPanel(large bool, children ...*fairy.El) *fairy.El {
	return popupSurface(fairy.Col(children...).Pad(4).MinW(128).ScrollY(), large)
}

// trackWidth records the width of trigger in st, for a popup at least as wide.
func trackWidth(st *popupState, trigger *fairy.El) {
	trigger.OnLayout(func(w, _ float32) { st.anchorW = w })
}

type menuRowState struct {
	checked, radio, highlighted, danger, disabled, loading bool
	tall                                                   bool // 32 pixels, as combobox rows
	// checkable keeps the room of the check while unchecked and reports the
	// unchecked state.
	checkable bool
	// markStart shows the check, for radio items too, at the start of the
	// row, as shadcn/ui's Menubar does.
	markStart bool
	image     *fairy.Image
}

// menuRow is one selectable row of a popup list; role is RoleMenuItem or
// RoleListItem. A check or radio dot shows at the right end, or a check at
// the start with markStart.
func menuRow(role fairy.Role, icon *svg.Vector, label, right string, s menuRowState, onClick func()) *fairy.El {
	t := &current
	row := fairy.Row().Gap(6).Align(fairy.Center).H(28).PadEdges(0, 8, 0, 6).Radius(t.RadiusMd()).Role(role).Label(label)
	if s.tall {
		row.H(32).PadEdges(0, 8, 0, 8).Gap(8)
	}
	if s.markStart {
		row.PadEdges(0, 6, 0, 28)
		if s.checked {
			row.Child(fairy.Icon(lucide.Check).Size(16, 16).Left(6).Top(6))
		}
	}
	switch {
	case s.loading:
		row.Child(fairy.Spinner().Size(16, 16).Shrink(0))
	case s.image != nil:
		row.Child(fairy.ImageEl(s.image).Fit(fairy.FitContain).Size(16, 16).Shrink(0))
	case icon != nil:
		row.Child(fairy.Icon(icon).Size(16, 16))
	}
	row.Child(fairy.Text(label).Grow(1).MinW(0).Ellipsis().TooltipWhenTruncated())
	if right != "" {
		// shadcn/ui sets the shortcut in tracking-widest, 0.1em.
		row.Child(fairy.Text(right).FontSize(t.SmallSize).LetterSpacing(t.SmallSize*0.1).Color(t.MutedForeground).
			MarginEdges(0, 0, 0, 16))
	}
	switch {
	case s.markStart:
	case s.checked && s.radio:
		row.Child(fairy.Box(fairy.Box().Size(8, 8).Radius(4).Fill(t.PopoverForeground)).Size(16, 16).Center())
	case s.checked:
		row.Child(fairy.Icon(lucide.Check).Size(16, 16))
	case role == fairy.RoleListItem || s.radio || s.checkable:
		row.Child(fairy.Box().Size(16, 16))
	}
	if role == fairy.RoleListItem {
		row.Selected(s.checked)
	} else if s.checked || s.checkable {
		row.Checked(s.checked)
	}
	highlight := alpha(t.Foreground, 0.1)
	if s.danger {
		row.Color(t.Destructive)
		highlight = alpha(t.Destructive, 0.1)
		if t.Dark {
			highlight = alpha(t.Destructive, 0.2)
		}
	}
	if s.highlighted {
		row.Fill(highlight)
		if role == fairy.RoleListItem {
			row.ActiveDescendant()
		}
	}
	if s.disabled {
		return row.Opacity(0.5).Isolate().Disabled(true)
	}
	if role == fairy.RoleMenuItem {
		// Menu rows take focus, so keys and the pointer share one highlight.
		return row.Focusable().FocusHighlight(highlight).FocusSelected(s.checked).
			Cursor(fairy.CursorPointer).OnClick(onClick)
	}
	// List rows leave focus on the trigger or search field; the caller moves
	// the highlight with the pointer (listPointer), so only one row is filled.
	return row.Cursor(fairy.CursorPointer).OnClick(onClick)
}

// listPointer makes e, row i of a Select or Combobox list, take the highlight
// when the pointer moves over it and drop it when the pointer leaves, as Base
// UI does. Moving, not entering, takes it: keys keep the highlight until the
// pointer moves, and rows shifting under a still pointer do not take it.
// onHover, if set, also receives e's OnHover.
func listPointer(st *popupState, e *fairy.El, i int, onHover func(bool)) *fairy.El {
	return e.OnPointerMove(func(fairy.PointerEvent) bool {
		st.byPointer = true
		if st.highlight == i {
			return false
		}
		st.highlight = i
		return true
	}).OnHover(func(in bool) {
		if !in && st.byPointer && st.highlight == i {
			st.highlight = -1
		}
		if onHover != nil {
			onHover(in)
		}
	})
}

func menuSeparator() *fairy.El {
	return fairy.Box().H(1).Shrink(0).Fill(current.Border).MarginEdges(4, -4, 4, -4)
}

// menuOpts is how the menus of a Menubar differ from other menus.
type menuOpts struct {
	// bar shows the check of check and radio items at the start of the row,
	// keeps the menu open when they are chosen, and starts focus on the first
	// row (the last with fromEnd) rather than the checked one.
	bar, fromEnd bool
	// context gives submenus the minimum width of a ContextMenu's, as
	// shadcn/ui's ContextMenuSubContent.
	context bool
	// step opens the menu d places along the bar, for Left and Right.
	step func(d int)
}

// barStep returns which way Left or Right on a row of a Menubar's menu at
// depth moves along the bar, -1 or +1, or 0 when the key belongs to the
// menu: Right on a row with a submenu opens it, and Left in a submenu
// closes it.
func barStep(depth int, submenu bool, k keyboard.KeyEvent) int {
	switch {
	case k.Is(keyboard.KeyLeft, 0) && depth == 0:
		return -1
	case k.Is(keyboard.KeyRight, 0) && !submenu:
		return 1
	}
	return 0
}

// menuItems builds the rows of a menu at depth (0 for the menu itself, 1 for a
// submenu of it, ...). A submenu opens to the right of its item when the
// pointer rests on it, and closes when the pointer rests on another item of
// its menu, on Escape, or with the menu. The rows are a focus group: Up and
// Down move, typing jumps to a row, Return and Space choose. Right, Return or
// Space on a submenu item opens it with focus on its first item, and Left
// closes it.
func menuItems(st *popupState, depth int, items []MenuItem, close func(), o menuOpts) []*fairy.El {
	t := &current
	window := fairy.CurrentWindow()
	var rows []*fairy.El
	openAt := func(i int, byKey bool) {
		st.sub = append(st.sub[:min(depth, len(st.sub))], i)
		st.subByKey = byKey
	}
	closeBelow := func() {
		if len(st.sub) > depth {
			st.sub = st.sub[:depth]
		}
	}
	stepKey := func(submenu bool) func(keyboard.KeyEvent) bool {
		return func(k keyboard.KeyEvent) bool {
			if d := barStep(depth, submenu, k); d != 0 && o.step != nil {
				o.step(d)
				return true
			}
			return false
		}
	}
	// The row focus starts on: a menu opens on its checked item, else its
	// first (a Menubar's menu on its first, or its last with fromEnd); a
	// submenu opened from the keyboard on its first.
	first := -1
	if depth == 0 || st.subByKey && len(st.sub) == depth {
		for i, it := range items {
			if !it.takesFocus() {
				continue
			}
			if first < 0 || o.fromEnd && depth == 0 {
				first = i
			}
			if depth == 0 && it.Checked && !o.bar {
				first = i
				break
			}
		}
	}
	for i, it := range items {
		key := menuKey(depth, i, it.Label)
		switch {
		case it.Separator:
			rows = append(rows, menuSeparator())
		case it.Heading:
			rows = append(rows, fairy.Text(it.Label).FontSize(t.SmallSize).Color(t.MutedForeground).Medium().PadXY(6, 4))
		case it.Content != nil:
			rows = append(rows, fairy.Box(it.Content).PadXY(6, 4).FocusGroup().Key(key).OnHover(func(in bool) {
				if in {
					closeBelow()
				}
			}))
		case len(it.Submenu) > 0:
			open := len(st.sub) > depth && st.sub[depth] == i
			row := menuRow(fairy.RoleMenuItem, it.Icon, it.Label, "",
				menuRowState{highlighted: open, danger: it.Danger, disabled: it.Disabled, loading: it.Loading, image: it.Image},
				func() { openAt(i, false) }).Key(key).Expanded(open)
			if it.Disabled {
				rows = append(rows, row)
				continue
			}
			sub := it.Submenu
			row.Child(fairy.Icon(lucide.ChevronRight).Size(16, 16)).
				OnHover(func(in bool) {
					if in && !open {
						openAt(i, false)
					}
				}).
				OnKey(func(k keyboard.KeyEvent) bool {
					if stepKey(true)(k) {
						return true
					}
					if !k.Is(keyboard.KeyRight, 0) && !k.Is(keyboard.KeyEnter, 0) && !k.Is(keyboard.KeySpace, 0) {
						return false
					}
					if k.Repeat {
						return true
					}
					openAt(i, true)
					if open && window != nil {
						// Already open from the pointer: move into it.
						for j, s := range sub {
							if s.takesFocus() {
								window.Focus(menuKey(depth+1, j, s.Label))
								break
							}
						}
					}
					return true
				})
			if i == first {
				row.AutoFocus()
			}
			if open {
				minW := float32(96)
				switch {
				case o.bar:
					minW = 128
				case o.context:
					minW = 144
				}
				panel := popupPanel(true, menuItems(st, depth+1, sub, close, o)...).MinW(minW).Role(fairy.RoleMenu).FocusGroup().
					OnKey(func(k keyboard.KeyEvent) bool {
						if k.Is(keyboard.KeyLeft, 0) {
							closeBelow()
							return true
						}
						return false
					})
				if it.SubmenuWidth > 0 {
					panel.W(it.SubmenuWidth)
				}
				row.Popover(panel, fairy.PopoverOptions{Side: fairy.SideRight, Gap: 4, OnDismiss: closeBelow})
			}
			rows = append(rows, row)
		default:
			f := it.OnClick
			// Base UI's check and radio items leave the menu open.
			check := o.bar && (it.Checkbox || it.Radio || it.Checked)
			s := menuRowState{checked: it.Checked, radio: it.Radio, danger: it.Danger, disabled: it.Disabled, loading: it.Loading,
				checkable: it.Checkbox || check, markStart: check, image: it.Image}
			row := menuRow(fairy.RoleMenuItem, it.Icon, it.Label, it.Shortcut, s, func() {
				if !check {
					close()
				}
				if f != nil {
					f()
				}
			}).Key(key)
			if !it.Disabled {
				row.OnHover(func(in bool) {
					if in {
						closeBelow()
					}
				})
				if o.step != nil {
					row.OnKey(stepKey(false))
				}
			}
			if i == first {
				row.AutoFocus()
			}
			rows = append(rows, row)
		}
	}
	return rows
}

// menuKey keys a menu row by its depth, so Window.Focus can find a row of
// an open submenu.
func menuKey(depth, i int, label string) string {
	return fmt.Sprint("kit.menu.", depth, ":", itemKey(i, label))
}

func itemKey(i int, label string) string { return fmt.Sprint(i, label) }

// DropdownMenu opens a menu below trigger when it is clicked, at least as
// wide as trigger. It sets trigger's OnLayout to measure it. It returns
// trigger. Width, MaxWidth and MaxHeight size the menu, and AlignEnd lines
// it up with the trigger's end edge.
func DropdownMenu(id string, trigger *fairy.El, items []MenuItem, opts ...Option) *fairy.El {
	return DropdownMenuFunc(id, trigger, func() []MenuItem { return items }, opts...)
}

// DropdownMenuFunc is DropdownMenu with items built only while the menu is open.
func DropdownMenuFunc(id string, trigger *fairy.El, items func() []MenuItem, opts ...Option) *fairy.El {
	o := build(opts)
	st := fairy.Local[popupState]("kit.menu:" + id)
	close := func() { st.open, st.sub = false, st.sub[:0] }
	trigger.OnClick(func() { st.open, st.sub = !st.open, st.sub[:0] }).Expanded(st.open)
	trackWidth(st, trigger)
	if st.open {
		panel := sizeMenu(popupPanel(false, menuItems(st, 0, items(), close, menuOpts{})...).
			MinW(max(128, st.anchorW)).Role(fairy.RoleMenu).FocusGroup(), o)
		pop := fairy.PopoverOptions{OnDismiss: close}
		if o.alignEnd {
			pop.Align = fairy.AlignEnd
		}
		trigger.Popover(panel, pop)
	}
	return trigger
}

// sizeMenu applies the Width, MaxWidth and MaxHeight options to a menu panel.
func sizeMenu(panel *fairy.El, o options) *fairy.El {
	if o.width > 0 {
		panel.W(o.width)
	}
	if o.maxWidth > 0 {
		panel.MaxW(o.maxWidth)
	}
	if o.maxHeight > 0 {
		panel.MaxH(o.maxHeight)
	}
	return panel
}

// DropdownMenuOpen reports whether the DropdownMenu with id is open, for
// example to keep a trigger that shows only on hover visible meanwhile.
func DropdownMenuOpen(id string) bool { return fairy.Local[popupState]("kit.menu:" + id).open }

// ContextMenu opens a menu at the pointer when target is right-clicked. items
// runs when the menu opens. Width, MaxWidth and MaxHeight size the menu. It
// returns target.
func ContextMenu(id string, target *fairy.El, items func() []MenuItem, opts ...Option) *fairy.El {
	o := build(opts)
	st := fairy.Local[popupState]("kit.ctx:" + id)
	close := func() { st.open, st.sub = false, st.sub[:0] }
	target.OnRightClick(func(p fairy.PointerEvent) {
		st.open, st.x, st.y, st.sub = true, p.LocalX, p.LocalY, st.sub[:0]
	})
	if st.open {
		panel := popupPanel(false, menuItems(st, 0, items(), close, menuOpts{context: true})...).MinW(144).
			Role(fairy.RoleMenu).FocusGroup()
		target.Popover(sizeMenu(panel, o),
			fairy.PopoverOptions{
				AtPoint: true, X: st.x, Y: st.y, Side: fairy.SideRight, Gap: 1,
				OnDismiss: close, DismissOnAnchorPress: true,
			})
	}
	return target
}

// selectTrigger is the closed look of Select, Combobox and DatePicker.
func selectTrigger(label string, placeholder bool, o options) *fairy.El {
	t := &current
	text := fairy.Text(label).Grow(1).MinW(0).Ellipsis().TooltipWhenTruncated()
	if placeholder {
		text.Color(t.MutedForeground)
	}
	h, radius := float32(32), t.RadiusLg()
	if o.size == SizeSmall || o.size == SizeXS {
		h, radius = 28, t.RadiusMd()
	}
	e := fairy.Row().H(h).PadEdges(0, 8, 0, 10).Gap(6).Align(fairy.Center).Shrink(0).
		Radius(radius).Fill(fieldFill()).Border(1, t.Input).FontSize(t.FontSize).LineHeight(t.FontSize * 20 / 14).
		Animate(motion.Fast).Role(fairy.RoleComboBox)
	if o.label != "" {
		e.Label(o.label)
	}
	if o.icon != nil {
		e.Child(fairy.Icon(o.icon).Size(16, 16).Color(t.MutedForeground))
	}
	e.Child(text, fairy.Icon(lucide.ChevronDown).Size(16, 16).Color(t.MutedForeground))
	if o.invalid {
		invalidRing(e)
	}
	if o.disabled {
		return e.Opacity(0.5).Isolate().Cursor(fairy.CursorNotAllowed).Disabled(true)
	}
	if t.Dark {
		e.HoverFill(alpha(t.Input, 0.5))
	}
	return focusRing(e).Focusable().Cursor(fairy.CursorPointer)
}

func itemLabel(items []Item, value string) (string, bool) {
	for _, it := range items {
		if !it.Heading && it.Value == value {
			return it.Label, true
		}
	}
	return "", false
}

// listKeys handles arrow keys, Enter and Escape for an open popup list of n
// enabled entries; open opens the list and pick receives the highlighted index.
func listKeys(st *popupState, n int, open func(), pick func(int)) func(keyboard.KeyEvent) bool {
	return func(k keyboard.KeyEvent) bool {
		if !st.open {
			if k.Is(keyboard.KeyDown, 0) || k.Is(keyboard.KeyEnter, 0) || k.Is(keyboard.KeySpace, 0) {
				open()
				return true
			}
			return false
		}
		switch {
		case k.Is(keyboard.KeyDown, 0):
			st.highlight = min(st.highlight+1, n-1)
		case k.Is(keyboard.KeyUp, 0):
			if st.highlight < 0 {
				st.highlight = n - 1
			} else {
				st.highlight = max(st.highlight-1, 0)
			}
		case k.Is(keyboard.KeyEnter, 0):
			if st.highlight >= 0 && st.highlight < n {
				pick(st.highlight)
			}
		case k.Is(keyboard.KeyEscape, 0):
			st.open = false
		default:
			return false
		}
		st.byPointer = false
		return true
	}
}

// Select picks one of items. id must be unique in the window.
func Select(id, value string, items []Item, onChange func(string), opts ...Option) *fairy.El {
	o := build(opts)
	st := fairy.Local[popupState]("kit.select:" + id)
	label, ok := itemLabel(items, value)
	if !ok {
		label = o.placeholder
	}
	trigger := selectTrigger(label, !ok, o).Key("kit.select:" + id)
	if o.disabled {
		st.open = false
		st.report(o, "")
		return trigger
	}
	var enabled []Item
	hasTrailing := false
	for _, it := range items {
		hasTrailing = hasTrailing || it.Trailing != nil
		if !it.Disabled && !it.Heading {
			enabled = append(enabled, it)
		}
	}
	if hasTrailing {
		trigger.OnBlur(func() { fairy.Go(func() {}) })
	}
	pick := func(i int) {
		st.open = false
		if enabled[i].Value != value {
			onChange(enabled[i].Value)
		}
	}
	open := func() {
		st.open, st.highlight, st.byPointer = true, 0, false
		for i, it := range enabled {
			if it.Value == value {
				st.highlight = i
			}
		}
	}
	keys := listKeys(st, len(enabled), open, pick)
	trigger.OnClick(func() {
		if st.open {
			st.open = false
		} else {
			open()
		}
	}).OnKey(keys).Expanded(st.open)
	trackWidth(st, trigger)
	if st.open {
		var rows []*fairy.El
		ei := 0
		t := &current
		for _, it := range items {
			if it.Heading {
				rows = append(rows, fairy.Text(it.Label).FontSize(t.SmallSize).Color(t.MutedForeground).Medium().PadXY(6, 4))
				continue
			}
			i := -1
			var choose func()
			if !it.Disabled {
				i = ei
				ei++
				choose = func() { pick(i) }
			}
			row := menuRow(fairy.RoleListItem, it.Icon, it.Label, "", menuRowState{
				checked: it.Value == value, highlighted: i >= 0 && i == st.highlight, disabled: it.Disabled,
			}, choose)
			if it.Tooltip != "" {
				row.Tooltip(it.Tooltip)
			}
			if it.Trailing != nil || it.OnHover != nil {
				row = selectRichRow(st, row, i, id, it)
			} else if !it.Disabled && o.rowAction != nil {
				row = selectActionRow(st, row, i, it.Value, *o.rowAction)
			} else if !it.Disabled {
				listPointer(st, row, i, nil)
			}
			rows = append(rows, row.Key(it.Value))
		}
		panel := popupPanel(false, rows...).MinW(max(144, st.anchorW)).Role(fairy.RoleList)
		if o.maxHeight > 0 {
			panel.MaxH(o.maxHeight)
		}
		if hasTrailing {
			window := fairy.CurrentWindow()
			panel.OnKey(func(k keyboard.KeyEvent) bool {
				if !k.Is(keyboard.KeyUp, 0) && !k.Is(keyboard.KeyDown, 0) && !k.Is(keyboard.KeyLeft, 0) {
					return false
				}
				if !k.Is(keyboard.KeyLeft, 0) {
					keys(k)
				}
				if window != nil {
					window.Focus("kit.select:" + id)
				}
				return true
			})
		}
		trigger.Popover(panel,
			fairy.PopoverOptions{Align: fairy.AlignCenter, OnDismiss: func() { st.open = false }})
	}
	if !st.open {
		st.hovered = ""
	}
	highlighted := ""
	if st.open && st.highlight >= 0 && st.highlight < len(enabled) {
		highlighted = enabled[st.highlight].Value
	}
	if st.open && st.byPointer && st.hovered != "" {
		highlighted = st.hovered
	}
	st.report(o, highlighted)
	return trigger
}

func selectRichRow(st *popupState, row *fairy.El, i int, id string, item Item) *fairy.El {
	row.OnHover(item.OnHover)
	wrap := fairy.Row(row.Grow(1).MinW(0)).Align(fairy.Center).Role(fairy.RoleNone).
		FocusReturn("kit.select:" + id)
	hover := func(in bool) {
		if in {
			st.hovered = item.Value
		} else if st.hovered == item.Value {
			st.hovered = ""
		}
	}
	if i >= 0 {
		listPointer(st, wrap, i, hover)
	} else {
		wrap.OnHover(hover).OnPointerMove(func(fairy.PointerEvent) bool {
			changed := st.highlight != -1 || !st.byPointer
			st.highlight, st.byPointer = -1, true
			return changed
		})
	}
	if item.Trailing == nil {
		return wrap
	}
	key := "kit.select-action:" + id + ":" + item.Value
	window := fairy.CurrentWindow()
	focused := window != nil && window.Focused(key)
	// Keep the zero-width action in the focus order; Tab reveals it as CSS focus-within does.
	action := item.Trailing.Key(key).Absolute().Right(0).Top(0).
		OnBlur(func() { fairy.Go(func() {}) })
	slot := fairy.Row(action).W(0).H(controlHeight(SizeSmall)).Shrink(0).AnimateLayout(motion.Fast)
	if st.hovered == item.Value || focused {
		slot.W(controlHeight(SizeSmall) + 4)
	} else {
		action.Opacity(0).PassThrough().OnClick(nil).OnHover(nil)
	}
	return wrap.Child(slot)
}

// selectActionRow puts the RowAction button after row, sliding it open and
// fading it in while the pointer is over either of them. Row i stays
// highlighted while the pointer is on the button.
func selectActionRow(st *popupState, row *fairy.El, i int, value string, a rowAction) *fairy.El {
	wrap := listPointer(st, fairy.Row(row.Grow(1).MinW(0)).Align(fairy.Center).Role(fairy.RoleNone), i, func(in bool) {
		if in {
			st.hovered = value
		} else if st.hovered == value {
			st.hovered = ""
		}
	})
	button := IconButton(a.icon, func() {
		st.open, st.hovered = false, ""
		a.onClick(value)
	}, Ghost, Small, Tooltip(a.label)).Label(a.label).Shrink(0).Opacity(0)
	slot := fairy.Row(button).W(0).Shrink(0).Justify(fairy.JustifyEnd).Align(fairy.Center).Clip().
		AnimateLayout(motion.Fast)
	if st.hovered == value {
		button.Opacity(1)
		slot.W(4 + controlHeight(SizeSmall))
	} else {
		slot.Disabled(true)
	}
	return wrap.Child(slot)
}

// Combobox is a Select with a search field that filters items by label.
func Combobox(id, value string, items []Item, onChange func(string), opts ...Option) *fairy.El {
	o := build(opts)
	st := fairy.Local[popupState]("kit.combo:" + id)
	label, ok := itemLabel(items, value)
	if !ok {
		label = o.placeholder
	}
	trigger := selectTrigger(label, !ok, o)
	if o.disabled {
		st.open, st.query = false, ""
		st.report(o, "")
		return trigger
	}
	matching, shown := comboMatches(items, st.query)
	st.highlight = min(st.highlight, max(len(shown)-1, 0))
	pick := func(i int) {
		st.open, st.query = false, ""
		if shown[i].Value != value {
			onChange(shown[i].Value)
		}
	}
	open := func() { st.open, st.query, st.highlight, st.byPointer = true, "", 0, false }
	keys := listKeys(st, len(shown), open, pick)
	trigger.OnClick(func() {
		if st.open {
			st.open, st.query = false, ""
		} else {
			open()
		}
	}).OnKey(keys).Expanded(st.open)
	trackWidth(st, trigger)
	if st.open {
		panel := comboPanel(st, matching, func(v string) bool { return v == value }, pick, nil, o).OnKey(keys)
		trigger.Popover(panel, fairy.PopoverOptions{OnDismiss: func() { st.open, st.query = false, "" }})
	}
	highlighted := ""
	if st.open && st.highlight >= 0 && st.highlight < len(shown) {
		highlighted = shown[st.highlight].Value
	}
	st.report(o, highlighted)
	return trigger
}

// comboMatches returns the items whose label contains query, and of them the
// ones that can be picked, which the highlight and the keys move through.
func comboMatches(items []Item, query string) (matching, enabled []Item) {
	q := strings.ToLower(strings.TrimSpace(query))
	for _, it := range items {
		if it.Heading || q != "" && !strings.Contains(strings.ToLower(it.Label), q) {
			continue
		}
		matching = append(matching, it)
		if !it.Disabled {
			enabled = append(enabled, it)
		}
	}
	return matching, enabled
}

// comboPanel lists matching, the disabled items grayed out and skipped by the
// highlight, as Select does.
func comboPanel(
	st *popupState,
	matching []Item,
	checked func(string) bool,
	pick func(int),
	inputKey func(keyboard.KeyEvent) bool,
	o options,
) *fairy.El {
	t := &current
	input := fairy.Input(st.query, func(s string) { st.query, st.highlight, st.byPointer = s, 0, false }).
		Placeholder(Messages.Search).PlaceholderColor(t.MutedForeground).Grow(1).MinW(0).
		Key("kit.combo.search").AutoFocus()
	if inputKey != nil {
		input.OnInputKey(inputKey)
	}
	search := fairy.Row(fairy.Icon(lucide.Search).Size(16, 16).Color(t.MutedForeground), input).
		Gap(8).Align(fairy.Center).H(32).Shrink(0).PadXY(10, 0)
	list := fairy.Col().Pad(4).MaxH(240).ScrollY()
	ei := 0
	for _, it := range matching {
		if it.Disabled {
			list.Child(menuRow(fairy.RoleListItem, it.Icon, it.Label, "",
				menuRowState{checked: checked(it.Value), disabled: true, tall: true}, nil).Key(it.Value))
			continue
		}
		i := ei
		ei++
		row := menuRow(fairy.RoleListItem, it.Icon, it.Label, "",
			menuRowState{checked: checked(it.Value), highlighted: i == st.highlight, tall: true},
			func() { pick(i) })
		list.Child(listPointer(st, row, i, nil).Key(it.Value))
	}
	if len(matching) == 0 {
		empty := o.emptyText
		if empty == "" {
			empty = Messages.NoResults
		}
		list.Child(fairy.Text(empty).FontSize(t.SmallSize).Color(t.MutedForeground).
			TextAlign(fairy.TextCenter).PadXY(12, 8))
	}
	panel := fairy.Col(search, fairy.Box().H(1).Shrink(0).Fill(t.Border), list).MinW(max(192, st.anchorW)).
		Role(fairy.RoleList)
	return popupSurface(panel, false).Enter(fairy.Transition{Scale: 0.95, Motion: motion.Popup})
}

// tabTrigger is one trigger of Tabs or Segmented.
func tabTrigger(it Item, sel, line bool) *fairy.El {
	t := &current
	inactive := alpha(t.Foreground, 0.6)
	if t.Dark {
		inactive = t.MutedForeground
	}
	tab := fairy.Row().Gap(6).Align(fairy.Center).H(26).PadXY(6, 0).Radius(t.RadiusMd()).Border(1, fairy.Transparent).
		Color(inactive).Medium().Animate(motion.Fast).Key(it.Value).Disabled(it.Disabled)
	if !line {
		// The raised trigger's shadow fades in and out with it.
		s1, s2 := fairy.Transparent, fairy.Transparent
		if sel {
			s1, s2 = fairy.RGBA(0, 0, 0, 0.1), fairy.RGBA(0, 0, 0, 0.1)
		}
		tab.Shadow(0, 1, 3, 0, s1).Shadow(0, 1, 2, -1, s2)
	}
	if it.Icon != nil {
		tab.Child(fairy.Icon(it.Icon).Size(16, 16))
	}
	if it.Label != "" {
		tab.Child(fairy.Text(it.Label).NoWrap())
	}
	if it.Badge != "" {
		tab.Child(Badge(it.Badge, Secondary).H(20).PadXY(6, 0).FontSize(12).MarginEdges(0, 0, 0, 4))
	}
	if line {
		bar := fairy.Box().H(2).Left(0).Right(0).Bottom(-5).Fill(t.Foreground).Opacity(0).Animate(motion.Fast)
		if sel {
			bar.Opacity(1)
		}
		tab.Child(bar)
	}
	switch {
	case sel:
		tab.Color(t.Foreground)
		if !line {
			tab.Fill(t.Background)
			if t.Dark {
				tab.Fill(alpha(t.Input, 0.3)).Border(1, t.Input)
			}
		}
		focusRing(tab).Focusable().FocusSelected(true)
	case it.Disabled:
		tab.Opacity(0.5).Isolate()
	default:
		focusRing(tab).HoverColor(t.Foreground).Cursor(fairy.CursorPointer).Focusable()
	}
	return tab
}

// tabList is the row that holds tab triggers.
func tabList(line bool) *fairy.El {
	t := &current
	if line {
		return fairy.Row().Gap(4).H(32).Align(fairy.Center).Color(t.MutedForeground).FocusGroup()
	}
	return fairy.Row().H(32).Pad(3).Align(fairy.Center).Radius(t.RadiusLg()).Fill(t.Muted).
		Color(t.MutedForeground).FocusGroup()
}

// Tabs is a row of tab triggers on a muted track, the selected one raised;
// Line draws them over an underline instead. Tab enters the row on the
// selected tab and the arrow keys select the previous or next one.
func Tabs(value string, items []Item, onChange func(string), opts ...Option) *fairy.El {
	o := build(opts)
	row := tabList(o.line).Role(fairy.RoleTabList)
	for _, it := range items {
		sel := it.Value == value
		tab := tabTrigger(it, sel, o.line).Role(fairy.RoleTab).Selected(sel)
		if !sel && !it.Disabled {
			tab.OnClick(func() { onChange(it.Value) })
		}
		row.Child(tab)
	}
	return row
}

// Segmented is a compact group of mutually exclusive buttons that looks like
// Tabs. An item with only an icon is named for assistive technology by its
// Description. Tab enters the group on the selected segment and the arrow
// keys select the previous or next one.
func Segmented(value string, items []Item, onChange func(string)) *fairy.El {
	row := tabList(false).Role(fairy.RoleGroup)
	for _, it := range items {
		sel := it.Value == value
		seg := tabTrigger(it, sel, false).Role(fairy.RoleRadio).Selected(sel)
		if it.Label == "" && it.Description != "" {
			seg.Label(it.Description)
		}
		if !sel && !it.Disabled {
			seg.OnClick(func() { onChange(it.Value) })
		}
		row.Child(seg)
	}
	return row
}
