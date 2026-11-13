//go:build !windows && !darwin

package text

import (
	"errors"

	"github.com/myparsleycat/fairy/internal/render"
)

var errUnsupported = errors.New("text: no text backend for this platform yet")

func RegisterFont(data []byte) ([]string, error) { return nil, errUnsupported }
func SetFallbackFamilies(families []string)      {}

// System owns the layout cache and the glyph cache.
type System struct{}

func NewSystem(atlas *render.Atlas) (*System, error) { return nil, errUnsupported }

func (s *System) Layout(str string, st Style, width float32) *Layout {
	return &Layout{text: str}
}
func (s *System) Measure(str string, st Style, maxWidth float32) (w, h float32) { return 0, 0 }
func (s *System) BeginBuild()                                                   {}
func (s *System) EndBuild()                                                     {}
func (s *System) BeginPass(owner uint64)                                        {}
func (s *System) EndPass()                                                      {}
func (s *System) EndFrame()                                                     {}
func (s *System) ResetAtlas(a *render.Atlas)                                    {}
func (s *System) Release()                                                      {}
func (s *System) SetColorGlyphTextures(newTexture func(w, h int, rgba []byte) (*render.Texture, error)) {
}

// Layout is laid-out text.
type Layout struct{ text string }

func (l *Layout) Size() (float32, float32)                        { return 0, 0 }
func (l *Layout) Lines() int                                      { return 0 }
func (l *Layout) LineMetrics() []LineMetrics                      { return nil }
func (l *Layout) HitTest(x, y float32) int                        { return 0 }
func (l *Layout) LineRange(b int) (start, end int, upstream bool) { return 0, len(l.text), false }
func (l *Layout) Caret(b int) (x, y, h float32)                   { return 0, 0, 0 }
func (l *Layout) CaretAt(b int, upstream bool) (x, y, h float32)  { return 0, 0, 0 }
func (l *Layout) RangeRects(a, b int) []Rect                      { return nil }
func (l *Layout) Text() string                                    { return l.text }
func (l *Layout) Draw(scene *render.Scene, x, y float32, color [4]float32, clip [4]float32) bool {
	return true
}
func (l *Layout) DrawRuns(scene *render.Scene, x, y float32, base [4]float32, colors [][4]float32, clip [4]float32) bool {
	return true
}
