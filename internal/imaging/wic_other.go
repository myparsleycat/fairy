//go:build !windows

package imaging

// Without WIC every format uses the Go decoders.

func wicAccepts(Input) bool      { return false }
func wicWork(Input, Bound) int64 { return 0 }

// DecodeWIC decodes nothing where WIC is absent.
func DecodeWIC(in Input, bound Bound) *Bitmap { return nil }
