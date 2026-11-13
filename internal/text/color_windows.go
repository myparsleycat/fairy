package text

import "github.com/myparsleycat/fairy/internal/render"

// SetColorGlyphTextures does nothing on Windows, where DirectWrite draws color
// fonts as layers of single-color glyphs in the atlas.
func (s *System) SetColorGlyphTextures(newTexture func(w, h int, rgba []byte) (*render.Texture, error)) {
}
