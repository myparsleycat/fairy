package imaging

// orientSwaps reports whether EXIF orientation o swaps width and height.
func orientSwaps(o uint8) bool { return o >= 5 && o <= 8 }

// orientPixels returns w x h RGBA pixels as EXIF orientation o says to show
// them, and the size they then have.
func orientPixels(pix []byte, w, h int, o uint8) ([]byte, int, int) {
	if o <= 1 || o > 8 {
		return pix, w, h
	}
	ow, oh := w, h
	if orientSwaps(o) {
		ow, oh = h, w
	}
	out := make([]byte, len(pix))
	for y := range oh {
		for x := range ow {
			var sx, sy int
			switch o {
			case 2:
				sx, sy = w-1-x, y
			case 3:
				sx, sy = w-1-x, h-1-y
			case 4:
				sx, sy = x, h-1-y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, h-1-x
			case 7:
				sx, sy = w-1-y, h-1-x
			case 8:
				sx, sy = w-1-y, x
			}
			copy(out[(y*ow+x)*4:][:4], pix[(sy*w+sx)*4:][:4])
		}
	}
	return out, ow, oh
}
