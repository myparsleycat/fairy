package imaging

// orientSwaps reports whether EXIF orientation o swaps width and height.
func orientSwaps(o uint8) bool { return o >= 5 && o <= 8 }

// orientTurns reports whether EXIF orientation o moves any pixel.
func orientTurns(o uint8) bool { return o >= 2 && o <= 8 }

// orientPixels returns w x h RGBA pixels as EXIF orientation o says to show
// them, and the size they then have. Flips and the half turn reuse pix.
func orientPixels(pix []byte, w, h int, o uint8) ([]byte, int, int) {
	swap := func(i, j int) {
		a, b := (*[4]byte)(pix[i*4:]), (*[4]byte)(pix[j*4:])
		*a, *b = *b, *a
	}
	switch o {
	case 2:
		for y := range h {
			for x := range w / 2 {
				swap(y*w+x, y*w+w-1-x)
			}
		}
		return pix, w, h
	case 3:
		for i, n := 0, w*h; i < n/2; i++ {
			swap(i, n-1-i)
		}
		return pix, w, h
	case 4:
		for y := range h / 2 {
			for x := range w {
				swap(y*w+x, (h-1-y)*w+x)
			}
		}
		return pix, w, h
	}
	if !orientTurns(o) {
		return pix, w, h
	}
	t, ow, oh := newOrientWriter(make([]byte, len(pix)), w, h, o)
	for y := range h {
		t.put(y, pix[y*w*4:][:w*4])
	}
	return t.dst, ow, oh
}

// orientWriter writes the rows of a w x h image into dst at the places EXIF
// orientation o turns them to, so a decoder can turn an image as it produces
// it instead of turning a copy afterwards.
type orientWriter struct {
	dst  []byte
	w, h int
	o    uint8
}

// newOrientWriter returns a writer into dst and the size of the turned image.
func newOrientWriter(dst []byte, w, h int, o uint8) (*orientWriter, int, int) {
	if orientSwaps(o) {
		return &orientWriter{dst, w, h, o}, h, w
	}
	return &orientWriter{dst, w, h, o}, w, h
}

// put writes row y of the image as decoded, before it is turned.
func (t *orientWriter) put(y int, row []byte) {
	w, h := t.w, t.h
	// The pixel at x goes to dst pixel at + x*step.
	var at, step int
	switch t.o {
	case 2:
		at, step = y*w+w-1, -1
	case 3:
		at, step = (h-1-y)*w+w-1, -1
	case 4:
		copy(t.dst[(h-1-y)*w*4:][:w*4], row)
		return
	case 5:
		at, step = y, h
	case 6:
		at, step = h-1-y, h
	case 7:
		at, step = (w-1)*h+h-1-y, -h
	case 8:
		at, step = (w-1)*h+y, -h
	default:
		panic("imaging: orientWriter for an orientation that does not turn")
	}
	for x := range w {
		*(*[4]byte)(t.dst[(at+x*step)*4:]) = [4]byte(row[x*4:])
	}
}
