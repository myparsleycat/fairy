package imaging

import "math"

// BlurPixels blurs premultiplied RGBA pixels, so transparent pixels do not
// bleed their color into opaque ones.
func BlurPixels(src []byte, w, h int, sigma float32) []byte {
	kernel := gaussianKernel(sigma)
	n := w * h
	tmp := make([]float32, n*4)
	radius := len(kernel) / 2
	// Rows, from the bytes into tmp. Each row is converted to float once
	// rather than on every kernel tap.
	row := make([]float32, w*4)
	for y := range h {
		base := y * w
		for i, v := range src[base*4 : (base+w)*4] {
			row[i] = float32(v)
		}
		for x := range w {
			var sum [4]float32
			for k, weight := range kernel {
				p := min(max(x+k-radius, 0), w-1) * 4
				sum[0] += row[p] * weight
				sum[1] += row[p+1] * weight
				sum[2] += row[p+2] * weight
				sum[3] += row[p+3] * weight
			}
			copy(tmp[(base+x)*4:], sum[:])
		}
	}
	out := make([]byte, n*4)
	for x := range w {
		for y := range h {
			var sum [4]float32
			for k, weight := range kernel {
				p := (x + min(max(y+k-radius, 0), h-1)*w) * 4
				sum[0] += tmp[p] * weight
				sum[1] += tmp[p+1] * weight
				sum[2] += tmp[p+2] * weight
				sum[3] += tmp[p+3] * weight
			}
			p := (x + y*w) * 4
			a := clampByte(sum[3])
			out[p] = min(clampByte(sum[0]), a)
			out[p+1] = min(clampByte(sum[1]), a)
			out[p+2] = min(clampByte(sum[2]), a)
			out[p+3] = a
		}
	}
	return out
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
