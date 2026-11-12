package render

import "math"

// MaxLayerSize bounds the side of an offscreen layer in device pixels.
const MaxLayerSize = 4096

// backdropBudget bounds the area all backdrop blurs of a frame copy, in
// multiples of the target's area; backdrops past it are left out.
const backdropBudget = 4

// Layer is a range of prims drawn into an offscreen texture that the image
// prim at End then draws, or with Backdrop, a blur of what was drawn before
// Begin that the prim at End draws.
type Layer struct {
	Begin, End int
	// Bounds is in device pixels, whole. A backdrop's is worked out when the
	// frame is drawn, from where its composite prim ends up.
	Bounds   [4]float32
	Sigma    float32 // gaussian blur in device pixels
	Backdrop bool
}

// BeginLayer starts a layer of the prims added from now on, and returns the
// mark that EndLayer takes.
func (s *Scene) BeginLayer() int { return len(s.Prims) }

// EndLayer ends the layer begun at mark: its prims are drawn offscreen,
// blurred by sigma device pixels, and composited by one image prim with
// p's Clip, Radii and opacity (Color[3]). A layer that draws nothing is
// dropped with its prims.
func (s *Scene) EndLayer(mark int, p Prim, sigma float32) {
	b, ok := primsBounds(s.Prims[mark:])
	if ok && sigma > 0 {
		m := float32(math.Ceil(float64(3 * sigma)))
		b = [4]float32{b[0] - m, b[1] - m, b[2] + 2*m, b[3] + 2*m}
	}
	b[2], b[3] = min(b[2], MaxLayerSize), min(b[3], MaxLayerSize)
	if !ok || b[2] < 1 || b[3] < 1 {
		s.truncate(mark)
		return
	}
	s.Layers = append(s.Layers, Layer{Begin: mark, End: len(s.Prims), Bounds: b, Sigma: sigma})
	s.addComposite(p, b)
}

// BeginBackdrop returns the mark that EndBackdrop takes. The backdrop blurs
// what was drawn before the mark, so prims added between the two, such as
// the element's own outer shadows, stay out of it.
func (s *Scene) BeginBackdrop() int { return len(s.Prims) }

// EndBackdrop draws a blur, by sigma device pixels, of what was drawn before
// mark under p.Bounds, rounded by p.Radii, at opacity p.Color[3]. The blurred
// region follows a FlagXform given to the composite prim afterwards, exactly
// for scaling and translation and by its bounding box for rotation.
func (s *Scene) EndBackdrop(mark int, p Prim, sigma float32) {
	if sigma <= 0 || p.Bounds[2] <= 0 || p.Bounds[3] <= 0 {
		return
	}
	s.Layers = append(s.Layers, Layer{Begin: mark, End: len(s.Prims), Sigma: sigma, Backdrop: true})
	s.addComposite(p, p.Bounds)
}

// AddBackdrop is EndBackdrop with nothing between the copy and the composite.
func (s *Scene) AddBackdrop(p Prim, sigma float32) { s.EndBackdrop(len(s.Prims), p, sigma) }

func (s *Scene) addComposite(p Prim, bounds [4]float32) {
	p.Bounds = bounds
	p.Kind = KindImage
	p.Flags = p.Flags&^(FlagGradient|FlagBlur|FlagXform) | FlagPremultiplied | s.Clip<<ClipShift
	p.Color = [4]float32{1, 1, 1, p.Color[3]}
	s.images = append(s.images, imageRef{index: len(s.Prims), layer: len(s.Layers)})
	s.Prims = append(s.Prims, p)
}

// truncate drops the prims from mark on, with their images and layers.
func (s *Scene) truncate(mark int) {
	s.Prims = s.Prims[:mark]
	for len(s.images) > 0 && s.images[len(s.images)-1].index >= mark {
		s.images = s.images[:len(s.images)-1]
	}
	for len(s.Layers) > 0 && s.Layers[len(s.Layers)-1].End >= mark {
		s.Layers = s.Layers[:len(s.Layers)-1]
	}
}

// primsBounds returns the whole-pixel box the prims can draw in.
func primsBounds(ps []Prim) ([4]float32, bool) {
	x0, y0 := float32(math.Inf(1)), float32(math.Inf(1))
	x1, y1 := float32(math.Inf(-1)), float32(math.Inf(-1))
	for i := range ps {
		p := &ps[i]
		b := p.Bounds
		if p.Kind == KindShadow {
			m := 3 * p.Params[0]
			if p.Flags&FlagInset != 0 {
				b, m = p.UV, 0
			}
			b = [4]float32{b[0] - m, b[1] - m, b[2] + 2*m, b[3] + 2*m}
		}
		if p.Kind == KindCallout {
			e := p.Params[2]*math.Sqrt2 + 0.5
			lx, ly := min(b[0], p.Params[0]-e), min(b[1], p.Params[1]-e)
			hx, hy := max(b[0]+b[2], p.Params[0]+e), max(b[1]+b[3], p.Params[1]+e)
			b = [4]float32{lx, ly, hx - lx, hy - ly}
		}
		if p.Flags&FlagXform != 0 {
			b = intersect(b, p.Clip2)
			b = xformBounds(b, p.Xform, p.Xlate)
		}
		b = intersect(b, p.Clip)
		if b[2] <= 0 || b[3] <= 0 {
			continue
		}
		x0, y0 = min(x0, b[0]), min(y0, b[1])
		x1, y1 = max(x1, b[0]+b[2]), max(y1, b[1]+b[3])
	}
	if x1 <= x0 || y1 <= y0 {
		return [4]float32{}, false
	}
	return wholeRect([4]float32{x0, y0, x1 - x0, y1 - y0}), true
}

func wholeRect(b [4]float32) [4]float32 {
	x0, y0 := float32(math.Floor(float64(b[0]))), float32(math.Floor(float64(b[1])))
	x1, y1 := float32(math.Ceil(float64(b[0]+b[2]))), float32(math.Ceil(float64(b[1]+b[3])))
	return [4]float32{x0, y0, x1 - x0, y1 - y0}
}

func intersect(a, b [4]float32) [4]float32 {
	x0, y0 := max(a[0], b[0]), max(a[1], b[1])
	x1, y1 := min(a[0]+a[2], b[0]+b[2]), min(a[1]+a[3], b[1]+b[3])
	return [4]float32{x0, y0, max(x1-x0, 0), max(y1-y0, 0)}
}

func xformBounds(b [4]float32, m [4]float32, t [2]float32) [4]float32 {
	x0, y0 := float32(math.Inf(1)), float32(math.Inf(1))
	x1, y1 := float32(math.Inf(-1)), float32(math.Inf(-1))
	for _, c := range [4][2]float32{{b[0], b[1]}, {b[0] + b[2], b[1]}, {b[0], b[1] + b[3]}, {b[0] + b[2], b[1] + b[3]}} {
		x := m[0]*c[0] + m[2]*c[1] + t[0]
		y := m[1]*c[0] + m[3]*c[1] + t[1]
		x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
	}
	return [4]float32{x0, y0, x1 - x0, y1 - y0}
}
