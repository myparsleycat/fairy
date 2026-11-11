package fairy

import "github.com/myparsleycat/fairy/internal/text"

// RegisterFont makes the font families in a TTF, OTF or TTC file usable by
// name with Font and Options.Font, alongside the installed fonts. It returns the
// family names found in the file. Register fonts before opening windows; open
// windows use them from their next update.
func RegisterFont(data []byte) ([]string, error) {
	return text.RegisterFont(data)
}

// SetFontFallback makes text try these installed families, in order, for
// characters the requested font lacks, before the system fallback. A browser
// does the same with the generic family at the end of a font-family list,
// which can resolve to a different font than the system fallback. Families
// that are not installed are skipped; no families restores the system fallback.
func SetFontFallback(families ...string) {
	text.SetFallbackFamilies(families)
}
