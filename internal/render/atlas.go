package render

import (
	"errors"
	"sync/atomic"
)

// atlasPack is the shelf packer and pending uploads of an Atlas, apart from
// its texture. Each backend's Atlas embeds it and has allocTexture, which
// replaces the texture with an empty one and calls reset, and flush, which
// uploads the pending masks and calls flushed.
type atlasPack struct {
	size    int
	shelves []shelf
	nextY   int
	pending []upload
	// clearedAtCap is set when Grow left the atlas empty at its largest size
	// and no Draw has used it since.
	clearedAtCap bool
	// Generation changes whenever the atlas is cleared or replaced; cached
	// tiles from another generation must be re-rasterized.
	Generation int
}

type shelf struct{ y, h, x int }

type upload struct {
	x, y, w, h int
	data       []byte
}

// atlasGenerations numbers atlas contents across all atlases, so tiles cached
// for an atlas that a new renderer replaced are never taken as current.
var atlasGenerations atomic.Int64

// reset empties the packer for a new texture of size texels.
func (a *atlasPack) reset(size int) {
	a.size = size
	a.shelves = a.shelves[:0]
	a.nextY = 0
	a.pending = a.pending[:0]
	a.Generation = int(atlasGenerations.Add(1))
}

// flushed records that the pending masks were uploaded.
func (a *atlasPack) flushed() {
	a.pending = a.pending[:0]
	a.clearedAtCap = false
}

// Size is the atlas edge length in texels.
func (a *atlasPack) Size() int { return a.size }

// Insert reserves space for a w*h coverage mask and queues its upload.
// It returns false when the atlas is full.
func (a *atlasPack) Insert(w, h int, data []byte) (Tile, bool) {
	pw, ph := w+tilePad, h+tilePad
	if pw > a.size || ph > a.size {
		return Tile{}, false
	}
	best := -1
	for i, s := range a.shelves {
		if ph <= s.h && s.x+pw <= a.size && (best < 0 || s.h < a.shelves[best].h) {
			best = i
		}
	}
	if best < 0 {
		if a.nextY+ph > a.size {
			return Tile{}, false
		}
		h := ph
		// Round shelf heights up so glyphs of similar size share shelves.
		h = (h + 3) &^ 3
		if a.nextY+h > a.size {
			h = a.size - a.nextY
		}
		a.shelves = append(a.shelves, shelf{y: a.nextY, h: h})
		a.nextY += h
		best = len(a.shelves) - 1
	}
	s := &a.shelves[best]
	t := Tile{X: s.x, Y: s.y, W: w, H: h}
	s.x += pw
	if w > 0 && h > 0 {
		a.pending = append(a.pending, upload{x: t.X, y: t.Y, w: w, h: h, data: data})
	}
	return t, true
}

// errAtlasFull reports a frame whose glyphs do not fit even an empty atlas of the largest size.
var errAtlasFull = errors.New("render: glyph atlas full")

// Grow clears the atlas and doubles it up to 8192 texels, or just clears it at
// the cap. It fails, leaving the atlas as it is, when it cannot allocate the
// texture or when it already left the atlas empty at the cap since the last
// Draw, as painting the frame again would not fit either. Tiles from before a
// successful Grow are invalid, so the frame must be painted again.
func (a *Atlas) Grow() error {
	size := a.size
	if size < maxAtlasSize {
		size *= 2
	} else if a.clearedAtCap {
		return errAtlasFull
	}
	if err := a.allocTexture(size); err != nil {
		return err
	}
	a.clearedAtCap = size == maxAtlasSize
	return nil
}
