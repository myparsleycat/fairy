package kit

import (
	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/motion"
)

// MultiCombobox picks any number of items. The chosen ones show as chips in
// the field, in the order of values, each with a button that removes it.
// The list has a search field that filters items by label; choosing a row
// adds or removes its item and leaves the list open. Backspace with the
// search field empty removes the last chip. onChange receives the new values
// with an added one at the end. id must be unique in the window.
func MultiCombobox(id string, values []string, items []Item, onChange func([]string), opts ...Option) *fairy.El {
	o := build(opts)
	t := &current
	st := fairy.Local[popupState]("kit.multicombo:" + id)
	chosen := make(map[string]bool, len(values))
	for _, v := range values {
		chosen[v] = true
	}
	toggle := func(value string) {
		next := make([]string, 0, len(values)+1)
		for _, v := range values {
			if v != value {
				next = append(next, v)
			}
		}
		if !chosen[value] {
			next = append(next, value)
		}
		onChange(next)
	}

	h, radius := float32(32), t.RadiusLg()
	if o.size == SizeSmall || o.size == SizeXS {
		h, radius = 28, t.RadiusMd()
	}
	chips := fairy.Row().Wrap().Gap(4).Align(fairy.Center).Grow(1).MinW(0)
	for _, v := range values {
		label, ok := itemLabel(items, v)
		if !ok {
			label = v
		}
		chips.Child(multiComboChip(st, v, label, o.disabled, func() { toggle(v) }).Key(v))
	}
	field := fairy.Row(chips, fairy.Icon(lucide.ChevronDown).Size(16, 16).Shrink(0).Color(t.MutedForeground)).
		MinH(h).Gap(6).Align(fairy.Center).Shrink(0).Radius(radius).Fill(fieldFill()).Border(1, t.Input).
		FontSize(t.FontSize).Animate(motion.Fast).Role(fairy.RoleComboBox)
	if len(values) == 0 {
		field.PadEdges(0, 8, 0, 10)
		chips.Child(fairy.Text(o.placeholder).Color(t.MutedForeground).Grow(1).MinW(0).Ellipsis())
	} else {
		field.PadEdges(4, 8, 4, 4)
	}
	if o.label != "" {
		field.Label(o.label)
	}
	if o.invalid {
		invalidRing(field)
	}
	if o.disabled {
		st.open, st.query, st.chip = false, "", ""
		st.report(o, "")
		return field.Opacity(0.5).Isolate().Cursor(fairy.CursorNotAllowed).Disabled(true)
	}
	if t.Dark {
		field.HoverFill(alpha(t.Input, 0.5))
	}

	matching, shown := comboMatches(items, st.query)
	st.highlight = min(st.highlight, max(len(shown)-1, 0))
	pick := func(i int) { toggle(shown[i].Value) }
	open := func() { st.open, st.query, st.highlight, st.byPointer = true, "", 0, false }
	keys := listKeys(st, len(shown), open, pick)
	removeLast := func(k keyboard.KeyEvent) bool {
		if !k.Is(keyboard.KeyBackspace, 0) || st.query != "" || len(values) == 0 {
			return false
		}
		toggle(values[len(values)-1])
		return true
	}
	focusRing(field).Focusable().Cursor(fairy.CursorPointer).OnClick(func() {
		if st.open {
			st.open, st.query = false, ""
		} else {
			open()
		}
	}).OnKey(func(k keyboard.KeyEvent) bool { return removeLast(k) || keys(k) }).Expanded(st.open)
	trackWidth(st, field)
	if st.open {
		panel := comboPanel(st, matching, func(v string) bool { return chosen[v] }, pick, removeLast, o).OnKey(keys)
		field.Popover(panel, fairy.PopoverOptions{OnDismiss: func() { st.open, st.query = false, "" }})
	}

	highlighted := ""
	switch {
	case st.open && st.highlight >= 0 && st.highlight < len(shown):
		highlighted = shown[st.highlight].Value
	case chosen[st.chip]:
		highlighted = st.chip
	}
	st.report(o, highlighted)
	return field
}

// multiComboChip is a chosen item of a MultiCombobox, with a button that
// removes it unless disabled.
func multiComboChip(st *popupState, value, label string, disabled bool, remove func()) *fairy.El {
	t := &current
	chip := fairy.Row(fairy.Text(label).MinW(0).Ellipsis().TooltipWhenTruncated()).
		Gap(2).Align(fairy.Center).H(21).MinW(0).PadEdges(0, 6, 0, 6).Radius(t.RadiusSm()).
		Fill(t.Muted).Color(t.Foreground).FontSize(t.SmallSize).Medium()
	if disabled {
		return chip
	}
	button := IconButton(lucide.X, remove, Ghost, XS, Tooltip(Messages.Remove)).Label(Messages.Remove).Size(18, 18)
	return chip.PadEdges(0, 1, 0, 6).Child(button).OnHover(func(in bool) {
		switch {
		case in:
			st.chip = value
		case st.chip == value:
			st.chip = ""
		}
	})
}
