//go:build windows

package text

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"sync"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/com"
	"github.com/myparsleycat/fairy/internal/dwrite"
	"github.com/myparsleycat/fairy/internal/render"
	"github.com/myparsleycat/fairy/internal/win"
)

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
}

const formatCacheLimit = 128

type cachedFormat struct {
	dwrite.TextFormat
	sign unsafe.Pointer
}

func (f cachedFormat) release() {
	com.Release(f.sign)
	f.TextFormat.Release()
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

var (
	fontMu           sync.Mutex
	fontFiles        [][]byte
	fallbackFamilies []string
	fontVersion      int
)

// RegisterFont makes the families in a TTF, OTF or TTC file available by name
// to every System, in addition to the installed fonts. It returns the family names.
func RegisterFont(data []byte) ([]string, error) {
	f, err := dwrite.NewFactory()
	if err != nil {
		return nil, err
	}
	defer com.Release(f.P)
	c, err := f.CreateMemoryFontCollection([][]byte{data})
	if err != nil {
		return nil, err
	}
	names := c.FamilyNames()
	c.Release()
	if len(names) == 0 {
		return nil, errors.New("text: no font families in data")
	}
	fontMu.Lock()
	fontFiles = append(fontFiles, slices.Clone(data))
	fontVersion++
	fontMu.Unlock()
	return names, nil
}

// SetFallbackFamilies makes every System try the installed families in this
// order for characters the requested font lacks, before the system fallback.
func SetFallbackFamilies(families []string) {
	fontMu.Lock()
	fallbackFamilies = slices.Clone(families)
	fontVersion++
	fontMu.Unlock()
}

// System owns DirectWrite state, the layout cache and the glyph cache.
type System struct {
	factory     dwrite.Factory
	locale      string
	fonts       dwrite.FontCollection // registered fonts; zero when there are none
	fallback    dwrite.FontFallback   // SetFallbackFamilies; zero for the system fallback
	fontVersion int
	formats     map[formatKey]cachedFormat
	layouts     map[layoutKey]*Layout
	shapes      map[shapeKey]*Layout // the latest reusable layout of each shaping
	glyphs      map[glyphKey]glyphEntry
	faces       map[unsafe.Pointer]uint32
	colorFaces  map[unsafe.Pointer]bool
	atlas       *render.Atlas
	atlasGen    int
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
	loose   []layoutKey
	scratch []byte
}

type passMark struct{ pass, frame int }

// NewSystem creates a text system that rasterizes into atlas.
func NewSystem(atlas *render.Atlas) (*System, error) {
	f, err := dwrite.NewFactory()
	if err != nil {
		return nil, err
	}
	return &System{
		factory:    f,
		locale:     win.UserLocale(),
		formats:    map[formatKey]cachedFormat{},
		layouts:    map[layoutKey]*Layout{},
		shapes:     map[shapeKey]*Layout{},
		glyphs:     map[glyphKey]glyphEntry{},
		faces:      map[unsafe.Pointer]uint32{},
		colorFaces: map[unsafe.Pointer]bool{},
		latest:     map[uint64]passMark{},
		atlas:      atlas,
		atlasGen:   atlas.Generation,
	}, nil
}

// syncFonts rebuilds the registered font collection after RegisterFont and
// drops formats and layouts that resolved families without it.
func (s *System) syncFonts() {
	fontMu.Lock()
	v, files, families := fontVersion, fontFiles, fallbackFamilies
	fontMu.Unlock()
	if v == s.fontVersion {
		return
	}
	s.fontVersion = v
	if len(files) > 0 {
		c, err := s.factory.CreateMemoryFontCollection(files)
		if err != nil {
			return
		}
		if s.fonts.P != nil {
			s.fonts.Release()
		}
		s.fonts = c
	}
	s.setFallback(families)
	for k, l := range s.layouts {
		l.release()
		delete(s.layouts, k)
	}
	clear(s.shapes)
	s.clearFormats()
}

func keyOf(s Style) formatKey {
	fam := s.Family
	if fam == "" {
		fam = "Segoe UI"
	}
	w := s.Weight
	if w == 0 {
		w = 400
	}
	return formatKey{fam, s.Size, w, s.Italic, s.LineHeight, s.Align, s.NoWrap, s.Ellipsis, s.LetterSpacing}
}

func (s *System) clearFormats() {
	for _, f := range s.formats {
		f.release()
	}
	clear(s.formats)
}

func (s *System) format(k formatKey) (cachedFormat, error) {
	if f, ok := s.formats[k]; ok {
		return f, nil
	}
	style := uint32(dwrite.FONT_STYLE_NORMAL)
	if k.italic {
		style = dwrite.FONT_STYLE_ITALIC
	}
	size := k.size
	if size <= 0 {
		size = 14
	}
	var coll dwrite.FontCollection
	if s.fonts.HasFamily(k.family) {
		coll = s.fonts
	}
	f, err := s.factory.CreateTextFormat(k.family, coll, uint32(k.weight), style, size, s.locale)
	if err != nil {
		return cachedFormat{}, err
	}
	if s.fallback.P != nil {
		_ = f.SetFontFallback(s.fallback) // without it the format keeps the system fallback
	}
	switch k.align {
	case AlignCenter:
		f.SetTextAlignment(dwrite.TEXT_ALIGNMENT_CENTER)
	case AlignEnd:
		f.SetTextAlignment(dwrite.TEXT_ALIGNMENT_TRAILING)
	}
	if k.noWrap {
		f.SetWordWrapping(dwrite.WORD_WRAPPING_NO_WRAP)
	}
	if k.lineHeight > 0 {
		f.SetLineSpacing(dwrite.LINE_SPACING_METHOD_UNIFORM, k.lineHeight, s.baseline(coll, k, style, size))
	}
	cached := cachedFormat{TextFormat: f}
	if k.ellipsis {
		cached.sign = s.factory.CreateEllipsisTrimmingSign(f)
	}
	// Rotate the cache on style churn; layouts own their native references.
	if len(s.formats) >= formatCacheLimit {
		s.clearFormats()
	}
	s.formats[k] = cached
	return cached, nil
}

// baseline places the baseline in a line of k.lineHeight pixels as Chromium
// does for CSS line-height: the font's ascent and descent rounded to whole
// pixels, the rest split evenly above and below with the half above floored.
// Text then sits on the same pixel row as in a web page with the same font
// and line height.
func (s *System) baseline(coll dwrite.FontCollection, k formatKey, style uint32, size float32) float32 {
	m, ok := s.factory.FontMetrics(coll, k.family, uint32(k.weight), style)
	if !ok {
		return k.lineHeight * 0.8
	}
	return cssBaseline(m, size, k.lineHeight)
}

func cssBaseline(m dwrite.FontMetrics, size, lineHeight float32) float32 {
	scale := float64(size) / float64(m.DesignUnitsPerEm)
	ascent := math.Round(float64(m.Ascent) * scale)
	descent := math.Round(float64(m.Descent) * scale)
	return float32(math.Floor((float64(lineHeight)-ascent-descent)/2) + ascent)
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
	f, err := s.format(k.style)
	if err != nil {
		return &Layout{sys: s, text: str}
	}
	u := utf16.Encode([]rune(str))
	dl, err := s.factory.CreateTextLayout(u, f.TextFormat, width, Unbounded)
	if err != nil {
		return &Layout{sys: s, text: str}
	}
	if k.style.ellipsis {
		// Setting the sign on its source format can retain a native cycle.
		dwrite.TextFormat{P: dl.P}.SetTrimming(&dwrite.Trimming{Granularity: dwrite.TRIMMING_GRANULARITY_CHARACTER}, f.sign)
	}
	if k.style.letterSpacing != 0 {
		dl.SetCharacterSpacing(0, k.style.letterSpacing, 0, dwrite.TextRange{Length: uint32(len(u))})
	}
	l := &Layout{sys: s, dl: dl, text: str, u16: u, frame: s.frame, painted: s.paints, width: width}
	l.applyRuns(st.Runs)
	l.metrics = dl.Metrics()
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

func (l *Layout) applyRuns(runs []Run) {
	for _, r := range runs {
		if r.End <= r.Start {
			continue
		}
		a, b := l.utf8ToUTF16(r.Start), l.utf8ToUTF16(r.End)
		rg := dwrite.TextRange{Start: a, Length: b - a}
		if r.Weight > 0 {
			l.dl.SetFontWeight(uint32(r.Weight), rg)
		}
		if r.Italic {
			l.dl.SetFontStyle(dwrite.FONT_STYLE_ITALIC, rg)
		}
		if r.Size > 0 {
			l.dl.SetFontSize(r.Size, rg)
		}
		if r.Family != "" {
			l.dl.SetFontFamilyName(r.Family, rg)
		}
		if r.Underline {
			l.dl.SetUnderline(true, rg)
		}
		if r.Strike {
			l.dl.SetStrikethrough(true, rg)
		}
		if r.Color > 0 {
			l.dl.SetDrawingEffect(dwrite.Effect(r.Color), rg)
		}
	}
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
}

// ResetAtlas makes the system rasterize into a, which replaces the atlas of a
// renderer that was recreated. Layouts stay valid; glyphs are rasterized again.
func (s *System) ResetAtlas(a *render.Atlas) {
	s.atlas = a
	s.checkAtlas()
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
	s.clearFormats()
	for face := range s.faces {
		com.Release(face)
	}
	if s.fonts.P != nil {
		s.fonts.Release()
	}
	if s.fallback.P != nil {
		s.fallback.Release()
	}
	com.Release(s.factory.P)
}

// setFallback builds the fallback for the installed ones of families. A
// family that is not installed would only slow down every lookup.
func (s *System) setFallback(families []string) {
	if s.fallback.P != nil {
		s.fallback.Release()
		s.fallback = dwrite.FontFallback{}
	}
	if len(families) == 0 {
		return
	}
	system, err := s.factory.SystemFontCollection()
	if err != nil {
		return
	}
	installed := slices.DeleteFunc(slices.Clone(families), func(f string) bool { return !system.HasFamily(f) })
	system.Release()
	if len(installed) == 0 {
		return
	}
	if fb, err := s.factory.FontFallback(installed); err == nil {
		s.fallback = fb
	}
}

// Layout is a shaped, wrapped paragraph.
type Layout struct {
	sys     *System
	dl      dwrite.TextLayout
	text    string
	u16     []uint16
	metrics dwrite.TextMetrics
	frame   int    // System.frame when last used
	painted int    // System.paints when last used
	owner   uint64 // owner of the pass that last measured it
	pass    int    // System.pass of that pass; 0 when none did
	width   float32
	shape   shapeKey // set when the layout is in System.shapes
	offsets []offsetCheckpoint

	quads    []glyphQuad
	fixed    [][4]float32 // color-font layer colors that quads refer to
	quadsGen int          // atlas generation the quads were recorded in; 0 when none
}

func (l *Layout) release() {
	if l.dl.P != nil {
		l.dl.Release()
		l.dl.P = nil
	}
}

// fits reports whether laying the text out at width would break and place every
// line as l does, so l can stand in for that layout. Only start-aligned layouts
// without trimming are asked: with them, the width affects only line breaking.
// Breaking is greedy, so lines that already fit a narrower width break at the
// same places there; a single line also stays one line at any wider width.
func (l *Layout) fits(width float32) bool {
	if l.dl.P == nil {
		return false
	}
	used := l.metrics.WidthIncludingTrailingWhitespace
	return used <= width && used <= l.width && (width <= l.width || l.metrics.LineCount == 1)
}

// Size is the ink-independent layout size; trailing whitespace is included so
// that editable text measures its spaces.
func (l *Layout) Size() (float32, float32) {
	if l.dl.P == nil {
		return 0, 0
	}
	w := l.metrics.WidthIncludingTrailingWhitespace
	if l.width < Unbounded && w > l.width {
		w = l.width
	}
	// Widths keep Chromium's 1/64 px layout precision: whole pixels would widen
	// every text-sized box, which adds up across a row of buttons.
	return float32(math.Ceil(float64(w)*64) / 64), float32(math.Ceil(float64(l.metrics.Height)))
}

// Lines returns the number of lines.
func (l *Layout) Lines() int { return int(l.metrics.LineCount) }

// LineMetrics returns per-line height and baseline.
func (l *Layout) LineMetrics() []LineMetrics {
	if l.dl.P == nil {
		return nil
	}
	ms := l.dl.LineMetrics()
	out := make([]LineMetrics, len(ms))
	for i, m := range ms {
		out[i] = LineMetrics(m)
	}
	return out
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

// HitTest returns the byte offset of the caret position nearest to (x, y).
func (l *Layout) HitTest(x, y float32) int {
	if l.dl.P == nil {
		return 0
	}
	m, trailing, _ := l.dl.HitTestPoint(x, y)
	pos := m.TextPosition
	if trailing {
		pos += m.Length
	}
	return l.utf16ToUTF8(pos)
}

// LineRange returns the byte range of the line that holds byte offset b, as
// broken for display. end excludes the line's newline, and the spaces it ends
// with where it wraps. upstream reports that end is also the start of the next
// line, as when a line wraps without a space; a caret placed at end must then
// be drawn with CaretAt(end, true) to stay on this line.
func (l *Layout) LineRange(b int) (start, end int, upstream bool) {
	ms := l.LineMetrics()
	if len(ms) == 0 {
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
	if l.dl.P == nil {
		return 0, 0, 0
	}
	u := l.utf8ToUTF16(b)
	if upstream && u > 0 && int(u) < len(l.u16) {
		x, y, m := l.dl.HitTestTextPosition(u-1, true)
		return x, y, m.Height
	}
	if int(u) >= len(l.u16) && len(l.u16) > 0 {
		x, y, m := l.dl.HitTestTextPosition(uint32(len(l.u16)-1), true)
		return x, y, m.Height
	}
	x, y, m := l.dl.HitTestTextPosition(u, false)
	return x, y, m.Height
}

// RangeRects returns the selection rectangles covering bytes [a, b).
func (l *Layout) RangeRects(a, b int) []Rect {
	if l.dl.P == nil || a >= b {
		return nil
	}
	ua, ub := l.utf8ToUTF16(a), l.utf8ToUTF16(b)
	ms := l.dl.HitTestTextRange(ua, ub-ua)
	out := make([]Rect, 0, len(ms))
	for _, m := range ms {
		out = append(out, Rect{m.Left, m.Top, m.Width, m.Height})
	}
	return out
}

// Text returns the laid out string.
func (l *Layout) Text() string { return l.text }
