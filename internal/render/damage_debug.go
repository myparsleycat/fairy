//go:build windows

package render

import (
	"bytes"
	"log"
	"slices"
)

// damageCheck is the state of DamageDebugVerify.
type damageCheck struct {
	ref     *Target // the frame drawn whole
	frames  int     // frames drawn in part and checked
	matched int     // frames that matched their full redraw
	logged  bool    // a mismatch was logged
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

// verifyFrame draws s whole into the reference target and compares it with
// the frame just drawn in part. Call it outside draw: the reference draw and
// the readback each take the device lock.
func (r *Renderer) verifyFrame(s *Scene, clear [4]float32) {
	c := &r.check
	frame := r.target
	if frame == nil {
		frame = r.retained
	}
	if frame == nil || c.logged {
		return
	}
	c.frames++
	if !r.drawReference(s, clear, frame.Format) {
		return
	}
	var got, want []byte
	var err error
	r.withDevice(func() {
		if got, _, err = r.ctx.ReadPixels(frame.tex); err != nil {
			return
		}
		want, _, err = r.ctx.ReadPixels(c.ref.tex)
	})
	if err != nil {
		return
	}
	if bytes.Equal(got, want) {
		c.matched++
		if c.matched == 1 || c.matched%100 == 0 {
			log.Printf("fairy: damage verify: %d frames drawn in part matched a full redraw", c.matched)
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
	log.Printf("fairy: damage verify: frame %d: drawn in part, it differs from a full redraw in %d bytes, first at (%d, %d)",
		c.frames, n, px%r.width, px/r.width)
	c.logged = true
}

// drawReference draws s whole into the reference target and reports whether
// it did.
func (r *Renderer) drawReference(s *Scene, clear [4]float32, format uint32) bool {
	c := &r.check
	if t := c.ref; t == nil || t.Width != r.width || t.Height != r.height || t.Format != format {
		t.Release()
		t, err := NewTargetFormat(r.ds, r.width, r.height, format)
		if err != nil {
			c.ref = nil
			return false
		}
		c.ref = t
	}
	rtv, target, on, timer, snapshot, region, stats := r.rtv, r.target, r.damageOn, r.timer, r.snapshot, r.region, r.stats
	r.target, r.damageOn, r.timer, r.snapshot = c.ref, false, nil, nil
	err := r.draw(s, clear)
	r.rtv, r.target, r.damageOn, r.timer, r.snapshot, r.region, r.stats = rtv, target, on, timer, snapshot, region, stats
	if err != nil {
		c.ref.Release()
		c.ref = nil
		return false
	}
	return true
}

func (c *damageCheck) release() {
	c.ref.Release()
	c.ref = nil
}
