//go:build !windows && !darwin

package imaging

// Without a platform image decoder every format uses the Go decoders.

func nativeAccepts(Input) bool      { return false }
func nativeWork(Input, Bound) int64 { return 0 }

// DecodeNative decodes nothing where the platform has no image decoder.
func DecodeNative(in Input, bound Bound) *Bitmap { return nil }
