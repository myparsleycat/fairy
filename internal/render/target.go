//go:build windows

package render

import (
	"github.com/myparsleycat/fairy/d3d"
)

// Target is a BGRA texture that can be rendered into and drawn as an image.
type Target struct {
	Texture
	rtv d3d.RenderTargetView
}

// NewTarget creates a width x height render target on d.
func NewTarget(d *Device, width, height int) (*Target, error) {
	tex, err := d.Dev.CreateTexture2D(&d3d.Texture2DDesc{
		Width:      uint32(width),
		Height:     uint32(height),
		MipLevels:  1,
		ArraySize:  1,
		Format:     d3d.FORMAT_B8G8R8A8_UNORM,
		SampleDesc: d3d.SampleDesc{Count: 1},
		Usage:      d3d.USAGE_DEFAULT,
		BindFlags:  d3d.BIND_SHADER_RESOURCE | d3d.BIND_RENDER_TARGET,
	}, nil)
	if err != nil {
		return nil, err
	}
	t := &Target{Texture: Texture{tex: tex, Width: width, Height: height}}
	if t.srv, err = d.Dev.CreateShaderResourceView(tex, nil); err != nil {
		t.Release()
		return nil, err
	}
	if t.rtv, err = d.Dev.CreateRenderTargetView(tex, nil); err != nil {
		t.Release()
		return nil, err
	}
	return t, nil
}

// View returns the render target view.
func (t *Target) View() d3d.RenderTargetView { return t.rtv }

func (t *Target) Release() {
	if t == nil {
		return
	}
	t.rtv.Release()
	t.rtv = d3d.RenderTargetView{}
	t.Texture.Release()
}
