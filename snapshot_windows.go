package fairy

import (
	"errors"
	"image"
	"image/png"
	"os"
)

// SaveSnapshot writes the next rendered frame to a PNG file and calls done with the result.
func (w *Window) SaveSnapshot(path string, done func(error)) {
	if w.r == nil {
		if done != nil {
			w.Post(func() { done(errors.New("fairy: snapshot: no renderer while the GPU device is being recreated")) })
		}
		return
	}
	w.r.RequestSnapshot(func(img *image.NRGBA) {
		err := writePNG(path, img)
		if done != nil {
			w.Post(func() { done(err) })
		}
	})
	w.invalidate()
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
