package diff

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/kit/syntax"
	"github.com/myparsleycat/fairy/svg"
)

const (
	barW     = 3
	codePadL = 12
	codePadR = 24
	fontSize = 13
	chunkLen = 128 // lines highlighted together, so a token spanning lines colors right
)

// ViewProps configures a View.
type ViewProps struct {
	// OnSelect runs when a press, Shift+press or drag on the line numbers
	// ends, with the selection it leaves (nil when it cleared it).
	OnSelect func(*LineRange)
	// HeaderActions builds elements at the end of file f's header, such as
	// buttons to open or stage the file.
	HeaderActions func(f int) *fairy.El
}

// View draws the diff s holds: a header per file with its path and line
// counts, its hunks with line numbers and word changes, and folded
// unchanged ranges that open 20 lines at a time. Only the rows in view are
// built, so it suits long diffs; bound its height like any scroll
// container. Long lines wrap.
//
// Pressing a line number selects the line; Shift+press extends the
// selection and dragging down the numbers selects the lines passed. With
// the view focused, Ctrl+C copies the selected lines of the side the
// selection started on, Escape clears the selection, j and k go to the next
// and previous hunk, and ] and [ to the next and previous file. id must be
// unique in the window.
func View(id string, s *State, p ViewProps) *fairy.El {
	t := kit.T()
	lk := newLook(t)
	key := "kit.diff:" + id
	w := fairy.CurrentWindow()
	if s.reveal >= 0 && w != nil {
		w.ScrollToRowTop(key, s.reveal)
		s.reveal = -1
	}
	v := &view{s: s, p: p, l: lk}
	list := fairy.VirtualListMeasured(len(s.rows), lineH, func(i int) string { return s.keys[i] }, v.row).
		Key(key).Grow(1).MinH(0).WFull().
		OnScroll(func(_, y float32) { s.scrollY = y })
	return fairy.Col(list).Key(key+".root").Grow(1).MinH(0).
		Fill(lk.bg).Radius(t.RadiusLg()).Border(1, t.Border).Clip().
		Focusable().FocusVisibleBorder(t.Ring).FocusRing(3, t.Ring.Alpha(0.5)).
		Role(fairy.RoleList).Label(kit.Messages.Diff).FontSize(fontSize).
		OnKey(func(ev keyboard.KeyEvent) bool { return v.key(ev, w) })
}

type view struct {
	s *State
	p ViewProps
	l look
}

type tints struct{ line, gutter, word, number, bar, count fairy.Color }

type look struct {
	bg, text, muted, faint, border, headerBg, hover fairy.Color
	add, del                                        tints
	gapBg, emptyBg, selected, selectedGutter        fairy.Color
	mono                                            string
	adv                                             float32
	syntax                                          *kit.SyntaxColors
}

// newLook derives the diff's colors from the theme: added and removed lines
// are tints of the background toward the success and danger hues.
func newLook(t *kit.Theme) look {
	bg := t.Background
	amt := func(dark, light float32) float32 {
		if t.Dark {
			return dark
		}
		return light
	}
	tint := func(hue fairy.Color) tints {
		return tints{
			line:   bg.Mix(hue, amt(0.13, 0.10)),
			gutter: bg.Mix(hue, amt(0.20, 0.17)),
			word:   bg.Mix(hue, amt(0.32, 0.27)),
			number: t.MutedForeground.Mix(hue, 0.7),
			bar:    hue,
			count:  hue,
		}
	}
	return look{
		bg: bg, text: t.Foreground, muted: t.MutedForeground, faint: t.MutedForeground.Alpha(0.7), border: t.Border,
		headerBg: t.Muted, hover: t.Foreground.Alpha(amt(0.06, 0.035)),
		add: tint(t.Success), del: tint(t.Destructive),
		gapBg:          bg.Mix(t.Foreground, amt(0.06, 0.04)),
		emptyBg:        bg.Mix(t.Foreground, amt(0.03, 0.025)),
		selected:       bg.Mix(t.Foreground, amt(0.10, 0.07)),
		selectedGutter: bg.Mix(t.Foreground, amt(0.16, 0.12)),
		mono:           t.MonoFont,
		adv:            0.6 * fontSize,
		syntax:         &t.Syntax,
	}
}

func (l *look) tints(k LineKind) *tints {
	switch k {
	case Added:
		return &l.add
	case Removed:
		return &l.del
	}
	return nil
}

func (l *look) gutterFill(k LineKind, selected bool) fairy.Color {
	if selected {
		return l.selectedGutter
	}
	if t := l.tints(k); t != nil {
		return t.gutter
	}
	return l.bg
}

func (l *look) numberW(digits int) float32 {
	return float32(int(float32(digits)*l.adv+16) + 1)
}

func (v *view) gutterW(f int) float32 {
	n := v.l.numberW(v.s.digits[f])
	if v.s.mode == Unified {
		return barW + 2*n
	}
	return barW + n
}

func (v *view) key(ev keyboard.KeyEvent, w *fairy.Window) bool {
	s := v.s
	if ev.Is(keyboard.KeyC, keyboard.ModCtrl) {
		text, ok := s.SelectedText()
		if ok && w != nil {
			w.SetClipboardText(text)
		}
		return ok
	}
	if ev.Mods&(keyboard.ModCtrl|keyboard.ModAlt|keyboard.ModSuper) != 0 {
		return false
	}
	if ev.Key == keyboard.KeyEscape && s.selecting {
		// A press on the line numbers that hasn't dragged yet: put the
		// selection from before it back.
		s.cancelLine()
		v.notify()
		return true
	}
	if ev.Key == keyboard.KeyEscape && s.selection != nil {
		s.selection = nil
		v.notify()
		return true
	}
	switch ev.Text {
	case "j":
		return s.NextHunk(true)
	case "k":
		return s.NextHunk(false)
	case "]":
		return s.NextFile(true)
	case "[":
		return s.NextFile(false)
	}
	return false
}

func (v *view) notify() {
	if v.p.OnSelect != nil {
		v.p.OnSelect(v.s.selection)
	}
}

// row builds row i and records its laid-out height, so the state maps
// scroll offsets and drags to rows as the list does.
func (v *view) row(i int) *fairy.El {
	s := v.s
	var e *fairy.El
	switch r := s.rows[i]; r.kind {
	case rowFile:
		e = v.fileHeader(r.f)
	case rowNote:
		e = v.note(r.f)
	case rowGap:
		e = v.gap(r.f, r.i)
	default:
		e = v.line(i)
	}
	key := s.keys[i]
	return e.OnLayout(func(_, h float32) { s.heights[key] = h })
}

// cell is one side's line in a row: what a half draws.
type cell struct {
	kind      LineKind
	old, new  int
	text      string
	changes   []Range
	noNewline bool
}

// source is the side its text is read from and its number there.
func (c *cell) source() (Side, int) {
	switch {
	case c.kind == Removed && c.old > 0:
		return Old, c.old
	case c.new > 0:
		return New, c.new
	case c.old > 0:
		return Old, c.old
	}
	return New, 1
}

func (c *cell) label() string {
	_, n := c.source()
	f := kit.Messages.DiffUnchangedLine
	switch c.kind {
	case Added:
		f = kit.Messages.DiffAddedLine
	case Removed:
		f = kit.Messages.DiffRemovedLine
	}
	return fmt.Sprintf(f, n, c.text)
}

func (v *view) cells(r row) (*cell, *cell) {
	of := func(h, i int) *cell {
		l := &v.s.files[r.f].Hunks[h].Lines[i]
		return &cell{kind: l.Kind, old: l.Old, new: l.New, text: l.Text, changes: l.Changes, noNewline: l.NoNewline}
	}
	switch r.kind {
	case rowCtx:
		text, ok := v.s.SideLine(r.f, New, r.new)
		if !ok {
			text, _ = v.s.SideLine(r.f, Old, r.old)
		}
		return &cell{kind: Context, old: r.old, new: r.new, text: text}, nil
	case rowLine:
		return of(r.h, r.i), nil
	case rowPair:
		return of(r.h, r.del), of(r.h, r.add)
	}
	return nil, nil
}

func (v *view) line(i int) *fairy.El {
	r := v.s.rows[i]
	a, b := v.cells(r)
	if a == nil {
		return fairy.Box()
	}
	if v.s.mode == Unified {
		return v.unified(i, r.f, a)
	}
	old, new := a, b
	if b == nil {
		switch a.kind {
		case Removed:
			new = nil
		case Added:
			old, new = nil, a
		default:
			new = a
		}
	}
	return v.split(i, r.f, old, new)
}

func (v *view) unified(i, f int, c *cell) *fairy.El {
	l := &v.l
	sel := v.s.rowSelected(i, New, true)
	fill := l.bg
	if t := l.tints(c.kind); t != nil {
		fill = t.line
	}
	if sel {
		fill = l.selected
	}
	digits := v.s.digits[f]
	gutter := fairy.Row(
		v.bar(c.kind),
		v.number(i, f, c.old, Old, c.kind, sel, digits),
		v.number(i, f, c.new, New, c.kind, sel, digits),
	).AlignSelf(fairy.Stretch).Shrink(0).Fill(l.gutterFill(c.kind, sel))
	e := fairy.Row(gutter, v.code(f, c)).WFull().MinH(lineH).Align(fairy.Start).Fill(fill).
		Role(fairy.RoleListItem).Label(c.label())
	if !sel {
		e.HoverFill(fill.Mix(l.text, 0.05))
	}
	return e
}

func (v *view) split(i, f int, old, new *cell) *fairy.El {
	l := &v.l
	digits := v.s.digits[f]
	half := func(side Side, c *cell) *fairy.El {
		h := fairy.Row().Grow(1).Basis(0).MinW(0).MinH(lineH).Align(fairy.Start).AlignSelf(fairy.Stretch)
		if c == nil {
			return h.Fill(l.emptyBg).Role(fairy.RoleHidden)
		}
		sel := v.s.rowSelected(i, side, false)
		fill := l.bg
		if t := l.tints(c.kind); t != nil {
			fill = t.line
		}
		if sel {
			fill = l.selected
		}
		n := c.old
		if side == New {
			n = c.new
		}
		shown := *c
		if c.kind == Context {
			shown.text, _ = v.s.SideLine(f, side, max(n, 1))
		}
		return h.Fill(fill).Role(fairy.RoleListItem).Label(shown.label()).Child(
			fairy.Row(v.bar(c.kind), v.number(i, f, n, side, c.kind, sel, digits)).
				AlignSelf(fairy.Stretch).Shrink(0).Fill(l.gutterFill(c.kind, sel)),
			v.code(f, &shown),
		)
	}
	return fairy.Row(half(Old, old), fairy.Box().W(1).AlignSelf(fairy.Stretch).Fill(l.border).Role(fairy.RoleHidden), half(New, new)).
		WFull().MinH(lineH).Role(fairy.RoleNone)
}

func (v *view) bar(k LineKind) *fairy.El {
	b := fairy.Box().W(barW).AlignSelf(fairy.Stretch).Shrink(0).Role(fairy.RoleHidden)
	if t := v.l.tints(k); t != nil {
		b.Fill(t.bar)
	}
	return b
}

// number is a line-number cell: pressing it selects the line, Shift+press
// extends the selection to it and dragging down the numbers selects the
// lines passed.
func (v *view) number(i, f, n int, side Side, k LineKind, sel bool, digits int) *fairy.El {
	l := &v.l
	s := v.s
	c := fairy.Row().W(l.numberW(digits)).Shrink(0).MinH(lineH).PadXY(8, 0).Justify(fairy.JustifyEnd).Align(fairy.Start).
		Role(fairy.RoleHidden)
	if n > 0 {
		color := l.faint
		if t := l.tints(k); t != nil {
			color = t.number
		}
		if sel {
			color = l.text
		}
		c.Child(fairy.Text(strconv.Itoa(n)).Font(l.mono).LineHeight(lineH).Color(color).NoWrap())
	}
	at := LineRef{side, n}
	if n == 0 {
		// A unified row's empty column selects the line on the other side.
		other := Old
		if side == Old {
			other = New
		}
		m, ok := s.rowLine(s.rows[i], other)
		if !ok || s.mode != Unified {
			return c
		}
		at = LineRef{other, m}
	}
	unified := s.mode == Unified
	return c.Cursor(fairy.CursorPointer).
		OnPointerDown(func(e fairy.PointerEvent) {
			if e.Button == 0 {
				s.pressLine(f, at, e.Mods&keyboard.ModShift != 0)
			}
		}).
		OnClick(func() {
			s.releaseLine()
			v.notify()
		}).
		OnDrag(func(d fairy.DragEvent) {
			switch d.Phase {
			case fairy.DragMove:
				to := s.rowFrom(i, d.LocalY)
				r := s.rows[to]
				if r.f != f {
					return
				}
				// Unified rows hold one side or both: keep to the pressed side
				// where it can.
				m, ok := s.rowLine(r, at.Side)
				side := at.Side
				if !ok && unified {
					side = 1 - at.Side
					m, ok = s.rowLine(r, side)
				}
				if ok {
					s.dragTo(f, LineRef{side, m})
				}
			case fairy.DragEnd:
				if d.Canceled {
					s.cancelLine()
					return
				}
				s.releaseLine()
				v.notify()
			}
		})
}

// code is the text of a line: syntax colors under word changes, tabs
// padded to four columns.
func (v *view) code(f int, c *cell) *fairy.El {
	l := &v.l
	src, n := c.source()
	var word fairy.Color
	var changes []Range
	if t := l.tints(c.kind); t != nil {
		word, changes = t.word, c.changes
	}
	spans := layer(c.text, v.s.highlights(f, src, n, l.syntax), changes, word)
	text := fairy.RichText(tabbed(spans)...).Font(l.mono).LineHeight(lineH).Color(l.text).WFull()
	e := fairy.Col(text).Grow(1).MinW(0).PadEdges(0, codePadR, 0, codePadL)
	if c.noNewline {
		e.Child(fairy.Text(kit.Messages.DiffNoNewline).FontSize(12).Color(l.muted).Opacity(0.6).PadEdges(0, 0, 2, 0))
	}
	return e
}

// gap is the unchanged range before a hunk, folded: buttons to show more,
// and the hunk's header.
func (v *view) gap(f, gi int) *fairy.El {
	l := &v.l
	s := v.s
	file := &s.files[f]
	hidden := max(s.Hidden(f, gi), 0)
	last := gi == len(file.Hunks)
	button := func(how Expand, glyph *svg.Vector, label string) *fairy.El {
		return fairy.Row(fairy.Icon(glyph).Size(16, 16).Color(l.muted)).Size(24, 24).Center().Radius(6).
			HoverFill(l.hover).Cursor(fairy.CursorPointer).Role(fairy.RoleButton).Label(label).
			OnClick(func() { s.Expand(f, gi, how) })
	}
	buttons := fairy.Row().W(v.gutterW(f)).Shrink(0).H(gapH).Center().Gap(2)
	if file.Expandable() {
		step := min(hidden, ExpandStep)
		if hidden <= ExpandStep {
			buttons.Child(button(ExpandAll, lucide.ArrowUpDown, fmt.Sprintf(kit.Messages.DiffShowAll, hidden)))
		} else {
			if gi > 0 {
				buttons.Child(button(ExpandDown, lucide.ChevronDown, fmt.Sprintf(kit.Messages.DiffShowBelow, step)))
			}
			if !last {
				buttons.Child(button(ExpandUp, lucide.ChevronUp, fmt.Sprintf(kit.Messages.DiffShowAbove, step)))
			}
		}
	}
	count := kit.Messages.DiffHiddenLine
	if hidden != 1 {
		count = fmt.Sprintf(kit.Messages.DiffHiddenLines, hidden)
	}
	inner := fairy.Row(buttons).H(gapH).Align(fairy.Center).Gap(12).MinW(0)
	if hidden > 0 {
		c := fairy.Row(fairy.Text(count).FontSize(12).Color(l.muted).NoWrap()).PadXY(6, 0).H(24).Align(fairy.Center).Radius(6)
		if file.Expandable() && hidden > ExpandStep {
			c.HoverFill(l.hover).Cursor(fairy.CursorPointer).Role(fairy.RoleButton).
				Label(fmt.Sprintf(kit.Messages.DiffShowAll, hidden)).OnClick(func() { s.Expand(f, gi, ExpandAll) })
		}
		inner.Child(c)
	}
	label := count
	if !last {
		h := file.Hunks[gi].Header()
		inner.Child(fairy.Text(h).Font(l.mono).FontSize(12).Color(l.faint).Ellipsis().MinW(0).Shrink(1))
		label += ", " + h
	}
	return fairy.Row(inner).WFull().H(gapH).Align(fairy.Center).Fill(l.gapBg).Role(fairy.RoleGroup).Label(label)
}

// note stands for a file with no lines to show.
func (v *view) note(f int) *fairy.El {
	l := &v.l
	file := &v.s.files[f]
	m := kit.Messages
	msg := m.DiffNoChanges
	switch {
	case file.Binary:
		msg = m.DiffBinary
	case file.Status == Renamed || file.Status == Copied:
		msg = m.DiffMoved
	case file.OldMode != file.NewMode:
		msg = m.DiffModeChanged
	case file.Status == FileAdded || file.Status == FileDeleted:
		msg = m.DiffEmpty
	}
	return fairy.Row(fairy.Text(msg).FontSize(12).Color(l.muted)).WFull().H(noteH).Align(fairy.Center).
		PadEdges(0, 0, 0, v.gutterW(f)+codePadL).Fill(l.bg).Role(fairy.RoleText).Label(msg)
}

func (v *view) fileHeader(f int) *fairy.El {
	l := &v.l
	s := v.s
	m := kit.Messages
	file := &s.files[f]
	collapsed := s.Collapsed(f)
	glyph, toggleLabel := lucide.ChevronDown, m.DiffCollapseFile
	if collapsed {
		glyph, toggleLabel = lucide.ChevronRight, m.DiffExpandFile
	}
	toggle := fairy.Row(fairy.Icon(glyph).Size(16, 16).Color(l.muted)).Size(24, 24).Shrink(0).Center().Radius(6).
		HoverFill(l.hover).Cursor(fairy.CursorPointer).Role(fairy.RoleButton).Label(toggleLabel).Expanded(!collapsed).
		OnClick(func() { s.SetCollapsed(f, !s.Collapsed(f)) })
	path := func(p string, c fairy.Color) *fairy.El {
		return fairy.Text(p).Medium().Color(c).Ellipsis().TooltipWhenTruncated().MinW(0).Shrink(1)
	}
	name := fairy.Row().Align(fairy.Center).Gap(6).MinW(0).Shrink(1)
	if (file.Status == Renamed || file.Status == Copied) && file.OldPath != "" && file.NewPath != "" {
		name.Child(path(file.OldPath, l.muted), fairy.Text("→").Color(l.faint), path(file.NewPath, l.text))
	} else {
		name.Child(path(file.Path(), l.text))
	}
	inner := fairy.Row(toggle, name.Role(fairy.RoleHidden)).WFull().Grow(1).PadXY(10, 0).Gap(8).Align(fairy.Center).Role(fairy.RoleNone)
	small := func(s string, c fairy.Color) *fairy.El {
		return fairy.Text(s).FontSize(12).Color(c).NoWrap().Role(fairy.RoleHidden)
	}
	switch file.Status {
	case FileAdded:
		inner.Child(small(m.DiffAdded, l.add.count))
	case FileDeleted:
		inner.Child(small(m.DiffDeleted, l.del.count))
	case Renamed:
		inner.Child(small(m.DiffRenamed, l.muted))
	case Copied:
		inner.Child(small(m.DiffCopied, l.muted))
	}
	if file.OldMode != "" && file.NewMode != "" && file.OldMode != file.NewMode {
		inner.Child(small(file.OldMode+" → "+file.NewMode, l.faint))
	}
	inner.Child(fairy.Spacer())
	adds, dels := file.Additions(), file.Deletions()
	if adds > 0 {
		inner.Child(small(fmt.Sprintf("+%d", adds), l.add.count))
	}
	if dels > 0 {
		inner.Child(small(fmt.Sprintf("−%d", dels), l.del.count))
	}
	label := file.Path()
	if file.Status == Renamed && file.OldPath != "" {
		label += fmt.Sprintf(m.DiffRenamedFrom, file.OldPath)
	}
	label += fmt.Sprintf(m.DiffCounts, adds, dels)
	h := fairy.Col(inner).WFull().H(headerH).Fill(l.headerBg).BorderEdges(0, 0, 1, 0, l.border).
		Role(fairy.RoleGroup).Heading(2).Label(label)
	if f > 0 {
		h.BorderEdges(1, 0, 1, 0, l.border)
	}
	if v.p.HeaderActions != nil {
		// Actions sit outside the hidden row so assistive technology reaches them.
		h = fairy.Row(h.Grow(1).MinW(0), v.p.HeaderActions(f)).WFull().H(headerH).Align(fairy.Center).
			Fill(l.headerBg).Role(fairy.RoleNone)
	}
	return h
}

// layer splits text into spans by its syntax colors and the byte ranges
// given bg (word changes), cutting where either starts or ends.
func layer(text string, syn []kit.Highlight, words []Range, bg fairy.Color) []fairy.Span {
	cuts := []int{0, len(text)}
	for _, h := range syn {
		cuts = append(cuts, h.Start, h.End)
	}
	for _, r := range words {
		cuts = append(cuts, r.Start, r.End)
	}
	slices.Sort(cuts)
	cuts = slices.Compact(cuts)
	var out []fairy.Span
	si, wi := 0, 0
	for k := 0; k+1 < len(cuts); k++ {
		a, b := cuts[k], cuts[k+1]
		if a >= len(text) || a == b {
			continue
		}
		b = min(b, len(text))
		sp := fairy.Span{Text: text[a:b]}
		for si < len(syn) && syn[si].End <= a {
			si++
		}
		if si < len(syn) && syn[si].Start <= a {
			sp.Color = syn[si].Color
		}
		for wi < len(words) && words[wi].End <= a {
			wi++
		}
		if wi < len(words) && words[wi].Start <= a {
			sp.Background = bg
		}
		out = append(out, sp)
	}
	return out
}

// tabbed replaces tabs in spans with spaces to four-column stops.
func tabbed(spans []fairy.Span) []fairy.Span {
	col := 0
	for i := range spans {
		t := spans[i].Text
		if !strings.Contains(t, "\t") {
			col += len([]rune(t))
			continue
		}
		var b strings.Builder
		for _, c := range t {
			if c == '\t' {
				n := 4 - col%4
				b.WriteString(strings.Repeat(" ", n))
				col += n
				continue
			}
			b.WriteRune(c)
			col++
		}
		spans[i].Text = b.String()
	}
	return spans
}

type chunkKey struct {
	f    int
	side Side
	c    int
}

type chunk struct {
	starts []int // byte offset of each line in the chunk's text, and one past the last
	hl     []kit.Highlight
}

// highlights are the syntax colors of line n of side of file f, relative
// to the line, highlighted with the 128 lines around it.
func (s *State) highlights(f int, side Side, n int, colors *kit.SyntaxColors) []kit.Highlight {
	lang := s.files[f].Language()
	if lang == "" || !syntax.Known(lang) || n < 1 {
		return nil
	}
	c := (n - 1) / chunkLen
	k := chunkKey{f, side, c}
	ch := s.hlCache[k]
	if ch == nil {
		var text strings.Builder
		ch = &chunk{starts: make([]int, 0, chunkLen+1)}
		for m := c*chunkLen + 1; m <= (c+1)*chunkLen; m++ {
			ch.starts = append(ch.starts, text.Len())
			l, _ := s.SideLine(f, side, m)
			text.WriteString(l)
			text.WriteByte('\n')
		}
		ch.starts = append(ch.starts, text.Len())
		ch.hl = kit.BuiltinHighlights(lang, text.String())
		if s.hlCache == nil || len(s.hlCache) > 512 {
			s.hlCache = map[chunkKey]*chunk{}
		}
		s.hlCache[k] = ch
	}
	i := (n - 1) % chunkLen
	a, b := ch.starts[i], ch.starts[i+1]-1
	first, _ := slices.BinarySearchFunc(ch.hl, a, func(h kit.Highlight, a int) int {
		if h.End <= a {
			return -1
		}
		return 1
	})
	var out []kit.Highlight
	for _, h := range ch.hl[first:] {
		if h.Start >= b {
			break
		}
		h.Start, h.End = max(h.Start, a)-a, min(h.End, b)-a
		if h.Start < h.End {
			h.Color = colors.Paint(h)
			out = append(out, h)
		}
	}
	return out
}
