package fairy

import (
	"errors"
	"math"

	"github.com/myparsleycat/fairy/internal/imaging"
)

func blurRequestError(w, h int, sigma float32) error {
	if w < 0 || h < 0 || w == 0 && h == 0 {
		return errors.New("fairy: cached blur needs a nonnegative size with at least one positive axis")
	}
	if !finite(sigma) || sigma > 0 && math.Ceil(float64(sigma)*3) > float64((math.MaxInt/4-1)/2) {
		return errors.New("fairy: blur sigma exceeds the supported range")
	}
	return nil
}

// Blurred loads src within maxW by maxH pixels and blurs its first frame on
// a cache worker. sigma is measured in output pixels. At least one size must
// be positive; zero leaves the other axis unlimited. Nonpositive sigma loads
// without blur. Pending and failed requests return nil; windows update when
// ready. The source decode and blurred result share the cache's pixel budget.
func (c *ImageCache) Blurred(src string, maxW, maxH int, sigma float32, read func() ([]byte, error)) *Image {
	if blurRequestError(maxW, maxH, sigma) != nil {
		return nil
	}
	if sigma <= 0 {
		return c.Load(src, maxW, maxH, read)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.load(imageKey{src: src, Bound: imaging.Bound{W: maxW, H: maxH}, sigma: sigma}, read)
}

// BlurredErr reports an invalid or failed Blurred request with the same arguments.
func (c *ImageCache) BlurredErr(src string, maxW, maxH int, sigma float32) error {
	if err := blurRequestError(maxW, maxH, sigma); err != nil {
		return err
	}
	if sigma <= 0 {
		return c.Err(src, maxW, maxH)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	k := imageKey{src: src, Bound: imaging.Bound{W: maxW, H: maxH}, sigma: sigma}
	c.touchFailure(k)
	return c.failed[k]
}

func (c *ImageCache) decodeBlur(k imageKey, p *imageLoad, read func() ([]byte, error)) (*Image, error) {
	c.mu.Lock()
	if c.loading[k] != p {
		c.mu.Unlock()
		return nil, errImageLoadInvalidated
	}
	base := k
	base.sigma = 0
	src, pending, err := c.request(base, read)
	if pending != nil {
		p.dependency = pending
		if pending.dependents == nil {
			pending.dependents = make(map[*imageLoad]struct{})
		}
		pending.dependents[p] = struct{}{}
	}
	c.mu.Unlock()
	if pending != nil {
		defer func() {
			c.mu.Lock()
			delete(pending.dependents, p)
			p.dependency = nil
			c.mu.Unlock()
		}()
		select {
		case <-p.done:
			return nil, errImageLoadInvalidated
		case <-pending.done:
			src, err = pending.img, pending.err
		}
	}
	if err != nil {
		return nil, err
	}
	if src == nil {
		return nil, errImageLoadInvalidated
	}
	kernel := min(imaging.WorkBudget, int64(2*math.Ceil(float64(k.sigma)*3)+1)*4)
	weight := min(imaging.WorkBudget, src.data.ByteSize()+int64(src.data.W)*int64(src.data.H)*36+kernel)
	var img *Image
	c.withWork(k, p, weight, func() {
		img = wrapImage(&imaging.Bitmap{W: src.data.W, H: src.data.H, Pixels: imaging.BlurPixels(src.data.Pixels, src.data.W, src.data.H, k.sigma)})
	})
	return img, nil
}
