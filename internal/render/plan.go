package render

import "math"

// The layer planning, target pool and text gamma helpers here are shared by
// every backend.

// backdropRegion sets the bounds of backdrop l to what it copies: the part of
// target its composite prim p shows in, with room for the blur around it. It
// returns the box p is drawn over, and false when none of p shows.
func backdropRegion(l *Layer, p *Prim, target [4]float32) ([4]float32, bool) {
	box, shown := p.Bounds, p.Bounds
	if p.Flags&FlagXform != 0 {
		box = xformBounds(p.Bounds, p.Xform, p.Xlate)
		shown = xformBounds(intersect(p.Bounds, p.Clip2), p.Xform, p.Xlate)
	}
	shown = intersect(shown, p.Clip)
	if shown[2] <= 0 || shown[3] <= 0 {
		return box, false
	}
	m := float32(math.Ceil(float64(3 * l.Sigma)))
	b := wholeRect(intersect([4]float32{shown[0] - m, shown[1] - m, shown[2] + 2*m, shown[3] + 2*m}, target))
	b[2], b[3] = min(b[2], MaxLayerSize), min(b[3], MaxLayerSize)
	l.Bounds = b
	return box, b[2] >= 1 && b[3] >= 1
}

// blurPrim is a pass that fills a w x h target with the source region
// (0, 0, uw, uh) of src, blurred along one axis by sigma texels.
func blurPrim(w, h int, uw, uh float32, src *Texture, sigma float32, horizontal bool) Prim {
	size := src.Height
	if horizontal {
		size = src.Width
	}
	// Taps reach 3 sigma, at most 64 to a side; wider blurs space them out.
	step := max(float32(math.Ceil(float64(3*sigma/64))), 1)
	d := step / float32(size)
	// Taps stay on the region's texels, so its edge pixels repeat instead of
	// fading into the cleared texels past it.
	hx, hy := 0.5/float32(src.Width), 0.5/float32(src.Height)
	p := Prim{
		Bounds: [4]float32{0, 0, float32(w), float32(h)},
		Clip:   NoClip,
		Color:  [4]float32{1, 1, 1, 1},
		Color2: [4]float32{hx, hy, uw - hx, uh - hy},
		UV:     [4]float32{0, 0, uw, uh},
		Params: [4]float32{0, d, sigma / step},
		Kind:   KindImage,
		Flags:  FlagPremultiplied | FlagBlur,
	}
	if horizontal {
		p.Params[0], p.Params[1] = d, 0
	}
	return p
}

// targetPool keeps offscreen layer targets between frames.
type targetPool struct {
	free, used []*Target
}

const poolKeep = 12

// targetSize is the size of the pooled target for a w x h layer.
func targetSize(w, h int) (int, int) { return (w + 63) / 64 * 64, (h + 63) / 64 * 64 }

func (p *targetPool) acquire(d *Device, w, h int) (*Target, error) {
	w, h = targetSize(w, h)
	for i, t := range p.free {
		if t.Width == w && t.Height == h {
			p.free = append(p.free[:i], p.free[i+1:]...)
			p.used = append(p.used, t)
			return t, nil
		}
	}
	t, err := NewTarget(d, w, h)
	if err != nil {
		return nil, err
	}
	p.used = append(p.used, t)
	return t, nil
}

// endFrame returns the targets in use to the pool, keeping the most recent:
// at least those this frame used, so a frame with many backdrops does not
// create them all again the next.
func (p *targetPool) endFrame() {
	keep := max(poolKeep, len(p.used))
	p.free = append(p.free, p.used...)
	clear(p.used)
	p.used = p.used[:0]
	if n := len(p.free) - keep; n > 0 {
		for _, t := range p.free[:n] {
			t.Release()
		}
		p.free = append(p.free[:0], p.free[n:]...)
	}
}

func (p *targetPool) release() {
	p.endFrame()
	for _, t := range p.free {
		t.Release()
	}
	p.free = nil
}

func textureBytes(w, h int, mips bool) int64 {
	var n int64
	for w > 0 && h > 0 {
		n += int64(w) * int64(h) * 4
		if !mips || w == 1 && h == 1 {
			break
		}
		w, h = max(1, w/2), max(1, h/2)
	}
	return n
}

// gammaRatios ports DirectWrite's gamma correction table as used by Windows Terminal and GPUI.
func gammaRatios(gamma float32) [4]float32 {
	table := [13][4]float32{
		{0.0000 / 4, 0.0000 / 4, 0.0000 / 4, 0.0000 / 4},
		{0.0166 / 4, -0.0807 / 4, 0.2227 / 4, -0.0751 / 4},
		{0.0350 / 4, -0.1760 / 4, 0.4325 / 4, -0.1370 / 4},
		{0.0543 / 4, -0.2821 / 4, 0.6302 / 4, -0.1876 / 4},
		{0.0739 / 4, -0.3963 / 4, 0.8167 / 4, -0.2287 / 4},
		{0.0933 / 4, -0.5161 / 4, 0.9926 / 4, -0.2616 / 4},
		{0.1121 / 4, -0.6395 / 4, 1.1588 / 4, -0.2877 / 4},
		{0.1300 / 4, -0.7649 / 4, 1.3159 / 4, -0.3080 / 4},
		{0.1469 / 4, -0.8911 / 4, 1.4644 / 4, -0.3234 / 4},
		{0.1627 / 4, -1.0170 / 4, 1.6051 / 4, -0.3347 / 4},
		{0.1773 / 4, -1.1420 / 4, 1.7385 / 4, -0.3426 / 4},
		{0.1908 / 4, -1.2652 / 4, 1.8650 / 4, -0.3476 / 4},
		{0.2031 / 4, -1.3864 / 4, 1.9851 / 4, -0.3501 / 4},
	}
	const norm13 = float32(float64(0x10000) / (255.0 * 255.0) * 4.0)
	const norm24 = float32(float64(0x100) / 255.0 * 4.0)
	idx := int(math.Round(float64(gamma*10))) - 10
	idx = min(max(idx, 0), 12)
	t := table[idx]
	return [4]float32{t[0] * norm13, t[1] * norm24, t[2] * norm13, t[3] * norm24}
}
