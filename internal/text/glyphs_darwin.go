package text

import (
	"encoding/binary"
	"math"
	"runtime"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/darwin"
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

// faceLimit bounds the CTFonts kept as glyph cache identities. CoreText makes
// a font object per size, so a long session of zooming would otherwise keep
// every size alive.
const faceLimit = 2048

func (s *System) faceID(font uintptr) uint32 {
	if id, ok := s.faces[font]; ok {
		return id
	}
	if len(s.faces) >= faceLimit {
		for f := range s.faces {
			cfRelease(f)
		}
		clear(s.faces)
		clear(s.colorFaces)
		clear(s.glyphs)
		s.color.reset()
	}
	// Keep the font alive so its address stays a stable identity.
	cfRetain(font)
	id := uint32(len(s.faces) + 1)
	s.faces[font] = id
	return id
}

func (s *System) checkAtlas() {
	if g := s.atlas.generation(); g != s.atlasGen {
		clear(s.glyphs)
		s.atlasGen = g
	}
}

func (s *System) isColorFace(font uintptr) bool {
	c, ok := s.colorFaces[font]
	if !ok {
		s.faceID(font) // keeps the font alive, so the address stays valid as a key
		c = ctFontGetSymbolicTraits(font)&ctFontColorGlyphsTrait != 0
		s.colorFaces[font] = c
	}
	return c
}

func (s *System) glyph(font uintptr, index uint16, subpix uint8) (glyphEntry, bool) {
	k := glyphKey{face: s.faceID(font), index: index, size: uint32(ctFontGetSize(font) * 64), subpixX: subpix}
	if g, ok := s.glyphs[k]; ok {
		return g, true
	}
	// A glyph that cannot be rasterized is cached as empty: reporting false
	// would make the window treat it as a full atlas and clear it every frame.
	blank := glyphEntry{empty: true}
	m, ok := rasterize(font, index, float64(subpix)/subpixelVariants, 0)
	if !ok {
		s.glyphs[k] = blank
		return blank, true
	}
	tile, ok := s.atlas.Insert(m.w, m.h, m.pix)
	if !ok {
		return glyphEntry{}, false
	}
	g := glyphEntry{tile: tile, left: int32(m.left), top: int32(m.top)}
	s.glyphs[k] = g
	return g, true
}

// mask is a rasterized glyph: w*h pixels of one byte of coverage, or four
// bytes of premultiplied RGBA, placed at (left, top) from the pen position.
type mask struct {
	pix             []byte
	w, h, left, top int
}

// rasterize draws one glyph with CoreGraphics, offset right by ox pixels,
// and crops it to the pixels it covers. With a color space it draws RGBA,
// as color glyphs need; otherwise coverage alone. Font smoothing is off, so
// stems are not emboldened; the renderer applies its own text gamma.
func rasterize(font uintptr, glyph uint16, ox float64, space uintptr) (mask, bool) {
	var r darwin.CGRect
	ctFontGetBoundingRectsForGlyphs(font, ctFontOrientationDefault, &glyph, &r, 1)
	if r.Size.W <= 0 || r.Size.H <= 0 {
		return mask{}, false
	}
	// The bounds are the outline's; antialiasing and synthetic slant reach a
	// little past them, and the crop below trims what stays empty.
	const pad = 2
	left := int(math.Floor(r.Origin.X+ox)) - pad
	right := int(math.Ceil(r.Origin.X+r.Size.W+ox)) + pad
	top := int(math.Floor(-(r.Origin.Y + r.Size.H))) - pad
	bottom := int(math.Ceil(-r.Origin.Y)) + pad
	w, h := right-left, bottom-top
	if w <= 0 || h <= 0 || !render.Fits(w, h) {
		return mask{}, false
	}
	bpp, info := 1, uint32(cgImageAlphaOnly)
	if space != 0 {
		bpp, info = 4, cgImageAlphaPremultipliedLast
	}
	buf := make([]byte, w*h*bpp)
	ctx := cgBitmapContextCreate(unsafe.Pointer(&buf[0]), w, h, 8, w*bpp, space, info)
	if ctx == 0 {
		return mask{}, false
	}
	cgContextSetShouldAntialias(ctx, true)
	cgContextSetAllowsFontSmoothing(ctx, false)
	cgContextSetShouldSmoothFonts(ctx, false)
	cgContextSetAllowsFontSubpixelPositioning(ctx, true)
	cgContextSetShouldSubpixelPositionFonts(ctx, true)
	cgContextSetAllowsFontSubpixelQuantization(ctx, false)
	cgContextSetShouldSubpixelQuantizeFonts(ctx, false)
	cgContextSetGrayFillColor(ctx, 1, 1)
	// CoreGraphics puts the origin at the bottom left of the bitmap, whose
	// first row in memory is the top one.
	pos := darwin.CGPoint{X: ox - float64(left), Y: float64(h + top)}
	ctFontDrawGlyphs(font, &glyph, &pos, 1, ctx)
	cgContextRelease(ctx)
	runtime.KeepAlive(buf)

	x0, y0, x1, y1 := w, h, -1, -1
	for y := range h {
		row := buf[y*w*bpp : (y+1)*w*bpp]
		for x := range w {
			if row[x*bpp+bpp-1] != 0 {
				x0, x1 = min(x0, x), max(x1, x)
				y0, y1 = min(y0, y), max(y1, y)
			}
		}
	}
	if x1 < 0 {
		return mask{}, false
	}
	cw, ch := x1-x0+1, y1-y0+1
	pix := buf
	if cw != w || ch != h {
		pix = make([]byte, cw*ch*bpp)
		for y := range ch {
			copy(pix[y*cw*bpp:(y+1)*cw*bpp], buf[((y0+y)*w+x0)*bpp:])
		}
	}
	return mask{pix: pix, w: cw, h: ch, left: left + x0, top: top + y0}, true
}

// colorGlyphs holds the textures of color glyphs, such as Apple Color Emoji,
// whose bitmaps the single-channel atlas cannot hold.
type colorGlyphs struct {
	newTexture func(w, h int, rgba []byte) (*render.Texture, error)
	entries    map[glyphKey]colorEntry
	owned      []*render.Texture
	dead       []*render.Texture // released at the next EndFrame, after the frame that drew them
	gen        int               // changes whenever entries are dropped
}

type colorEntry struct {
	tex             *render.Texture
	left, top, w, h int32
}

// colorGlyphLimit bounds the color glyph textures; past it they are all
// dropped before a layout records its glyphs again.
const colorGlyphLimit = 512

func (c *colorGlyphs) reset() {
	c.dead = append(c.dead, c.owned...)
	c.owned = nil
	clear(c.entries)
	c.gen++
}

func (c *colorGlyphs) endFrame() {
	for _, t := range c.dead {
		t.Release()
	}
	clear(c.dead)
	c.dead = c.dead[:0]
}

func (c *colorGlyphs) release() {
	c.reset()
	c.endFrame()
}

// SetColorGlyphTextures makes the system draw color glyphs, such as emoji,
// as image prims of textures that newTexture makes from premultiplied RGBA
// pixels. Without it they are drawn as single-color coverage in the text
// color. The system releases the textures it made when it drops them, on
// ResetAtlas, on the next call and on Release. newTexture is called while
// Draw records a layout, on the thread that draws.
func (s *System) SetColorGlyphTextures(newTexture func(w, h int, rgba []byte) (*render.Texture, error)) {
	s.color.reset()
	s.color.newTexture = newTexture
}

func (s *System) colorGlyph(font uintptr, index uint16) (colorEntry, bool) {
	k := glyphKey{face: s.faceID(font), index: index, size: uint32(ctFontGetSize(font) * 64)}
	if e, ok := s.color.entries[k]; ok {
		return e, e.tex != nil
	}
	if s.color.entries == nil {
		s.color.entries = map[glyphKey]colorEntry{}
	}
	if s.srgb == 0 {
		s.srgb = cgColorSpaceCreateWithName(kCGColorSpaceSRGB)
	}
	m, ok := rasterize(font, index, 0, s.srgb)
	if !ok {
		s.color.entries[k] = colorEntry{}
		return colorEntry{}, false
	}
	tex, err := s.color.newTexture(m.w, m.h, m.pix)
	if err != nil || tex == nil {
		// Cached as empty, so a failing renderer is not asked on every frame.
		s.color.entries[k] = colorEntry{}
		return colorEntry{}, false
	}
	s.color.owned = append(s.color.owned, tex)
	e := colorEntry{tex: tex, left: int32(m.left), top: int32(m.top), w: int32(m.w), h: int32(m.h)}
	s.color.entries[k] = e
	return e, true
}

// decoMetrics places a font's underline and strikethrough, as offsets of
// their top edges below the baseline.
type decoMetrics struct {
	underline, underlineThick float32
	strike, strikeThick       float32
}

func fontDecoMetrics(font uintptr) decoMetrics {
	size := ctFontGetSize(font)
	thick := ctFontGetUnderlineThickness(font)
	if thick <= 0 {
		thick = size / 14
	}
	m := decoMetrics{
		underline:      float32(-ctFontGetUnderlinePosition(font)),
		underlineThick: float32(thick),
		strike:         float32(-(ctFontGetXHeight(font)/2 + thick/2)),
		strikeThick:    float32(thick),
	}
	// The OS/2 table has the font's own strikeout, as DirectWrite uses.
	if t := ctFontCopyTable(font, tagOS2, 0); t != 0 {
		if cfDataGetLength(t) >= 30 {
			b := unsafe.Slice(cfDataGetBytePtr(t), 30)
			upem := float64(ctFontGetUnitsPerEm(font))
			sz := float64(int16(binary.BigEndian.Uint16(b[26:])))
			pos := float64(int16(binary.BigEndian.Uint16(b[28:])))
			if upem > 0 && sz > 0 && pos > 0 {
				m.strike = float32(-pos * size / upem)
				m.strikeThick = float32(sz * size / upem)
			}
		}
		cfRelease(t)
	}
	return m
}

// Draw appends the glyphs of l at (x, y) in the given straight-alpha color,
// clipped to clip. Color glyphs such as emoji are drawn as image prims (see
// SetColorGlyphTextures). It returns false when the atlas ran out of space;
// the caller should grow the atlas and redraw the frame.
func (l *Layout) Draw(scene *render.Scene, x, y float32, color [4]float32, clip [4]float32) bool {
	return l.DrawRuns(scene, x, y, color, nil, clip)
}

// glyphQuad is a primitive of a layout in whole pixels, relative to its
// rounded origin. The first Draw records them; later draws replay them without
// walking the CoreText lines, until the atlas is cleared. It is kept small
// because a layout holds one per glyph for as long as it is cached.
type glyphQuad struct {
	x, y   int32
	w, h   uint16
	tx, ty uint16 // atlas tile position of a glyph
	image  uint32 // 1 + the index in Layout.images of a color glyph's texture
	effect uint32 // Run.Color of the run: 0 selects the base color
	kind   uint8
	flags  uint8
}

// DrawRuns is Draw with the colors that Run.Color selects (colors[i-1]).
func (l *Layout) DrawRuns(scene *render.Scene, x, y float32, base [4]float32, colors [][4]float32, clip [4]float32) bool {
	if !l.ok || len(l.u16) == 0 {
		return true
	}
	s := l.sys
	s.checkAtlas()
	ox, oy := float32(math.Round(float64(x))), float32(math.Round(float64(y)))
	ok := true
	if l.quadsGen != s.atlasGen || l.colorGen != s.color.gen || l.quads == nil {
		ok = l.record()
	}
	as := float32(s.atlas.Size())
	for i := range l.quads {
		q := &l.quads[i]
		b := [4]float32{ox + float32(q.x), oy + float32(q.y), float32(q.w), float32(q.h)}
		if q.kind != render.KindRect && (b[0] > clip[0]+clip[2] || b[1] > clip[1]+clip[3] || b[0]+b[2] < clip[0] || b[1]+b[3] < clip[1]) {
			continue
		}
		color := base
		if e := int(q.effect); e > 0 && e <= len(colors) {
			color = colors[e-1]
		}
		p := render.Prim{Bounds: b, Clip: clip, Color: color, Kind: uint32(q.kind), Flags: uint32(q.flags)}
		switch q.kind {
		case render.KindGlyph:
			p.UV = [4]float32{float32(q.tx) / as, float32(q.ty) / as, b[2] / as, b[3] / as}
		case render.KindImage:
			// A color glyph keeps its own colors; the run color's alpha fades it.
			p.Color = [4]float32{1, 1, 1, color[3]}
			p.UV = [4]float32{0, 0, 1, 1}
			scene.AddImage(p, l.images[q.image-1])
			continue
		}
		scene.Add(p)
	}
	if !ok {
		// Glyphs that did not fit in the atlas are missing; record again after it grows.
		l.quads, l.images, l.quadsGen = nil, l.images[:0], 0
	}
	return ok
}

// record walks the lines at the origin and stores their primitives in
// l.quads. It returns false when the atlas ran out of space.
func (l *Layout) record() bool {
	s := l.sys
	if len(s.color.owned) >= colorGlyphLimit {
		s.color.reset()
	}
	l.quads, l.images = make([]glyphQuad, 0, len(l.u16)), l.images[:0]
	ok := true
	for li := range l.lines {
		ln := &l.lines[li]
		if ln.draw == 0 {
			continue
		}
		by := float32(math.Round(float64(ln.top + ln.baseline)))
		forRuns(ln.draw, func(_, font uintptr, glyphs []uint16, pos []darwin.CGPoint, adv []darwin.CGSize, idx []int) {
			if font == 0 {
				return
			}
			images := s.color.newTexture != nil && s.isColorFace(font)
			for g, index := range glyphs {
				gx := ln.x + float32(pos[g].X)
				gy := int32(math.Round(float64(by - float32(pos[g].Y))))
				fx := float32(math.Floor(float64(gx)))
				q := glyphQuad{effect: uint32(l.colorAt(idx[g]))}
				if images {
					e, found := s.colorGlyph(font, index)
					if !found {
						continue
					}
					l.images = append(l.images, e.tex)
					q.image = uint32(len(l.images))
					q.x, q.y = int32(fx)+e.left, gy+e.top
					q.w, q.h = uint16(e.w), uint16(e.h)
					q.kind, q.flags = render.KindImage, render.FlagPremultiplied
					l.quads = append(l.quads, q)
					continue
				}
				sub := uint8(math.Floor(float64(gx-fx)*subpixelVariants)) % subpixelVariants
				e, found := s.glyph(font, index, sub)
				if !found {
					ok = false
					continue
				}
				if e.empty {
					continue
				}
				q.x, q.y = int32(fx)+e.left, gy+e.top
				q.w, q.h = uint16(e.tile.W), uint16(e.tile.H)
				q.tx, q.ty = uint16(e.tile.X), uint16(e.tile.Y)
				q.kind = render.KindGlyph
				l.quads = append(l.quads, q)
			}
			l.decorate(ln, font, by, pos, adv, idx)
		})
	}
	l.quadsGen, l.colorGen = s.atlasGen, s.color.gen
	return ok
}

// colorAt returns the Run.Color of UTF-16 index i.
func (l *Layout) colorAt(i int) int {
	for _, d := range l.deco {
		if uint32(i) >= d.start && uint32(i) < d.end {
			return d.color
		}
	}
	return 0
}

// decorate appends the underlines and strikethroughs of a glyph run.
func (l *Layout) decorate(ln *line, font uintptr, by float32, pos []darwin.CGPoint, adv []darwin.CGSize, idx []int) {
	var m *decoMetrics
	for _, d := range l.deco {
		if !d.underline && !d.strike {
			continue
		}
		x0, x1 := math.Inf(1), math.Inf(-1)
		for g := range pos {
			if i := uint32(idx[g]); i >= d.start && i < d.end {
				x0, x1 = min(x0, pos[g].X), max(x1, pos[g].X+adv[g].W)
			}
		}
		if x0 >= x1 {
			continue
		}
		if m == nil {
			dm := fontDecoMetrics(font)
			m = &dm
		}
		add := func(offset, thick float32) {
			l.quads = append(l.quads, glyphQuad{
				x:      int32(math.Round(float64(ln.x) + x0)),
				y:      int32(math.Round(float64(by + offset))),
				w:      uint16(min(max(math.Round(x1-x0), 0), math.MaxUint16)),
				h:      uint16(max(math.Round(float64(thick)), 1)),
				kind:   render.KindRect,
				effect: uint32(d.color),
			})
		}
		if d.underline {
			add(m.underline, m.underlineThick)
		}
		if d.strike {
			add(m.strike, m.strikeThick)
		}
	}
}
