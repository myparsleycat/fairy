//go:build windows

package render

import (
	"bytes"
	"log"
	"slices"
)

// DamageDebug is a diagnostic mode of damage tracking.
type DamageDebug int

const (
	DamageDebugOff DamageDebug = iota
	// DamageDebugShow tints the part of each frame that was redrawn.
	DamageDebugShow
	// DamageDebugVerify draws every frame whole into a reference target as
	// well, and logs when a swap chain buffer, as it comes back for drawing,
	// does not hold the frame presented from it. Present1 is documented to
	// copy the pixels outside the dirty rects from the previous buffer; this
	// checks that it does.
	DamageDebugVerify
)

// damageCheck is the state of DamageDebugVerify.
type damageCheck struct {
	refs    [2]*Target
	frames  int  // frames drawn
	good    int  // frames presented in a row since anything made buffers stale
	matched int  // buffers found as expected
	logged  bool // a mismatch was logged
}

// SetDamageDebug sets a diagnostic mode of damage tracking.
func (r *Renderer) SetDamageDebug(m DamageDebug) {
	r.debugDamage = m
	r.dmg.invalidate(DamageInvalidate)
}

// showRegion adds the rects tinted in the frame before to this frame's
// region, so the tint is drawn over, and returns the rects to tint. top lists
// the layers drawn straight into the target.
func (r *Renderer) showRegion(top []int) rectSet {
	var tint rectSet
	if !r.region.full {
		tint = slices.Clone(r.region.rects)
		r.region = r.dmg.grow(r.shownTint, top)
	}
	r.shownTint = tint
	return tint
}

func tintPrim(g rectI) Prim {
	return Prim{
		Bounds: [4]float32{float32(g.x0), float32(g.y0), float32(g.x1 - g.x0), float32(g.y1 - g.y0)},
		Clip:   NoClip,
		Color:  [4]float32{0.25, 0, 0.25, 0.25},
		Kind:   KindRect,
	}
}

// checkBuffer compares the swap chain buffer about to be drawn with the
// reference drawn for the frame last presented from it, two frames ago.
func (r *Renderer) checkBuffer() {
	c := &r.check
	if c.good < 2 || c.logged {
		return
	}
	ref := c.refs[c.frames%2]
	if ref == nil || ref.Width != r.width || ref.Height != r.height {
		return
	}
	buf, err := r.swap.buffer()
	if err != nil {
		return
	}
	got, _, err := r.ctx.ReadPixels(buf)
	buf.Release()
	if err != nil {
		return
	}
	want, _, err := r.ctx.ReadPixels(ref.tex)
	if err != nil {
		return
	}
	if bytes.Equal(got, want) {
		c.matched++
		if c.matched == 1 || c.matched%100 == 0 {
			log.Printf("fairy: damage verify: %d buffers held the frame presented from them", c.matched)
		}
		return
	}
	n, first := 0, -1
	for i := range got {
		if got[i] != want[i] {
			n++
			if first < 0 {
				first = i
			}
		}
	}
	px := first / 4
	log.Printf("fairy: damage verify: frame %d: back buffer differs from the frame presented from it in %d bytes, first at (%d, %d)",
		c.frames, n, px%r.width, px/r.width)
	c.logged = true
}

// drawReference draws s whole into the reference target for this frame.
func (r *Renderer) drawReference(s *Scene, clear [4]float32) {
	c := &r.check
	i := c.frames % 2
	if t := c.refs[i]; t == nil || t.Width != r.width || t.Height != r.height {
		t.Release()
		t, err := NewTargetFormat(r.ds, r.width, r.height, backBufferFormat)
		if err != nil {
			c.refs[i] = nil
			return
		}
		c.refs[i] = t
	}
	rtv, target, on, timer, snapshot, region, stats := r.rtv, r.target, r.damageOn, r.timer, r.snapshot, r.region, r.stats
	r.rtv, r.target, r.damageOn, r.timer, r.snapshot = c.refs[i].rtv, c.refs[i], false, nil, nil
	err := r.draw(s, clear)
	r.rtv, r.target, r.damageOn, r.timer, r.snapshot, r.region, r.stats = rtv, target, on, timer, snapshot, region, stats
	if err != nil {
		c.refs[i].Release()
		c.refs[i] = nil
	}
}

func (c *damageCheck) release() {
	for i := range c.refs {
		c.refs[i].Release()
		c.refs[i] = nil
	}
}
