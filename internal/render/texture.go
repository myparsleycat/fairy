//go:build windows

package render

import (
	"errors"
	"runtime"
	"sync"
	"unsafe"

	"github.com/myparsleycat/fairy/d3d"
)

// Texture is an image on the GPU. Textures from NewTexture hold
// premultiplied alpha, as Premultiplied says; wrapped app textures may hold
// either. The image prim's FlagPremultiplied tells the shader which.
type Texture struct {
	mu            sync.RWMutex
	tex           d3d.Texture2D
	srv           d3d.ShaderResourceView
	Width, Height int
	Format        uint32
	Premultiplied bool
	owned         bool // made here, so EnsureMips may replace it
	mips          bool
}

// NewTexture is NewTexture on the renderer's device.
func (r *Renderer) NewTexture(width, height int, rgba []byte, mutable bool) (*Texture, error) {
	return NewTexture(r.ds, width, height, rgba, mutable)
}

// NewTexture uploads width*height premultiplied RGBA pixels on d. A mutable
// texture can be rewritten with Update.
func NewTexture(d *Device, width, height int, rgba []byte, mutable bool) (*Texture, error) {
	desc := d3d.Texture2DDesc{
		Width:      uint32(width),
		Height:     uint32(height),
		MipLevels:  1,
		ArraySize:  1,
		Format:     d3d.FORMAT_R8G8B8A8_UNORM,
		SampleDesc: d3d.SampleDesc{Count: 1},
		Usage:      d3d.USAGE_IMMUTABLE,
		BindFlags:  d3d.BIND_SHADER_RESOURCE,
	}
	if mutable {
		desc.Usage = d3d.USAGE_DEFAULT
	}
	init := d3d.SubresourceData{SysMem: unsafe.Pointer(&rgba[0]), Pitch: uint32(width * 4)}
	tex, err := d.Dev.CreateTexture2D(&desc, &init)
	runtime.KeepAlive(rgba)
	if err != nil {
		return nil, err
	}
	srv, err := d.Dev.CreateShaderResourceView(tex, nil)
	if err != nil {
		tex.Release()
		return nil, err
	}
	return &Texture{tex: tex, srv: srv, Width: width, Height: height, Format: desc.Format, Premultiplied: true, owned: true}, nil
}

// WrapTexture shares an app's texture: it takes a reference to tex and
// makes a shader resource view of it. The texture must have been made on d
// with BIND_SHADER_RESOURCE.
func WrapTexture(d *Device, tex d3d.Texture2D) (*Texture, error) {
	desc := tex.Desc()
	srv, err := d.Dev.CreateShaderResourceView(tex, nil)
	if err != nil {
		return nil, err
	}
	tex.AddRef()
	return &Texture{tex: tex, srv: srv, Width: int(desc.Width), Height: int(desc.Height), Format: desc.Format}, nil
}

// CopyTexture copies tex into a new texture of the same size and format
// that can be sampled. Hold the device lock around it.
func CopyTexture(d *Device, tex d3d.Texture2D) (*Texture, error) {
	desc := tex.Desc()
	if desc.SampleDesc.Count > 1 {
		return nil, errors.New("render: copy of a multisampled texture")
	}
	out, err := d.Dev.CreateTexture2D(&d3d.Texture2DDesc{
		Width:      desc.Width,
		Height:     desc.Height,
		MipLevels:  1,
		ArraySize:  1,
		Format:     desc.Format,
		SampleDesc: d3d.SampleDesc{Count: 1},
		Usage:      d3d.USAGE_DEFAULT,
		BindFlags:  d3d.BIND_SHADER_RESOURCE,
	}, nil)
	if err != nil {
		return nil, err
	}
	d.Ctx.CopySubresourceRegion(out, 0, 0, 0, 0, tex, 0, nil)
	srv, err := d.Dev.CreateShaderResourceView(out, nil)
	if err != nil {
		out.Release()
		return nil, err
	}
	return &Texture{tex: out, srv: srv, Width: int(desc.Width), Height: int(desc.Height), Format: desc.Format, owned: true}, nil
}

// Update replaces the pixels of a mutable texture with premultiplied RGBA.
// Hold the device lock around it.
func (t *Texture) Update(d *Device, rgba []byte) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	ctx := d.Ctx
	ctx.UpdateSubresource(t.tex, nil, unsafe.Pointer(&rgba[0]), uint32(t.Width*4), 0)
	runtime.KeepAlive(rgba)
	if t.mips {
		ctx.GenerateMips(t.srv)
	}
}

// CanMip reports whether EnsureMips can give the texture mipmaps: it was
// made by NewTexture or CopyTexture in an 8-bit RGBA format.
func (t *Texture) CanMip() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.canMip()
}

func (t *Texture) canMip() bool {
	return t.tex.P != nil && t.owned && !t.mips && (t.Format == d3d.FORMAT_R8G8B8A8_UNORM || t.Format == d3d.FORMAT_B8G8R8A8_UNORM)
}

// ByteSize reports the texel storage, including generated mip levels.
func (t *Texture) ByteSize() int64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.tex.P == nil {
		return 0
	}
	return textureBytes(t.Width, t.Height, t.mips)
}

// MipWork reports the base copy and lower-level writes needed for promotion.
func (t *Texture) MipWork() int64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if !t.canMip() {
		return 0
	}
	return textureBytes(t.Width, t.Height, true)
}

// EnsureMips replaces the texture with a copy that has a full mip chain, so
// that it can be drawn reduced to half its size or less without aliasing.
// Prims already holding the texture draw the copy.
func (t *Texture) EnsureMips(d *Device) error {
	_, err := t.PromoteMips(d)
	return err
}

// PromoteMips is EnsureMips that also returns the bytes it wrote, or 0 when
// the texture already had mips or cannot have them.
func (t *Texture) PromoteMips(d *Device) (int64, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	d.Lock()
	defer d.Unlock()
	// Draw also takes the device before reading a texture's resource fields.
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.canMip() {
		return 0, nil
	}
	tex, err := d.Dev.CreateTexture2D(&d3d.Texture2DDesc{
		Width:      uint32(t.Width),
		Height:     uint32(t.Height),
		ArraySize:  1,
		Format:     t.Format,
		SampleDesc: d3d.SampleDesc{Count: 1},
		Usage:      d3d.USAGE_DEFAULT,
		BindFlags:  d3d.BIND_SHADER_RESOURCE | d3d.BIND_RENDER_TARGET,
		MiscFlags:  d3d.RESOURCE_MISC_GENERATE_MIPS,
	}, nil)
	if err != nil {
		return 0, err
	}
	srv, err := d.Dev.CreateShaderResourceView(tex, nil)
	if err != nil {
		tex.Release()
		return 0, err
	}
	d.Ctx.CopySubresourceRegion(tex, 0, 0, 0, 0, t.tex, 0, nil)
	d.Ctx.GenerateMips(srv)
	t.srv.Release()
	t.tex.Release()
	t.tex, t.srv, t.mips = tex, srv, true
	return textureBytes(t.Width, t.Height, true), nil
}

// Resource returns the ID3D11Texture2D.
func (t *Texture) Resource() d3d.Texture2D {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.tex
}

func (t *Texture) shaderView() d3d.ShaderResourceView {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.srv
}

func (t *Texture) Release() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.srv.Release()
	t.tex.Release()
	t.srv, t.tex = d3d.ShaderResourceView{}, d3d.Texture2D{}
}

// Atlas is a single-channel glyph texture filled with a shelf packer.
type Atlas struct {
	atlasPack
	r   *Renderer
	tex d3d.Texture2D
	srv d3d.ShaderResourceView
}

func newAtlas(r *Renderer, size int) (*Atlas, error) {
	a := &Atlas{r: r}
	return a, a.allocTexture(size)
}

// allocTexture replaces the texture with an empty one of size texels. On
// failure the atlas keeps its texture and contents.
func (a *Atlas) allocTexture(size int) error {
	desc := d3d.Texture2DDesc{
		Width:      uint32(size),
		Height:     uint32(size),
		MipLevels:  1,
		ArraySize:  1,
		Format:     d3d.FORMAT_R8_UNORM,
		SampleDesc: d3d.SampleDesc{Count: 1},
		Usage:      d3d.USAGE_DEFAULT,
		BindFlags:  d3d.BIND_SHADER_RESOURCE,
	}
	tex, err := a.r.dev.CreateTexture2D(&desc, nil)
	if err != nil {
		return err
	}
	srv, err := a.r.dev.CreateShaderResourceView(tex, nil)
	if err != nil {
		tex.Release()
		return err
	}
	a.release()
	a.tex, a.srv = tex, srv
	a.reset(size)
	return nil
}

func (a *Atlas) flush() {
	for _, u := range a.pending {
		box := d3d.Box{Left: uint32(u.x), Top: uint32(u.y), Front: 0, Right: uint32(u.x + u.w), Bottom: uint32(u.y + u.h), Back: 1}
		a.r.ctx.UpdateSubresource(a.tex, &box, unsafe.Pointer(&u.data[0]), uint32(u.w), 0)
	}
	a.flushed()
}

func (a *Atlas) release() {
	a.srv.Release()
	a.tex.Release()
	a.srv, a.tex = d3d.ShaderResourceView{}, d3d.Texture2D{}
}
