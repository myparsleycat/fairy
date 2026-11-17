package render

import (
	"errors"
	"image"
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
	// Damage bounds the part of the target the frame drew, empty when nothing
	// changed; the frame drew DamageArea pixels in DamageRects rects.
	// DamageReason says why a frame with damage tracking was drawn whole, and
	// is empty otherwise.
	Damage       image.Rectangle
	DamageArea   int
	DamageRects  int
	DamageReason string
}

// DamageDebug is a diagnostic mode of damage tracking.
type DamageDebug int

const (
	DamageDebugOff DamageDebug = iota
	// DamageDebugShow tints the part of each frame that was redrawn.
	DamageDebugShow
	// DamageDebugVerify draws every frame whole into a reference target as
	// well, and logs when a swap chain buffer, as it comes back for drawing,
	// does not hold the frame presented from it. Present1 is documented to
	// copy the pixels outside the dirty rects from the previous buffer; this
	// checks that it does.
	DamageDebugVerify
)

// Tile is a rectangle inside the atlas.
type Tile struct{ X, Y, W, H int }

const (
	maxAtlasSize = 8192
	tilePad      = 1
)

// Fits reports whether a w*h mask fits an atlas grown to its largest size.
// A mask that does not would make every Insert fail however often it grows.
func Fits(w, h int) bool { return w+tilePad <= maxAtlasSize && h+tilePad <= maxAtlasSize }
