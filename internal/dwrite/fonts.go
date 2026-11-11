//go:build windows

package dwrite

import (
	"syscall"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/com"
)

var (
	IID_IDWriteFactory5  = com.MustGUID("958db99a-be2a-4f09-af7d-65189803d1d3")
	IID_IDWriteFontFace2 = com.MustGUID("d8b768ff-64bc-4e66-982b-ec8e87f693f7")
)

// E_NOCOLOR is returned by TranslateColorGlyphRun for runs without color glyphs.
const E_NOCOLOR = com.HRESULT(-2004316148) // 0x8898500C

// FontCollection wraps IDWriteFontCollection. For memory collections it also
// owns the registered in-memory loader that the font files reference.
type FontCollection struct {
	P       unsafe.Pointer
	factory unsafe.Pointer
	loader  unsafe.Pointer
}

func (c FontCollection) Release() {
	com.Release(c.P)
	if c.loader != nil {
		com.HR(c.factory, 14, uintptr(c.loader))
		com.Release(c.loader)
		com.Release(c.factory)
	}
}

// HasFamily reports whether the collection contains a family with this name.
func (c FontCollection) HasFamily(name string) bool {
	if c.P == nil {
		return false
	}
	n, _ := syscall.UTF16PtrFromString(name)
	var index uint32
	var exists int32
	if com.HR(c.P, 5, uintptr(unsafe.Pointer(n)), uintptr(unsafe.Pointer(&index)), uintptr(unsafe.Pointer(&exists))).Failed() {
		return false
	}
	return exists != 0
}

// FamilyNames returns the first localized name of each family.
func (c FontCollection) FamilyNames() []string {
	if c.P == nil {
		return nil
	}
	count := uint32(com.Call(c.P, 3))
	var names []string
	for i := uint32(0); i < count; i++ {
		var fam unsafe.Pointer
		if com.HR(c.P, 4, uintptr(i), uintptr(unsafe.Pointer(&fam))).Failed() {
			continue
		}
		var strs unsafe.Pointer // IDWriteLocalizedStrings
		if !com.HR(fam, 6, uintptr(unsafe.Pointer(&strs))).Failed() {
			var n uint32
			if !com.HR(strs, 7, 0, uintptr(unsafe.Pointer(&n))).Failed() {
				buf := make([]uint16, n+1)
				if !com.HR(strs, 8, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(n+1)).Failed() {
					names = append(names, syscall.UTF16ToString(buf))
				}
			}
			com.Release(strs)
		}
		com.Release(fam)
	}
	return names
}

// CreateMemoryFontCollection builds a font collection from font file data
// (TTF, OTF or TTC). DirectWrite copies the data. It needs IDWriteFactory5
// (Windows 10 1703 or later).
func (f Factory) CreateMemoryFontCollection(files [][]byte) (FontCollection, error) {
	f5, err := com.QueryInterface(f.P, &IID_IDWriteFactory5)
	if err != nil {
		return FontCollection{}, err
	}
	defer com.Release(f5)
	var loader unsafe.Pointer
	if err := com.HR(f5, 44, uintptr(unsafe.Pointer(&loader))).Err("CreateInMemoryFontFileLoader"); err != nil {
		return FontCollection{}, err
	}
	if err := com.HR(f5, 13, uintptr(loader)).Err("RegisterFontFileLoader"); err != nil {
		com.Release(loader)
		return FontCollection{}, err
	}
	ok := false
	defer func() {
		if !ok {
			com.HR(f5, 14, uintptr(loader))
			com.Release(loader)
		}
	}()

	var builder unsafe.Pointer
	if err := com.HR(f5, 43, uintptr(unsafe.Pointer(&builder))).Err("CreateFontSetBuilder"); err != nil {
		return FontCollection{}, err
	}
	defer com.Release(builder)
	for _, data := range files {
		if len(data) == 0 {
			continue
		}
		var file unsafe.Pointer
		hr := com.HR(loader, 4, uintptr(f5), uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), 0, uintptr(unsafe.Pointer(&file)))
		if err := hr.Err("CreateInMemoryFontFileReference"); err != nil {
			return FontCollection{}, err
		}
		hr = com.HR(builder, 7, uintptr(file))
		com.Release(file)
		if err := hr.Err("AddFontFile"); err != nil {
			return FontCollection{}, err
		}
	}
	var set unsafe.Pointer
	if err := com.HR(builder, 6, uintptr(unsafe.Pointer(&set))).Err("CreateFontSet"); err != nil {
		return FontCollection{}, err
	}
	defer com.Release(set)
	var coll unsafe.Pointer
	if err := com.HR(f5, 37, uintptr(set), uintptr(unsafe.Pointer(&coll))).Err("CreateFontCollectionFromFontSet"); err != nil {
		return FontCollection{}, err
	}
	ok = true
	com.AddRef(f5)
	return FontCollection{P: coll, factory: f5, loader: loader}, nil
}

// IsColorFont reports whether face has color glyph tables.
func IsColorFont(face unsafe.Pointer) bool {
	f2, err := com.QueryInterface(face, &IID_IDWriteFontFace2)
	if err != nil {
		return false
	}
	defer com.Release(f2)
	return com.Call(f2, 30)&0xff != 0
}

// ColorGlyphRun is one single-color layer of a color glyph run.
type ColorGlyphRun struct {
	GlyphRun     GlyphRun
	Desc         *GlyphRunDescription
	OriginX      float32
	OriginY      float32
	Color        [4]float32
	PaletteIndex uint16 // 0xFFFF: use the text color
}

// ColorRunEnumerator wraps IDWriteColorGlyphRunEnumerator.
type ColorRunEnumerator struct{ P unsafe.Pointer }

func (e ColorRunEnumerator) Release() { com.Release(e.P) }

// Next returns the next layer, or nil when there are no more.
func (e ColorRunEnumerator) Next() *ColorGlyphRun {
	var has int32
	if com.HR(e.P, 3, uintptr(unsafe.Pointer(&has))).Failed() || has == 0 {
		return nil
	}
	var run uintptr
	if com.HR(e.P, 4, uintptr(unsafe.Pointer(&run))).Failed() || run == 0 {
		return nil
	}
	return (*ColorGlyphRun)(com.Ptr(run))
}

// TranslateColorGlyphRun splits a run into color layers. It returns E_NOCOLOR
// when the run has no color glyphs.
func (f Factory) TranslateColorGlyphRun(x, y float32, run *GlyphRun, desc *GlyphRunDescription) (ColorRunEnumerator, error) {
	var out unsafe.Pointer
	hr := com.HR(f.P, 28, com.F32(x), com.F32(y), uintptr(unsafe.Pointer(run)), uintptr(unsafe.Pointer(desc)),
		MEASURING_MODE_NATURAL, 0, 0, uintptr(unsafe.Pointer(&out)))
	if hr == E_NOCOLOR {
		return ColorRunEnumerator{}, hr
	}
	if err := hr.Err("TranslateColorGlyphRun"); err != nil {
		return ColorRunEnumerator{}, err
	}
	return ColorRunEnumerator{out}, nil
}

// FontMetrics is DWRITE_FONT_METRICS, in design units.
type FontMetrics struct {
	DesignUnitsPerEm       uint16
	Ascent                 uint16
	Descent                uint16
	LineGap                int16
	CapHeight              uint16
	XHeight                uint16
	UnderlinePosition      int16
	UnderlineThickness     uint16
	StrikethroughPosition  int16
	StrikethroughThickness uint16
}

// FontMetrics returns the metrics of the font of family in coll that best
// matches weight and style; a zero collection means the installed fonts. ok
// is false when the collection has no such family.
func (f Factory) FontMetrics(coll FontCollection, family string, weight, style uint32) (m FontMetrics, ok bool) {
	c := coll.P
	if c == nil {
		if com.HR(f.P, 3, uintptr(unsafe.Pointer(&c)), 0).Failed() {
			return m, false
		}
		defer com.Release(c)
	}
	name, _ := syscall.UTF16PtrFromString(family)
	var index uint32
	var exists int32
	if com.HR(c, 5, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&index)), uintptr(unsafe.Pointer(&exists))).Failed() ||
		exists == 0 {
		return m, false
	}
	var fam unsafe.Pointer
	if com.HR(c, 4, uintptr(index), uintptr(unsafe.Pointer(&fam))).Failed() {
		return m, false
	}
	defer com.Release(fam)
	var font unsafe.Pointer
	if com.HR(fam, 7, uintptr(weight), FONT_STRETCH_NORMAL, uintptr(style), uintptr(unsafe.Pointer(&font))).Failed() {
		return m, false
	}
	defer com.Release(font)
	com.Call(font, 11, uintptr(unsafe.Pointer(&m)))
	return m, m.DesignUnitsPerEm != 0
}

// CreateTextFormat creates a text format whose family is looked up in coll;
// a zero collection means the installed fonts.
func (f Factory) CreateTextFormat(family string, coll FontCollection, weight, style uint32, size float32, locale string) (TextFormat, error) {
	fam, _ := syscall.UTF16PtrFromString(family)
	loc, _ := syscall.UTF16PtrFromString(locale)
	var out unsafe.Pointer
	hr := com.HR(f.P, 15, uintptr(unsafe.Pointer(fam)), uintptr(coll.P), uintptr(weight), uintptr(style), FONT_STRETCH_NORMAL,
		com.F32(size), uintptr(unsafe.Pointer(loc)), uintptr(unsafe.Pointer(&out)))
	if err := hr.Err("CreateTextFormat"); err != nil {
		return TextFormat{}, err
	}
	return TextFormat{out}, nil
}

var IID_IDWriteTextFormat1 = com.MustGUID("5f174b49-0d8b-4cfb-8bca-f1cce9d06c67")

// FontFallback wraps IDWriteFontFallback.
type FontFallback struct{ P unsafe.Pointer }

func (f FontFallback) Release() { com.Release(f.P) }

// SystemFontCollection returns the installed fonts.
func (f Factory) SystemFontCollection() (FontCollection, error) {
	var c unsafe.Pointer
	if err := com.HR(f.P, 3, uintptr(unsafe.Pointer(&c)), 0).Err("GetSystemFontCollection"); err != nil {
		return FontCollection{}, err
	}
	return FontCollection{P: c}, nil
}

// FontFallback returns a fallback that tries the installed families in order
// for every character, then the system fallback.
func (f Factory) FontFallback(families []string) (FontFallback, error) {
	var builder unsafe.Pointer
	if err := com.HR(f.P, 27, uintptr(unsafe.Pointer(&builder))).Err("CreateFontFallbackBuilder"); err != nil {
		return FontFallback{}, err
	}
	defer com.Release(builder)

	names := make([]*uint16, 0, len(families))
	for _, family := range families {
		p, err := syscall.UTF16PtrFromString(family)
		if err != nil {
			return FontFallback{}, err
		}
		names = append(names, p)
	}
	if len(names) > 0 {
		all := [2]uint32{0, 0x10FFFF}
		hr := com.HR(builder, 3, uintptr(unsafe.Pointer(&all)), 1, uintptr(unsafe.Pointer(&names[0])),
			uintptr(len(names)), 0, 0, 0, com.F32(1))
		if err := hr.Err("AddMapping"); err != nil {
			return FontFallback{}, err
		}
	}

	var system unsafe.Pointer
	if err := com.HR(f.P, 26, uintptr(unsafe.Pointer(&system))).Err("GetSystemFontFallback"); err != nil {
		return FontFallback{}, err
	}
	defer com.Release(system)
	if err := com.HR(builder, 4, uintptr(system)).Err("AddMappings"); err != nil {
		return FontFallback{}, err
	}
	var out unsafe.Pointer
	if err := com.HR(builder, 5, uintptr(unsafe.Pointer(&out))).Err("CreateFontFallback"); err != nil {
		return FontFallback{}, err
	}
	return FontFallback{out}, nil
}

// SetFontFallback replaces the format's font fallback.
func (t TextFormat) SetFontFallback(fb FontFallback) error {
	f1, err := com.QueryInterface(t.P, &IID_IDWriteTextFormat1)
	if err != nil {
		return err
	}
	defer com.Release(f1)
	return com.HR(f1, 34, uintptr(fb.P)).Err("SetFontFallback")
}
