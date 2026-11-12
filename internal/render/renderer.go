//go:build windows

// Package render draws a Scene with Direct3D 11 into a window swap chain or
// an offscreen texture.
package render

import (
	"errors"
	"fmt"
	"image"
	"log"
	"math"
	"runtime"
	"unsafe"

	"github.com/myparsleycat/fairy/d3d"
	"github.com/myparsleycat/fairy/internal/win"
)

const (
	backBufferFormat = d3d.FORMAT_B8G8R8A8_UNORM
	bufferCount      = 2
)

type globals struct {
	Viewport          [2]float32
	AtlasSize         [2]float32
	GammaRatios       [4]float32
	GrayscaleContrast float32
	_                 [3]float32
}

type batchParams struct {
	Start  uint32
	_      uint32
	Origin [2]float32
}

// Renderer owns the swap chain, or an offscreen target, and pipeline state
// for one window. The device is shared.
type Renderer struct {
	stats      Stats
	timer      *gpuTimer
	submission uint64
	ds         *Device
	dev        d3d.Device
	ctx        d3d.Context
	factory    unsafe.Pointer
	hwnd       uintptr
	swap       swapChain
	comp       *composition // DirectComposition tree when the swap chain has alpha
	target     *Target      // offscreen renderers draw here instead of a swap chain
	rtv        d3d.RenderTargetView

	vs         d3d.VertexShader
	ps         d3d.PixelShader
	blend      d3d.BlendState
	blendErase d3d.BlendState // scales the target by one less the prim's alpha
	blendAdd   d3d.BlendState
	raster     d3d.RasterizerState
	sampler    d3d.SamplerState
	globalsBuf d3d.Buffer
	batchBuf   d3d.Buffer
	prims      structBuf
	clips      structBuf
	frame      []Prim // the scene's prims and this frame's blur passes
	plan       []layerPlan
	pool       targetPool
	cur        renderTarget
	glob       globals
	emptyImage *Texture

	Atlas *Atlas

	width, height int
	snapshot      func(*image.NRGBA)
	gamma         [4]float32
	contrast      float32
	occluded      bool
}

// New creates a renderer for hwnd with an initial back buffer size in device
// pixels. With transparent, the swap chain carries premultiplied alpha
// through DirectComposition, so the window's system backdrop shows where the
// frame is not opaque.
func New(hwnd uintptr, width, height int, transparent bool) (*Renderer, error) {
	r := &Renderer{width: max(width, 1), height: max(height, 1), hwnd: hwnd}
	var err error
	if r.ds, err = AcquireDevice(); err != nil {
		return nil, err
	}
	r.dev, r.ctx, r.factory = r.ds.Dev, r.ds.Ctx, r.ds.factory
	if err = r.createSwapChain(transparent); err != nil {
		r.Release()
		return nil, err
	}
	if err = r.init(); err != nil {
		r.Release()
		return nil, err
	}
	return r, nil
}

// NewOffscreen creates a renderer that draws into a width x height texture,
// read back with ReadTarget.
func NewOffscreen(width, height int) (*Renderer, error) {
	r := &Renderer{width: max(width, 1), height: max(height, 1)}
	var err error
	if r.ds, err = AcquireDevice(); err != nil {
		return nil, err
	}
	r.dev, r.ctx, r.factory = r.ds.Dev, r.ds.Ctx, r.ds.factory
	if r.target, err = NewTarget(r.ds, r.width, r.height); err != nil {
		r.Release()
		return nil, err
	}
	r.rtv = r.target.rtv
	if err = r.init(); err != nil {
		r.Release()
		return nil, err
	}
	return r, nil
}

func (r *Renderer) init() error {
	var err error
	if err = r.createPipeline(); err != nil {
		return err
	}
	if r.Atlas, err = newAtlas(r, 2048); err != nil {
		return err
	}
	if r.emptyImage, err = r.NewTexture(1, 1, []byte{0, 0, 0, 0}, false); err != nil {
		return err
	}
	r.gamma = gammaRatios(1.8)
	r.contrast = 1.0
	return nil
}

func (r *Renderer) createSwapChain(transparent bool) error {
	desc := swapChainDesc1{
		Width:       uint32(r.width),
		Height:      uint32(r.height),
		Format:      backBufferFormat,
		SampleDesc:  d3d.SampleDesc{Count: 1},
		BufferUsage: dxgiUsageRenderTargetOutput,
		BufferCount: bufferCount,
		Scaling:     dxgiScalingStretch,
		SwapEffect:  dxgiSwapEffectFlipDiscard,
		AlphaMode:   dxgiAlphaModeIgnore,
	}
	var err error
	if transparent {
		desc.AlphaMode = dxgiAlphaModePremultiplied
		if r.swap, err = createSwapChainForComposition(r.factory, r.dev, &desc); err != nil {
			return err
		}
		if r.comp, err = newComposition(r.dev, r.hwnd, r.swap); err != nil {
			r.swap.release()
			r.swap = swapChain{}
			return err
		}
	} else if r.swap, err = createSwapChainForHwnd(r.factory, r.dev, r.hwnd, &desc); err != nil {
		return err
	}
	if err := r.createTarget(); err != nil {
		r.comp.release()
		r.comp = nil
		r.swap.release()
		r.swap = swapChain{}
		return err
	}
	return nil
}

// Transparent reports whether the swap chain carries alpha.
func (r *Renderer) Transparent() bool { return r.comp != nil }

// SetTransparent replaces the swap chain with one that has or lacks alpha.
// When the transparent one cannot be made, the renderer keeps an opaque one
// and returns the error.
func (r *Renderer) SetTransparent(on bool) error {
	if r.target != nil || on == (r.comp != nil) {
		return nil
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r.ds.Lock()
	defer r.ds.Unlock()
	r.ctx.OMSetRenderTargets(nil, d3d.DepthStencilView{})
	r.rtv.Release()
	r.rtv = d3d.RenderTargetView{}
	r.comp.release()
	r.comp = nil
	r.swap.release()
	r.swap = swapChain{}
	r.ctx.ClearState()
	r.ctx.Flush()
	err := r.createSwapChain(on)
	if err != nil && on {
		if err2 := r.createSwapChain(false); err2 != nil {
			return errors.Join(err, err2)
		}
	}
	return err
}

func (r *Renderer) createTarget() error {
	buf, err := r.swap.buffer()
	if err != nil {
		return err
	}
	defer buf.Release()
	r.rtv, err = r.dev.CreateRenderTargetView(buf, nil)
	return err
}

func (r *Renderer) createPipeline() error {
	vsCode, psCode, err := shaders()
	if err != nil {
		return err
	}
	if r.vs, err = r.dev.CreateVertexShader(vsCode); err != nil {
		return err
	}
	if r.ps, err = r.dev.CreatePixelShader(psCode); err != nil {
		return err
	}
	bd := d3d.BlendDesc{}
	bd.RenderTarget[0] = d3d.RenderTargetBlendDesc{
		BlendEnable:           1,
		SrcBlend:              d3d.BLEND_ONE,
		DestBlend:             d3d.BLEND_INV_SRC_ALPHA,
		BlendOp:               d3d.BLEND_OP_ADD,
		SrcBlendAlpha:         d3d.BLEND_ONE,
		DestBlendAlpha:        d3d.BLEND_INV_SRC_ALPHA,
		BlendOpAlpha:          d3d.BLEND_OP_ADD,
		RenderTargetWriteMask: d3d.COLOR_WRITE_ALL,
	}
	if r.blend, err = r.dev.CreateBlendState(&bd); err != nil {
		return err
	}
	erase := bd
	erase.RenderTarget[0].SrcBlend, erase.RenderTarget[0].SrcBlendAlpha = d3d.BLEND_ZERO, d3d.BLEND_ZERO
	if r.blendErase, err = r.dev.CreateBlendState(&erase); err != nil {
		return err
	}
	add := bd
	add.RenderTarget[0].DestBlend, add.RenderTarget[0].DestBlendAlpha = d3d.BLEND_ONE, d3d.BLEND_ONE
	if r.blendAdd, err = r.dev.CreateBlendState(&add); err != nil {
		return err
	}
	rd := d3d.RasterizerDesc{FillMode: d3d.FILL_SOLID, CullMode: d3d.CULL_NONE, DepthClipEnable: 1}
	if r.raster, err = r.dev.CreateRasterizerState(&rd); err != nil {
		return err
	}
	sd := d3d.SamplerDesc{
		Filter:   d3d.FILTER_MIN_MAG_MIP_LINEAR,
		AddressU: d3d.TEXTURE_ADDRESS_CLAMP,
		AddressV: d3d.TEXTURE_ADDRESS_CLAMP,
		AddressW: d3d.TEXTURE_ADDRESS_CLAMP,
		MaxLOD:   math.MaxFloat32,
	}
	if r.sampler, err = r.dev.CreateSamplerState(&sd); err != nil {
		return err
	}
	if r.globalsBuf, err = r.dev.CreateBuffer(&d3d.BufferDesc{
		ByteWidth: uint32(unsafe.Sizeof(globals{})),
		Usage:     d3d.USAGE_DEFAULT,
		BindFlags: d3d.BIND_CONSTANT_BUFFER,
	}, nil); err != nil {
		return err
	}
	r.batchBuf, err = r.dev.CreateBuffer(&d3d.BufferDesc{
		ByteWidth: uint32(unsafe.Sizeof(batchParams{})),
		Usage:     d3d.USAGE_DEFAULT,
		BindFlags: d3d.BIND_CONSTANT_BUFFER,
	}, nil)
	return err
}

// structBuf is a dynamic structured buffer the shaders read.
type structBuf struct {
	buf d3d.Buffer
	srv d3d.ShaderResourceView
	cap int
}

func (b *structBuf) release() {
	b.srv.Release()
	b.buf.Release()
	*b = structBuf{}
}

// uploadStruct copies items into b, growing it first if needed.
func uploadStruct[T any](r *Renderer, b *structBuf, items []T) error {
	stride := int(unsafe.Sizeof(*new(T)))
	n := max(len(items), 1)
	if n > b.cap {
		c := max(b.cap, 256)
		for c < n {
			c *= 2
		}
		b.release()
		buf, err := r.dev.CreateBuffer(&d3d.BufferDesc{
			ByteWidth:           uint32(c * stride),
			Usage:               d3d.USAGE_DYNAMIC,
			BindFlags:           d3d.BIND_SHADER_RESOURCE,
			CPUAccessFlags:      d3d.CPU_ACCESS_WRITE,
			MiscFlags:           d3d.RESOURCE_MISC_BUFFER_STRUCTURED,
			StructureByteStride: uint32(stride),
		}, nil)
		if err != nil {
			return err
		}
		srv, err := r.dev.CreateShaderResourceView(buf, &d3d.SRVDesc{
			Format:        d3d.FORMAT_UNKNOWN,
			ViewDimension: d3d.SRV_DIMENSION_BUFFER,
			U:             [4]uint32{0, uint32(c)},
		})
		if err != nil {
			buf.Release()
			return err
		}
		*b = structBuf{buf, srv, c}
	}
	if len(items) == 0 {
		return nil
	}
	m, err := r.ctx.Map(b.buf, d3d.MAP_WRITE_DISCARD)
	if err != nil {
		return err
	}
	copy(unsafe.Slice((*T)(m.Data), len(items)), items)
	r.ctx.Unmap(b.buf)
	return nil
}

// Device returns the shared device the renderer draws with.
func (r *Renderer) Device() *Device { return r.ds }

// Size returns the back buffer size in device pixels.
func (r *Renderer) Size() (int, int) { return r.width, r.height }

// Resize resizes the swap chain buffers.
func (r *Renderer) Resize(width, height int) error {
	width, height = max(width, 1), max(height, 1)
	if width == r.width && height == r.height || r.target != nil {
		return nil
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r.ds.Lock()
	defer r.ds.Unlock()
	r.ctx.OMSetRenderTargets(nil, d3d.DepthStencilView{})
	r.rtv.Release()
	r.rtv = d3d.RenderTargetView{}
	r.ctx.Flush()
	err := r.swap.resizeBuffers(bufferCount, uint32(width), uint32(height), backBufferFormat, 0)
	if err == nil {
		err = r.createTarget()
	}
	if err != nil {
		// Leave no size recorded so the next Resize retries instead of
		// returning early with no render target.
		r.width, r.height = 0, 0
		if r.ds.Lost() {
			return ErrDeviceLost
		}
		return err
	}
	r.width, r.height = width, height
	return nil
}

// HasTarget reports whether the renderer can draw; false after a failed Resize.
func (r *Renderer) HasTarget() bool { return r.rtv.P != nil }

// errNoTarget reports a Draw while a failed Resize left no render target.
var errNoTarget = errors.New("render: no render target")

// Draw renders the scene and presents it, or leaves it in the offscreen target.
func (r *Renderer) Draw(s *Scene, clear [4]float32) error {
	r.stats.GPU, r.stats.GPUFrame, r.stats.GPUValid = 0, 0, false
	if r.rtv.P == nil {
		return errNoTarget
	}
	r.submission++
	t0 := win.Ticks()
	if err := r.draw(s, clear); err != nil {
		r.stats.GPUValid = false
		if d3d.DeviceLost(err) {
			r.ds.MarkLost()
		}
		if r.ds.Lost() {
			return ErrDeviceLost
		}
		return err
	}
	t1 := win.Ticks()
	r.stats.Record = t1 - t0
	r.stats.Present = 0
	if r.target != nil {
		if r.ds.Lost() {
			return ErrDeviceLost
		}
		return nil
	}
	hr := r.swap.present(1, 0)
	r.stats.Present = win.Ticks() - t1
	r.occluded = uint32(hr) == dxgiStatusOccluded
	if hr.Failed() {
		if uint32(hr) == d3d.DXGI_ERROR_DEVICE_REMOVED || uint32(hr) == d3d.DXGI_ERROR_DEVICE_RESET {
			r.ds.MarkLost()
			return ErrDeviceLost
		}
		return fmt.Errorf("present: %w", hr)
	}
	return nil
}

// draw records the frame while holding the device lock, so app threads using
// the shared context (UseDevice) cannot change pipeline state halfway.
func (r *Renderer) draw(s *Scene, clear [4]float32) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r.ds.Lock()
	defer r.ds.Unlock()
	ctx := r.ctx
	if r.timer != nil {
		sample, err := r.timer.collect()
		if err != nil {
			if err := r.timingError(err); err != nil {
				return err
			}
		} else {
			r.stats.GPU, r.stats.GPUFrame, r.stats.GPUValid = sample.duration, sample.frame, sample.valid
		}
	}
	ctx.OMSetRenderTargets([]d3d.RenderTargetView{r.rtv}, d3d.DepthStencilView{})
	timer, slot := r.timer, -1
	if timer != nil {
		var err error
		if slot, err = timer.start(r.submission); err != nil {
			if err := r.timingError(err); err != nil {
				return err
			}
		}
	}
	defer func() {
		if slot >= 0 {
			timer.finish(slot)
			timer.slots[slot].discard = true
		}
	}()
	r.Atlas.flush()
	ctx.ClearRenderTargetView(r.rtv, [4]float32{clear[0] * clear[3], clear[1] * clear[3], clear[2] * clear[3], clear[3]})
	ctx.RSSetViewports(d3d.Viewport{Width: float32(r.width), Height: float32(r.height), MaxDepth: 1})

	if len(s.Prims) > 0 {
		r.frame = append(r.frame[:0], s.Prims...)
		defer r.pool.endFrame()
		top, err := r.planLayers(s)
		if err != nil {
			return err
		}
		if err := uploadStruct(r, &r.prims, r.frame); err != nil {
			return err
		}
		if err := uploadStruct(r, &r.clips, s.Clips); err != nil {
			return err
		}

		r.glob = globals{
			AtlasSize:         [2]float32{float32(r.Atlas.size), float32(r.Atlas.size)},
			GammaRatios:       r.gamma,
			GrayscaleContrast: r.contrast,
		}

		ctx.IASetInputLayout(d3d.InputLayout{})
		ctx.IASetPrimitiveTopology(d3d.PRIMITIVE_TOPOLOGY_TRIANGLESTRIP)
		ctx.VSSetShader(r.vs)
		ctx.PSSetShader(r.ps)
		ctx.VSSetConstantBuffers(0, r.globalsBuf, r.batchBuf)
		ctx.PSSetConstantBuffers(0, r.globalsBuf, r.batchBuf)
		ctx.VSSetShaderResources(0, r.prims.srv)
		ctx.PSSetShaderResources(0, r.prims.srv, r.Atlas.srv)
		ctx.PSSetShaderResources(3, r.clips.srv)
		ctx.PSSetSamplers(0, r.sampler)
		ctx.OMSetBlendState(r.blend, [4]float32{}, 0xFFFFFFFF)
		ctx.RSSetState(r.raster)

		r.stats.Batches = 0
		r.setTarget(renderTarget{rtv: r.rtv, tex: r.backBuffer, w: r.width, h: r.height})
		r.drawRange(s, 0, len(s.Prims), top)
	}

	if slot >= 0 {
		timer.finish(slot)
		slot = -1
	}
	if f := r.snapshot; f != nil {
		r.snapshot = nil
		if img, err := r.captureBackBuffer(); err == nil {
			f(img)
		}
	}
	ctx.OMSetRenderTargets(nil, d3d.DepthStencilView{})
	return nil
}

// backBuffer returns the texture the window draws into, and a function
// that lets it go.
func (r *Renderer) backBuffer() (d3d.Texture2D, func()) {
	if r.target != nil {
		return r.target.tex, func() {}
	}
	buf, err := r.swap.buffer()
	if err != nil {
		return d3d.Texture2D{}, func() {}
	}
	return buf, func() { buf.Release() }
}

// SetGPUTiming enables asynchronous timing of renderer commands, excluding
// Present, readback and work submitted by the app before Draw. Call between draws.
func (r *Renderer) SetGPUTiming(enabled bool) {
	if r.timer != nil {
		r.timer.release()
		r.timer = nil
	}
	if enabled {
		r.timer = &gpuTimer{backend: deviceQueries{r.dev, r.ctx}}
	}
	r.stats.GPU, r.stats.GPUFrame, r.stats.GPUValid = 0, 0, false
}

func (r *Renderer) timingError(err error) error {
	if d3d.DeviceLost(err) {
		return err
	}
	r.SetGPUTiming(false)
	log.Printf("fairy: GPU timing disabled: %v", err)
	return nil
}

// Stats returns how the last Draw went.
func (r *Renderer) Stats() Stats { return r.stats }

// Occluded reports whether the last Present found the window invisible, in
// which case it returned without waiting for vblank.
func (r *Renderer) Occluded() bool { return r.occluded }

func (r *Renderer) drawBatch(start, end int) {
	if end <= start {
		return
	}
	r.stats.Batches++
	b := batchParams{Start: uint32(start), Origin: r.cur.origin}
	r.ctx.UpdateSubresource(r.batchBuf, nil, unsafe.Pointer(&b), 0, 0)
	r.ctx.DrawInstanced(4, uint32(end-start), 0, 0)
}

// Release frees all GPU resources.
func (r *Renderer) Release() {
	r.SetGPUTiming(false)
	if r.Atlas != nil {
		r.Atlas.release()
	}
	if r.emptyImage != nil {
		r.emptyImage.Release()
	}
	r.prims.release()
	r.clips.release()
	r.pool.release()
	r.batchBuf.Release()
	r.globalsBuf.Release()
	r.sampler.Release()
	r.raster.Release()
	r.blend.Release()
	r.blendErase.Release()
	r.blendAdd.Release()
	r.ps.Release()
	r.vs.Release()
	if r.target != nil {
		r.target.Release()
	} else {
		r.rtv.Release()
	}
	r.comp.release()
	r.swap.release()
	if r.ds != nil {
		r.ds.Release()
	}
	*r = Renderer{}
}

// gammaRatios ports DirectWrite's gamma correction table as used by Windows Terminal and GPUI.
func gammaRatios(gamma float32) [4]float32 {
	table := [13][4]float32{
		{0.0000 / 4, 0.0000 / 4, 0.0000 / 4, 0.0000 / 4},
		{0.0166 / 4, -0.0807 / 4, 0.2227 / 4, -0.0751 / 4},
		{0.0350 / 4, -0.1760 / 4, 0.4325 / 4, -0.1370 / 4},
		{0.0543 / 4, -0.2821 / 4, 0.6302 / 4, -0.1876 / 4},
		{0.0739 / 4, -0.3963 / 4, 0.8167 / 4, -0.2287 / 4},
		{0.0933 / 4, -0.5161 / 4, 0.9926 / 4, -0.2616 / 4},
		{0.1121 / 4, -0.6395 / 4, 1.1588 / 4, -0.2877 / 4},
		{0.1300 / 4, -0.7649 / 4, 1.3159 / 4, -0.3080 / 4},
		{0.1469 / 4, -0.8911 / 4, 1.4644 / 4, -0.3234 / 4},
		{0.1627 / 4, -1.0170 / 4, 1.6051 / 4, -0.3347 / 4},
		{0.1773 / 4, -1.1420 / 4, 1.7385 / 4, -0.3426 / 4},
		{0.1908 / 4, -1.2652 / 4, 1.8650 / 4, -0.3476 / 4},
		{0.2031 / 4, -1.3864 / 4, 1.9851 / 4, -0.3501 / 4},
	}
	const norm13 = float32(float64(0x10000) / (255.0 * 255.0) * 4.0)
	const norm24 = float32(float64(0x100) / 255.0 * 4.0)
	idx := int(math.Round(float64(gamma*10))) - 10
	idx = min(max(idx, 0), 12)
	t := table[idx]
	return [4]float32{t[0] * norm13, t[1] * norm24, t[2] * norm13, t[3] * norm24}
}
