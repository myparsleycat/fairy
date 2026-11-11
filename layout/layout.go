// Package layout is a flexbox layout engine covering the subset of CSS flexbox
// that application UIs use: direction, wrap, grow/shrink/basis, min/max sizes,
// padding, margin, gap, alignment, absolute positioning and scroll containers.
package layout

import (
	"math"
	"sync/atomic"
)

// Unit is how a Dim is interpreted.
type Unit uint8

const (
	Auto Unit = iota // sized by content and flex rules
	Px               // pixels
	// Pct is a fraction of the parent's content box from 0 to 1, not a
	// percentage: 0.5 is half.
	Pct
)

// Dim is a length.
type Dim struct {
	Unit  Unit
	Value float32
}

// PxDim returns a length of v pixels.
func PxDim(v float32) Dim { return Dim{Px, v} }

// PctDim returns a length of fraction f of the parent's content box, where 1
// is the whole box and 0.5 half of it.
func PctDim(f float32) Dim { return Dim{Pct, f} }

// Direction is the main axis children are laid out along, as CSS
// flex-direction.
type Direction uint8

const (
	Column Direction = iota
	Row
)

// Align places children on the cross axis, as CSS align-items and align-self.
type Align uint8

const (
	AlignAuto Align = iota // AlignSelf only: inherit the parent's AlignItems
	AlignStretch
	AlignStart
	AlignCenter
	AlignEnd
)

// Justify distributes children along the main axis, as CSS justify-content.
type Justify uint8

const (
	JustifyStart Justify = iota
	JustifyCenter
	JustifyEnd
	JustifyBetween
	JustifyAround
	JustifyEvenly
)

// Edges holds per-side lengths in pixels.
type Edges struct{ Top, Right, Bottom, Left float32 }

// Inset positions an absolutely positioned node; unset sides are NaN.
type Inset struct{ Top, Right, Bottom, Left float32 }

// NoInset has every side unset.
var NoInset = Inset{nan, nan, nan, nan}

var nan = float32(math.NaN())

// Style is the layout input of a node.
type Style struct {
	Direction  Direction
	Wrap       bool
	Width      Dim
	Height     Dim
	MinWidth   Dim
	MinHeight  Dim
	MaxWidth   Dim
	MaxHeight  Dim
	Basis      Dim
	Grow       float32
	Shrink     float32
	Padding    Edges
	Margin     Edges
	Gap        float32 // between items on the main axis
	CrossGap   float32 // between wrapped lines
	AlignItems Align
	AlignSelf  Align
	Justify    Justify
	Absolute   bool
	Inset      Inset
	ScrollX    bool // children may extend past the node horizontally
	ScrollY    bool
	Hidden     bool // takes no space and is not laid out
}

// MeasureFunc sizes leaf content. width is the content-box width to lay out in:
// NaN means unbounded; exact means the node's width is already fixed to width.
type MeasureFunc func(width float32, exact bool) (w, h float32)

// Node is one box in the layout tree.
type Node struct {
	Style    Style
	Children []*Node
	Measure  MeasureFunc

	// Outputs. X and Y are relative to the parent's border box.
	X, Y, W, H float32
	// ContentW and ContentH cover the children's extent plus padding, for scrolling.
	ContentW, ContentH float32

	cache    [4]cacheEntry
	cacheN   int
	cacheGen uint32

	mcGen     uint32
	mcW, mcAW float32
	mcH       float32
}

type cacheEntry struct {
	kw, kh, aw, ah float32
	w, h           float32
}

var generations atomic.Uint32

// Compute lays out root at a fixed size.
// Different trees may be computed concurrently, but one tree must not be
// computed from two goroutines at once.
func Compute(root *Node, width, height float32) {
	gen := generations.Add(1)
	root.X, root.Y = 0, 0
	root.compute(gen, width, height, width, height, true)
}

// ComputeFit lays out root at the width and height its own style gives, or
// else at its content size, bounded by maxWidth and maxHeight.
// Different trees may be computed concurrently, but one tree must not be
// computed from two goroutines at once.
func ComputeFit(root *Node, maxWidth, maxHeight float32) {
	gen := generations.Add(1)
	root.X, root.Y = 0, 0
	kw, kh := resolve(root.Style.Width, maxWidth), resolve(root.Style.Height, maxHeight)
	if !isNaN(kw) {
		kw = min(root.clampW(kw, maxWidth), maxWidth)
	}
	if !isNaN(kh) {
		kh = min(root.clampH(kh, maxHeight), maxHeight)
	}
	w, h := root.compute(gen, kw, kh, maxWidth, maxHeight, false)
	w, h = min(w, maxWidth), min(h, maxHeight)
	root.compute(gen, w, h, w, h, true)
}

// ComputeAtWidth lays out root at a fixed width and the height that its own
// style, or else its content, gives.
// Different trees may be computed concurrently, but one tree must not be
// computed from two goroutines at once.
func ComputeAtWidth(root *Node, width float32) {
	gen := generations.Add(1)
	root.X, root.Y = 0, 0
	h := resolve(root.Style.Height, nan)
	if isNaN(h) {
		_, ch := root.compute(gen, width, nan, width, nan, true)
		if h = root.clampH(ch, nan); h == ch {
			return
		}
	}
	root.compute(gen, width, h, width, h, true)
}

func isNaN(f float32) bool { return f != f }

func same(a, b float32) bool { return a == b || (isNaN(a) && isNaN(b)) }

func resolve(d Dim, parent float32) float32 {
	switch d.Unit {
	case Px:
		return d.Value
	case Pct:
		if isNaN(parent) {
			return nan
		}
		return d.Value * parent
	}
	return nan
}

func clamp(v, lo, hi float32) float32 {
	if !isNaN(hi) && v > hi {
		v = hi
	}
	if !isNaN(lo) && v < lo {
		v = lo
	}
	return v
}

func sub(a, b float32) float32 {
	if isNaN(a) {
		return nan
	}
	return max(a-b, 0)
}

func (n *Node) clampW(w, parentW float32) float32 {
	return clamp(w, resolve(n.Style.MinWidth, parentW), resolve(n.Style.MaxWidth, parentW))
}

// contentAvailW bounds the content-box width avail that a content-sized n
// lays its children out in by n's own minimum and maximum width, which its
// final width will respect.
func (n *Node) contentAvailW(avail float32) float32 {
	ph := n.Style.Padding.Left + n.Style.Padding.Right
	if hi := sub(resolve(n.Style.MaxWidth, nan), ph); !isNaN(hi) && !(avail <= hi) {
		avail = hi
	}
	if lo := sub(resolve(n.Style.MinWidth, nan), ph); avail < lo {
		avail = lo
	}
	return avail
}

func (n *Node) clampH(h, parentH float32) float32 {
	return clamp(h, resolve(n.Style.MinHeight, parentH), resolve(n.Style.MaxHeight, parentH))
}

type item struct {
	n                      *Node
	base, hyp, main, cross float32
	minMain, maxMain       float32
	minCross, maxCross     float32
	specCross              float32
	mMain0, mMain1         float32
	mCross0, mCross1       float32
	stretch                bool
	frozen                 bool
	violation              float32
}

func (it *item) outerMain() float32  { return it.main + it.mMain0 + it.mMain1 }
func (it *item) outerCross() float32 { return it.cross + it.mCross0 + it.mCross1 }

type line struct {
	items []*item
	cross float32
}

// compute returns the border-box size of n. kw/kh are definite sizes (NaN when
// content-sized); aw/ah bound content-sized nodes (NaN when unbounded).
func (n *Node) compute(gen uint32, kw, kh, aw, ah float32, perform bool) (float32, float32) {
	if !perform {
		if n.cacheGen != gen {
			n.cacheGen, n.cacheN = gen, 0
		}
		for i := 0; i < n.cacheN; i++ {
			c := &n.cache[i]
			if same(c.kw, kw) && same(c.kh, kh) && same(c.aw, aw) && same(c.ah, ah) {
				return c.w, c.h
			}
		}
	}
	w, h := n.computeUncached(gen, kw, kh, aw, ah, perform)
	if !perform {
		i := n.cacheN
		if i == len(n.cache) {
			copy(n.cache[:], n.cache[1:])
			i--
		} else {
			n.cacheN++
		}
		n.cache[i] = cacheEntry{kw, kh, aw, ah, w, h}
	}
	return w, h
}

func (n *Node) computeUncached(gen uint32, kw, kh, aw, ah float32, perform bool) (float32, float32) {
	st := &n.Style
	pad := st.Padding
	ph, pv := pad.Left+pad.Right, pad.Top+pad.Bottom

	if n.Measure != nil && len(n.Children) == 0 {
		cw, exact := nan, false
		if !isNaN(kw) {
			cw, exact = max(kw-ph, 0), true
		} else if !isNaN(aw) {
			cw = max(aw-ph, 0)
		}
		mw, mh := n.Measure(cw, exact)
		w, h := kw, kh
		if isNaN(w) {
			w = n.clampW(mw+ph, nan)
			if !isNaN(aw) && w > aw && isNaN(resolve(st.MinWidth, nan)) {
				w = max(aw, ph)
			}
		}
		if isNaN(h) {
			if !exact && w-ph < mw {
				// The width was clamped below the natural width: re-measure for the height.
				_, mh = n.Measure(max(w-ph, 0), true)
			}
			h = n.clampH(mh+pv, nan)
		}
		if perform {
			n.W, n.H, n.ContentW, n.ContentH = w, h, w, h
		}
		return w, h
	}

	row := st.Direction == Row
	innerW, innerH := sub(kw, ph), sub(kh, pv)
	availW, availH := innerW, innerH
	if isNaN(availW) {
		availW = n.contentAvailW(sub(aw, ph))
	}
	if isNaN(availH) {
		availH = sub(ah, pv)
	}
	if st.ScrollX {
		availW = nan
	}
	if st.ScrollY {
		availH = nan
	}

	var innerMain, innerCross, availMain, availCross float32
	if row {
		innerMain, innerCross, availMain, availCross = innerW, innerH, availW, availH
		if st.ScrollX {
			innerMain = nan
		}
	} else {
		innerMain, innerCross, availMain, availCross = innerH, innerW, availH, availW
		if st.ScrollY {
			innerMain = nan
		}
	}

	items := make([]*item, 0, len(n.Children))
	for _, c := range n.Children {
		if c.Style.Hidden {
			if perform {
				c.X, c.Y, c.W, c.H = 0, 0, 0, 0
			}
			continue
		}
		if c.Style.Absolute {
			continue
		}
		items = append(items, n.prepareItem(gen, c, row, innerMain, innerCross, availMain, availCross))
	}

	// Break into lines.
	var lines []line
	limit := innerMain
	if isNaN(limit) {
		limit = availMain
	}
	if st.Wrap && !isNaN(limit) {
		cur := line{}
		used := float32(0)
		for _, it := range items {
			add := it.hyp + it.mMain0 + it.mMain1
			if len(cur.items) > 0 && used+st.Gap+add > limit+0.01 {
				lines = append(lines, cur)
				cur, used = line{}, 0
			}
			if len(cur.items) > 0 {
				used += st.Gap
			}
			used += add
			cur.items = append(cur.items, it)
		}
		if len(cur.items) > 0 {
			lines = append(lines, cur)
		}
	} else {
		lines = []line{{items: items}}
	}

	// Main sizes.
	maxLineMain := float32(0)
	for li := range lines {
		ln := &lines[li]
		gaps := st.Gap * float32(max(len(ln.items)-1, 0))
		if !isNaN(innerMain) {
			resolveFlex(ln.items, innerMain-gaps)
		} else {
			for _, it := range ln.items {
				it.main = it.hyp
			}
		}
		total := gaps
		for _, it := range ln.items {
			total += it.outerMain()
		}
		maxLineMain = max(maxLineMain, total)
	}
	if row && isNaN(kw) {
		if w := n.clampW(maxLineMain+ph, nan); w != maxLineMain+ph {
			// Lay out again at the clamped width, so items shrink or grow to it
			// before their heights are measured, as they will when placed.
			return n.computeUncached(gen, w, kh, aw, ah, perform)
		}
	}

	// Cross sizes.
	single := len(lines) == 1 && !st.Wrap
	for li := range lines {
		ln := &lines[li]
		for _, it := range ln.items {
			switch {
			case !isNaN(it.specCross):
				it.cross = clamp(it.specCross, it.minCross, it.maxCross)
			case it.stretch && single && !isNaN(innerCross):
				it.cross = clamp(max(innerCross-it.mCross0-it.mCross1, 0), it.minCross, it.maxCross)
			default:
				it.cross = it.measureCross(gen, row, availCross)
			}
		}
		if single && !isNaN(innerCross) {
			ln.cross = innerCross
		} else {
			for _, it := range ln.items {
				ln.cross = max(ln.cross, it.outerCross())
			}
		}
	}

	totalCross := st.CrossGap * float32(max(len(lines)-1, 0))
	for _, ln := range lines {
		totalCross += ln.cross
	}

	var w, h float32
	if row {
		w, h = kw, kh
		if isNaN(w) {
			w = n.clampW(maxLineMain+ph, nan)
		}
		if isNaN(h) {
			h = n.clampH(totalCross+pv, nan)
		}
	} else {
		w, h = kw, kh
		if isNaN(h) {
			h = n.clampH(maxLineMain+pv, nan)
		}
		if isNaN(w) {
			if w = n.clampW(totalCross+ph, nan); w != totalCross+ph {
				return n.computeUncached(gen, w, kh, aw, ah, perform)
			}
		}
	}

	// Stretch items to the final line cross size when the container was content-sized.
	finalInnerCross := sub(h, pv)
	if row {
		finalInnerCross = sub(h, pv)
	} else {
		finalInnerCross = sub(w, ph)
	}
	if single && isNaN(innerCross) {
		lines[0].cross = finalInnerCross
		for _, it := range lines[0].items {
			if it.stretch && isNaN(it.specCross) {
				it.cross = clamp(max(finalInnerCross-it.mCross0-it.mCross1, 0), it.minCross, it.maxCross)
			}
		}
	} else if !single {
		for li := range lines {
			for _, it := range lines[li].items {
				if it.stretch && isNaN(it.specCross) {
					it.cross = clamp(max(lines[li].cross-it.mCross0-it.mCross1, 0), it.minCross, it.maxCross)
				}
			}
		}
	}

	if !perform {
		return w, h
	}

	n.W, n.H = w, h
	finalInnerMain := sub(w, ph)
	if !row {
		finalInnerMain = sub(h, pv)
	}
	if (row && st.ScrollX) || (!row && st.ScrollY) {
		finalInnerMain = max(finalInnerMain, maxLineMain)
	}

	contentW, contentH := float32(0), float32(0)
	crossPos := float32(0)
	for _, ln := range lines {
		gaps := st.Gap * float32(max(len(ln.items)-1, 0))
		used := gaps
		for _, it := range ln.items {
			used += it.outerMain()
		}
		free := finalInnerMain - used
		pos, between := justify(st.Justify, free, len(ln.items))
		for i, it := range ln.items {
			if i > 0 {
				pos += st.Gap + between
			}
			pos += it.mMain0
			align := it.n.Style.AlignSelf
			if align == AlignAuto {
				align = st.AlignItems
			}
			cpos := crossPos + it.mCross0
			switch align {
			case AlignCenter:
				cpos = crossPos + (ln.cross-it.outerCross())/2 + it.mCross0
			case AlignEnd:
				cpos = crossPos + ln.cross - it.cross - it.mCross1
			}
			c := it.n
			var cw, chh float32
			if row {
				cw, chh = it.main, it.cross
				c.compute(gen, cw, chh, cw, chh, true)
				c.X, c.Y = pad.Left+pos, pad.Top+cpos
			} else {
				cw, chh = it.cross, it.main
				c.compute(gen, cw, chh, cw, chh, true)
				c.X, c.Y = pad.Left+cpos, pad.Top+pos
			}
			contentW = max(contentW, c.X+c.W+c.Style.Margin.Right)
			contentH = max(contentH, c.Y+c.H+c.Style.Margin.Bottom)
			pos += it.main + it.mMain1
		}
		crossPos += ln.cross + st.CrossGap
	}

	for _, c := range n.Children {
		if !c.Style.Absolute || c.Style.Hidden {
			continue
		}
		n.placeAbsolute(gen, c, w, h)
	}

	n.ContentW = max(w, contentW+pad.Right)
	n.ContentH = max(h, contentH+pad.Bottom)
	return w, h
}

func (n *Node) prepareItem(gen uint32, c *Node, row bool, innerMain, innerCross, availMain, availCross float32) *item {
	cs := &c.Style
	it := &item{n: c}
	var mainDim, crossDim, minMainDim, maxMainDim, minCrossDim, maxCrossDim Dim
	if row {
		mainDim, crossDim = cs.Width, cs.Height
		minMainDim, maxMainDim, minCrossDim, maxCrossDim = cs.MinWidth, cs.MaxWidth, cs.MinHeight, cs.MaxHeight
		it.mMain0, it.mMain1, it.mCross0, it.mCross1 = cs.Margin.Left, cs.Margin.Right, cs.Margin.Top, cs.Margin.Bottom
	} else {
		mainDim, crossDim = cs.Height, cs.Width
		minMainDim, maxMainDim, minCrossDim, maxCrossDim = cs.MinHeight, cs.MaxHeight, cs.MinWidth, cs.MaxWidth
		it.mMain0, it.mMain1, it.mCross0, it.mCross1 = cs.Margin.Top, cs.Margin.Bottom, cs.Margin.Left, cs.Margin.Right
	}
	specMain := resolve(mainDim, innerMain)
	it.maxMain = resolve(maxMainDim, innerMain)
	it.minCross = resolve(minCrossDim, innerCross)
	it.maxCross = resolve(maxCrossDim, innerCross)
	if v := resolve(crossDim, innerCross); !isNaN(v) {
		it.specCross = clamp(v, it.minCross, it.maxCross)
	} else {
		it.specCross = nan
	}

	align := cs.AlignSelf
	if align == AlignAuto {
		align = n.Style.AlignItems
	}
	it.stretch = align == AlignStretch

	crossKnown := it.specCross
	if isNaN(crossKnown) && it.stretch && !isNaN(innerCross) {
		crossKnown = clamp(max(innerCross-it.mCross0-it.mCross1, 0), it.minCross, it.maxCross)
	}
	am := sub(availMain, it.mMain0+it.mMain1)
	ac := sub(availCross, it.mCross0+it.mCross1)

	it.minMain = resolve(minMainDim, innerMain)
	if isNaN(it.minMain) {
		switch {
		case mainDim.Unit == Px:
			// An explicit pixel size is a promise: do not shrink below it.
			it.minMain = specMain
		case !row:
			// Vertically, items do not shrink below their content (scroll containers excepted).
			// As in CSS, a definite size such as a percentage caps that minimum.
			it.minMain = c.minContentHeight(gen, crossKnown, ac)
			if !isNaN(specMain) {
				it.minMain = min(it.minMain, specMain)
			}
		default:
			// Horizontally, items may shrink to nothing so text can wrap or ellipsize.
			it.minMain = 0
		}
	}

	basis := resolve(cs.Basis, innerMain)
	if isNaN(basis) {
		basis = specMain
	}
	if isNaN(basis) && cs.Grow > 0 && !isNaN(innerMain) {
		// Like CSS flex: 1, growing items share the space regardless of content.
		basis = 0
	}
	if isNaN(basis) {
		if row {
			basis, _ = c.compute(gen, nan, crossKnown, am, ac, false)
		} else {
			_, basis = c.compute(gen, crossKnown, nan, ac, am, false)
		}
	}
	it.base = basis
	it.hyp = clamp(basis, it.minMain, it.maxMain)
	return it
}

func (it *item) measureCross(gen uint32, row bool, availCross float32) float32 {
	ac := sub(availCross, it.mCross0+it.mCross1)
	var v float32
	if row {
		_, v = it.n.compute(gen, it.main, nan, it.main, ac, false)
	} else {
		v, _ = it.n.compute(gen, nan, it.main, ac, it.main, false)
	}
	return clamp(v, it.minCross, it.maxCross)
}

func resolveFlex(items []*item, space float32) {
	used := float32(0)
	for _, it := range items {
		used += it.hyp + it.mMain0 + it.mMain1
	}
	growing := space > used
	for _, it := range items {
		it.main = it.hyp
		factor := it.n.Style.Shrink
		if growing {
			factor = it.n.Style.Grow
		}
		it.frozen = factor == 0 || (growing && it.base > it.hyp) || (!growing && it.base < it.hyp)
	}
	for iter := 0; iter < len(items)+1; iter++ {
		sumUsed, sumFactor := float32(0), float32(0)
		for _, it := range items {
			if it.frozen {
				sumUsed += it.main + it.mMain0 + it.mMain1
			} else {
				sumUsed += it.base + it.mMain0 + it.mMain1
				if growing {
					sumFactor += it.n.Style.Grow
				} else {
					sumFactor += it.n.Style.Shrink * it.base
				}
			}
		}
		if sumFactor == 0 {
			for _, it := range items {
				if !it.frozen {
					it.main = clamp(it.base, it.minMain, it.maxMain)
				}
			}
			return
		}
		free := space - sumUsed
		total := float32(0)
		for _, it := range items {
			if it.frozen {
				continue
			}
			var target float32
			if growing {
				target = it.base + free*it.n.Style.Grow/sumFactor
			} else {
				target = it.base + free*it.n.Style.Shrink*it.base/sumFactor
			}
			c := clamp(max(target, 0), it.minMain, it.maxMain)
			it.main = c
			it.violation = c - target
			total += it.violation
		}
		done := true
		for _, it := range items {
			if it.frozen {
				continue
			}
			switch {
			case abs(total) < 0.001:
				it.frozen = true
			case total > 0 && it.violation > 0:
				it.frozen = true
				done = false
			case total < 0 && it.violation < 0:
				it.frozen = true
				done = false
			}
		}
		if done && abs(total) < 0.001 {
			return
		}
	}
}

func justify(j Justify, free float32, n int) (start, between float32) {
	if n == 0 {
		return 0, 0
	}
	switch j {
	case JustifyCenter:
		return free / 2, 0
	case JustifyEnd:
		return free, 0
	case JustifyBetween:
		if n > 1 && free > 0 {
			return 0, free / float32(n-1)
		}
	case JustifyAround:
		if free > 0 {
			b := free / float32(n)
			return b / 2, b
		}
	case JustifyEvenly:
		if free > 0 {
			b := free / float32(n+1)
			return b, b
		}
	}
	return 0, 0
}

func (n *Node) placeAbsolute(gen uint32, c *Node, w, h float32) {
	cs := &c.Style
	m := cs.Margin
	in := cs.Inset
	cw := resolve(cs.Width, w)
	if isNaN(cw) && !isNaN(in.Left) && !isNaN(in.Right) {
		cw = max(w-in.Left-in.Right-m.Left-m.Right, 0)
	}
	chh := resolve(cs.Height, h)
	if isNaN(chh) && !isNaN(in.Top) && !isNaN(in.Bottom) {
		chh = max(h-in.Top-in.Bottom-m.Top-m.Bottom, 0)
	}
	if isNaN(cw) || isNaN(chh) {
		mw, mh := c.compute(gen, cw, chh, w, h, false)
		if isNaN(cw) {
			cw = mw
		}
		if isNaN(chh) {
			chh = mh
		}
	}
	cw, chh = c.clampW(cw, w), c.clampH(chh, h)
	c.compute(gen, cw, chh, cw, chh, true)
	switch {
	case !isNaN(in.Left):
		c.X = in.Left + m.Left
	case !isNaN(in.Right):
		c.X = w - in.Right - cw - m.Right
	default:
		c.X = n.Style.Padding.Left + m.Left
	}
	switch {
	case !isNaN(in.Top):
		c.Y = in.Top + m.Top
	case !isNaN(in.Bottom):
		c.Y = h - in.Bottom - chh - m.Bottom
	default:
		c.Y = n.Style.Padding.Top + m.Top
	}
}

func abs(f float32) float32 {
	if f < 0 {
		return -f
	}
	return f
}

// minContentHeight is the smallest border-box height n can take at width without
// overflowing its content. Scroll containers report 0 since they can scroll instead.
func (n *Node) minContentHeight(gen uint32, width, aw float32) float32 {
	st := &n.Style
	maxH := resolve(st.MaxHeight, nan)
	if v := resolve(st.MinHeight, nan); !isNaN(v) {
		return v
	}
	if st.Height.Unit == Px {
		return clamp(st.Height.Value, nan, maxH)
	}
	if st.ScrollY {
		return 0
	}
	if n.mcGen == gen && same(n.mcW, width) && same(n.mcAW, aw) {
		return n.mcH
	}
	var h float32
	if n.Measure != nil && len(n.Children) == 0 {
		_, h = n.compute(gen, width, nan, aw, nan, false)
	} else if st.Direction == Row && st.Wrap {
		_, h = n.compute(gen, width, nan, aw, nan, false)
	} else if st.Direction == Row {
		pad := st.Padding
		innerW := sub(width, pad.Left+pad.Right)
		availW := innerW
		if isNaN(availW) {
			availW = n.contentAvailW(sub(aw, pad.Left+pad.Right))
		}
		if st.ScrollX {
			innerW, availW = nan, nan
		}
		items := make([]*item, 0, len(n.Children))
		for _, c := range n.Children {
			if c.Style.Hidden || c.Style.Absolute {
				continue
			}
			items = append(items, n.prepareItem(gen, c, true, innerW, nan, availW, nan))
		}
		if !isNaN(innerW) {
			resolveFlex(items, innerW-st.Gap*float32(max(len(items)-1, 0)))
		} else {
			for _, it := range items {
				it.main = it.hyp
			}
		}
		// Wrapping depends on the width left after sibling flex sizes and gaps.
		// Keep the recursive minimum so vertical scroll areas can still shrink.
		for _, it := range items {
			ch := it.n.minContentHeight(gen, it.main, it.main) + it.mCross0 + it.mCross1
			h = max(h, ch)
		}
		h = clamp(h+pad.Top+pad.Bottom, nan, maxH)
	} else {
		pad := st.Padding
		ph := pad.Left + pad.Right
		innerW := sub(width, ph)
		availW := innerW
		if isNaN(availW) {
			availW = n.contentAvailW(sub(aw, ph))
		}
		count := 0
		for _, c := range n.Children {
			cs := &c.Style
			if cs.Hidden || cs.Absolute {
				continue
			}
			mh := cs.Margin.Left + cs.Margin.Right
			cw := resolve(cs.Width, innerW)
			if isNaN(cw) && st.Direction == Column && !isNaN(innerW) {
				align := cs.AlignSelf
				if align == AlignAuto {
					align = st.AlignItems
				}
				if align == AlignStretch {
					cw = max(innerW-mh, 0)
				}
			}
			if !isNaN(cw) {
				cw = c.clampW(cw, innerW)
			}
			ch := c.minContentHeight(gen, cw, sub(availW, mh)) + cs.Margin.Top + cs.Margin.Bottom
			if st.Direction == Column {
				h += ch
			} else {
				h = max(h, ch)
			}
			count++
		}
		if st.Direction == Column && count > 1 {
			h += st.Gap * float32(count-1)
		}
		h = clamp(h+pad.Top+pad.Bottom, nan, maxH)
	}
	n.mcGen, n.mcW, n.mcAW, n.mcH = gen, width, aw, h
	return h
}
