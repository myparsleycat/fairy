//go:build windows

package fairy

import (
	"strings"

	"github.com/myparsleycat/fairy/internal/render"
	"github.com/myparsleycat/fairy/internal/text"
)

// Span is a run of text with its own style inside RichText. Zero fields
// inherit the element's text style.
type Span struct {
	Text       string
	Weight     int
	Italic     bool
	Size       float32
	Font       string
	Color      Color
	Background Color // drawn behind the span's text, as for inline code
	Underline  bool
	Strike     bool
	// OnClick makes the span a link: clicking its text runs it.
	OnClick func()
	// URL is the link's address, written when the span is copied as HTML.
	URL string
}

// RichText is a paragraph of differently styled spans. It wraps like Text.
func RichText(spans ...Span) *El {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	e := Text(b.String())
	e.spans = spans
	for _, s := range spans {
		if s.OnClick != nil {
			e.links = true
		}
	}
	return e
}

// elTextStyle is the text style of a text element, with its spans as runs.
func (w *Window) elTextStyle(e *El) text.Style {
	st := w.textStyle(e.tp)
	if len(e.spans) == 0 {
		return st
	}
	runs := make([]text.Run, 0, len(e.spans))
	pos, colors := 0, 0
	for _, s := range e.spans {
		r := text.Run{Start: pos, End: pos + len(s.Text), Weight: s.Weight, Italic: s.Italic,
			Size: s.Size * w.scale, Family: s.Font, Underline: s.Underline, Strike: s.Strike}
		if s.Color.isSet() {
			colors++
			r.Color = colors
		}
		pos = r.End
		runs = append(runs, r)
	}
	st.Runs = runs
	return st
}

// spanColors lists the span colors in the order elTextStyle numbered them.
func spanColors(e *El, opacity float32) [][4]float32 {
	var out [][4]float32
	for _, s := range e.spans {
		if s.Color.isSet() {
			out = append(out, s.Color.arr(opacity))
		}
	}
	return out
}

// paintRichBackgrounds draws span backgrounds behind the text laid out at (x, y) device pixels.
func (w *Window) paintRichBackgrounds(e *El, l *text.Layout, x, y float32, clip [4]float32, opacity float32) {
	pos := 0
	pad := 2 * w.scale
	for _, s := range e.spans {
		start := pos
		pos += len(s.Text)
		if !s.Background.isSet() || s.Text == "" {
			continue
		}
		for _, r := range l.RangeRects(start, pos) {
			rad := 3 * w.scale
			w.scene.Add(primRect([4]float32{x + r.X - pad, y + r.Y, r.W + 2*pad, r.H}, clip, s.Background.arr(opacity), rad))
		}
	}
}

// linkAt returns the link span under the window point (x, y), or nil.
func (w *Window) linkAt(e *El, x, y float32) *Span {
	if !e.links {
		return nil
	}
	l, ox, oy := w.textLayout(e)
	lx, ly := x*w.scale-ox, y*w.scale-oy
	b := l.HitTest(lx, ly)
	pos := 0
	for i := range e.spans {
		sp := &e.spans[i]
		start := pos
		pos += len(sp.Text)
		if sp.OnClick == nil || b < start || b > pos {
			continue
		}
		for _, r := range l.RangeRects(start, pos) {
			if lx >= r.X && lx < r.X+r.W && ly >= r.Y && ly < r.Y+r.H {
				return sp
			}
		}
	}
	return nil
}

func primRect(bounds, clip, color [4]float32, radius float32) render.Prim {
	return render.Prim{Bounds: bounds, Clip: clip, Color: color, Radii: [4]float32{radius, radius, radius, radius}, Kind: render.KindRect}
}

// RegisterFont makes the font families in a TTF, OTF or TTC file usable by
// name with Font and Options.Font, alongside the installed fonts. It returns the
// family names found in the file. Register fonts before opening windows; open
// windows use them from their next update.
func RegisterFont(data []byte) ([]string, error) {
	names, err := text.RegisterFont(data)
	if err == nil {
		// Memoized views keep their laid-out text sizes.
		InvalidateMemos()
	}
	return names, err
}

// SetFontFallback makes text try these installed families, in order, for
// characters the requested font lacks, before the system fallback. A browser
// does the same with the generic family at the end of a font-family list,
// which can resolve to a different font than the system fallback. Families
// that are not installed are skipped; no families restores the system fallback.
func SetFontFallback(families ...string) {
	text.SetFallbackFamilies(families)
	InvalidateMemos()
}
