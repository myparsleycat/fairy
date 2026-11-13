//go:build darwin

package render

import (
	"image/color"
	"testing"
)

// An app's texture is cleared, wrapped, copied and read back; the wrapped
// texture follows later changes and the copy keeps its pixels.
func TestExternalTextures(t *testing.T) {
	d := testDevice(t)
	app, err := NewTarget(d, 4, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Release()
	if err := ClearTexture(d, app.MTLTexture(), [4]float32{0, 0.5, 0, 0.5}); err != nil {
		t.Fatal(err)
	}
	wrapped, err := WrapTexture(d, app.MTLTexture())
	if err != nil {
		t.Fatal(err)
	}
	defer wrapped.Release()
	copied, err := CopyTexture(d, app.MTLTexture())
	if err != nil {
		t.Fatal(err)
	}
	defer copied.Release()
	if !copied.CanMip() || wrapped.CanMip() {
		t.Errorf("CanMip: copy %v, wrapped %v", copied.CanMip(), wrapped.CanMip())
	}
	if err := ClearTexture(d, app.MTLTexture(), [4]float32{0, 0, 1, 1}); err != nil {
		t.Fatal(err)
	}
	img, err := ReadTexture(d, copied, true)
	if err != nil {
		t.Fatal(err)
	}
	wantPixel(t, img, 3, 1, color.NRGBA{0, 255, 0, 128}, 1)
	img, err = ReadTexture(d, wrapped, true)
	if err != nil {
		t.Fatal(err)
	}
	wantPixel(t, img, 0, 0, color.NRGBA{0, 0, 255, 255}, 0)

	rgba, err := NewTexture(d, 4, 2, func() []byte {
		p := make([]byte, 4*2*4)
		for i := 0; i < len(p); i += 4 {
			copy(p[i:], []byte{255, 0, 0, 255})
		}
		return p
	}(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer rgba.Release()
	if _, err := CopyToTexture(d, rgba.MTLTexture(), &app.Texture); err == nil {
		t.Error("copy between formats succeeded")
	}
	other, err := NewTarget(d, 4, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Release()
	ClearTexture(d, other.MTLTexture(), [4]float32{1, 0, 0, 1})
	f, err := CopyToTexture(d, other.MTLTexture(), &app.Texture)
	if err != nil {
		t.Fatal(err)
	}
	f.Wait()
	if !f.Done() {
		t.Error("fence not done after Wait")
	}
	f.Release()
	img, err = ReadTexture(d, wrapped, true)
	if err != nil {
		t.Fatal(err)
	}
	wantPixel(t, img, 2, 1, color.NRGBA{255, 0, 0, 255}, 0)
	img, err = ReadTexture(d, rgba, true)
	if err != nil {
		t.Fatal(err)
	}
	wantPixel(t, img, 1, 1, color.NRGBA{255, 0, 0, 255}, 0)

	if _, err := WrapTexture(d, 0); err == nil {
		t.Error("WrapTexture of no texture succeeded")
	}
}
