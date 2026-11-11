package fairy

import (
	"image"
	"sync"
	"weak"

	"github.com/myparsleycat/fairy/internal/imaging"
)

// lazyImage decodes an image at the size it is drawn, as browsers decode to
// scale: the cache keeps one decode per drawn size, rounded by sizeStep.
type lazyImage struct {
	cache *ImageCache
	src   string
	read  func() ([]byte, error)

	mu    sync.Mutex
	shown weak.Pointer[Image] // the decode drawn last
}

func newLazyImage(c *ImageCache, src string, read func() ([]byte, error), in imaging.Input) (*Image, error) {
	w, h := in.Config.Width, in.Config.Height
	if in.Animation.Count > 0 {
		w, h = in.Animation.W, in.Animation.H
	}
	if w <= 0 || h <= 0 {
		return nil, image.ErrFormat
	}
	return &Image{id: imageSeq.Add(1), data: imaging.Bitmap{W: w, H: h}, lazy: &lazyImage{cache: c, src: src, read: read}}, nil
}

// variant returns the image decoded for bound, starting the decode when
// needed. Until it is ready, it returns the decode drawn last if that is
// still in memory.
func (l *lazyImage) variant(natW, natH int, bound imaging.Bound) *Image {
	if v, _, _ := l.requestVariant(natW, natH, bound); v != nil {
		l.show(v)
		return v
	}
	return l.last()
}

// show records v as the decode drawn last.
func (l *lazyImage) show(v *Image) {
	l.mu.Lock()
	l.shown = weak.Make(v)
	l.mu.Unlock()
}

func (l *lazyImage) requestVariant(natW, natH int, bound imaging.Bound) (*Image, *imageLoad, error) {
	if dw, dh := bound.Size(natW, natH); dw == natW && dh == natH {
		bound = imaging.Bound{W: natW, H: natH}
	}
	c := l.cache
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.request(imageKey{src: l.src, Bound: bound}, l.read)
}

func (l *lazyImage) last() *Image {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.shown.Value()
}

// decodeFull decodes the image at its natural size, for callers that need
// the pixels rather than a texture.
func (l *lazyImage) decodeFull() (*Image, error) {
	data, err := l.read()
	if err != nil {
		return nil, err
	}
	in, err := imaging.Inspect(data)
	if err != nil {
		return nil, err
	}
	l.cache.mu.Lock()
	wic := l.cache.wic
	l.cache.mu.Unlock()
	img, _, err := in.DecodePixels(imaging.Bound{}, 0, 0, true, wic, nil)
	return wrapImage(img), err
}
