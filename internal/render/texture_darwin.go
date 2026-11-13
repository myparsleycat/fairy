//go:build darwin

package render

import (
	"errors"
	"runtime"
	"sync"

	"github.com/myparsleycat/fairy/internal/darwin"
)

// Texture is an image on the GPU. Textures from NewTexture hold
// premultiplied alpha, as Premultiplied says. The image prim's
// FlagPremultiplied tells the shader which.
type Texture struct {
	mu            sync.RWMutex
	tex           id // id<MTLTexture>
	Width, Height int
	Format        uint32 // MTLPixelFormat
	Premultiplied bool
	owned         bool // made here, so EnsureMips may replace it
	mips          bool
}

// NewTexture is NewTexture on the renderer's device.
func (r *Renderer) NewTexture(width, height int, rgba []byte, mutable bool) (*Texture, error) {
	return NewTexture(r.ds, width, height, rgba, mutable)
}

// NewTexture uploads width*height premultiplied RGBA pixels on d. A mutable
// texture can be rewritten with Update; on Metal every texture can.
func NewTexture(d *Device, width, height int, rgba []byte, mutable bool) (*Texture, error) {
	if len(rgba) < width*height*4 {
		return nil, errors.New("render: texture data too short")
	}
	tex, err := newTexture2D(d.dev, pixelRGBA8Unorm, width, height, false, usageShaderRead, storagePrivate)
	if err != nil {
		return nil, err
	}
	if err := uploadTexture(d, tex, 0, 0, width, height, rgba, width*4, false); err != nil {
		darwin.Release(tex)
		return nil, err
	}
	return &Texture{tex: tex, Width: width, Height: height, Format: pixelRGBA8Unorm, Premultiplied: true, owned: true}, nil
}

// Update replaces the pixels of a texture with premultiplied RGBA. The copy
// runs on the GPU after the frames already drawn, so they show the old
// pixels. Hold the device lock around it.
func (t *Texture) Update(d *Device, rgba []byte) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.tex == 0 || len(rgba) < t.Width*t.Height*4 {
		return
	}
	uploadTexture(d, t.tex, 0, 0, t.Width, t.Height, rgba, t.Width*4, t.mips)
}

// CanMip reports whether EnsureMips can give the texture mipmaps: it was
// made by NewTexture in an 8-bit RGBA format.
func (t *Texture) CanMip() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.canMip()
}

func (t *Texture) canMip() bool {
	return t.tex != 0 && t.owned && !t.mips && (t.Format == pixelRGBA8Unorm || t.Format == pixelBGRA8Unorm)
}

// ByteSize reports the texel storage, including generated mip levels.
func (t *Texture) ByteSize() int64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.tex == 0 {
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
	tex, err := newTexture2D(d.dev, int(t.Format), t.Width, t.Height, true, usageShaderRead, storagePrivate)
	if err != nil {
		return 0, err
	}
	withPool(func() {
		cb := darwin.Send(d.queue, "commandBuffer")
		if cb == 0 {
			err = errors.New("render: could not create a command buffer")
			return
		}
		blit := darwin.Send(cb, "blitCommandEncoder")
		blit.Send(darwin.Sel("copyFromTexture:sourceSlice:sourceLevel:toTexture:destinationSlice:destinationLevel:sliceCount:levelCount:"),
			t.tex, uint64(0), uint64(0), tex, uint64(0), uint64(0), uint64(1), uint64(1))
		blit.Send(darwin.Sel("generateMipmapsForTexture:"), tex)
		blit.Send(darwin.Sel("endEncoding"))
		cb.Send(darwin.Sel("commit"))
	})
	if err != nil {
		darwin.Release(tex)
		return 0, err
	}
	// The command buffer keeps the old texture until the copy ran.
	darwin.Release(t.tex)
	t.tex, t.mips = tex, true
	return textureBytes(t.Width, t.Height, true), nil
}

// resource returns the id<MTLTexture>.
func (t *Texture) resource() id {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.tex
}

// MTLTexture returns the texture's id<MTLTexture>, for drawing into it or
// sampling it with Metal directly.
func (t *Texture) MTLTexture() uintptr { return uintptr(t.resource()) }

func (t *Texture) Release() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	darwin.Release(t.tex)
	t.tex = 0
}

// Target is a BGRA texture that can be rendered into and drawn as an image.
type Target struct {
	Texture
}

// NewTarget creates a width x height render target on d.
func NewTarget(d *Device, width, height int) (*Target, error) {
	tex, err := newTexture2D(d.dev, pixelBGRA8Unorm, width, height, false, usageShaderRead|usageRenderTarget, storagePrivate)
	if err != nil {
		return nil, err
	}
	return &Target{Texture: Texture{tex: tex, Width: width, Height: height, Format: pixelBGRA8Unorm}}, nil
}

func (t *Target) Release() {
	if t == nil {
		return
	}
	t.Texture.Release()
}

// Atlas is a single-channel glyph texture filled with a shelf packer.
type Atlas struct {
	atlasPack
	r   *Renderer
	tex id
	// fresh is set while the texture has not been cleared: Metal leaves
	// new textures undefined, and glyphs need the padding between tiles empty.
	fresh bool
}

func newAtlas(r *Renderer, size int) (*Atlas, error) {
	a := &Atlas{r: r}
	return a, a.allocTexture(size)
}

// allocTexture replaces the texture with an empty one of size texels. On
// failure the atlas keeps its texture and contents.
func (a *Atlas) allocTexture(size int) error {
	tex, err := newTexture2D(a.r.ds.dev, pixelR8Unorm, size, size, false, usageShaderRead|usageRenderTarget, storagePrivate)
	if err != nil {
		return err
	}
	a.release()
	a.tex, a.fresh = tex, true
	a.reset(size)
	return nil
}

// stagingSize is the bytes flush stages for the pending masks.
func (a *Atlas) stagingSize() int {
	n := 0
	for _, u := range a.pending {
		n += align(u.w, 4) * u.h
	}
	return n
}

// flush encodes into the renderer's command buffer the clear of a new
// texture and the upload of the pending masks, copied to stage, which
// starts at offset in buf.
func (a *Atlas) flush(cb, buf id, stage []byte, offset int) {
	if a.fresh {
		pass := clearPass(a.tex, [4]float32{})
		darwin.Send(cb, "renderCommandEncoderWithDescriptor:", pass).Send(darwin.Sel("endEncoding"))
		a.fresh = false
	}
	if len(a.pending) > 0 {
		blit := darwin.Send(cb, "blitCommandEncoder")
		at := 0
		for _, u := range a.pending {
			row := align(u.w, 4)
			for y := range u.h {
				copy(stage[at+y*row:at+y*row+u.w], u.data[y*u.w:])
			}
			blitUpload(blit, buf, offset+at, row, a.tex, u.x, u.y, u.w, u.h)
			at += row * u.h
		}
		blit.Send(darwin.Sel("endEncoding"))
	}
	a.flushed()
}

func (a *Atlas) release() {
	darwin.Release(a.tex)
	a.tex = 0
}

// clearPass returns a render pass descriptor that clears tex to c,
// premultiplied, and stores it.
func clearPass(tex id, c [4]float32) id {
	pass := darwin.SendClass("MTLRenderPassDescriptor", "renderPassDescriptor")
	ca := darwin.Send(darwin.Send(pass, "colorAttachments"), "objectAtIndexedSubscript:", uint64(0))
	ca.Send(darwin.Sel("setTexture:"), tex)
	ca.Send(darwin.Sel("setLoadAction:"), uint64(loadActionClear))
	ca.Send(darwin.Sel("setStoreAction:"), uint64(storeActionStore))
	ca.Send(darwin.Sel("setClearColor:"), mtlClearColor{float64(c[0]), float64(c[1]), float64(c[2]), float64(c[3])})
	return pass
}

func align(n, a int) int { return (n + a - 1) / a * a }
