//go:build darwin

package render

import (
	"errors"
	"image"
	"runtime"
)

// RequestSnapshot makes the next Draw read back the frame before presenting it.
func (r *Renderer) RequestSnapshot(f func(*image.NRGBA)) { r.snapshot = f }

// ReadTarget reads an offscreen renderer's target as straight alpha. It
// waits for the frames drawn before.
func (r *Renderer) ReadTarget() (*image.NRGBA, error) {
	if r.target == nil {
		return nil, errors.New("render: ReadTarget needs an offscreen renderer")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r.ds.Lock()
	defer r.ds.Unlock()
	t := r.target
	pix, err := readTexture(r.ds, t.tex, t.Width, t.Height)
	if err != nil {
		return nil, err
	}
	img := image.NewNRGBA(image.Rect(0, 0, t.Width, t.Height))
	toNRGBA(img.Pix, pix, true)
	return img, nil
}
