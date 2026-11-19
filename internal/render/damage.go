package render

import "unsafe"

// damageFullRatio is the share of the target above which a frame is drawn
// whole instead of in part.
const damageFullRatio = 0.5

// Damage reasons reported in Stats.DamageReason when a frame is drawn whole.
const (
	DamageFirst      = "first"
	DamageResize     = "resize"
	DamageClear      = "clear"
	DamageAtlas      = "atlas"
	DamageInvalidate = "invalidate"
	DamageLarge      = "large"
	DamageOff        = "off"
)

// imageKind says what an image prim samples.
type imageKind uint8

const (
	imageNone     imageKind = iota // no texture: the empty image
	imageTexture                   // a texture whose pixels the stamp identifies
	imageVolatile                  // a texture the app writes without telling
	imageLayer                     // a layer's output, compared through the layer
)

type imageKey struct {
	tex   *Texture
	stamp uint64
	kind  imageKind
}

// frameRecord is what a frame drew, kept to find what the next one changes.
type frameRecord struct {
	w, h     int
	clear    [4]float32
	atlasGen int
	prims    []Prim
	clips    []ClipRec
	layers   []Layer
	skip     []bool     // per layer: a backdrop left out by the budget
	images   []imageKey // per prim
}

func (f *frameRecord) capture(s *Scene, plan []layerPlan, w, h int, bg [4]float32, atlasGen int) {
	f.w, f.h, f.clear, f.atlasGen = w, h, bg, atlasGen
	f.prims = append(f.prims[:0], s.Prims...)
	f.clips = append(f.clips[:0], s.Clips...)
	f.layers = append(f.layers[:0], s.Layers...)
	f.skip = f.skip[:0]
	for i := range s.Layers {
		f.skip = append(f.skip, i < len(plan) && plan[i].skip)
	}
	if cap(f.images) < len(s.Prims) {
		f.images = make([]imageKey, len(s.Prims))
	}
	f.images = f.images[:len(s.Prims)]
	clear(f.images)
	for _, ref := range s.images {
		k := imageKey{}
		switch {
		case ref.layer > 0:
			k.kind = imageLayer
		case ref.tex != nil && ref.tex.Volatile():
			k.kind = imageVolatile
		case ref.tex != nil:
			k = imageKey{tex: ref.tex, stamp: ref.tex.Stamp(), kind: imageTexture}
		}
		f.images[ref.index] = k
	}
}

// rectI is a pixel rectangle: x0, y0 inclusive, x1, y1 exclusive.
type rectI struct{ x0, y0, x1, y1 int }

func (a rectI) empty() bool { return a.x1 <= a.x0 || a.y1 <= a.y0 }

func (a rectI) area() int {
	if a.empty() {
		return 0
	}
	return (a.x1 - a.x0) * (a.y1 - a.y0)
}

func (a rectI) union(b rectI) rectI {
	if a.empty() {
		return b
	}
	if b.empty() {
		return a
	}
	return rectI{min(a.x0, b.x0), min(a.y0, b.y0), max(a.x1, b.x1), max(a.y1, b.y1)}
}

func (a rectI) overlaps(b rectI) bool {
	return !a.empty() && !b.empty() && a.x0 < b.x1 && b.x0 < a.x1 && a.y0 < b.y1 && b.y0 < a.y1
}

// toRectI turns a whole-pixel float box into a rectI, grown by pad and
// clamped to a w x h target.
func toRectI(b [4]float32, pad, w, h int) rectI {
	r := rectI{
		x0: int(max(b[0], -1)) - pad,
		y0: int(max(b[1], -1)) - pad,
		x1: int(min(b[0]+b[2], float32(w)+1)) + pad,
		y1: int(min(b[1]+b[3], float32(h)+1)) + pad,
	}
	r.x0, r.y0 = max(r.x0, 0), max(r.y0, 0)
	r.x1, r.y1 = min(r.x1, w), min(r.y1, h)
	return r
}

// primExtent is the pixels p can change, with a pixel to spare for rounding.
func primExtent(p *Prim, w, h int) rectI {
	b, ok := primsBounds(unsafe.Slice(p, 1))
	if !ok {
		return rectI{}
	}
	return toRectI(b, 1, w, h)
}

// maxDamageRects bounds the rects a frame is drawn in; each is a pass over
// the scene.
const maxDamageRects = 4

// rectSet is a few pixel rects that do not overlap.
type rectSet []rectI

// add adds g, merging it with the rects it overlaps or nearly touches, and
// merges the closest rects while there are more than maxDamageRects.
func (s *rectSet) add(g rectI) {
	if g.empty() {
		return
	}
	for merged := true; merged; {
		merged = false
		for i, a := range *s {
			if a.overlaps(g) || cheapUnion(a, g) {
				g = a.union(g)
				*s = append((*s)[:i], (*s)[i+1:]...)
				merged = true
				break
			}
		}
	}
	*s = append(*s, g)
	if len(*s) <= maxDamageRects {
		return
	}
	bi, bj, best := 0, 1, -1
	for i := range *s {
		for j := i + 1; j < len(*s); j++ {
			a, b := (*s)[i], (*s)[j]
			if grow := a.union(b).area() - a.area() - b.area(); best < 0 || grow < best {
				bi, bj, best = i, j, grow
			}
		}
	}
	u := (*s)[bi].union((*s)[bj])
	*s = append((*s)[:bj], (*s)[bj+1:]...)
	*s = append((*s)[:bi], (*s)[bi+1:]...)
	s.add(u)
}

// cheapUnion reports whether drawing a and b as one rect costs little more
// than drawing them apart, as for neighbors in a list.
func cheapUnion(a, b rectI) bool {
	return 2*a.union(b).area() <= 3*(a.area()+b.area())
}

func (s rectSet) overlaps(g rectI) bool {
	for _, a := range s {
		if a.overlaps(g) {
			return true
		}
	}
	return false
}

// covers reports whether one rect of s holds all of g.
func (s rectSet) covers(g rectI) bool {
	for _, a := range s {
		if a.x0 <= g.x0 && a.y0 <= g.y0 && g.x1 <= a.x1 && g.y1 <= a.y1 {
			return true
		}
	}
	return false
}

func (s rectSet) area() int {
	n := 0
	for _, a := range s {
		n += a.area()
	}
	return n
}

func (s rectSet) bounds() rectI {
	var b rectI
	for _, a := range s {
		b = b.union(a)
	}
	return b
}

// damageResult is the part of the target a frame draws.
type damageResult struct {
	full   bool
	reason string
	rects  rectSet // when not full; none when nothing changed
}

// damageTracker finds the pixels a frame changes from the frame before.
type damageTracker struct {
	prev, cur frameRecord
	havePrev  bool
	reason    string // why the next frame is drawn whole, when havePrev is false

	oldChanged, newChanged []bool
	oldLayerAt, newLayerAt map[int]int // composite prim index to layer index
	oldLayerSeen           []bool
	match                  []int // per new layer: the old layer it is compared with, or -1
	clipMemo               map[uint64]bool
	compExt                []rectI // per layer of the current frame
	set                    rectSet // damage of the frame being compared
}

func (d *damageTracker) invalidate(reason string) {
	d.havePrev = false
	d.reason = reason
}

// commit makes the frame last computed the one the next is compared with.
func (d *damageTracker) commit() {
	d.prev, d.cur = d.cur, d.prev
	d.havePrev = true
	d.reason = ""
}

// compute records the frame in d.cur and returns what it changes. top lists
// the layers drawn straight into the target, as planLayers returns them.
func (d *damageTracker) compute(s *Scene, plan []layerPlan, top []int, w, h int, clear [4]float32, atlasGen int) damageResult {
	d.cur.capture(s, plan, w, h, clear, atlasGen)
	cur := &d.cur
	d.compExt = d.compExt[:0]
	for i := range cur.layers {
		d.compExt = append(d.compExt, primExtent(&cur.prims[cur.layers[i].End], w, h))
	}
	switch {
	case !d.havePrev:
		reason := d.reason
		if reason == "" {
			reason = DamageFirst
		}
		return damageResult{full: true, reason: reason}
	case d.prev.w != w || d.prev.h != h:
		return damageResult{full: true, reason: DamageResize}
	case d.prev.clear != clear:
		return damageResult{full: true, reason: DamageClear}
	case d.prev.atlasGen != atlasGen:
		return damageResult{full: true, reason: DamageAtlas}
	}
	d.set = d.set[:0]
	d.diff()
	return d.settle(top)
}

// grow adds rects to the damage of the frame last computed, which compute
// returned in part, and returns what the frame then draws.
func (d *damageTracker) grow(rects rectSet, top []int) damageResult {
	for _, g := range rects {
		d.set.add(g)
	}
	return d.settle(top)
}

// settle applies the backdrop rule to d.set and draws the frame whole when
// that leaves too much of the target.
func (d *damageTracker) settle(top []int) damageResult {
	d.backdrops(top)
	if d.set.area() > int(damageFullRatio*float64(d.cur.w*d.cur.h)) {
		return damageResult{full: true, reason: DamageLarge}
	}
	return damageResult{rects: d.set}
}

// diff compares d.prev with d.cur prim by prim and layer by layer, adding
// what changed to d.set.
func (d *damageTracker) diff() {
	old, cur := &d.prev, &d.cur
	no, nn := len(old.prims), len(cur.prims)
	d.oldChanged = resetBools(d.oldChanged, no)
	d.newChanged = resetBools(d.newChanged, nn)
	if d.clipMemo == nil {
		d.clipMemo = map[uint64]bool{}
	}
	clear(d.clipMemo)

	p := 0
	for p < min(no, nn) && d.samePrim(p, p) {
		p++
	}
	m := 0
	for m < min(no, nn)-p && d.samePrim(no-1-m, nn-1-m) {
		m++
	}
	// Indices old[p:no-m] and new[p:nn-m] differ somewhere. Of the same
	// length, they are compared index by index, as when a prim changes color;
	// otherwise all of them count as changed.
	sameLen := no == nn
	for i := p; i < max(no, nn)-m; i++ {
		if sameLen && d.samePrim(i, i) {
			continue
		}
		if i < no-m {
			d.oldChanged[i] = true
			d.set.add(primExtent(&old.prims[i], cur.w, cur.h))
		}
		if i < nn-m {
			d.newChanged[i] = true
			d.set.add(primExtent(&cur.prims[i], cur.w, cur.h))
		}
	}
	// newToOld maps an index of the new frame to the old index it was
	// compared with, or -1 inside a middle of another length.
	newToOld := func(i int) int {
		switch {
		case i < p:
			return i
		case i >= nn-m:
			return i - nn + no
		case sameLen:
			return i
		}
		return -1
	}
	d.diffLayers(newToOld)
}

// diffLayers damages the composites of layers whose content, range or
// settings changed, and marks their composite prims changed so the layers
// around them change too. A layer is matched to the old one whose composite
// its own composite was compared with.
func (d *damageTracker) diffLayers(newToOld func(int) int) {
	old, cur := &d.prev, &d.cur
	d.oldLayerAt = resetMap(d.oldLayerAt)
	d.newLayerAt = resetMap(d.newLayerAt)
	for i := range old.layers {
		d.oldLayerAt[old.layers[i].End] = i
	}
	for i := range cur.layers {
		d.newLayerAt[cur.layers[i].End] = i
	}
	d.oldLayerSeen = resetBools(d.oldLayerSeen, len(old.layers))
	d.match = append(d.match[:0], make([]int, len(cur.layers))...)
	match := d.match
	for i := range cur.layers {
		match[i] = -1
		if o := newToOld(cur.layers[i].End); o >= 0 {
			if j, ok := d.oldLayerAt[o]; ok {
				match[i] = j
				d.oldLayerSeen[j] = true
			}
		}
	}
	// Old layers with no new one are gone; their composites mark the old
	// layers around them before those are compared.
	for j := range old.layers {
		if !d.oldLayerSeen[j] {
			e := old.layers[j].End
			d.oldChanged[e] = true
			d.set.add(primExtent(&old.prims[e], cur.w, cur.h))
		}
	}
	// Layers end in order, inner ones first, so a changed inner composite is
	// marked before the layer around it is compared.
	for i := range cur.layers {
		l := &cur.layers[i]
		j := match[i]
		changed := j < 0 || anyTrue(d.newChanged[l.Begin:l.End+1])
		if !changed {
			ol := &old.layers[j]
			changed = newToOld(l.Begin) != ol.Begin || l.Sigma != ol.Sigma || l.Backdrop != ol.Backdrop ||
				l.Bounds != ol.Bounds || cur.skip[i] != old.skip[j] || anyTrue(d.oldChanged[ol.Begin:ol.End+1])
		}
		if !changed {
			continue
		}
		d.newChanged[l.End] = true
		d.set.add(d.compExt[i])
		if j >= 0 {
			d.oldChanged[old.layers[j].End] = true
			d.set.add(primExtent(&old.prims[old.layers[j].End], cur.w, cur.h))
		}
	}
}

// backdrops grows d.set until no backdrop drawn straight into the target
// reads pixels both inside and outside it: such a backdrop copies what the
// target holds under its region before its composite, and outside the damage
// that is the previous frame, already holding later prims and its own
// composite. The region and composite join one rect, so the pass drawing the
// composite also draws everything the backdrop copies.
func (d *damageTracker) backdrops(top []int) {
	cur := &d.cur
	for {
		grown := false
		for _, k := range top {
			l := &cur.layers[k]
			if !l.Backdrop || cur.skip[k] {
				continue
			}
			region := toRectI(l.Bounds, 0, cur.w, cur.h)
			if !d.set.overlaps(region) {
				continue
			}
			g := region.union(d.compExt[k])
			if !d.set.covers(g) {
				d.set.add(g)
				grown = true
			}
		}
		if !grown {
			return
		}
	}
}

// samePrim reports whether old prim o and new prim n draw the same pixels.
func (d *damageTracker) samePrim(o, n int) bool {
	a, b := d.prev.prims[o], d.cur.prims[n]
	ca, cb := a.Flags>>ClipShift, b.Flags>>ClipShift
	a.Flags &= 1<<ClipShift - 1
	b.Flags &= 1<<ClipShift - 1
	if primBytes(&a) != primBytes(&b) {
		return false
	}
	ia, ib := d.prev.images[o], d.cur.images[n]
	if ia != ib || ia.kind == imageVolatile {
		return false
	}
	return d.sameClip(ca, cb)
}

// sameClip compares two clip chains, 1-based indices into the old and new
// clips, by content rather than by index.
// A clip's parent always has a lower index, so the recursion ends.
func (d *damageTracker) sameClip(a, b uint32) bool {
	if a == 0 || b == 0 {
		return a == b
	}
	key := uint64(a)<<32 | uint64(b)
	if same, ok := d.clipMemo[key]; ok {
		return same
	}
	ra, rb := d.prev.clips[a-1], d.cur.clips[b-1]
	pa, pb := ra.Parent, rb.Parent
	ra.Parent, rb.Parent = 0, 0
	same := clipBytes(&ra) == clipBytes(&rb) && d.sameClip(pa, pb)
	d.clipMemo[key] = same
	return same
}

func primBytes(p *Prim) [unsafe.Sizeof(Prim{})]byte {
	return *(*[unsafe.Sizeof(Prim{})]byte)(unsafe.Pointer(p))
}

func clipBytes(c *ClipRec) [unsafe.Sizeof(ClipRec{})]byte {
	return *(*[unsafe.Sizeof(ClipRec{})]byte)(unsafe.Pointer(c))
}

func anyTrue(bs []bool) bool {
	for _, b := range bs {
		if b {
			return true
		}
	}
	return false
}

func resetBools(bs []bool, n int) []bool {
	if cap(bs) < n {
		return make([]bool, n)
	}
	bs = bs[:n]
	clear(bs)
	return bs
}

func resetMap(m map[int]int) map[int]int {
	if m == nil {
		return map[int]int{}
	}
	clear(m)
	return m
}
