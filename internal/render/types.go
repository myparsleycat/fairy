package render

import (
	"errors"
	"time"
)

// Verbose logs the selected driver, feature level, flags and adapter when a
// device is created. The fairy package sets it from FAIRY_DEBUG_FRAMES and
// FAIRY_DEBUG_GPU.
var Verbose bool

// ErrDeviceLost reports that the GPU device was removed or reset.
var ErrDeviceLost = errors.New("render: device lost")

// Stats is how the last Draw went.
type Stats struct {
	Record   time.Duration // building and submitting the draw calls
	Present  time.Duration // Present, which waits for a free buffer
	Batches  int           // draw calls
	GPU      time.Duration
	GPUFrame uint64 // renderer submission number of the earlier sample
	GPUValid bool
}

// Tile is a rectangle inside the atlas.
type Tile struct{ X, Y, W, H int }

const (
	maxAtlasSize = 8192
	tilePad      = 1
)

// Fits reports whether a w*h mask fits an atlas grown to its largest size.
// A mask that does not would make every Insert fail however often it grows.
func Fits(w, h int) bool { return w+tilePad <= maxAtlasSize && h+tilePad <= maxAtlasSize }
