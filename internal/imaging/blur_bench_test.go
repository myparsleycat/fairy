package imaging

import "testing"

func BenchmarkBlurPixels(b *testing.B) {
	const w, h = 1920, 1080
	src := make([]byte, w*h*4)
	for i := range src {
		src[i] = byte(i * 131 >> 3)
	}
	for i := 3; i < len(src); i += 4 {
		src[i] = 255
	}
	b.SetBytes(int64(len(src)))
	for b.Loop() {
		BlurPixels(src, w, h, 40)
	}
}
