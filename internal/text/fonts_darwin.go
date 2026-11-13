package text

import (
	"errors"
	"math"
	"slices"
	"strings"
	"sync"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/darwin"
)

// fontFace is one face of a family: a font descriptor with the CSS weight
// and style it is matched by.
type fontFace struct {
	desc   uintptr // CTFontDescriptor, retained
	weight int
	italic bool
}

var (
	fontMu           sync.Mutex
	registered       = map[string][]fontFace{} // by lower-case family; never released
	fallbackFamilies []string
	fontVersion      int
)

// RegisterFont makes the families in a TTF, OTF or TTC file available by name
// to every System, in addition to the installed fonts. It returns the family names.
func RegisterFont(data []byte) ([]string, error) {
	if len(data) == 0 {
		return nil, errors.New("text: no font families in data")
	}
	d := cfDataCreate(0, &data[0], len(data))
	if d == 0 {
		return nil, errors.New("text: cannot copy font data")
	}
	defer cfRelease(d)
	arr := ctFontManagerCreateFontDescriptorsFromData(d)
	if arr == 0 {
		return nil, errors.New("text: no font families in data")
	}
	defer cfRelease(arr)
	var names []string
	faces := map[string][]fontFace{}
	for i := range cfArrayGetCount(arr) {
		desc := cfArrayGetValueAtIndex(arr, i)
		name := descString(desc, kCTFontFamilyNameAttribute)
		if name == "" {
			continue
		}
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
		f := describeFace(desc)
		f.weight = os2Weight(desc, f.weight)
		f.desc = cfRetain(desc)
		k := strings.ToLower(name)
		faces[k] = append(faces[k], f)
	}
	if len(names) == 0 {
		return nil, errors.New("text: no font families in data")
	}
	fontMu.Lock()
	for k, fs := range faces {
		registered[k] = append(registered[k], fs...)
	}
	fontVersion++
	fontMu.Unlock()
	return names, nil
}

// SetFallbackFamilies makes every System try these families in this order
// for characters the requested font lacks, before the system fallback.
// Families that are neither installed nor registered are skipped.
func SetFallbackFamilies(families []string) {
	fontMu.Lock()
	fallbackFamilies = slices.Clone(families)
	fontVersion++
	fontMu.Unlock()
}

func registeredFaces(family string) []fontFace {
	fontMu.Lock()
	defer fontMu.Unlock()
	return registered[strings.ToLower(family)]
}

// descString reads a string attribute of a font descriptor.
func descString(desc, key uintptr) string {
	v := ctFontDescriptorCopyAttribute(desc, key)
	defer cfRelease(v)
	if v == 0 || cfGetTypeID(v) != cfStringGetTypeID() {
		return ""
	}
	return goString(v)
}

// describeFace reads the weight and style of a descriptor from its traits.
func describeFace(desc uintptr) fontFace {
	f := fontFace{weight: 400}
	traits := ctFontDescriptorCopyAttribute(desc, kCTFontTraitsAttribute)
	if traits == 0 {
		return f
	}
	defer cfRelease(traits)
	if w, ok := numberValue(cfDictionaryGetValue(traits, kCTFontWeightTrait)); ok {
		f.weight = cssWeight(w)
	}
	if sym, ok := numberValue(cfDictionaryGetValue(traits, kCTFontSymbolicTrait)); ok {
		f.italic = uint32(int64(sym))&ctFontItalicTrait != 0
	}
	return f
}

// os2Weight returns the usWeightClass of a face's OS/2 table, which matches
// CSS weights more exactly than the CoreText weight trait.
func os2Weight(desc uintptr, def int) int {
	font := ctFontCreateWithFontDescriptor(desc, 12, nil)
	if font == 0 {
		return def
	}
	defer cfRelease(font)
	t := ctFontCopyTable(font, tagOS2, 0)
	if t == 0 {
		return def
	}
	defer cfRelease(t)
	if cfDataGetLength(t) < 6 {
		return def
	}
	b := unsafe.Slice(cfDataGetBytePtr(t), 6)
	if w := int(b[4])<<8 | int(b[5]); w >= 1 && w <= 1000 {
		return w
	}
	return def
}

// ctWeights are the CoreText weight traits of the CSS weights 100 to 900,
// the values of NSFontWeightUltraLight to NSFontWeightBlack.
var ctWeights = [9]float64{-0.8, -0.6, -0.4, 0, 0.23, 0.3, 0.4, 0.56, 0.62}

// ctWeight maps a CSS weight to a CoreText weight trait, interpolating
// between the named weights.
func ctWeight(css int) float64 {
	c := min(max(float64(css), 100), 900)
	i := min(int(c/100)-1, 7)
	t := (c - float64(i+1)*100) / 100
	return ctWeights[i] + (ctWeights[i+1]-ctWeights[i])*t
}

// cssWeight maps a CoreText weight trait to the nearest CSS weight.
func cssWeight(w float64) int {
	best := 0
	for i, v := range ctWeights {
		if math.Abs(v-w) < math.Abs(ctWeights[best]-w) {
			best = i
		}
	}
	return (best + 1) * 100
}

// weightRank orders the faces of a family for a wanted weight as CSS font
// matching does: lower is better.
func weightRank(have, want int) int {
	if have == want {
		return 0
	}
	switch {
	case want >= 400 && want <= 500:
		if have > want && have <= 500 {
			return have - want
		}
		if have < want {
			return 1000 + want - have
		}
		return 2000 + have - want
	case want < 400:
		if have < want {
			return want - have
		}
		return 1000 + have - want
	default:
		if have > want {
			return have - want
		}
		return 1000 + want - have
	}
}

// matchFace picks the face of faces that CSS would draw weight and italic
// with: the wanted style first, then the closest weight.
func matchFace(faces []fontFace, weight int, italic bool) fontFace {
	best, found := fontFace{}, false
	for _, f := range faces {
		if !found || betterFace(f, best, weight, italic) {
			best, found = f, true
		}
	}
	return best
}

func betterFace(a, b fontFace, weight int, italic bool) bool {
	if (a.italic == italic) != (b.italic == italic) {
		return a.italic == italic
	}
	return weightRank(a.weight, weight) < weightRank(b.weight, weight)
}

type familyKind uint8

const (
	familyNamed familyKind = iota
	familySystem
	familyMono
)

// familyAliases maps the families that Windows apps and kit name to their
// macOS counterparts. Keys are lower case.
var familyAliases = map[string]string{
	"segoe ui emoji":        "Apple Color Emoji",
	"segoe ui symbol":       "Apple Symbols",
	"malgun gothic":         "Apple SD Gothic Neo",
	"yu gothic":             "Hiragino Sans",
	"yu gothic ui":          "Hiragino Sans",
	"meiryo":                "Hiragino Sans",
	"meiryo ui":             "Hiragino Sans",
	"ms gothic":             "Hiragino Sans",
	"microsoft yahei":       "PingFang SC",
	"microsoft yahei ui":    "PingFang SC",
	"simsun":                "Songti SC",
	"microsoft jhenghei":    "PingFang TC",
	"microsoft jhenghei ui": "PingFang TC",
	"calibri":               "Helvetica Neue",
	"tahoma":                "Verdana",
	"serif":                 "Times New Roman",
}

// classify resolves a requested family. Registered families keep their names;
// Windows UI and code fonts become the system UI and monospaced fonts.
func classify(family string) (familyKind, string) {
	name := strings.TrimSpace(family)
	if len(registeredFaces(name)) > 0 {
		return familyNamed, name
	}
	l := strings.ToLower(name)
	switch {
	case l == "", l == ".applesystemuifont", l == "system-ui", l == "-apple-system", l == "blinkmacsystemfont",
		l == "sans-serif", l == "san francisco", l == "sf pro", l == "sf pro text", l == "sf pro display",
		l == "segoe ui", strings.HasPrefix(l, "segoe ui variable"):
		return familySystem, ""
	case l == "consolas", l == "cascadia code", l == "cascadia mono", l == "sf mono", l == "ui-monospace",
		l == "monospace", l == "lucida console":
		return familyMono, ""
	}
	if a, ok := familyAliases[l]; ok {
		return familyNamed, a
	}
	return familyNamed, name
}

type fontKey struct {
	family string
	weight int
	italic bool
	size   float32
	scale  float32
}

const fontCacheLimit = 128

// font returns a cached CTFont for a run of text of size device pixels, at
// scale device pixels per point. It stays valid until the next font cache
// eviction, which happens only between layouts.
func (s *System) font(family string, weight int, italic bool, size, scale float32) uintptr {
	if size <= 0 {
		size = 14
	}
	if weight <= 0 {
		weight = 400
	}
	if scale <= 0 {
		scale = 1
	}
	k := fontKey{family, weight, italic, size, scale}
	if f, ok := s.fonts[k]; ok {
		return f
	}
	f := s.createFont(family, weight, italic, float64(size))
	// Fonts are made at their device size, which CoreText would otherwise
	// also take as the optical size: SF would set text on a 2x screen with
	// the narrower shapes and tighter tracking of twice the point size.
	var kv []uintptr
	if s.cascade != 0 {
		kv = append(kv, kCTFontCascadeListAttribute, s.cascade)
	}
	if scale != 1 && kCTFontOpticalSizeAttribute != 0 {
		opsz := cfFloat(float64(size / scale))
		defer cfRelease(opsz)
		kv = append(kv, kCTFontOpticalSizeAttribute, opsz)
	}
	if len(kv) > 0 && f != 0 {
		attrs := cfDict(kv...)
		d := ctFontDescriptorCreateWithAttributes(attrs)
		cfRelease(attrs)
		if c := ctFontCreateCopyWithAttributes(f, 0, nil, d); c != 0 {
			cfRelease(f)
			f = c
		}
		cfRelease(d)
	}
	s.fonts[k] = f
	return f
}

func (s *System) clearFonts() {
	for _, f := range s.fonts {
		cfRelease(f)
	}
	clear(s.fonts)
}

// obliqueSkew slants upright faces for italic text, as DirectWrite's oblique
// simulation does.
var obliqueSkew = cgAffine{A: 1, C: 0.2, D: 1}

func (s *System) createFont(family string, weight int, italic bool, size float64) uintptr {
	kind, name := classify(family)
	if kind == familyNamed {
		faces := registeredFaces(name)
		if len(faces) == 0 {
			faces = s.familyFaces(name)
		}
		if len(faces) > 0 {
			return faceFont(matchFace(faces, weight, italic), weight, italic, size)
		}
		// A family that is not installed draws with the system font, as
		// DirectWrite draws a missing family with its fallback.
		kind = familySystem
	}
	sel := "systemFontOfSize:weight:"
	if kind == familyMono {
		sel = "monospacedSystemFontOfSize:weight:"
	}
	var font uintptr
	darwin.WithPool(func() {
		font = cfRetain(uintptr(darwin.SendClass("NSFont", sel, size, ctWeight(weight))))
	})
	if font == 0 {
		font = ctFontCreateUIFontForLanguage(ctFontUIFontSystem, size, 0)
	}
	if italic {
		font = italicize(font, size)
	}
	return font
}

// faceFont creates a font of face, set to weight on a variable weight axis
// and slanted when italic is wanted but the face is upright.
func faceFont(face fontFace, weight int, italic bool, size float64) uintptr {
	var m *cgAffine
	if italic && !face.italic {
		m = &obliqueSkew
	}
	font := ctFontCreateWithFontDescriptor(face.desc, size, m)
	if font == 0 || face.weight == weight {
		return font
	}
	axes := ctFontCopyVariationAxes(font)
	if axes == 0 {
		return font
	}
	defer cfRelease(axes)
	for i := range cfArrayGetCount(axes) {
		axis := cfArrayGetValueAtIndex(axes, i)
		id, _ := numberValue(cfDictionaryGetValue(axis, kCTFontVariationAxisIDKey))
		if uint32(id) != tagWght {
			continue
		}
		lo, _ := numberValue(cfDictionaryGetValue(axis, kCTFontVariationAxisMinKey))
		hi, _ := numberValue(cfDictionaryGetValue(axis, kCTFontVariationAxisMaxKey))
		w := min(max(float64(weight), lo), hi)
		tag, val := cfInt(tagWght), cfFloat(w)
		vars := cfDict(tag, val)
		attrs := cfDict(kCTFontVariationAttribute, vars)
		d := ctFontDescriptorCreateWithAttributes(attrs)
		if v := ctFontCreateCopyWithAttributes(font, size, m, d); v != 0 {
			cfRelease(font)
			font = v
		}
		for _, r := range []uintptr{d, attrs, vars, val, tag} {
			cfRelease(r)
		}
		break
	}
	return font
}

// italicize returns the italic face of font's family, or font slanted when
// there is none. It consumes font.
func italicize(font uintptr, size float64) uintptr {
	if font == 0 || ctFontGetSymbolicTraits(font)&ctFontItalicTrait != 0 {
		return font
	}
	it := ctFontCreateCopyWithSymbolicTraits(font, size, nil, ctFontItalicTrait, ctFontItalicTrait)
	if it == 0 {
		it = ctFontCreateCopyWithAttributes(font, size, &obliqueSkew, 0)
	}
	if it == 0 {
		return font
	}
	cfRelease(font)
	return it
}

// familyFaces returns the installed faces of a family, or nil when it is not
// installed. Lookups are cached for the life of the System.
func (s *System) familyFaces(name string) []fontFace {
	k := strings.ToLower(name)
	if faces, ok := s.families[k]; ok {
		return faces
	}
	str := cfString(name)
	attrs := cfDict(kCTFontFamilyNameAttribute, str)
	desc := ctFontDescriptorCreateWithAttributes(attrs)
	key := kCTFontFamilyNameAttribute
	mandatory := cfSetCreate(0, &key, 1, cfTypeSetCallBacks)
	arr := ctFontDescriptorCreateMatchingFontDescriptors(desc, mandatory)
	var faces []fontFace
	n := 0
	if arr != 0 {
		n = cfArrayGetCount(arr)
	}
	for i := range n {
		d := cfArrayGetValueAtIndex(arr, i)
		if !sameFamily(descString(d, kCTFontFamilyNameAttribute), name) {
			continue
		}
		f := describeFace(d)
		f.desc = cfRetain(d)
		faces = append(faces, f)
	}
	for _, r := range []uintptr{arr, mandatory, desc, attrs, str} {
		cfRelease(r)
	}
	s.families[k] = faces
	return faces
}

// setFallback builds the cascade list of families: registered or installed
// faces of the regular weight, tried before the system fallback.
func (s *System) setFallback(families []string) {
	cfRelease(s.cascade)
	s.cascade = 0
	var descs []uintptr
	for _, fam := range families {
		kind, name := classify(fam)
		var faces []fontFace
		switch kind {
		case familyNamed:
			if faces = registeredFaces(name); len(faces) == 0 {
				faces = s.familyFaces(name)
			}
		default:
			continue // the system font is first in the system fallback anyway
		}
		if len(faces) > 0 {
			descs = append(descs, matchFace(faces, 400, false).desc)
		}
	}
	if len(descs) > 0 {
		s.cascade = cfArray(descs)
	}
}
