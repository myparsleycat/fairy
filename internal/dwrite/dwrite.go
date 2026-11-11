//go:build windows

// Package dwrite binds the DirectWrite calls used for text layout and glyph rasterization.
package dwrite

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/myparsleycat/fairy/internal/com"
)

var (
	dwriteDLL            = windows.NewLazySystemDLL("dwrite.dll")
	pDWriteCreateFactory = dwriteDLL.NewProc("DWriteCreateFactory")

	IID_IDWriteFactory2    = com.MustGUID("0439fc60-ca44-4994-8dee-3a9af7b732ec")
	IID_IDWriteTextLayout1 = com.MustGUID("9064d822-80a7-465c-a986-df65f78b8feb")
)

const (
	FONT_STYLE_NORMAL = 0
	FONT_STYLE_ITALIC = 2

	FONT_STRETCH_NORMAL = 5

	WORD_WRAPPING_WRAP    = 0
	WORD_WRAPPING_NO_WRAP = 1

	TEXT_ALIGNMENT_LEADING  = 0
	TEXT_ALIGNMENT_TRAILING = 1
	TEXT_ALIGNMENT_CENTER   = 2

	TRIMMING_GRANULARITY_NONE      = 0
	TRIMMING_GRANULARITY_CHARACTER = 1

	LINE_SPACING_METHOD_DEFAULT = 0
	LINE_SPACING_METHOD_UNIFORM = 1

	MEASURING_MODE_NATURAL = 0

	RENDERING_MODE_NATURAL_SYMMETRIC = 5

	GRID_FIT_MODE_DEFAULT = 0

	TEXT_ANTIALIAS_MODE_GRAYSCALE = 1

	TEXTURE_ALIASED_1x1 = 0
)

type TextRange struct{ Start, Length uint32 }

func (r TextRange) arg() uintptr { return uintptr(r.Start) | uintptr(r.Length)<<32 }

type TextMetrics struct {
	Left, Top                        float32
	Width                            float32
	WidthIncludingTrailingWhitespace float32
	Height                           float32
	LayoutWidth, LayoutHeight        float32
	MaxBidiReorderingDepth           uint32
	LineCount                        uint32
}

type LineMetrics struct {
	Length                   uint32
	TrailingWhitespaceLength uint32
	NewlineLength            uint32
	Height                   float32
	Baseline                 float32
	IsTrimmed                int32
}

type HitTestMetrics struct {
	TextPosition uint32
	Length       uint32
	Left, Top    float32
	Width        float32
	Height       float32
	BidiLevel    uint32
	IsText       int32
	IsTrimmed    int32
}

type Trimming struct {
	Granularity    uint32
	Delimiter      uint32
	DelimiterCount uint32
}

type GlyphOffset struct{ AdvanceOffset, AscenderOffset float32 }

type GlyphRun struct {
	FontFace      unsafe.Pointer
	FontEmSize    float32
	GlyphCount    uint32
	GlyphIndices  *uint16
	GlyphAdvances *float32
	GlyphOffsets  *GlyphOffset
	IsSideways    int32
	BidiLevel     uint32
}

type GlyphRunDescription struct {
	LocaleName   *uint16
	String       *uint16
	StringLength uint32
	ClusterMap   *uint16
	TextPosition uint32
}

type Rect struct{ Left, Top, Right, Bottom int32 }

// Factory wraps IDWriteFactory2.
type Factory struct{ P unsafe.Pointer }

func NewFactory() (Factory, error) {
	var f unsafe.Pointer
	r, _, _ := pDWriteCreateFactory.Call(0, uintptr(unsafe.Pointer(&IID_IDWriteFactory2)), uintptr(unsafe.Pointer(&f)))
	if err := com.HRESULT(int32(r)).Err("DWriteCreateFactory"); err != nil {
		return Factory{}, err
	}
	return Factory{f}, nil
}

// TextFormat wraps IDWriteTextFormat.
type TextFormat struct{ P unsafe.Pointer }

func (t TextFormat) Release() { com.Release(t.P) }

func (t TextFormat) SetTextAlignment(a uint32) { com.HR(t.P, 3, uintptr(a)) }
func (t TextFormat) SetWordWrapping(w uint32)  { com.HR(t.P, 5, uintptr(w)) }

func (t TextFormat) SetTrimming(tr *Trimming, sign unsafe.Pointer) {
	com.HR(t.P, 9, uintptr(unsafe.Pointer(tr)), uintptr(sign))
}

func (t TextFormat) SetLineSpacing(method uint32, spacing, baseline float32) {
	com.HR(t.P, 10, uintptr(method), com.F32(spacing), com.F32(baseline))
}

func (f Factory) CreateEllipsisTrimmingSign(format TextFormat) unsafe.Pointer {
	var sign unsafe.Pointer
	if com.HR(f.P, 20, uintptr(format.P), uintptr(unsafe.Pointer(&sign))).Failed() {
		return nil
	}
	return sign
}

// TextLayout wraps IDWriteTextLayout.
type TextLayout struct{ P unsafe.Pointer }

func (f Factory) CreateTextLayout(text []uint16, format TextFormat, maxW, maxH float32) (TextLayout, error) {
	var p *uint16
	if len(text) > 0 {
		p = &text[0]
	} else {
		empty := uint16(0)
		p = &empty
	}
	var out unsafe.Pointer
	hr := com.HR(f.P, 18, uintptr(unsafe.Pointer(p)), uintptr(len(text)), uintptr(format.P), com.F32(maxW), com.F32(maxH), uintptr(unsafe.Pointer(&out)))
	if err := hr.Err("CreateTextLayout"); err != nil {
		return TextLayout{}, err
	}
	return TextLayout{out}, nil
}

func (l TextLayout) Release() { com.Release(l.P) }

func (l TextLayout) SetFontWeight(weight uint32, r TextRange) {
	com.HR(l.P, 32, uintptr(weight), r.arg())
}
func (l TextLayout) SetFontStyle(style uint32, r TextRange) { com.HR(l.P, 33, uintptr(style), r.arg()) }
func (l TextLayout) SetFontSize(size float32, r TextRange)  { com.HR(l.P, 35, com.F32(size), r.arg()) }
func (l TextLayout) SetUnderline(on bool, r TextRange)      { com.HR(l.P, 36, b2u(on), r.arg()) }

func (l TextLayout) SetFontFamilyName(name string, r TextRange) {
	p, _ := syscall.UTF16PtrFromString(name)
	com.HR(l.P, 31, uintptr(unsafe.Pointer(p)), r.arg())
}

// SetDrawingEffect tags a range with an opaque value that DrawGlyphRun reports back.
func (l TextLayout) SetDrawingEffect(effect unsafe.Pointer, r TextRange) {
	com.HR(l.P, 38, uintptr(effect), r.arg())
}

// SetCharacterSpacing adds space before and after each character of r, as
// IDWriteTextLayout1 does.
func (l TextLayout) SetCharacterSpacing(leading, trailing, minAdvance float32, r TextRange) error {
	p, err := com.QueryInterface(l.P, &IID_IDWriteTextLayout1)
	if err != nil {
		return err
	}
	defer com.Release(p)
	return com.HR(p, 69, com.F32(leading), com.F32(trailing), com.F32(minAdvance), r.arg()).
		Err("IDWriteTextLayout1.SetCharacterSpacing")
}

func (l TextLayout) Draw(renderer unsafe.Pointer, x, y float32) error {
	return com.HR(l.P, 58, 0, uintptr(renderer), com.F32(x), com.F32(y)).Err("IDWriteTextLayout.Draw")
}

func (l TextLayout) LineMetrics() []LineMetrics {
	var n uint32
	com.HR(l.P, 59, 0, 0, uintptr(unsafe.Pointer(&n)))
	if n == 0 {
		return nil
	}
	out := make([]LineMetrics, n)
	com.HR(l.P, 59, uintptr(unsafe.Pointer(&out[0])), uintptr(n), uintptr(unsafe.Pointer(&n)))
	return out[:n]
}

func (l TextLayout) Metrics() TextMetrics {
	var m TextMetrics
	com.HR(l.P, 60, uintptr(unsafe.Pointer(&m)))
	return m
}

func (l TextLayout) HitTestPoint(x, y float32) (m HitTestMetrics, trailing, inside bool) {
	var t, in int32
	com.HR(l.P, 64, com.F32(x), com.F32(y), uintptr(unsafe.Pointer(&t)), uintptr(unsafe.Pointer(&in)), uintptr(unsafe.Pointer(&m)))
	return m, t != 0, in != 0
}

func (l TextLayout) HitTestTextPosition(pos uint32, trailing bool) (x, y float32, m HitTestMetrics) {
	com.HR(l.P, 65, uintptr(pos), b2u(trailing), uintptr(unsafe.Pointer(&x)), uintptr(unsafe.Pointer(&y)), uintptr(unsafe.Pointer(&m)))
	return
}

func (l TextLayout) HitTestTextRange(pos, length uint32) []HitTestMetrics {
	var n uint32
	com.HR(l.P, 66, uintptr(pos), uintptr(length), 0, 0, 0, 0, uintptr(unsafe.Pointer(&n)))
	if n == 0 {
		return nil
	}
	out := make([]HitTestMetrics, n)
	com.HR(l.P, 66, uintptr(pos), uintptr(length), 0, 0, uintptr(unsafe.Pointer(&out[0])), uintptr(n), uintptr(unsafe.Pointer(&n)))
	return out[:n]
}

// GlyphRunAnalysis wraps IDWriteGlyphRunAnalysis.
type GlyphRunAnalysis struct{ P unsafe.Pointer }

func (f Factory) CreateGlyphRunAnalysis(run *GlyphRun, originX, originY float32) (GlyphRunAnalysis, error) {
	var out unsafe.Pointer
	hr := com.HR(f.P, 30, uintptr(unsafe.Pointer(run)), 0, RENDERING_MODE_NATURAL_SYMMETRIC, MEASURING_MODE_NATURAL,
		GRID_FIT_MODE_DEFAULT, TEXT_ANTIALIAS_MODE_GRAYSCALE, com.F32(originX), com.F32(originY), uintptr(unsafe.Pointer(&out)))
	if err := hr.Err("CreateGlyphRunAnalysis"); err != nil {
		return GlyphRunAnalysis{}, err
	}
	return GlyphRunAnalysis{out}, nil
}

func (a GlyphRunAnalysis) Release() { com.Release(a.P) }

func (a GlyphRunAnalysis) Bounds() Rect {
	var r Rect
	com.HR(a.P, 3, TEXTURE_ALIASED_1x1, uintptr(unsafe.Pointer(&r)))
	return r
}

func (a GlyphRunAnalysis) AlphaTexture(r Rect, buf []byte) error {
	return com.HR(a.P, 4, TEXTURE_ALIASED_1x1, uintptr(unsafe.Pointer(&r)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))).Err("CreateAlphaTexture")
}

func b2u(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}
