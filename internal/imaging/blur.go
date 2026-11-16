package imaging

import "math"

// boxMinSigma is the smallest sigma blurred with three box passes; below it
// the boxes are too narrow to approximate a Gaussian well.
const boxMinSigma = 2

// BlurPixels blurs premultiplied RGBA pixels, so transparent pixels do not
// bleed their color into opaque ones. Pixels past the border repeat the edge.
// From boxMinSigma up, the Gaussian is approximated by three box blurs per
// axis, which costs the same per pixel at any sigma.
func BlurPixels(src []byte, w, h int, sigma float32) []byte {
	if sigma < boxMinSigma {
		return gaussianBlur(src, w, h, sigma)
	}
	return boxBlur(src, w, h, boxRadii(float64(sigma)))
}

func gaussianBlur(src []byte, w, h int, sigma float32) []byte {
	kernel := gaussianKernel(sigma)
	radius := len(kernel) / 2
	// The vertical pass reads only the 2*radius+1 rows around each output
	// row, so the horizontally blurred rows are kept in a ring: row y is at
	// y % rows.
	rows := min(len(kernel), h)
	stride := w * 4
	ring := make([]float32, rows*stride)
	// Each row is converted to float once rather than on every kernel tap.
	row := make([]float32, stride)
	taps := make([]int, len(kernel))
	out := make([]byte, w*h*4)
	next := 0 // the next row to blur horizontally
	for y := range h {
		for ; next <= min(h-1, y+radius); next++ {
			for i, v := range src[next*stride : (next+1)*stride] {
				row[i] = float32(v)
			}
			blurred := ring[next%rows*stride:][:stride]
			for x := range w {
				var sum [4]float32
				for k, weight := range kernel {
					p := min(max(x+k-radius, 0), w-1) * 4
					sum[0] += row[p] * weight
					sum[1] += row[p+1] * weight
					sum[2] += row[p+2] * weight
					sum[3] += row[p+3] * weight
				}
				copy(blurred[x*4:], sum[:])
			}
		}
		for k := range kernel {
			taps[k] = min(max(y+k-radius, 0), h-1) % rows * stride
		}
		dst := out[y*stride:][:stride]
		for x := range w {
			var sum [4]float32
			for k, weight := range kernel {
				p := taps[k] + x*4
				sum[0] += ring[p] * weight
				sum[1] += ring[p+1] * weight
				sum[2] += ring[p+2] * weight
				sum[3] += ring[p+3] * weight
			}
			p := x * 4
			a := clampByte(sum[3])
			dst[p] = min(clampByte(sum[0]), a)
			dst[p+1] = min(clampByte(sum[1]), a)
			dst[p+2] = min(clampByte(sum[2]), a)
			dst[p+3] = a
		}
	}
	return out
}

// BlurWork is the memory BlurPixels holds for a w x h image.
func BlurWork(w, h int, sigma float32) int64 {
	if sigma < boxMinSigma {
		// The output, the rows of the ring and the one being converted, and
		// the kernel with its taps.
		kernel := 2*max(1, math.Ceil(float64(sigma)*3)) + 1
		rows := min(kernel, float64(h))
		n := 4*float64(w)*float64(h) + 16*(rows+1)*float64(w) + 12*kernel
		return int64(min(n, float64(WorkBudget)))
	}
	// The output, the two rows of the horizontal passes, and each vertical
	// pass's ring, running sum and output row.
	radii := boxRadii(float64(sigma))
	ext := boxExtents(radii)
	n := 4*float64(w)*float64(h) + 32*(float64(w)+2*float64(max(ext[1], ext[2])))
	for k, r := range radii {
		n += 16 * float64(w) * (min(2*float64(r)+2, float64(h)+2*float64(ext[k])) + 2)
	}
	return int64(min(n, float64(WorkBudget)))
}

// boxRadii returns the radii of three box blurs whose combined variance is
// closest to sigma squared, from Kovesi's "Fast Almost-Gaussian Filtering".
func boxRadii(sigma float64) [3]int {
	const n = 3
	v := 12 * sigma * sigma
	wl := math.Floor(math.Sqrt(v/n + 1))
	if int64(wl)%2 == 0 {
		wl--
	}
	m := int(min(max(math.Round((v-n*wl*wl-4*n*wl-3*n)/(-4*wl-4)), 0), n))
	var radii [3]int
	for i := range radii {
		size := wl
		if i >= m {
			size += 2
		}
		radii[i] = int(size-1) / 2
	}
	return radii
}

func boxBlur(src []byte, w, h int, radii [3]int) []byte {
	stride := w * 4
	out := make([]byte, w*h*4)
	if w == 0 || h == 0 {
		return out
	}
	ext := boxExtents(radii)
	emit := func(y int, row []float32) {
		dst := out[y*stride:][:stride]
		for p := 0; p < stride; p += 4 {
			a := clampByte(row[p+3])
			dst[p] = min(clampByte(row[p]), a)
			dst[p+1] = min(clampByte(row[p+1]), a)
			dst[p+2] = min(clampByte(row[p+2]), a)
			dst[p+3] = a
		}
	}
	for k := len(radii) - 1; k >= 0; k-- {
		emit = newBoxPass(radii[k], h+2*ext[k], h+2*ext[k+1], ext[k]-ext[k+1], stride, emit).push
	}
	n := (w + 2*max(ext[1], ext[2])) * 4
	a := make([]float32, n)
	b := make([]float32, n)
	for y := range h {
		for i, v := range src[y*stride : (y+1)*stride] {
			a[i] = float32(v)
		}
		boxRow(b, a[:stride], w+2*ext[1], ext[0]-ext[1], radii[0])
		boxRow(a, b[:(w+2*ext[1])*4], w+2*ext[2], ext[1]-ext[2], radii[1])
		boxRow(b, a[:(w+2*ext[2])*4], w, ext[2], radii[2])
		emit(y, b[:stride])
	}
	return out
}

// boxExtents returns how far past each edge the input of each pass, and the
// final output, must extend so the passes together blur the image as if its
// edge pixels repeated forever. The output of pass k is constant beyond the
// sum of the radii so far, and pass k+1 reads no farther than the sum of the
// radii still to come.
func boxExtents(radii [3]int) [4]int {
	return [4]int{0, min(radii[0], radii[1]+radii[2]), min(radii[0]+radii[1], radii[2]), 0}
}

// clampWindow splits the window [a, b] over m values into the count of
// positions before 0, the range [lo, hi] inside, and the count past m-1.
func clampWindow(a, b, m int) (left, lo, hi, right int) {
	lo, hi = max(a, 0), min(b, m-1)
	if lo > hi {
		if b < 0 {
			return b - a + 1, 0, -1, 0
		}
		return 0, 0, -1, b - a + 1
	}
	return lo - a, lo, hi, b - hi
}

// boxRow sets n RGBA values of dst, value j to the mean of src values j+d-r
// through j+d+r, with positions outside src taking its nearest end.
func boxRow(dst, src []float32, n, d, r int) {
	m := len(src) / 4
	last := (m - 1) * 4
	src = src[:last+4]
	dst = dst[:n*4]
	left, lo, hi, right := clampWindow(d-r, d+r, m)
	l, rt := float32(left), float32(right)
	s0 := l*src[0] + rt*src[last]
	s1 := l*src[1] + rt*src[last+1]
	s2 := l*src[2] + rt*src[last+2]
	s3 := l*src[3] + rt*src[last+3]
	for p := lo * 4; p <= hi*4; p += 4 {
		s0 += src[p]
		s1 += src[p+1]
		s2 += src[p+2]
		s3 += src[p+3]
	}
	inv := 1 / float32(2*r+1)
	for j := range n {
		p := j * 4
		dst[p] = s0 * inv
		dst[p+1] = s1 * inv
		dst[p+2] = s2 * inv
		dst[p+3] = s3 * inv
		in := min(max(j+d+r+1, 0), m-1) * 4
		out := min(max(j+d-r, 0), m-1) * 4
		s0 += src[in] - src[out]
		s1 += src[in+1] - src[out+1]
		s2 += src[in+2] - src[out+2]
		s3 += src[in+3] - src[out+3]
	}
}

// boxPass is the vertical counterpart of boxRow. It takes the m input rows in
// order and emits each of the n output rows once the input rows it covers
// have arrived, holding only the rows its running sum still has to add or
// remove.
type boxPass struct {
	r, m, n, d int
	inv        float32
	ring       [][]float32 // input row i is ring[i%len(ring)]
	sum, row   []float32
	next       int // the next output row
	emit       func(y int, row []float32)
}

func newBoxPass(r, m, n, d, stride int, emit func(int, []float32)) *boxPass {
	ring := make([][]float32, min(2*r+2, m))
	rows := make([]float32, len(ring)*stride)
	for i := range ring {
		ring[i] = rows[i*stride:][:stride:stride]
	}
	return &boxPass{r: r, m: m, n: n, d: d, inv: 1 / float32(2*r+1), ring: ring,
		sum: make([]float32, stride), row: make([]float32, stride), emit: emit}
}

func (b *boxPass) at(i int) []float32 {
	return b.ring[min(max(i, 0), b.m-1)%len(b.ring)]
}

func (b *boxPass) push(i int, row []float32) {
	copy(b.ring[i%len(b.ring)], row)
	first := min(max(b.d+b.r, 0), b.m-1)
	if i < first {
		return
	}
	if i == first {
		left, lo, hi, right := clampWindow(b.d-b.r, b.d+b.r, b.m)
		l, r := float32(left), float32(right)
		top, bottom := b.at(0), b.at(b.m-1)
		for p := range b.sum {
			b.sum[p] = l*top[p] + r*bottom[p]
		}
		for j := lo; j <= hi; j++ {
			for p, v := range b.at(j) {
				b.sum[p] += v
			}
		}
	}
	for ; b.next < b.n && min(max(b.next+b.d+b.r, 0), b.m-1) <= i; b.next++ {
		y := b.next
		if y > 0 {
			in, out := b.at(y+b.d+b.r), b.at(y+b.d-b.r-1)
			for p := range b.sum {
				b.sum[p] += in[p] - out[p]
			}
		}
		for p, v := range b.sum {
			b.row[p] = v * b.inv
		}
		b.emit(y, b.row)
	}
}

func gaussianKernel(sigma float32) []float32 {
	radius := max(1, int(math.Ceil(float64(sigma)*3)))
	kernel := make([]float32, radius*2+1)
	var total float32
	for k := range kernel {
		d := float64(k - radius)
		kernel[k] = float32(math.Exp(-d * d / (2 * float64(sigma) * float64(sigma))))
		total += kernel[k]
	}
	for k := range kernel {
		kernel[k] /= total
	}
	return kernel
}

func clampByte(v float32) byte {
	return byte(min(max(v+0.5, 0), 255))
}
