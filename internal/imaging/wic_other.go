//go:build !windows

package imaging

// Without WIC every format uses the Go decoders.

func wicAccepts(Input) bool      { return false }
func wicWork(Input, Bound) int64 { return 0 }
