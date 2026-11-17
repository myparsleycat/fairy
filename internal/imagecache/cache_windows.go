package imagecache

import (
	"github.com/myparsleycat/fairy/internal/imaging"
	"github.com/myparsleycat/fairy/internal/win"
)

// nativeDecoder returns the decoder a cache tries before the Go decoders
// while native decoders are on.
func nativeDecoder() func(imaging.Input, imaging.Bound) *imaging.Bitmap {
	return imaging.DecodeWIC
}

// screenPixels returns the area of all attached monitors in device pixels.
func screenPixels() int64 { return win.ScreenPixels() }
