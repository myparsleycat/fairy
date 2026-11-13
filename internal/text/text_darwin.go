package text

import (
	"fmt"
	"math"
	"runtime"
	"slices"
	"sort"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/darwin"
	"github.com/myparsleycat/fairy/internal/render"
)

// On macOS, CoreText shapes and breaks the text and CoreGraphics rasterizes
// the glyphs. The layout reproduces the line model of DirectWrite that the
// rest of fairy expects: lines own their trailing whitespace and newline,
// a text ending in a newline has an empty last line, and an empty text has
// one empty line of the font's height.

type formatKey struct {
	family        string
	size          float32
	weight        int
	italic        bool
	lineHeight    float32
	align         Align
	noWrap        bool
	ellipsis      bool
	letterSpacing float32
	scale         float32
}

type layoutKey struct {
	text  string
	style formatKey
	width float32
	runs  string
}

// shapeKey is a layoutKey without the width: what shaping depends on.
type shapeKey struct {
	text  string
	style formatKey
	runs  string
}

func (k layoutKey) shape() shapeKey { return shapeKey{k.text, k.style, k.runs} }

func runsKey(runs []Run) string {
	if len(runs) == 0 {
		return ""
	}
	return fmt.Sprint(runs)
}

func keyOf(s Style) formatKey {
	w := s.Weight
	if w == 0 {
		w = 400
	}
	scale := s.Scale
	if scale <= 0 {
		scale = 1
	}
	return formatKey{s.Family, s.Size, w, s.Italic, s.LineHeight, s.Align, s.NoWrap, s.Ellipsis, s.LetterSpacing, scale}
}

// glyphAtlas is the part of render.Atlas the system uses, so tests can
// rasterize without a GPU.
type glyphAtlas interface {
	Insert(w, h int, data []byte) (render.Tile, bool)
	Size() int
	generation() int
}

type renderAtlas struct{ *render.Atlas }

func (a renderAtlas) generation() int { return a.Generation }

// System owns the font, layout and glyph caches.
type System struct {
	fontVersion int
	fonts       map[fontKey]uintptr   // CTFont of each run style
	families    map[string][]fontFace // installed faces by lower-case family
	fallback    []string              // the SetFallbackFamilies families
	cascades    map[styleKey]uintptr  // CFArray of their faces for each style, or 0
	layouts     map[layoutKey]*Layout
	shapes      map[shapeKey]*Layout // the latest reusable layout of each shaping
	glyphs      map[glyphKey]glyphEntry
	faces       map[uintptr]uint32    // retained CTFonts, the first of each identity
	faceHashes  map[uintptr][]uintptr // the fonts of faces by CFHash
	colorFaces  map[uint32]bool       // by face identity
	atlas       glyphAtlas
	atlasGen    int
	color       colorGlyphs
	frame       int // rebuilds started by BeginBuild
	paints      int // painted frames seen by EndFrame
	pass        int // layout passes started by BeginPass
	owner       uint64
	passing     bool
	latest      map[uint64]passMark // the last pass of each owner
	rescan      bool                // a pass ran since the last EndFrame
	// loose holds the layouts that the latest pass of their owner did not
	// measure. Only they can expire between rebuilds, so EndFrame checks them
	// on every frame and the whole cache only after a pass.
	loose []layoutKey
	srgb  uintptr // CGColorSpace for color glyphs
}

type passMark struct{ pass, frame int }

// NewSystem creates a text system that rasterizes into atlas.
func NewSystem(atlas *render.Atlas) (*System, error) {
	return newSystem(renderAtlas{atlas}), nil
}

func newSystem(atlas glyphAtlas) *System {
	return &System{
		fonts:      map[fontKey]uintptr{},
		families:   map[string][]fontFace{},
		cascades:   map[styleKey]uintptr{},
		layouts:    map[layoutKey]*Layout{},
		shapes:     map[shapeKey]*Layout{},
		glyphs:     map[glyphKey]glyphEntry{},
		faces:      map[uintptr]uint32{},
		faceHashes: map[uintptr][]uintptr{},
		colorFaces: map[uint32]bool{},
		latest:     map[uint64]passMark{},
		atlas:      atlas,
		atlasGen:   atlas.generation(),
	}
}

// syncFonts picks up RegisterFont and SetFallbackFamilies and drops fonts and
// layouts that resolved families without them.
func (s *System) syncFonts() {
	fontMu.Lock()
	v, families := fontVersion, fallbackFamilies
	fontMu.Unlock()
	if v == s.fontVersion {
		return
	}
	s.fontVersion = v
	s.setFallback(families)
	for k, l := range s.layouts {
		l.release()
		delete(s.layouts, k)
	}
	clear(s.shapes)
	s.clearFonts()
}

// cssBaseline places the baseline in a line of lineHeight pixels as Chromium
// does for CSS line-height: the font's ascent and descent rounded to whole
// pixels, the rest split evenly above and below with the half above floored.
func cssBaseline(ascent, descent, lineHeight float64) float32 {
	a, d := math.Round(ascent), math.Round(descent)
	return float32(math.Floor((lineHeight-a-d)/2) + a)
}

// Layout returns a cached layout of str. width bounds wrapping, alignment and trimming.
func (s *System) Layout(str string, st Style, width float32) *Layout {
	s.syncFonts()
	if width <= 0 || math.IsInf(float64(width), 0) || width > Unbounded {
		width = Unbounded
	}
	width = float32(math.Ceil(float64(width)*8) / 8)
	k := layoutKey{text: str, style: keyOf(st), width: width, runs: runsKey(st.Runs)}
	if l, ok := s.layouts[k]; ok {
		l.frame, l.painted = s.frame, s.paints
		return l
	}
	sk := k.shape()
	if l := s.shapes[sk]; l != nil && l.fits(width) {
		l.frame, l.painted = s.frame, s.paints
		return l
	}
	// Rotate the font cache on style churn; layouts own their native references.
	if len(s.fonts) >= fontCacheLimit {
		s.clearFonts()
	}
	l := &Layout{sys: s, text: str, u16: utf16.Encode([]rune(str)), frame: s.frame, painted: s.paints, width: width}
	l.build(k.style, st.Runs)
	s.layouts[k] = l
	if k.style.align == AlignStart && !k.style.ellipsis {
		l.shape = sk
		s.shapes[sk] = l
	}
	if !s.passing {
		s.loose = append(s.loose, k)
	}
	return l
}

// Measure returns the size of str laid out within maxWidth.
func (s *System) Measure(str string, st Style, maxWidth float32) (w, h float32) {
	l := s.Layout(str, st, maxWidth)
	if s.passing {
		l.owner, l.pass = s.owner, s.pass
	}
	return l.Size()
}

// BeginBuild and EndBuild bracket a rebuild of the window's tree, which
// measures the tree's text in a pass of owner 0.
func (s *System) BeginBuild() { s.frame++; s.BeginPass(0) }

func (s *System) EndBuild() { s.EndPass() }

// BeginPass and EndPass bracket a layout pass that measures all the text owner
// currently shows, such as the rows a virtual list keeps in range. Layouts that
// an earlier pass of owner measured and this one does not may expire.
func (s *System) BeginPass(owner uint64) {
	s.pass++
	s.owner, s.passing, s.rescan = owner, true, true
	s.latest[owner] = passMark{s.pass, s.frame}
}

func (s *System) EndPass() { s.passing = false }

// current reports whether the latest pass of l's owner measured l.
func (s *System) current(l *Layout) bool {
	m, ok := s.latest[l.owner]
	return ok && l.pass != 0 && m.pass == l.pass
}

// EndFrame is called after every painted frame. Layouts that the latest pass
// of their owner measured stay, so text that the frames since did not paint,
// such as content scrolled out of view, is not shaped again. Other layouts,
// such as those of virtual list rows that left the range or of the
// intermediate widths of a layout animation, are dropped after two painted
// frames without use. No layout outlives two rebuilds without use.
func (s *System) EndFrame() {
	if s.rescan {
		s.rescan = false
		s.loose = s.loose[:0]
		for k, l := range s.layouts {
			switch {
			case s.frame-l.frame > 2:
				s.drop(k, l)
			case !s.current(l):
				s.loose = append(s.loose, k)
			}
		}
		// Owners still shown pass on every rebuild.
		for o, m := range s.latest {
			if s.frame-m.frame > 2 {
				delete(s.latest, o)
			}
		}
	}
	n := 0
	for _, k := range s.loose {
		l, ok := s.layouts[k]
		if !ok || s.current(l) {
			continue
		}
		if s.paints-l.painted > 2 {
			s.drop(k, l)
			continue
		}
		s.loose[n] = k
		n++
	}
	s.loose = s.loose[:n]
	s.paints++
	s.color.endFrame()
}

// ResetAtlas makes the system rasterize into a, which replaces the atlas of a
// renderer that was recreated. Layouts stay valid; glyphs are rasterized
// again, and color glyph textures, which belonged to the old renderer, are
// made again.
func (s *System) ResetAtlas(a *render.Atlas) {
	s.atlas = renderAtlas{a}
	s.checkAtlas()
	if len(s.color.owned) > 0 {
		s.color.reset()
	}
}

func (s *System) drop(k layoutKey, l *Layout) {
	l.release()
	delete(s.layouts, k)
	if s.shapes[l.shape] == l {
		delete(s.shapes, l.shape)
	}
}

func (s *System) Release() {
	for k, l := range s.layouts {
		l.release()
		delete(s.layouts, k)
	}
	clear(s.shapes)
	s.clearFonts()
	for _, faces := range s.families {
		for _, f := range faces {
			cfRelease(f.desc)
		}
	}
	clear(s.families)
	s.clearFaces()
	clear(s.glyphs)
	s.clearCascades()
	s.color.release()
	if s.srgb != 0 {
		cgColorSpaceRelease(s.srgb)
		s.srgb = 0
	}
}

// Layout is a shaped, wrapped paragraph.
type Layout struct {
	sys     *System
	ok      bool
	text    string
	u16     []uint16
	lines   []line
	metrics []LineMetrics
	deco    []decoSpan
	// widthWS is the width of the widest line with its trailing whitespace,
	// height the sum of the line heights.
	widthWS float32
	height  float32
	frame   int    // System.frame when last used
	painted int    // System.paints when last used
	owner   uint64 // owner of the pass that last measured it
	pass    int    // System.pass of that pass; 0 when none did
	width   float32
	shape   shapeKey // set when the layout is in System.shapes
	offsets []offsetCheckpoint

	quads    []glyphQuad
	images   []*render.Texture // color glyph textures that quads refer to
	quadsGen int               // atlas generation the quads were recorded in; 0 when none
	colorGen int               // color glyph generation of the quads
}

// line is one line of a layout. ct is the CTLine of its text, which hit
// testing uses; draw is the line drawn, which an ellipsis may have trimmed.
// An empty last line has neither.
type line struct {
	ct, draw              uintptr
	start, length         uint32 // UTF-16, with trailing whitespace and newline
	trailing, newline     uint32
	top, height, baseline float32
	x                     float32 // alignment offset
	inkWidth              float32 // width without trailing whitespace
}

// decoSpan carries the Run fields that do not change shaping, for UTF-16
// range [start, end).
type decoSpan struct {
	start, end        uint32
	color             int
	underline, strike bool
}

func (l *Layout) release() {
	for i := range l.lines {
		ln := &l.lines[i]
		if ln.draw != ln.ct {
			cfRelease(ln.draw)
		}
		cfRelease(ln.ct)
		ln.ct, ln.draw = 0, 0
	}
	l.ok = false
}

// runStyle is the style of a range of the text after the Runs that cover it.
type runStyle struct {
	family    string
	weight    int
	italic    bool
	size      float32
	color     int
	underline bool
	strike    bool
}

// segments splits the text at the run edges and resolves the style of each
// piece, applying the runs in order as DirectWrite's range setters do.
func (l *Layout) segments(k formatKey, runs []Run) ([]uint32, []runStyle) {
	n := uint32(len(l.u16))
	base := runStyle{family: k.family, weight: k.weight, italic: k.italic, size: k.size}
	type urun struct {
		a, b uint32
		r    Run
	}
	var rs []urun
	edges := []uint32{0, n}
	for _, r := range runs {
		if r.End <= r.Start {
			continue
		}
		a, b := l.utf8ToUTF16(r.Start), l.utf8ToUTF16(r.End)
		if a >= b {
			continue
		}
		rs = append(rs, urun{a, b, r})
		edges = append(edges, a, b)
	}
	slices.Sort(edges)
	edges = slices.Compact(edges)
	styles := make([]runStyle, 0, len(edges))
	for i := 0; i+1 < len(edges); i++ {
		st := base
		for _, r := range rs {
			if r.a > edges[i] || r.b < edges[i+1] {
				continue
			}
			if r.r.Weight > 0 {
				st.weight = r.r.Weight
			}
			if r.r.Italic {
				st.italic = true
			}
			if r.r.Size > 0 {
				st.size = r.r.Size
			}
			if r.r.Family != "" {
				st.family = r.r.Family
			}
			if r.r.Underline {
				st.underline = true
			}
			if r.r.Strike {
				st.strike = true
			}
			if r.r.Color > 0 {
				st.color = r.r.Color
			}
		}
		styles = append(styles, st)
	}
	return edges, styles
}

// build shapes the text and breaks it into lines.
func (l *Layout) build(k formatKey, runs []Run) {
	s := l.sys
	n := len(l.u16)
	baseFont := cfRetain(s.font(k.family, k.weight, k.italic, k.size, k.scale))
	defer cfRelease(baseFont)
	if baseFont == 0 {
		return
	}

	str := cfString16(l.u16)
	as := cfAttributedStringCreateMutable(0, 0)
	cfAttributedStringReplaceString(as, cfRange{0, 0}, str)
	cfRelease(str)
	defer cfRelease(as)
	cfAttributedStringBeginEditing(as)
	edges, styles := l.segments(k, runs)
	for i, st := range styles {
		a, b := int(edges[i]), int(edges[i+1])
		if f := s.font(st.family, st.weight, st.italic, st.size, k.scale); f != 0 {
			cfAttributedStringSetAttribute(as, cfRange{a, b - a}, kCTFontAttributeName, f)
		}
		if st.color > 0 || st.underline || st.strike {
			l.deco = append(l.deco, decoSpan{uint32(a), uint32(b), st.color, st.underline, st.strike})
		}
	}
	if n > 0 {
		ps := paragraphStyle(float64(k.size))
		cfAttributedStringSetAttribute(as, cfRange{0, n}, kCTParagraphStyleAttributeName, ps)
		cfRelease(ps)
	}
	if k.letterSpacing != 0 && n > 0 {
		kern := cfFloat(float64(k.letterSpacing))
		cfAttributedStringSetAttribute(as, cfRange{0, n}, kCTKernAttributeName, kern)
		cfRelease(kern)
	}
	cfAttributedStringEndEditing(as)

	ts := ctTypesetterCreateWithAttributedString(as)
	if ts == 0 {
		return
	}
	defer cfRelease(ts)

	// The base font sets the metrics of empty lines and of uniform spacing.
	bAscent, bDescent, bLeading := ctFontGetAscent(baseFont), ctFontGetDescent(baseFont), ctFontGetLeading(baseFont)
	var uniformBaseline float32
	if k.lineHeight > 0 {
		uniformBaseline = cssBaseline(bAscent, bDescent, float64(k.lineHeight))
	}
	place := func(ln *line, ascent, descent, leading float64) {
		if k.lineHeight > 0 {
			ln.height, ln.baseline = k.lineHeight, uniformBaseline
		} else {
			ln.height, ln.baseline = float32(ascent+descent+leading), float32(ascent)
		}
		ln.top = l.height
		l.height += ln.height
	}

	var token uintptr // the ellipsis line, made on first use
	defer func() { cfRelease(token) }()
	wrapWidth := float64(l.width)
	for pos := 0; pos < n; {
		var count int
		if k.noWrap {
			count = hardLineEnd(l.u16, pos) - pos
		} else {
			count = ctTypesetterSuggestLineBreak(ts, pos, wrapWidth)
		}
		if count <= 0 {
			count = hardLineEnd(l.u16, pos) - pos
		}
		ct := ctTypesetterCreateLine(ts, cfRange{pos, count})
		ln := line{ct: ct, draw: ct, start: uint32(pos), length: uint32(count)}
		ln.newline = newlineLength(l.u16[pos : pos+count])
		ln.trailing = ln.newline + trailingSpaces(l.u16[pos:pos+count-int(ln.newline)])
		var ascent, descent, leading float64
		w := ctLineGetTypographicBounds(ct, &ascent, &descent, &leading)
		ws := ctLineGetTrailingWhitespaceWidth(ct)
		ln.inkWidth = float32(w - ws)
		lineWS := float32(w)
		if k.ellipsis && l.width < Unbounded && ln.inkWidth > l.width {
			if token == 0 {
				token = ellipsisLine(baseFont)
			}
			if t := ctLineCreateTruncatedLine(ct, wrapWidth, ctLineTruncationEnd, token); t != 0 {
				ln.draw = t
			} else {
				ln.draw = cfRetain(token)
			}
			tw := ctLineGetTypographicBounds(ln.draw, nil, nil, nil)
			ln.inkWidth = float32(tw - ctLineGetTrailingWhitespaceWidth(ln.draw))
			lineWS = float32(tw)
		}
		place(&ln, ascent, descent, leading)
		l.widthWS = max(l.widthWS, lineWS)
		l.lines = append(l.lines, ln)
		pos += count
	}
	if n == 0 || isNewline(l.u16[n-1]) {
		ln := line{start: uint32(n)}
		place(&ln, bAscent, bDescent, bLeading)
		l.lines = append(l.lines, ln)
	}

	box := l.width
	if box >= Unbounded {
		// DirectWrite would align in the unbounded width itself; align in the
		// widest line instead so that the text stays at the origin.
		box = 0
		for _, ln := range l.lines {
			box = max(box, ln.inkWidth)
		}
	}
	l.metrics = make([]LineMetrics, len(l.lines))
	for i := range l.lines {
		ln := &l.lines[i]
		switch k.align {
		case AlignCenter:
			ln.x = (box - ln.inkWidth) / 2
		case AlignEnd:
			ln.x = box - ln.inkWidth
		}
		l.metrics[i] = LineMetrics{
			Length:                   ln.length,
			TrailingWhitespaceLength: ln.trailing,
			NewlineLength:            ln.newline,
			Height:                   ln.height,
			Baseline:                 ln.baseline,
		}
		if ln.draw != ln.ct {
			l.metrics[i].IsTrimmed = 1
		}
	}
	l.ok = true
}

// paragraphStyle returns the paragraph style of DirectWrite's defaults: tab
// stops every four font sizes, where CoreText would stop every 28 pixels
// up to 336 and then not advance, and a left-to-right reading direction, so
// right-to-left text does not place the caret at negative x.
func paragraphStyle(size float64) uintptr {
	if size <= 0 {
		size = 14
	}
	stops := cfArray(nil)
	defer cfRelease(stops)
	interval := 4 * size
	dir := int8(ctWritingDirectionLeftToRight)
	settings := []ctParagraphStyleSetting{
		{ctParagraphStyleSpecifierTabStops, unsafe.Sizeof(stops), unsafe.Pointer(&stops)},
		{ctParagraphStyleSpecifierDefaultTabInterval, unsafe.Sizeof(interval), unsafe.Pointer(&interval)},
		{ctParagraphStyleSpecifierBaseWritingDirection, unsafe.Sizeof(dir), unsafe.Pointer(&dir)},
	}
	ps := ctParagraphStyleCreate(&settings[0], len(settings))
	runtime.KeepAlive(settings)
	return ps
}

// ellipsisLine returns the trimming sign in font.
func ellipsisLine(font uintptr) uintptr {
	str := cfString("…")
	as := cfAttributedStringCreateMutable(0, 0)
	cfAttributedStringReplaceString(as, cfRange{0, 0}, str)
	cfAttributedStringSetAttribute(as, cfRange{0, 1}, kCTFontAttributeName, font)
	line := ctLineCreateWithAttributedString(as)
	cfRelease(as)
	cfRelease(str)
	return line
}

func isNewline(c uint16) bool {
	return c == '\n' || c == '\r' || c == 0x0B || c == 0x0C || c == 0x85 || c == 0x2028 || c == 0x2029
}

// hardLineEnd returns the index after the line break that ends the
// paragraph starting at pos, or the text length.
func hardLineEnd(u []uint16, pos int) int {
	for i := pos; i < len(u); i++ {
		if isNewline(u[i]) {
			if u[i] == '\r' && i+1 < len(u) && u[i+1] == '\n' {
				return i + 2
			}
			return i + 1
		}
	}
	return len(u)
}

// newlineLength is the length of the line break that ends line.
func newlineLength(line []uint16) uint32 {
	n := len(line)
	switch {
	case n >= 2 && line[n-2] == '\r' && line[n-1] == '\n':
		return 2
	case n >= 1 && isNewline(line[n-1]):
		return 1
	}
	return 0
}

// trailingSpaces counts the whitespace at the end of u.
func trailingSpaces(u []uint16) uint32 {
	n := uint32(0)
	for i := len(u) - 1; i >= 0; i-- {
		c := rune(u[i])
		if c != ' ' && c != '\t' && c != 0x3000 && (c < 0x2000 || c > 0x200A) && c != 0x205F && c != 0x1680 {
			break
		}
		n++
	}
	return n
}

// fits reports whether laying the text out at width would break and place every
// line as l does, so l can stand in for that layout. Only start-aligned layouts
// without trimming are asked: with them, the width affects only line breaking.
// Breaking is greedy, so lines that already fit a narrower width break at the
// same places there; a single line also stays one line at any wider width.
func (l *Layout) fits(width float32) bool {
	if !l.ok {
		return false
	}
	used := l.widthWS
	return used <= width && used <= l.width && (width <= l.width || len(l.lines) == 1)
}

// Size is the ink-independent layout size; trailing whitespace is included so
// that editable text measures its spaces.
func (l *Layout) Size() (float32, float32) {
	if !l.ok {
		return 0, 0
	}
	w := l.widthWS
	if l.width < Unbounded && w > l.width {
		w = l.width
	}
	// Widths keep Chromium's 1/64 px layout precision: whole pixels would widen
	// every text-sized box, which adds up across a row of buttons.
	return float32(math.Ceil(float64(w)*64) / 64), float32(math.Ceil(float64(l.height)))
}

// Lines returns the number of lines.
func (l *Layout) Lines() int { return len(l.lines) }

// LineMetrics returns per-line height and baseline. The slice is the
// layout's own; callers must not change it.
func (l *Layout) LineMetrics() []LineMetrics {
	if !l.ok {
		return nil
	}
	return l.metrics
}

const offsetStride = 256

// offsetCheckpoint records the UTF-16 length of the text before byteOffset.
// Each checkpoint precedes its byte bucket at a rune boundary.
type offsetCheckpoint struct {
	byteOffset int
	units      uint32
}

// indexOffsets builds the checkpoint index for long text on first use. It
// costs one pair per 256 bytes and bounds rescans even for unordered calls.
func (l *Layout) indexOffsets() {
	if l.offsets != nil || len(l.text) < offsetStride {
		return
	}
	l.offsets = make([]offsetCheckpoint, len(l.text)/offsetStride+1)
	i, n := 0, uint32(0)
	for c := 1; c < len(l.offsets); c++ {
		for i < c*offsetStride {
			r, size := utf8.DecodeRuneInString(l.text[i:])
			if i+size > c*offsetStride {
				break
			}
			i += size
			n++
			if r >= 0x10000 {
				n++
			}
		}
		l.offsets[c] = offsetCheckpoint{i, n}
	}
}

// utf8ToUTF16 rounds a mid-rune byte offset up, as a full scan would.
func (l *Layout) utf8ToUTF16(b int) uint32 {
	b = min(max(b, 0), len(l.text))
	if b == 0 {
		return 0
	}
	l.indexOffsets()
	p := offsetCheckpoint{}
	if len(l.offsets) > 0 {
		p = l.offsets[b/offsetStride]
	}
	n := p.units
	for i, r := range l.text[p.byteOffset:] {
		if p.byteOffset+i >= b {
			break
		}
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// utf16ToUTF8 converts a UTF-16 index to a byte offset in the text.
func (l *Layout) utf16ToUTF8(u uint32) int {
	if u == 0 {
		return 0
	}
	l.indexOffsets()
	p := offsetCheckpoint{}
	if len(l.offsets) > 0 {
		i := sort.Search(len(l.offsets), func(i int) bool { return l.offsets[i].units > u }) - 1
		p = l.offsets[i]
	}
	n := p.units
	for i, r := range l.text[p.byteOffset:] {
		if n >= u {
			return p.byteOffset + i
		}
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return len(l.text)
}

// lineAt returns the index of the line that holds UTF-16 index u, or the
// last line for the end of the text.
func (l *Layout) lineAt(u uint32) int {
	i := sort.Search(len(l.lines), func(i int) bool { return l.lines[i].start+l.lines[i].length > u })
	return min(i, len(l.lines)-1)
}

// offset returns the x of the caret before UTF-16 index u in line ln.
func (l *Layout) offset(ln *line, u uint32) float32 {
	if ln.ct == 0 {
		return ln.x
	}
	return ln.x + float32(ctLineGetOffsetForStringIndex(ln.ct, int(u), nil))
}

// HitTest returns the byte offset of the caret position nearest to (x, y).
func (l *Layout) HitTest(x, y float32) int {
	if !l.ok {
		return 0
	}
	i := sort.Search(len(l.lines), func(i int) bool { return y < l.lines[i].top+l.lines[i].height })
	ln := &l.lines[min(i, len(l.lines)-1)]
	if ln.ct == 0 {
		return l.utf16ToUTF8(ln.start)
	}
	u := ctLineGetStringIndexForPosition(ln.ct, darwin.CGPoint{X: float64(x - ln.x)})
	// The newline is not a caret position on its line.
	lo, hi := int(ln.start), int(ln.start+ln.length-ln.newline)
	return l.utf16ToUTF8(uint32(min(max(u, lo), hi)))
}

// LineRange returns the byte range of the line that holds byte offset b, as
// broken for display. end excludes the line's newline, and the spaces it ends
// with where it wraps. upstream reports that end is also the start of the next
// line, as when a line wraps without a space; a caret placed at end must then
// be drawn with CaretAt(end, true) to stay on this line.
func (l *Layout) LineRange(b int) (start, end int, upstream bool) {
	ms := l.metrics
	if !l.ok || len(ms) == 0 {
		return 0, len(l.text), false
	}
	u := l.utf8ToUTF16(b)
	var s uint32
	for i, m := range ms {
		next := s + m.Length
		if u < next || i == len(ms)-1 {
			e := next - m.NewlineLength
			if i < len(ms)-1 && m.NewlineLength == 0 {
				e = next - m.TrailingWhitespaceLength
				upstream = m.TrailingWhitespaceLength == 0
			}
			return l.utf16ToUTF8(s), l.utf16ToUTF8(e), upstream
		}
		s = next
	}
	return 0, len(l.text), false
}

// Caret returns the caret rectangle (x, top, height) before byte offset b.
func (l *Layout) Caret(b int) (x, y, h float32) { return l.CaretAt(b, false) }

// CaretAt is Caret, except that with upstream a caret at the start of a
// wrapped line is drawn at the end of the line before it.
func (l *Layout) CaretAt(b int, upstream bool) (x, y, h float32) {
	if !l.ok {
		return 0, 0, 0
	}
	u := l.utf8ToUTF16(b)
	n := uint32(len(l.u16))
	if upstream && u > 0 && u < n {
		return l.trailingEdge(u - 1)
	}
	if u >= n && n > 0 {
		return l.trailingEdge(n - 1)
	}
	ln := &l.lines[l.lineAt(u)]
	return l.offset(ln, u), ln.top, ln.height
}

// trailingEdge returns the caret after the character at UTF-16 index i, on
// the line of that character. After a newline the caret is at the start of
// the next line.
func (l *Layout) trailingEdge(i uint32) (x, y, h float32) {
	li := l.lineAt(i)
	ln := &l.lines[li]
	if ln.newline > 0 && i >= ln.start+ln.length-ln.newline && li+1 < len(l.lines) {
		next := &l.lines[li+1]
		return l.offset(next, next.start), next.top, next.height
	}
	return l.offset(ln, min(i+1, ln.start+ln.length)), ln.top, ln.height
}

// RangeRects returns the selection rectangles covering bytes [a, b).
func (l *Layout) RangeRects(a, b int) []Rect {
	if !l.ok || a >= b {
		return nil
	}
	ua, ub := l.utf8ToUTF16(a), l.utf8ToUTF16(b)
	if ua >= ub {
		return nil
	}
	var out []Rect
	for li := l.lineAt(ua); li < len(l.lines); li++ {
		ln := &l.lines[li]
		if ln.start >= ub {
			break
		}
		lo, hi := max(ua, ln.start), min(ub, ln.start+ln.length)
		if lo >= hi || ln.ct == 0 {
			continue
		}
		out = append(out, l.lineRects(ln, lo, hi)...)
	}
	return out
}

// lineRects returns the rectangles of the glyphs of [lo, hi) in ln, one per
// visually contiguous span.
func (l *Layout) lineRects(ln *line, lo, hi uint32) []Rect {
	type span struct{ x0, x1 float32 }
	var spans []span
	forRuns(ln.ct, func(run uintptr, _ uintptr, glyphs []uint16, pos []darwin.CGPoint, adv []darwin.CGSize, idx []int) {
		x0, x1 := float32(math.Inf(1)), float32(math.Inf(-1))
		for g := range glyphs {
			if i := uint32(idx[g]); i >= lo && i < hi {
				x0 = min(x0, float32(pos[g].X))
				x1 = max(x1, float32(pos[g].X+adv[g].W))
			}
		}
		if x0 <= x1 {
			spans = append(spans, span{x0, x1})
		}
	})
	if len(spans) == 0 {
		// The range holds only characters without glyphs, such as a newline.
		x := l.offset(ln, lo)
		return []Rect{{x, ln.top, 0, ln.height}}
	}
	slices.SortFunc(spans, func(a, b span) int {
		switch {
		case a.x0 < b.x0:
			return -1
		case a.x0 > b.x0:
			return 1
		}
		return 0
	})
	var out []Rect
	cur := spans[0]
	for _, sp := range spans[1:] {
		if sp.x0 <= cur.x1+0.01 {
			cur.x1 = max(cur.x1, sp.x1)
			continue
		}
		out = append(out, Rect{ln.x + cur.x0, ln.top, cur.x1 - cur.x0, ln.height})
		cur = sp
	}
	return append(out, Rect{ln.x + cur.x0, ln.top, cur.x1 - cur.x0, ln.height})
}

// forRuns calls f with the glyphs of each run of a CTLine: the run's font,
// glyph indices, positions relative to the line origin (y up), advances and
// UTF-16 string indices.
func forRuns(ct uintptr, f func(run, font uintptr, glyphs []uint16, pos []darwin.CGPoint, adv []darwin.CGSize, idx []int)) {
	runs := ctLineGetGlyphRuns(ct)
	if runs == 0 {
		return
	}
	for r := range cfArrayGetCount(runs) {
		run := cfArrayGetValueAtIndex(runs, r)
		n := ctRunGetGlyphCount(run)
		if n <= 0 {
			continue
		}
		glyphs := make([]uint16, n)
		pos := make([]darwin.CGPoint, n)
		adv := make([]darwin.CGSize, n)
		idx := make([]int, n)
		all := cfRange{0, 0}
		ctRunGetGlyphs(run, all, &glyphs[0])
		ctRunGetPositions(run, all, &pos[0])
		ctRunGetAdvances(run, all, &adv[0])
		ctRunGetStringIndices(run, all, &idx[0])
		font := cfDictionaryGetValue(ctRunGetAttributes(run), kCTFontAttributeName)
		f(run, font, glyphs, pos, adv, idx)
	}
}

// Text returns the laid out string.
func (l *Layout) Text() string { return l.text }
