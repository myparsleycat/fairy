//go:build windows

package render

import (
	"image"
	"runtime"

	"github.com/myparsleycat/fairy/d3d"
)

// RequestSnapshot makes the next Draw read back the frame before presenting it.
func (r *Renderer) RequestSnapshot(f func(*image.NRGBA)) { r.snapshot = f }

// captureBackBuffer reads the frame as an opaque image, as the screen shows it
// over an opaque window.
func (r *Renderer) captureBackBuffer() (*image.NRGBA, error) {
	var buf d3d.Texture2D
	if r.target != nil {
		buf = r.target.tex
	} else {
		var err error
		if buf, err = r.swap.buffer(); err != nil {
			return nil, err
		}
		defer buf.Release()
	}
	img, err := r.ctx.ReadImage(buf, false)
	if err != nil {
		return nil, err
	}
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	return img, nil
}

// ReadTarget reads an offscreen renderer's target as straight alpha.
func (r *Renderer) ReadTarget() (*image.NRGBA, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r.ds.Lock()
	defer r.ds.Unlock()
	return r.ctx.ReadImage(r.target.tex, true)
}
