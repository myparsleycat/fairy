package kit

import (
	"math"
	"slices"
	"unicode/utf8"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/kit/syntax"
)

// Highlight colors a byte range of code: with the theme's color for Kind,
// or with Color when it is set. Highlighting changes color only, never
// weight or style, so it never moves a glyph.
type Highlight struct {
	Start, End int
	Kind       syntax.Kind
	Color      fairy.Color
}

// Highlighter turns code into highlights. A nil *Highlighter is the built-in
// scanners (see package syntax for the languages); anything they don't know,
// and code with no language, stays plain.
//
// Func gets the code's language as written (a fence's first info word, ""
// when it has none) and the code, and returns byte ranges of it. Ranges may
// come in any order; ones out of bounds, inside a character, empty or
// overlapping an earlier one are dropped.
//
// Output is cached per Key, language and code, so highlighters that share a
// Key share entries: give a highlighter whose output depends on anything else
// (a light or dark palette, say) a Key that names it.
type Highlighter struct {
	Key  string
	Func func(lang, code string) []Highlight
}

// BuiltinHighlights returns the built-in scanners' highlights of code,
// uncached: empty for a language they don't know or none. A Highlighter's
// Func can fall back on it.
func BuiltinHighlights(lang, code string) []Highlight {
	toks := syntax.ScanName(lang, code)
	if len(toks) == 0 {
		return nil
	}
	out := make([]Highlight, len(toks))
	for i, t := range toks {
		out[i] = Highlight{Start: t.Start, End: t.End, Kind: t.Kind}
	}
	return out
}

type hlKey struct {
	custom    bool
	key, lang string
	code      string
}

// hlCacheBytes bounds the estimated size of cached highlights. Streaming text
// produces a new source per update, so the cache is dropped when it fills up
// rather than kept in order.
const hlCacheBytes = 4 << 20

var (
	hlCache     = map[hlKey][]Highlight{}
	hlCacheSize int
)

// Highlight returns the highlights of code's first syntax.MaxSource bytes
// (cut at a line end), cached. It is only called on the UI thread.
func (h *Highlighter) Highlight(lang, code string) []Highlight {
	code = code[:syntax.ColoredLen(code)]
	k := hlKey{lang: lang, code: code}
	if h != nil && h.Func != nil {
		k.custom, k.key = true, h.Key
	}
	if out, ok := hlCache[k]; ok {
		return out
	}
	var out []Highlight
	if k.custom {
		out = tidyHighlights(h.Func(lang, code), code)
	} else {
		out = BuiltinHighlights(lang, code)
	}
	size := len(code) + len(lang) + len(k.key) + len(out)*40 + 96
	if hlCacheSize+size > hlCacheBytes {
		clear(hlCache)
		hlCacheSize = 0
	}
	hlCache[k] = out
	hlCacheSize += size
	return out
}

// tidyHighlights sorts an app highlighter's ranges and keeps those in bounds,
// on character boundaries and not overlapping, at most syntax.MaxTokens.
func tidyHighlights(v []Highlight, src string) []Highlight {
	boundary := func(i int) bool { return i == len(src) || utf8.RuneStart(src[i]) }
	v = slices.DeleteFunc(slices.Clone(v), func(h Highlight) bool {
		return h.Start < 0 || h.Start >= h.End || h.End > len(src) || !boundary(h.Start) || !boundary(h.End)
	})
	slices.SortStableFunc(v, func(a, b Highlight) int { return a.Start - b.Start })
	last := 0
	v = slices.DeleteFunc(v, func(h Highlight) bool {
		if h.Start < last {
			return true
		}
		last = h.End
		return false
	})
	return v[:min(len(v), syntax.MaxTokens)]
}

// SyntaxColors holds a color per kind of token.
type SyntaxColors [syntax.KindCount]fairy.Color

// Paint returns the color of h.
func (c *SyntaxColors) Paint(h Highlight) fairy.Color {
	if h.Color.A > 0 {
		return h.Color
	}
	return c[h.Kind]
}

// Spans splits code into color-only spans by its highlights.
func (c *SyntaxColors) Spans(code string, hs []Highlight) []fairy.Span {
	out := make([]fairy.Span, 0, 2*len(hs)+1)
	pos := 0
	for _, h := range hs {
		if h.Start > pos {
			out = append(out, fairy.Span{Text: code[pos:h.Start]})
		}
		out = append(out, fairy.Span{Text: code[h.Start:h.End], Color: c.Paint(h)})
		pos = h.End
	}
	if pos < len(code) {
		out = append(out, fairy.Span{Text: code[pos:]})
	}
	return out
}

// syntaxColors returns muted token colors: the familiar hues (violet
// keywords, green strings, orange numbers, teal types, blue functions, rose
// properties) at one lightness a step from the text's, so contrast follows
// the theme. Comments and punctuation take the quieter text grays, a diff's
// lines the success and danger colors, and parameters the text color.
func syntaxColors(t *Theme) SyntaxColors {
	textL := oklabL(t.Foreground)
	l, chroma := min(max(textL+0.3, 0.38), 0.52), float32(0.12)
	if t.Dark {
		l, chroma = min(max(textL-0.16, 0.62), 0.84), 0.1
	}
	var s SyntaxColors
	for i := range s {
		s[i] = t.Foreground
	}
	s[syntax.Keyword] = oklch(l, chroma, 305)
	s[syntax.String] = oklch(l, chroma, 145)
	s[syntax.Number] = oklch(l, chroma, 60)
	s[syntax.Comment] = alpha(t.MutedForeground, 0.8)
	s[syntax.Type] = oklch(l, chroma, 195)
	s[syntax.Function] = oklch(l, chroma, 255)
	s[syntax.Property] = oklch(l, chroma, 15)
	s[syntax.Punctuation] = t.MutedForeground
	s[syntax.Inserted] = t.Success
	s[syntax.Deleted] = t.Destructive
	return s
}

func srgbToLinear(c float32) float64 {
	v := float64(c)
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func linearToSRGB(v float64) float32 {
	if v <= 0.0031308 {
		v *= 12.92
	} else {
		v = 1.055*math.Pow(v, 1/2.4) - 0.055
	}
	return float32(math.Max(0, math.Min(1, v)))
}

// oklabL returns the OKLab lightness of c.
func oklabL(c fairy.Color) float32 {
	r, g, b := srgbToLinear(c.R), srgbToLinear(c.G), srgbToLinear(c.B)
	l := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	m := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	s := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)
	return float32(0.2104542553*l + 0.7936177850*m - 0.0040720468*s)
}

// oklch returns the opaque sRGB color at OKLCH lightness l, chroma c and hue
// h degrees, clipped to the gamut.
func oklch(l, c, h float32) fairy.Color {
	hr := float64(h) * math.Pi / 180
	L, a, b := float64(l), float64(c)*math.Cos(hr), float64(c)*math.Sin(hr)
	l_ := L + 0.3963377774*a + 0.2158037573*b
	m_ := L - 0.1055613458*a - 0.0638541728*b
	s_ := L - 0.0894841775*a - 1.2914855480*b
	l3, m3, s3 := l_*l_*l_, m_*m_*m_, s_*s_*s_
	return fairy.Color{
		R: linearToSRGB(4.0767416621*l3 - 3.3077115913*m3 + 0.2309699292*s3),
		G: linearToSRGB(-1.2684380046*l3 + 2.6097574011*m3 - 0.3413193965*s3),
		B: linearToSRGB(-0.0041960863*l3 - 0.7034186147*m3 + 1.7076147010*s3),
		A: 1,
	}
}
