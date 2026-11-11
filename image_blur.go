package fairy

import (
	"math"
	"slices"
	"sync"
	"weak"

	"github.com/myparsleycat/fairy/internal/imaging"
)

type imageBlurs struct {
	mu    sync.Mutex
	m     map[float32]weak.Pointer[Image]
	order []float32
}

// Blurred returns i blurred by a Gaussian with a standard deviation of sigma
// pixels, extending the edge pixels past the border so the edges do not fade.
// The result is computed on the calling goroutine and weakly memoized with
// i, so blur a small image, such as one loaded scaled down to 64 pixels, and
// draw it stretched with FitFill. Animated images blur their first frame.
// An image an ImageCache loaded without a size limit is decoded at its
// natural size for this. Images without pixels in memory return nil.
// Results belong to the caller, outside ImageCache budgets. Use ImageCache.Blurred
// to bound the output size and run the operation on a cache worker.
func (i *Image) Blurred(sigma float32) *Image {
	if i == nil || i.data.Pixels == nil && i.lazy == nil {
		return nil
	}
	if sigma <= 0 {
		return i
	}
	if !finite(sigma) || math.Ceil(float64(sigma)*3) > float64((math.MaxInt/4-1)/2) {
		return nil
	}
	i.blurs.mu.Lock()
	defer i.blurs.mu.Unlock()
	if b := i.blurs.m[sigma].Value(); b != nil {
		at := slices.Index(i.blurs.order, sigma)
		i.blurs.order = append(slices.Delete(i.blurs.order, at, at+1), sigma)
		return b
	}
	src := i
	if i.lazy != nil {
		full, err := i.lazy.decodeFull()
		if err != nil {
			return nil
		}
		src = full
	}
	b := wrapImage(&imaging.Bitmap{W: src.data.W, H: src.data.H, Pixels: imaging.BlurPixels(src.data.Pixels, src.data.W, src.data.H, sigma)})
	if i.blurs.m == nil {
		i.blurs.m = map[float32]weak.Pointer[Image]{}
	}
	for _, key := range slices.Clone(i.blurs.order) {
		if key == sigma || i.blurs.m[key].Value() == nil {
			delete(i.blurs.m, key)
			at := slices.Index(i.blurs.order, key)
			i.blurs.order = slices.Delete(i.blurs.order, at, at+1)
		}
	}
	if len(i.blurs.order) == 16 {
		delete(i.blurs.m, i.blurs.order[0])
		i.blurs.order = i.blurs.order[1:]
	}
	i.blurs.m[sigma] = weak.Make(b)
	i.blurs.order = append(i.blurs.order, sigma)
	return b
}
