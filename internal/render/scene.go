package render

// Kinds and flags mirror shaders.hlsl.
const (
	KindRect    = 0
	KindShadow  = 1
	KindGlyph   = 2
	KindImage   = 3
	KindCallout = 4 // Params: diamond center (x, y), half side, corner radius
	// KindArc strokes a circular arc centered in Bounds. Params: start angle
	// (radians, 0 at the top, clockwise), sweep, stroke width, 1 for round caps.
	KindArc = 5
	// KindWave draws a wavy line across Bounds. Params: wavelength, line width.
	KindWave = 6

	FlagGradient      = 1
	FlagInset         = 2
	FlagRawAlpha      = 4 // glyph prims: use atlas coverage as is, without text gamma
	FlagPremultiplied = 8 // image prims: the texture holds premultiplied alpha
	// FlagXform maps the prim through Xform and Xlate, and clips it to Clip2
	// before that map as well as to Clip after it.
	FlagXform = 16
	// FlagBlur makes an image prim a gaussian blur pass: Params.xy is the UV
	// step between taps and Params.z the sigma in taps. Taps are clamped to the
	// UV rect Color2 (u0, v0, u1, v1).
	FlagBlur = 32
	// FlagDashed draws a rect prim's border as CSS dashes.
	FlagDashed = 64
	// FlagPaddingBox keeps a rect prim's fill inside its border, so the border
	// covers what is behind the prim instead of the fill.
	FlagPaddingBox = 128

	// ClipShift is where Flags holds the prim's clip: 1 + an index into
	// Scene.Clips, or 0 for none.
	ClipShift = 16
)

// NoClip is a clip rectangle that clips nothing.
var NoClip = [4]float32{-1e9, -1e9, 2e9, 2e9}

// Prim is one instanced primitive. Its layout matches the HLSL Prim struct.
type Prim struct {
	Bounds      [4]float32
	Clip        [4]float32
	Color       [4]float32
	Color2      [4]float32
	BorderColor [4]float32
	Radii       [4]float32
	Border      [4]float32
	UV          [4]float32
	Params      [4]float32
	Kind        uint32
	Flags       uint32
	// With FlagXform, a point p of the prim is drawn at
	// (Xform[0]*p.x + Xform[2]*p.y + Xlate[0], Xform[1]*p.x + Xform[3]*p.y + Xlate[1]).
	Xlate [2]float32
	Xform [4]float32
	Clip2 [4]float32 // with FlagXform: clip in the prim's own space
}

type imageRef struct {
	index int
	tex   *Texture
	layer int // 1 + the index of the layer the prim composites, or 0
	slot  int // 1 + the capture's pending image index, or 0
}

// ClipRec is a rounded or feathered clip. Its layout matches the HLSL ClipRec.
type ClipRec struct {
	Rect    [4]float32 // x, y, w, h
	Radii   [4]float32 // top-left, top-right, bottom-right, bottom-left
	Feather [4]float32 // soft edge widths: top, right, bottom, left
	Parent  uint32     // 1 + index of the enclosing clip, or 0
	// Space is 0 for window pixels and 1 for a FlagXform prim's own space.
	// The shader walks at most 8 clips up from a prim.
	Space uint32
	_     [2]uint32
}

// Scene is the list of primitives for one frame, in paint order.
type Scene struct {
	Prims  []Prim
	Clips  []ClipRec
	Layers []Layer // in the order they end, so inner layers come first
	images []imageRef
	// Clip is stamped on each prim added: 1 + an index into Clips, or 0.
	Clip uint32
}

func (s *Scene) Reset() {
	s.Prims = s.Prims[:0]
	s.Clips = s.Clips[:0]
	s.Layers = s.Layers[:0]
	s.images = s.images[:0]
	s.Clip = 0
}

// PushClip adds c inside the current clip and makes it current. It returns
// the clip to restore afterwards.
func (s *Scene) PushClip(c ClipRec) (prev uint32) {
	prev = s.Clip
	c.Parent = prev
	s.Clips = append(s.Clips, c)
	s.Clip = uint32(len(s.Clips))
	return prev
}

func (s *Scene) Add(p Prim) {
	p.Flags |= s.Clip << ClipShift
	s.Prims = append(s.Prims, p)
}

// AddImage appends an image primitive that samples tex.
func (s *Scene) AddImage(p Prim, tex *Texture) {
	p.Kind = KindImage
	p.Flags |= s.Clip << ClipShift
	s.images = append(s.images, imageRef{index: len(s.Prims), tex: tex})
	s.Prims = append(s.Prims, p)
}

// AddDeferredImage reserves an image's position in the scene before its
// texture is ready, including any clipping or layer applied to it.
func (s *Scene) AddDeferredImage(p Prim, slot int) {
	s.AddImage(p, nil)
	s.images[len(s.images)-1].slot = slot + 1
}

func (s *Scene) DeferredImages(yield func(int) bool) {
	for _, ref := range s.images {
		if ref.slot != 0 && !yield(ref.slot-1) {
			return
		}
	}
}

func (s *Scene) BindDeferredImages(resolve func(int) (*Texture, bool, error)) error {
	for i := range s.images {
		ref := &s.images[i]
		if ref.slot == 0 {
			continue
		}
		tex, premul, err := resolve(ref.slot - 1)
		if err != nil {
			return err
		}
		ref.tex = tex
		ref.slot = 0
		if premul {
			s.Prims[ref.index].Flags |= FlagPremultiplied
		}
	}
	return nil
}
