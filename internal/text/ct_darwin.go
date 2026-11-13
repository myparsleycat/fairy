package text

import (
	"strings"
	"unicode/utf16"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/darwin"
)

// The CoreFoundation, CoreText and CoreGraphics functions the macOS backend
// calls. Every reference is a CF object (uintptr); the backend only uses the
// C APIs, which return owned or borrowed references by the Create/Get rule
// and need no autorelease pool, so a System works on any goroutine.

type cfRange struct{ Location, Length int }

const (
	cfNumberSInt32  = 3
	cfNumberFloat64 = 6

	ctFontUIFontSystem = 2

	ctFontItalicTrait      = 1 << 0
	ctFontBoldTrait        = 1 << 1
	ctFontColorGlyphsTrait = 1 << 13

	ctFontOrientationDefault = 0
	ctLineTruncationEnd      = 1
	ctRunStatusRightToLeft   = 1

	cgImageAlphaPremultipliedLast = 1
	cgImageAlphaOnly              = 7

	tagOS2  = 'O'<<24 | 'S'<<16 | '/'<<8 | '2'
	tagWght = 'w'<<24 | 'g'<<16 | 'h'<<8 | 't'
)

var (
	cfStringCreateWithCharacters    func(alloc uintptr, chars *uint16, n int) uintptr
	cfStringGetLength               func(s uintptr) int
	cfStringGetCharacters           func(s uintptr, r cfRange, buf *uint16)
	cfAttributedStringCreateMutable func(alloc uintptr, max int) uintptr
	cfAttributedStringReplaceString func(as uintptr, r cfRange, s uintptr)
	cfAttributedStringSetAttribute  func(as uintptr, r cfRange, key, value uintptr)
	cfAttributedStringBeginEditing  func(as uintptr)
	cfAttributedStringEndEditing    func(as uintptr)
	cfNumberCreate                  func(alloc uintptr, typ int, value unsafe.Pointer) uintptr
	cfNumberGetValue                func(n uintptr, typ int, value unsafe.Pointer) bool
	cfArrayCreate                   func(alloc uintptr, values *uintptr, n int, callbacks uintptr) uintptr
	cfArrayGetCount                 func(a uintptr) int
	cfArrayGetValueAtIndex          func(a uintptr, i int) uintptr
	cfDictionaryCreate              func(alloc uintptr, keys, values *uintptr, n int, keyCallbacks, valueCallbacks uintptr) uintptr
	cfDictionaryGetValue            func(d, key uintptr) uintptr
	cfDataCreate                    func(alloc uintptr, bytes *byte, n int) uintptr
	cfDataGetLength                 func(d uintptr) int
	cfDataGetBytePtr                func(d uintptr) *byte
	cfSetCreate                     func(alloc uintptr, values *uintptr, n int, callbacks uintptr) uintptr
	cfGetTypeID                     func(cf uintptr) uintptr
	cfNumberGetTypeID               func() uintptr
	cfStringGetTypeID               func() uintptr

	ctFontCreateUIFontForLanguage      func(typ uint32, size float64, lang uintptr) uintptr
	ctFontCreateWithFontDescriptor     func(desc uintptr, size float64, matrix *cgAffine) uintptr
	ctFontCreateCopyWithAttributes     func(font uintptr, size float64, matrix *cgAffine, desc uintptr) uintptr
	ctFontCreateCopyWithSymbolicTraits func(font uintptr, size float64, matrix *cgAffine, value, mask uint32) uintptr
	ctFontCopyFontDescriptor           func(font uintptr) uintptr
	ctFontCopyFamilyName               func(font uintptr) uintptr
	ctFontCopyTraits                   func(font uintptr) uintptr
	ctFontCopyTable                    func(font uintptr, tag uint32, opts uint32) uintptr
	ctFontCopyVariationAxes            func(font uintptr) uintptr
	ctFontGetSymbolicTraits            func(font uintptr) uint32
	ctFontGetSize                      func(font uintptr) float64
	ctFontGetAscent                    func(font uintptr) float64
	ctFontGetDescent                   func(font uintptr) float64
	ctFontGetLeading                   func(font uintptr) float64
	ctFontGetUnderlinePosition         func(font uintptr) float64
	ctFontGetUnderlineThickness        func(font uintptr) float64
	ctFontGetXHeight                   func(font uintptr) float64
	ctFontGetUnitsPerEm                func(font uintptr) uint32
	ctFontGetBoundingRectsForGlyphs    func(font uintptr, orientation uint32, glyphs *uint16, rects *darwin.CGRect, n int) darwin.CGRect
	ctFontDrawGlyphs                   func(font uintptr, glyphs *uint16, positions *darwin.CGPoint, n int, ctx uintptr)
	ctFontGetGlyphsForCharacters       func(font uintptr, chars *uint16, glyphs *uint16, n int) bool

	ctFontDescriptorCreateWithAttributes          func(attrs uintptr) uintptr
	ctFontDescriptorCreateCopyWithAttributes      func(desc, attrs uintptr) uintptr
	ctFontDescriptorCreateMatchingFontDescriptors func(desc, mandatory uintptr) uintptr
	ctFontDescriptorCopyAttribute                 func(desc, key uintptr) uintptr
	ctFontManagerCreateFontDescriptorsFromData    func(data uintptr) uintptr

	ctTypesetterCreateWithAttributedString func(as uintptr) uintptr
	ctTypesetterSuggestLineBreak           func(ts uintptr, start int, width float64) int
	ctTypesetterSuggestClusterBreak        func(ts uintptr, start int, width float64) int
	ctTypesetterCreateLine                 func(ts uintptr, r cfRange) uintptr
	ctLineCreateWithAttributedString       func(as uintptr) uintptr
	ctLineCreateTruncatedLine              func(line uintptr, width float64, typ uint32, token uintptr) uintptr
	ctLineGetTypographicBounds             func(line uintptr, ascent, descent, leading *float64) float64
	ctLineGetTrailingWhitespaceWidth       func(line uintptr) float64
	ctLineGetGlyphRuns                     func(line uintptr) uintptr
	ctLineGetStringIndexForPosition        func(line uintptr, p darwin.CGPoint) int
	ctLineGetOffsetForStringIndex          func(line uintptr, i int, secondary *float64) float64
	ctRunGetGlyphCount                     func(run uintptr) int
	ctRunGetAttributes                     func(run uintptr) uintptr
	ctRunGetStatus                         func(run uintptr) uint32
	ctRunGetStringRange                    func(run uintptr) cfRange
	ctRunGetGlyphs                         func(run uintptr, r cfRange, buf *uint16)
	ctRunGetPositions                      func(run uintptr, r cfRange, buf *darwin.CGPoint)
	ctRunGetAdvances                       func(run uintptr, r cfRange, buf *darwin.CGSize)
	ctRunGetStringIndices                  func(run uintptr, r cfRange, buf *int)

	cgBitmapContextCreate                      func(data unsafe.Pointer, w, h, bitsPerComponent, bytesPerRow int, space uintptr, info uint32) uintptr
	cgColorSpaceCreateWithName                 func(name uintptr) uintptr
	cgColorSpaceRelease                        func(space uintptr)
	cgContextRelease                           func(ctx uintptr)
	cgContextSetShouldAntialias                func(ctx uintptr, on bool)
	cgContextSetShouldSmoothFonts              func(ctx uintptr, on bool)
	cgContextSetAllowsFontSmoothing            func(ctx uintptr, on bool)
	cgContextSetShouldSubpixelPositionFonts    func(ctx uintptr, on bool)
	cgContextSetAllowsFontSubpixelPositioning  func(ctx uintptr, on bool)
	cgContextSetShouldSubpixelQuantizeFonts    func(ctx uintptr, on bool)
	cgContextSetAllowsFontSubpixelQuantization func(ctx uintptr, on bool)
	cgContextSetGrayFillColor                  func(ctx uintptr, gray, alpha float64)

	// Constants exported by the frameworks.
	cfTypeArrayCallBacks           uintptr
	cfTypeSetCallBacks             uintptr
	cfTypeDictionaryKeyCallBacks   uintptr
	cfTypeDictionaryValueCallBacks uintptr
	kCTFontAttributeName           uintptr
	kCTKernAttributeName           uintptr
	kCTFontFamilyNameAttribute     uintptr
	kCTFontTraitsAttribute         uintptr
	kCTFontWeightTrait             uintptr
	kCTFontSymbolicTrait           uintptr
	kCTFontCascadeListAttribute    uintptr
	kCTFontVariationAttribute      uintptr
	kCTFontVariationAxisIDKey      uintptr
	kCTFontVariationAxisMinKey     uintptr
	kCTFontVariationAxisMaxKey     uintptr
	kCGColorSpaceSRGB              uintptr
)

// cgAffine is a CGAffineTransform.
type cgAffine struct{ A, B, C, D, Tx, Ty float64 }

func init() {
	cf, ct, cg := darwin.CoreFoundation, darwin.CoreText, darwin.CoreGraphics
	for _, f := range []struct {
		ptr  any
		lib  uintptr
		name string
	}{
		{&cfStringCreateWithCharacters, cf, "CFStringCreateWithCharacters"},
		{&cfStringGetLength, cf, "CFStringGetLength"},
		{&cfStringGetCharacters, cf, "CFStringGetCharacters"},
		{&cfAttributedStringCreateMutable, cf, "CFAttributedStringCreateMutable"},
		{&cfAttributedStringReplaceString, cf, "CFAttributedStringReplaceString"},
		{&cfAttributedStringSetAttribute, cf, "CFAttributedStringSetAttribute"},
		{&cfAttributedStringBeginEditing, cf, "CFAttributedStringBeginEditing"},
		{&cfAttributedStringEndEditing, cf, "CFAttributedStringEndEditing"},
		{&cfNumberCreate, cf, "CFNumberCreate"},
		{&cfNumberGetValue, cf, "CFNumberGetValue"},
		{&cfArrayCreate, cf, "CFArrayCreate"},
		{&cfArrayGetCount, cf, "CFArrayGetCount"},
		{&cfArrayGetValueAtIndex, cf, "CFArrayGetValueAtIndex"},
		{&cfDictionaryCreate, cf, "CFDictionaryCreate"},
		{&cfDictionaryGetValue, cf, "CFDictionaryGetValue"},
		{&cfDataCreate, cf, "CFDataCreate"},
		{&cfDataGetLength, cf, "CFDataGetLength"},
		{&cfDataGetBytePtr, cf, "CFDataGetBytePtr"},
		{&cfSetCreate, cf, "CFSetCreate"},
		{&cfGetTypeID, cf, "CFGetTypeID"},
		{&cfNumberGetTypeID, cf, "CFNumberGetTypeID"},
		{&cfStringGetTypeID, cf, "CFStringGetTypeID"},

		{&ctFontCreateUIFontForLanguage, ct, "CTFontCreateUIFontForLanguage"},
		{&ctFontCreateWithFontDescriptor, ct, "CTFontCreateWithFontDescriptor"},
		{&ctFontCreateCopyWithAttributes, ct, "CTFontCreateCopyWithAttributes"},
		{&ctFontCreateCopyWithSymbolicTraits, ct, "CTFontCreateCopyWithSymbolicTraits"},
		{&ctFontCopyFontDescriptor, ct, "CTFontCopyFontDescriptor"},
		{&ctFontCopyFamilyName, ct, "CTFontCopyFamilyName"},
		{&ctFontCopyTraits, ct, "CTFontCopyTraits"},
		{&ctFontCopyTable, ct, "CTFontCopyTable"},
		{&ctFontCopyVariationAxes, ct, "CTFontCopyVariationAxes"},
		{&ctFontGetSymbolicTraits, ct, "CTFontGetSymbolicTraits"},
		{&ctFontGetSize, ct, "CTFontGetSize"},
		{&ctFontGetAscent, ct, "CTFontGetAscent"},
		{&ctFontGetDescent, ct, "CTFontGetDescent"},
		{&ctFontGetLeading, ct, "CTFontGetLeading"},
		{&ctFontGetUnderlinePosition, ct, "CTFontGetUnderlinePosition"},
		{&ctFontGetUnderlineThickness, ct, "CTFontGetUnderlineThickness"},
		{&ctFontGetXHeight, ct, "CTFontGetXHeight"},
		{&ctFontGetUnitsPerEm, ct, "CTFontGetUnitsPerEm"},
		{&ctFontGetBoundingRectsForGlyphs, ct, "CTFontGetBoundingRectsForGlyphs"},
		{&ctFontDrawGlyphs, ct, "CTFontDrawGlyphs"},
		{&ctFontGetGlyphsForCharacters, ct, "CTFontGetGlyphsForCharacters"},
		{&ctFontDescriptorCreateWithAttributes, ct, "CTFontDescriptorCreateWithAttributes"},
		{&ctFontDescriptorCreateCopyWithAttributes, ct, "CTFontDescriptorCreateCopyWithAttributes"},
		{&ctFontDescriptorCreateMatchingFontDescriptors, ct, "CTFontDescriptorCreateMatchingFontDescriptors"},
		{&ctFontDescriptorCopyAttribute, ct, "CTFontDescriptorCopyAttribute"},
		{&ctFontManagerCreateFontDescriptorsFromData, ct, "CTFontManagerCreateFontDescriptorsFromData"},
		{&ctTypesetterCreateWithAttributedString, ct, "CTTypesetterCreateWithAttributedString"},
		{&ctTypesetterSuggestLineBreak, ct, "CTTypesetterSuggestLineBreak"},
		{&ctTypesetterSuggestClusterBreak, ct, "CTTypesetterSuggestClusterBreak"},
		{&ctTypesetterCreateLine, ct, "CTTypesetterCreateLine"},
		{&ctLineCreateWithAttributedString, ct, "CTLineCreateWithAttributedString"},
		{&ctLineCreateTruncatedLine, ct, "CTLineCreateTruncatedLine"},
		{&ctLineGetTypographicBounds, ct, "CTLineGetTypographicBounds"},
		{&ctLineGetTrailingWhitespaceWidth, ct, "CTLineGetTrailingWhitespaceWidth"},
		{&ctLineGetGlyphRuns, ct, "CTLineGetGlyphRuns"},
		{&ctLineGetStringIndexForPosition, ct, "CTLineGetStringIndexForPosition"},
		{&ctLineGetOffsetForStringIndex, ct, "CTLineGetOffsetForStringIndex"},
		{&ctRunGetGlyphCount, ct, "CTRunGetGlyphCount"},
		{&ctRunGetAttributes, ct, "CTRunGetAttributes"},
		{&ctRunGetStatus, ct, "CTRunGetStatus"},
		{&ctRunGetStringRange, ct, "CTRunGetStringRange"},
		{&ctRunGetGlyphs, ct, "CTRunGetGlyphs"},
		{&ctRunGetPositions, ct, "CTRunGetPositions"},
		{&ctRunGetAdvances, ct, "CTRunGetAdvances"},
		{&ctRunGetStringIndices, ct, "CTRunGetStringIndices"},

		{&cgBitmapContextCreate, cg, "CGBitmapContextCreate"},
		{&cgColorSpaceCreateWithName, cg, "CGColorSpaceCreateWithName"},
		{&cgColorSpaceRelease, cg, "CGColorSpaceRelease"},
		{&cgContextRelease, cg, "CGContextRelease"},
		{&cgContextSetShouldAntialias, cg, "CGContextSetShouldAntialias"},
		{&cgContextSetShouldSmoothFonts, cg, "CGContextSetShouldSmoothFonts"},
		{&cgContextSetAllowsFontSmoothing, cg, "CGContextSetAllowsFontSmoothing"},
		{&cgContextSetShouldSubpixelPositionFonts, cg, "CGContextSetShouldSubpixelPositionFonts"},
		{&cgContextSetAllowsFontSubpixelPositioning, cg, "CGContextSetAllowsFontSubpixelPositioning"},
		{&cgContextSetShouldSubpixelQuantizeFonts, cg, "CGContextSetShouldSubpixelQuantizeFonts"},
		{&cgContextSetAllowsFontSubpixelQuantization, cg, "CGContextSetAllowsFontSubpixelQuantization"},
		{&cgContextSetGrayFillColor, cg, "CGContextSetGrayFillColor"},
	} {
		darwin.Func(f.ptr, f.lib, f.name)
	}

	cfTypeArrayCallBacks = darwin.Symbol(cf, "kCFTypeArrayCallBacks")
	cfTypeSetCallBacks = darwin.Symbol(cf, "kCFTypeSetCallBacks")
	cfTypeDictionaryKeyCallBacks = darwin.Symbol(cf, "kCFTypeDictionaryKeyCallBacks")
	cfTypeDictionaryValueCallBacks = darwin.Symbol(cf, "kCFTypeDictionaryValueCallBacks")
	for _, c := range []struct {
		dst  *uintptr
		lib  uintptr
		name string
	}{
		{&kCTFontAttributeName, ct, "kCTFontAttributeName"},
		{&kCTKernAttributeName, ct, "kCTKernAttributeName"},
		{&kCTFontFamilyNameAttribute, ct, "kCTFontFamilyNameAttribute"},
		{&kCTFontTraitsAttribute, ct, "kCTFontTraitsAttribute"},
		{&kCTFontWeightTrait, ct, "kCTFontWeightTrait"},
		{&kCTFontSymbolicTrait, ct, "kCTFontSymbolicTrait"},
		{&kCTFontCascadeListAttribute, ct, "kCTFontCascadeListAttribute"},
		{&kCTFontVariationAttribute, ct, "kCTFontVariationAttribute"},
		{&kCTFontVariationAxisIDKey, ct, "kCTFontVariationAxisIdentifierKey"},
		{&kCTFontVariationAxisMinKey, ct, "kCTFontVariationAxisMinimumValueKey"},
		{&kCTFontVariationAxisMaxKey, ct, "kCTFontVariationAxisMaximumValueKey"},
		{&kCGColorSpaceSRGB, cg, "kCGColorSpaceSRGB"},
	} {
		*c.dst = uintptr(darwin.SymbolID(c.lib, c.name))
	}
}

func cfRelease(p uintptr) {
	if p != 0 {
		darwin.CFRelease(p)
	}
}

func cfRetain(p uintptr) uintptr {
	if p != 0 {
		darwin.CFRetain(p)
	}
	return p
}

// cfString returns a new CFString holding s.
func cfString(s string) uintptr { return cfString16(utf16.Encode([]rune(s))) }

func cfString16(u []uint16) uintptr {
	if len(u) == 0 {
		return cfStringCreateWithCharacters(0, nil, 0)
	}
	return cfStringCreateWithCharacters(0, &u[0], len(u))
}

// goString returns the text of a CFString.
func goString(s uintptr) string {
	if s == 0 {
		return ""
	}
	n := cfStringGetLength(s)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n)
	cfStringGetCharacters(s, cfRange{0, n}, &buf[0])
	return string(utf16.Decode(buf))
}

func cfFloat(v float64) uintptr { return cfNumberCreate(0, cfNumberFloat64, unsafe.Pointer(&v)) }

func cfInt(v int32) uintptr { return cfNumberCreate(0, cfNumberSInt32, unsafe.Pointer(&v)) }

// numberValue reads a CFNumber as a float64; ok is false for another type.
func numberValue(n uintptr) (float64, bool) {
	if n == 0 || cfGetTypeID(n) != cfNumberGetTypeID() {
		return 0, false
	}
	var v float64
	ok := cfNumberGetValue(n, cfNumberFloat64, unsafe.Pointer(&v))
	return v, ok
}

// cfDict returns a new CFDictionary of the CF objects in kv, as key, value
// pairs. It does not consume the references.
func cfDict(kv ...uintptr) uintptr {
	n := len(kv) / 2
	keys, values := make([]uintptr, n), make([]uintptr, n)
	for i := range n {
		keys[i], values[i] = kv[2*i], kv[2*i+1]
	}
	if n == 0 {
		return cfDictionaryCreate(0, nil, nil, 0, cfTypeDictionaryKeyCallBacks, cfTypeDictionaryValueCallBacks)
	}
	return cfDictionaryCreate(0, &keys[0], &values[0], n, cfTypeDictionaryKeyCallBacks, cfTypeDictionaryValueCallBacks)
}

// cfArray returns a new CFArray of the CF objects in vs. It does not consume
// the references.
func cfArray(vs []uintptr) uintptr {
	if len(vs) == 0 {
		return cfArrayCreate(0, nil, 0, cfTypeArrayCallBacks)
	}
	return cfArrayCreate(0, &vs[0], len(vs), cfTypeArrayCallBacks)
}

// familyOf returns the family name of a font.
func familyOf(font uintptr) string {
	s := ctFontCopyFamilyName(font)
	defer cfRelease(s)
	return goString(s)
}

func sameFamily(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}
