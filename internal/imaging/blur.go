package imaging

import "math"

// BlurPixels blurs premultiplied RGBA pixels, so transparent pixels do not
// bleed their color into opaque ones.
func BlurPixels(src []byte, w, h int, sigma float32) []byte {
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

// BlurWork is the memory BlurPixels holds for a w x h image: the output, the
// rows of its ring and the one being converted, and the kernel with its taps.
func BlurWork(w, h int, sigma float32) int64 {
	kernel := 2*max(1, math.Ceil(float64(sigma)*3)) + 1
	rows := min(kernel, float64(h))
	n := 4*float64(w)*float64(h) + 16*(rows+1)*float64(w) + 12*kernel
	return int64(min(n, float64(WorkBudget)))
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
