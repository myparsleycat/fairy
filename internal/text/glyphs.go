//go:build windows

package text

import (
	"math"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/com"
	"github.com/myparsleycat/fairy/internal/dwrite"
	"github.com/myparsleycat/fairy/internal/render"
)

const subpixelVariants = 4

type glyphKey struct {
	face    uint32
	index   uint16
	size    uint32 // em size in 1/64 px
	subpixX uint8
}

type glyphEntry struct {
	tile      render.Tile
	left, top int32
	empty     bool
}

func (s *System) faceID(face unsafe.Pointer) uint32 {
	if id, ok := s.faces[face]; ok {
		return id
	}
	// Keep the face alive so its address stays a stable identity.
	com.AddRef(face)
	id := uint32(len(s.faces) + 1)
	s.faces[face] = id
	return id
}

func (s *System) checkAtlas() {
	if s.atlas.Generation != s.atlasGen {
		clear(s.glyphs)
		s.atlasGen = s.atlas.Generation
	}
}

func (s *System) glyph(face unsafe.Pointer, index uint16, emSize float32, subpix uint8) (glyphEntry, bool) {
	k := glyphKey{face: s.faceID(face), index: index, size: uint32(emSize * 64), subpixX: subpix}
	if g, ok := s.glyphs[k]; ok {
		return g, true
	}
	idx := index
	adv := float32(0)
	off := dwrite.GlyphOffset{}
	run := dwrite.GlyphRun{
		FontFace:      face,
		FontEmSize:    emSize,
		GlyphCount:    1,
		GlyphIndices:  &idx,
		GlyphAdvances: &adv,
		GlyphOffsets:  &off,
	}
	// A glyph that cannot be rasterized is cached as empty: reporting false
	// would make the window treat it as a full atlas and clear it every frame.
	blank := glyphEntry{empty: true}
	a, err := s.factory.CreateGlyphRunAnalysis(&run, float32(subpix)/subpixelVariants, 0)
	if err != nil {
		s.glyphs[k] = blank
		return blank, true
	}
	defer a.Release()
	r := a.Bounds()
	w, h := int(r.Right-r.Left), int(r.Bottom-r.Top)
	if w <= 0 || h <= 0 || !render.Fits(w, h) {
		s.glyphs[k] = blank
		return blank, true
	}
	buf := make([]byte, w*h)
	if err := a.AlphaTexture(r, buf); err != nil {
		s.glyphs[k] = blank
		return blank, true
	}
	tile, ok := s.atlas.Insert(w, h, buf)
	if !ok {
		return glyphEntry{}, false
	}
	g := glyphEntry{tile: tile, left: r.Left, top: r.Top}
	s.glyphs[k] = g
	return g, true
}

func (s *System) isColorFace(face unsafe.Pointer) bool {
	c, ok := s.colorFaces[face]
	if !ok {
		s.faceID(face) // keeps the face alive, so the address stays valid as a key
		c = dwrite.IsColorFont(face)
		s.colorFaces[face] = c
	}
	return c
}

// Draw appends the glyphs of l at (x, y) in the given straight-alpha color,
// clipped to clip. Color fonts such as Segoe UI Emoji are drawn as layers of
// single-color glyphs. It returns false when the atlas ran out of space; the
// caller should grow the atlas and redraw the frame.
func (l *Layout) Draw(scene *render.Scene, x, y float32, color [4]float32, clip [4]float32) bool {
	return l.DrawRuns(scene, x, y, color, nil, clip)
}

// glyphQuad is a primitive of a layout in whole pixels, relative to its
// rounded origin. The first Draw records them; later draws replay them without
// walking the DirectWrite layout, until the atlas is cleared. It is kept small
// because a layout holds one per glyph for as long as it is cached.
type glyphQuad struct {
	x, y   int32
	w, h   uint16
	tx, ty uint16 // atlas tile position of a glyph
	// fixed is 1 + the index in Layout.fixed of a color-font layer's own
	// color, whose alpha is scaled by the run color; 0 uses the run color.
	fixed  uint32
	effect uint32 // Run.Color of the run: 0 selects the base color
	kind   uint8
	flags  uint8
}

// DrawRuns is Draw with the colors that Run.Color selects (colors[i-1]).
func (l *Layout) DrawRuns(scene *render.Scene, x, y float32, base [4]float32, colors [][4]float32, clip [4]float32) bool {
	if l.dl.P == nil || len(l.u16) == 0 {
		return true
	}
	s := l.sys
	s.checkAtlas()
	ox, oy := float32(math.Round(float64(x))), float32(math.Round(float64(y)))
	ok := true
	if l.quadsGen != s.atlasGen {
		ok = l.record()
	}
	as := float32(s.atlas.Size())
	for i := range l.quads {
		q := &l.quads[i]
		b := [4]float32{ox + float32(q.x), oy + float32(q.y), float32(q.w), float32(q.h)}
		if q.kind == render.KindGlyph && (b[0] > clip[0]+clip[2] || b[1] > clip[1]+clip[3] || b[0]+b[2] < clip[0] || b[1]+b[3] < clip[1]) {
			continue
		}
		color := base
		if e := int(q.effect); e > 0 && e <= len(colors) {
			color = colors[e-1]
		}
		if q.fixed > 0 {
			c := l.fixed[q.fixed-1]
			c[3] *= color[3]
			color = c
		}
		p := render.Prim{Bounds: b, Clip: clip, Color: color, Kind: uint32(q.kind), Flags: uint32(q.flags)}
		if q.kind == render.KindGlyph {
			p.UV = [4]float32{float32(q.tx) / as, float32(q.ty) / as, b[2] / as, b[3] / as}
		}
		scene.Add(p)
	}
	if !ok {
		// Glyphs that did not fit in the atlas are missing; record again after it grows.
		l.quads, l.fixed, l.quadsGen = l.quads[:0], l.fixed[:0], 0
	}
	return ok
}

// record walks the layout at the origin and stores its primitives in l.quads.
// It returns false when the atlas ran out of space.
func (l *Layout) record() bool {
	s := l.sys
	l.quads, l.fixed = l.quads[:0], l.fixed[:0]
	ok := true
	dwrite.DrawLayout(l.dl, 0, 0, func(bx, by float32, run *dwrite.GlyphRun, desc *dwrite.GlyphRunDescription, effect unsafe.Pointer) {
		if run.GlyphCount == 0 {
			return
		}
		q := glyphQuad{effect: uint32(max(dwrite.EffectIndex(effect), 0))}
		if s.isColorFace(run.FontFace) {
			if e, err := s.factory.TranslateColorGlyphRun(bx, by, run, desc); err == nil {
				for layer := e.Next(); layer != nil; layer = e.Next() {
					lq := q
					lq.flags = render.FlagRawAlpha
					if layer.PaletteIndex != 0xFFFF {
						l.fixed = append(l.fixed, layer.Color)
						lq.fixed = uint32(len(l.fixed))
					}
					ok = s.recordRun(&l.quads, layer.OriginX, layer.OriginY, &layer.GlyphRun, lq) && ok
				}
				e.Release()
				return
			}
		}
		ok = s.recordRun(&l.quads, bx, by, run, q) && ok
	}, func(x, y, width, thickness, offset float32, effect unsafe.Pointer) {
		l.quads = append(l.quads, glyphQuad{
			x:      int32(math.Round(float64(x))),
			y:      int32(math.Round(float64(y + offset))),
			w:      uint16(min(max(math.Round(float64(width)), 0), math.MaxUint16)),
			h:      uint16(max(math.Round(float64(thickness)), 1)),
			kind:   render.KindRect,
			effect: uint32(max(dwrite.EffectIndex(effect), 0)),
		})
	})
	l.quadsGen = s.atlasGen
	return ok
}

// recordRun appends the glyphs of run, with tmpl's color and flags.
func (s *System) recordRun(quads *[]glyphQuad, bx, by float32, run *dwrite.GlyphRun, tmpl glyphQuad) bool {
	n := int(run.GlyphCount)
	if n == 0 {
		return true
	}
	ok := true
	indices := unsafe.Slice(run.GlyphIndices, n)
	advances := unsafe.Slice(run.GlyphAdvances, n)
	var offsets []dwrite.GlyphOffset
	if run.GlyphOffsets != nil {
		offsets = unsafe.Slice(run.GlyphOffsets, n)
	}
	rtl := run.BidiLevel&1 == 1
	pen := bx
	baseline := float32(math.Round(float64(by)))
	for i := 0; i < n; i++ {
		gx := pen
		if rtl {
			gx = pen - advances[i]
		}
		gy := baseline
		if offsets != nil {
			if rtl {
				gx -= offsets[i].AdvanceOffset
			} else {
				gx += offsets[i].AdvanceOffset
			}
			gy -= offsets[i].AscenderOffset
		}
		if rtl {
			pen -= advances[i]
		} else {
			pen += advances[i]
		}
		fx := float32(math.Floor(float64(gx)))
		sub := uint8(math.Floor(float64(gx-fx)*subpixelVariants)) % subpixelVariants
		g, found := s.glyph(run.FontFace, indices[i], run.FontEmSize, sub)
		if !found {
			ok = false
			continue
		}
		if g.empty {
			continue
		}
		q := tmpl
		q.x = int32(fx) + g.left
		q.y = int32(math.Round(float64(gy))) + g.top
		q.w, q.h = uint16(g.tile.W), uint16(g.tile.H)
		q.tx, q.ty = uint16(g.tile.X), uint16(g.tile.Y)
		q.kind = render.KindGlyph
		*quads = append(*quads, q)
	}
	return ok
}
