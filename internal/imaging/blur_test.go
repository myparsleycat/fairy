package imaging

import (
	"fmt"
	"testing"
)

// The box approximation must stay close to the Gaussian it replaces, keep
// uniform images and premultiplied invariants exact, and handle images
// smaller than its boxes.
func TestBoxBlurApproximatesGaussian(t *testing.T) {
	for _, tc := range []struct {
		w, h  int
		sigma float32
	}{
		{64, 48, 2}, {64, 48, 5.5}, {37, 23, 13}, {5, 90, 9}, {1, 1, 40}, {3, 1, 2.5}, {1, 7, 30},
	} {
		t.Run(fmt.Sprintf("%dx%d sigma %v", tc.w, tc.h, tc.sigma), func(t *testing.T) {
			// Blocks of flat color with transparent holes, like icons and
			// photos scaled down for a backdrop.
			src := make([]byte, tc.w*tc.h*4)
			for y := range tc.h {
				for x := range tc.w {
					p := (y*tc.w + x) * 4
					block := x/6*7 + y/5*13
					if block%5 == 0 {
						continue
					}
					a := 120 + block*53%136
					src[p] = byte(a * (block * 97 % 256) / 255)
					src[p+1] = byte(a * (block * 41 % 256) / 255)
					src[p+2] = byte(a * (block * 173 % 256) / 255)
					src[p+3] = byte(a)
				}
			}
			got := BlurPixels(src, tc.w, tc.h, tc.sigma)
			want := gaussianBlur(src, tc.w, tc.h, tc.sigma)
			worst := 0
			for p := range got {
				worst = max(worst, abs(int(got[p])-int(want[p])))
				if p%4 == 3 && (got[p-3] > got[p] || got[p-2] > got[p] || got[p-1] > got[p]) {
					t.Fatalf("pixel %d color %v exceeds alpha", p/4, got[p-3:p+1])
				}
			}
			if worst > 10 {
				t.Fatalf("box blur differs from the Gaussian by %d", worst)
			}

			uniform := make([]byte, len(src))
			for p := 0; p < len(uniform); p += 4 {
				copy(uniform[p:], []byte{90, 40, 200, 230})
			}
			if got := BlurPixels(uniform, tc.w, tc.h, tc.sigma); string(got) != string(uniform) {
				t.Fatal("uniform image changed")
			}
		})
	}
}

func abs(v int) int { return max(v, -v) }
