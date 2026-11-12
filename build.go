package fairy

import (
	"time"

	"github.com/myparsleycat/fairy/internal/text"
	"github.com/myparsleycat/fairy/layout"
	"github.com/myparsleycat/fairy/motion"
)

func (w *Window) baseText() textProps {
	return textProps{
		color:  w.opts.TextColor,
		size:   w.opts.FontSize,
		weight: 400,
		italic: -1,
		family: w.opts.Font,
	}
}

// build runs the view and lays it out.
func (w *Window) build() {
	w.dirty = false
	w.dirtyBy = 0
	w.gen++
	w.treeVer++
	w.prevByID, w.byID = w.byID, w.prevByID
	clear(w.byID)
	w.layers = w.layers[:0]
	w.growing = false
	clear(w.matchClaims)
	w.collecting = true
	focusBefore := w.focused
	w.building = true
	w.mark()
	viewStart := time.Now()
	root := w.withBuildWindow(func() *El { return w.opts.View(&Cx{w: w}) })
	if debugSlow > 0 {
		slow("View", viewStart)
	}
	w.lap(&w.ft.view)
	w.building = false
	for k, l := range w.locals {
		if w.gen-l.gen > stateTTL {
			delete(w.locals, k)
		}
	}
	w.sweepMemos()
	if root == nil {
		root = Box()
	}
	w.root = root
	base := w.baseText()
	w.autoFocused = false
	w.mark()
	w.prepare(root, nil, 0, 0, base)
	w.lap(&w.ft.prepare)
	lw, lh := float32(w.width)/w.scale, float32(w.height)/w.scale
	layout.Compute(&root.node, lw, lh)
	w.lap(&w.ft.layout)
	w.buildRows(root)
	w.lap(&w.ft.rows)

	// Popovers are collected while preparing, overlays while running the view.
	for i := 0; i < len(w.layers); i++ {
		l := w.layers[i]
		pid := l.anchor
		if pid == 0 {
			pid = 0x6c61796572
		}
		w.autoFocused = false
		w.mark()
		w.prepare(l.root, nil, pid, i, base)
		w.lap(&w.ft.prepare)
		if l.pop != nil {
			layout.ComputeFit(&l.root.node, lw, lh)
		} else {
			layout.Compute(&l.root.node, lw, lh)
		}
		w.lap(&w.ft.layout)
		w.buildRows(l.root)
		w.lap(&w.ft.rows)
	}
	w.collecting = false

	w.syncSelection()
	w.syncFocusReturns(focusBefore)
	w.syncReturnTo()
	w.syncFollows()
	w.applyFocusWant()
	w.collectGhosts()
	for id, s := range w.states {
		if w.gen-s.frame > stateTTL {
			delete(w.states, id)
		}
	}
	if w.focused != 0 {
		if _, ok := w.byID[w.focused]; !ok {
			w.focused = 0
			w.updateIME()
		}
	}
}

// hashID names an element by its parent, its kind and its key or index, so
// that an element of another kind taking a sibling's place starts afresh
// instead of inheriting its state and focus.
func hashID(parent uint64, kind elKind, key string, index int) uint64 {
	const prime = 1099511628211
	h := parent ^ 14695981039346656037
	h = (h ^ uint64(kind)) * prime
	if key != "" {
		h = (h ^ 'k') * prime
		for i := 0; i < len(key); i++ {
			h = (h ^ uint64(key[i])) * prime
		}
	} else {
		h = (h ^ 'i') * prime
		for v := uint64(index) + 1; v > 0; v >>= 8 {
			h = (h ^ (v & 0xff)) * prime
		}
	}
	return h
}

func (w *Window) state(id uint64) *elState {
	s := w.states[id]
	switch {
	case s == nil:
		s = &elState{born: w.gen}
		w.states[id] = s
	case s.frame < w.gen-1:
		// It was gone for a build or more, as a popover closed and opened
		// again: it appears afresh, with its enter transition and without
		// easing from how it was drawn before.
		s.born = w.gen
		s.anims = [animCount]*motion.Track{}
	}
	s.frame = w.gen
	return s
}

func (w *Window) needsState(e *El) bool {
	return e.style.ScrollX || e.style.ScrollY || e.kind == kindInput || e.focusable ||
		e.motion != nil || e.layoutMotion != nil || e.enter != nil || e.exit != nil || e.xfp != nil || e.heightMotion != nil || e.matchID != "" ||
		e.onLayout != nil
}

func (w *Window) prepare(e, parent *El, pid uint64, index int, inh textProps) {
	e.parent = parent
	if parent != nil {
		pid = parent.id
	}
	e.id = hashID(pid, e.kind, e.key, index)
	w.byID[e.id] = e
	e.fade = 1
	e.grow = growDelta{}
	if e.memo != nil {
		w.prepareMemo(e, parent)
	}
	e.applyDisabled(parent)
	if e.heightMotion != nil {
		w.growing = true
	}
	if e.matchID != "" {
		if w.matchClaims == nil {
			w.matchClaims = map[string]uint64{}
		}
		w.matchClaims[e.matchID] = e.id
	}
	if w.needsState(e) {
		e.state = w.state(e.id)
	} else if s, ok := w.states[e.id]; ok {
		s.frame = w.gen
		e.state = s
	}
	if e.focusable && w.focused == 0 && w.focusKey != "" && e.key == w.focusKey {
		w.focused = e.id
		w.updateIME()
	}
	if e.autoFocus && !w.autoFocused && w.prevByID[e.id] == nil {
		w.focused, w.focusKey = e.id, e.key
		w.autoFocused = true
		w.updateIME()
	}
	if e.popover != nil && w.collecting {
		po := e.popover
		w.layers = append(w.layers, &layer{root: po.content, anchor: e.id, pop: po, onDismiss: po.opts.OnDismiss})
	}

	if !e.tpSaved {
		e.ownTP, e.tpSaved = e.tp, true
	}
	tp := e.ownTP
	if !tp.color.isSet() {
		tp.color = inh.color
	}
	if tp.size == 0 {
		tp.size = inh.size
	}
	if tp.weight == 0 {
		tp.weight = inh.weight
	}
	if tp.italic == 0 {
		tp.italic = inh.italic
	}
	if tp.family == "" {
		tp.family = inh.family
	}
	if tp.lineHeight == 0 {
		tp.lineHeight = inh.lineHeight
	}
	if tp.letterSpacing == 0 {
		tp.letterSpacing = inh.letterSpacing
	}
	if !tp.alignSet {
		tp.align = inh.align
	}
	// Wrapping and trimming apply to the element they are set on only.
	childInh := tp
	childInh.noWrap, childInh.ellipsis, childInh.alignSet = false, false, false
	e.tp = tp

	n := &e.node
	n.Style = e.style
	n.Style.Padding = e.padding()
	n.Children = n.Children[:0]
	n.Measure = nil
	switch e.kind {
	case kindText:
		n.Measure = w.textMeasure(e)
	case kindImage:
		n.Measure = w.imageMeasure(e)
	case kindInput:
		n.Measure = w.inputMeasure(e)
	case kindIcon, kindSpinner, kindArc:
		n.Measure = w.iconMeasure(e)
	}
	if e.virtual != nil {
		e.virtual.inh = childInh
		e.virtual.built = false
		e.virtual.synced = false
		return
	}
	for i, c := range e.children {
		w.prepare(c, e, 0, i, childInh)
		n.Children = append(n.Children, &c.node)
	}
}

func (w *Window) textStyle(tp textProps) text.Style {
	al := text.AlignStart
	switch tp.align {
	case TextCenter:
		al = text.AlignCenter
	case TextEnd:
		al = text.AlignEnd
	}
	return text.Style{
		Family:        tp.family,
		Size:          tp.size * w.scale,
		Weight:        tp.weight,
		Italic:        tp.italic > 0,
		LineHeight:    tp.lineHeight * w.scale,
		Align:         al,
		NoWrap:        tp.noWrap,
		Ellipsis:      tp.ellipsis,
		LetterSpacing: tp.letterSpacing * w.scale,
	}
}

func (w *Window) devWidth(width float32) float32 {
	if width != width {
		return text.Unbounded
	}
	return width * w.scale
}

func (w *Window) textMeasure(e *El) layout.MeasureFunc {
	return func(width float32, exact bool) (float32, float32) {
		st := w.elTextStyle(e)
		lw := w.devWidth(width)
		if !exact && (st.Align != text.AlignStart || st.Ellipsis) {
			// Measure natural size without alignment slack.
			st.Align = text.AlignStart
		}
		tw, th := w.ts.Measure(e.text, st, lw)
		if exact && width == width {
			tw = width * w.scale
		}
		return tw / w.scale, th / w.scale
	}
}

func (w *Window) imageMeasure(e *El) layout.MeasureFunc {
	return func(width float32, exact bool) (float32, float32) {
		if e.image == nil {
			return 0, 0
		}
		iw, ih := float32(e.image.data.W), float32(e.image.data.H)
		if exact && width == width && iw > 0 {
			return width, width * ih / iw
		}
		if width == width && iw > width && iw > 0 {
			return width, width * ih / iw
		}
		return iw, ih
	}
}

func (w *Window) inputMeasure(e *El) layout.MeasureFunc {
	return func(width float32, exact bool) (float32, float32) {
		st := w.textStyle(e.tp)
		lh := st.LineHeight
		if lh == 0 {
			_, lh = w.ts.Measure("Ag", st, text.Unbounded)
		}
		if e.input.multiline {
			s := e.input.value
			if s == "" {
				s = " "
			}
			st.NoWrap = false
			_, h := w.ts.Measure(s+"​", st, w.devWidth(width))
			h = max(h, lh)
			fw := float32(160)
			if exact && width == width {
				fw = width
			}
			return fw, h / w.scale
		}
		fw := float32(160)
		if exact && width == width {
			fw = width
		}
		return fw, lh / w.scale
	}
}
