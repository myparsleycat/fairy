package imaging

import "math"

// BlurPixels blurs premultiplied RGBA pixels, so transparent pixels do not
// bleed their color into opaque ones.
func BlurPixels(src []byte, w, h int, sigma float32) []byte {
	kernel := gaussianKernel(sigma)
	n := w * h
	premul := make([]float32, n*4)
	for i, v := range src[:n*4] {
		premul[i] = float32(v)
	}

	tmp := make([]float32, n*4)
	blurPass(premul, tmp, w, h, kernel, 1, w)
	blurPass(tmp, premul, h, w, kernel, w, 1)

	out := make([]byte, n*4)
	for p := range n {
		a := clampByte(premul[p*4+3])
		for c := range 3 {
			out[p*4+c] = min(clampByte(premul[p*4+c]), a)
		}
		out[p*4+3] = a
	}
	return out
}

// blurPass convolves each of lines lines of length samples, whose pixels are
// step apart and whose starts are stride apart, clamping at the ends.
func blurPass(src, dst []float32, length, lines int, kernel []float32, step, stride int) {
	radius := len(kernel) / 2
	for line := range lines {
		base := line * stride
		for x := range length {
			var sum [4]float32
			for k, weight := range kernel {
				sx := min(max(x+k-radius, 0), length-1)
				p := (base + sx*step) * 4
				sum[0] += src[p] * weight
				sum[1] += src[p+1] * weight
				sum[2] += src[p+2] * weight
				sum[3] += src[p+3] * weight
			}
			copy(dst[(base+x*step)*4:], sum[:])
		}
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
