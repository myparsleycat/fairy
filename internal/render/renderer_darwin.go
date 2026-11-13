//go:build darwin

package render

import (
	"errors"
	"image"
	"runtime"
	"time"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/darwin"
)

// globals and batchParams match the Globals and Batch structs of shaders.metal.
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

const (
	primSize = int(unsafe.Sizeof(Prim{}))
	clipSize = int(unsafe.Sizeof(ClipRec{}))
)

// frameSlots is how many frames the CPU may record ahead of the GPU; each
// has its own upload buffer, which the CPU writes only once the GPU is done.
const frameSlots = 3

type frameSlot struct {
	cb  id // the last command buffer that read buf, retained until it is done
	buf id // shared buffer of atlas uploads, prims and clips
	cap int
}

// Renderer owns a CAMetalLayer's drawables, or an offscreen target, and the
// per-frame state for one window. The device is shared.
type Renderer struct {
	stats       Stats
	timer       *gpuTimer
	submission  uint64
	ds          *Device
	pipes       *pipelines
	layer       id      // the CAMetalLayer the window shows, or 0 offscreen
	target      *Target // offscreen renderers draw here instead of a drawable
	transparent bool

	slots [frameSlots]frameSlot
	slot  int

	frame      []Prim // the scene's prims and this frame's blur passes
	plan       []layerPlan
	pool       targetPool
	cur        renderTarget
	glob       globals
	emptyImage *Texture

	// Encoding state while draw runs.
	cb, enc            id // the frame's command buffer and the open render encoder, or 0
	buf                id
	primsOff, clipsOff int
	clearPending       bool // the next encoder on cur clears it to clearColor
	clearColor         [4]float32
	blend              int
	image              id // the texture bound as the image, or the one the next encoder binds

	Atlas *Atlas

	width, height int
	snapshot      func(*image.NRGBA)
	gamma         [4]float32
	contrast      float32
	occluded      bool
}

// renderTarget is where draws go.
type renderTarget struct {
	tex    id
	w, h   int
	origin [2]float32
}

// New creates a renderer for a CAMetalLayer, whose address is view, with a
// drawable size of width x height device pixels. The window attaches the
// layer to its view and sets its contentsScale; New sets its device, pixel
// format, color space, drawable size and opacity. With transparent, the
// layer is not opaque and composites its premultiplied alpha over what is
// behind the window.
func New(view uintptr, width, height int, transparent bool) (*Renderer, error) {
	if view == 0 {
		return nil, errors.New("render: no CAMetalLayer")
	}
	layer := id(view)
	var isLayer bool
	withPool(func() { isLayer = darwin.Get[bool](layer, "isKindOfClass:", darwin.Cls("CAMetalLayer")) })
	if !isLayer {
		return nil, errors.New("render: view is not a CAMetalLayer")
	}
	r := &Renderer{width: max(width, 1), height: max(height, 1)}
	var err error
	if r.ds, err = AcquireDevice(); err != nil {
		return nil, err
	}
	r.layer = darwin.Retain(layer)
	withPool(func() { r.configureLayer(transparent) })
	if err = r.init(); err != nil {
		r.Release()
		return nil, err
	}
	return r, nil
}

func (r *Renderer) configureLayer(transparent bool) {
	l := r.layer
	l.Send(darwin.Sel("setDevice:"), r.ds.dev)
	l.Send(darwin.Sel("setPixelFormat:"), uint64(pixelBGRA8Unorm))
	// Backdrop blurs and snapshots copy from the drawable.
	l.Send(darwin.Sel("setFramebufferOnly:"), false)
	l.Send(darwin.Sel("setDrawableSize:"), darwin.CGSize{W: float64(r.width), H: float64(r.height)})
	l.Send(darwin.Sel("setOpaque:"), !transparent)
	l.Send(darwin.Sel("setDisplaySyncEnabled:"), true)
	l.Send(darwin.Sel("setMaximumDrawableCount:"), uint64(3))
	// The frame holds sRGB values, as a Windows swap chain does, which macOS
	// converts to the display's color space as for other sRGB content.
	if cs := cgColorSpaceCreateWithName(uintptr(darwin.SymbolID(darwin.CoreGraphics, "kCGColorSpaceSRGB"))); cs != 0 {
		l.Send(darwin.Sel("setColorspace:"), cs)
		cgColorSpaceRelease(cs)
	}
	r.transparent = transparent
}

// NewOffscreen creates a renderer that draws into a width x height texture,
// read back with ReadTarget.
func NewOffscreen(width, height int) (*Renderer, error) {
	r := &Renderer{width: max(width, 1), height: max(height, 1)}
	var err error
	if r.ds, err = AcquireDevice(); err != nil {
		return nil, err
	}
	if r.target, err = NewTarget(r.ds, r.width, r.height); err != nil {
		r.Release()
		return nil, err
	}
	if err = r.init(); err != nil {
		r.Release()
		return nil, err
	}
	return r, nil
}

func (r *Renderer) init() error {
	var err error
	if r.pipes, err = r.ds.pipelines(); err != nil {
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

// Transparent reports whether the layer carries alpha.
func (r *Renderer) Transparent() bool { return r.transparent }

// SetTransparent makes the layer composite its alpha, or makes it opaque.
func (r *Renderer) SetTransparent(on bool) error {
	if r.target != nil || on == r.transparent {
		return nil
	}
	r.layer.Send(darwin.Sel("setOpaque:"), !on)
	r.transparent = on
	return nil
}

// Device returns the shared device the renderer draws with.
func (r *Renderer) Device() *Device { return r.ds }

// Size returns the drawable size in device pixels.
func (r *Renderer) Size() (int, int) { return r.width, r.height }

// Resize sets the layer's drawable size. Call it on the main thread when
// the view's size or backing scale changes.
func (r *Renderer) Resize(width, height int) error {
	width, height = max(width, 1), max(height, 1)
	if width == r.width && height == r.height || r.target != nil {
		return nil
	}
	r.layer.Send(darwin.Sel("setDrawableSize:"), darwin.CGSize{W: float64(width), H: float64(height)})
	r.width, r.height = width, height
	return nil
}

// HasTarget reports whether the renderer can draw.
func (r *Renderer) HasTarget() bool { return r.target != nil || r.layer != 0 }

// errNoTarget reports a Draw on a renderer that was released.
var errNoTarget = errors.New("render: no render target")

// Draw renders the scene and presents it, or leaves it in the offscreen target.
func (r *Renderer) Draw(s *Scene, clear [4]float32) error {
	r.stats.GPU, r.stats.GPUFrame, r.stats.GPUValid = 0, 0, false
	if !r.HasTarget() {
		return errNoTarget
	}
	r.submission++
	t0 := time.Now()
	wait, err := r.draw(s, clear)
	if err != nil {
		r.stats.GPUValid = false
		if errors.Is(err, ErrDeviceLost) || r.ds.Lost() {
			return ErrDeviceLost
		}
		return err
	}
	r.stats.Record = time.Since(t0) - wait
	r.stats.Present = wait
	if r.ds.Lost() {
		return ErrDeviceLost
	}
	return nil
}

// draw records and commits the frame while holding the device lock. It
// returns how long it waited for a drawable, which is when Metal holds the
// frame back for the display, as Present does on Windows.
func (r *Renderer) draw(s *Scene, clear [4]float32) (wait time.Duration, err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r.ds.Lock()
	defer r.ds.Unlock()
	pool := darwin.NewPool()
	defer pool.Drain()
	if r.timer != nil {
		sample := r.timer.collect()
		r.stats.GPU, r.stats.GPUFrame, r.stats.GPUValid = sample.duration, sample.frame, sample.valid
	}
	slot := &r.slots[r.slot]
	r.slot = (r.slot + 1) % frameSlots
	if err := r.waitSlot(slot); err != nil {
		return 0, err
	}
	cb := darwin.Send(r.ds.queue, "commandBuffer")
	if cb == 0 {
		return 0, errors.New("render: could not create a command buffer")
	}
	r.cb = cb
	defer r.abandon()

	var back, drawable id
	if r.layer != 0 {
		t := time.Now()
		drawable = darwin.Send(r.layer, "nextDrawable")
		wait = time.Since(t)
		// None comes within a second while the window is hidden; skip the frame.
		r.occluded = drawable == 0
		if drawable == 0 {
			return wait, nil
		}
		back = darwin.Send(drawable, "texture")
	} else {
		back = r.target.tex
	}

	r.frame = append(r.frame[:0], s.Prims...)
	var top []int
	if len(s.Prims) > 0 {
		defer r.pool.endFrame()
		if top, err = r.planLayers(s); err != nil {
			return wait, err
		}
	}
	if err := r.stage(slot, s.Clips); err != nil {
		return wait, err
	}
	r.glob = globals{
		AtlasSize:         [2]float32{float32(r.Atlas.size), float32(r.Atlas.size)},
		GammaRatios:       r.gamma,
		GrayscaleContrast: r.contrast,
	}
	r.blend = blendOver
	r.setTarget(renderTarget{tex: back, w: r.width, h: r.height})
	r.clearCur([4]float32{clear[0] * clear[3], clear[1] * clear[3], clear[2] * clear[3], clear[3]})
	if len(s.Prims) > 0 {
		r.stats.Batches = 0
		r.drawRange(s, 0, len(s.Prims), top)
	}
	r.endPass()

	var snap id
	f := r.snapshot
	if f != nil {
		r.snapshot = nil
		if snap, err = newBuffer(r.ds.dev, r.width*r.height*4); err == nil {
			defer darwin.Release(snap)
			blit := darwin.Send(cb, "blitCommandEncoder")
			blitDownload(blit, back, r.width, r.height, snap)
			blit.Send(darwin.Sel("endEncoding"))
		}
		err = nil
	}
	if drawable != 0 {
		cb.Send(darwin.Sel("presentDrawable:"), drawable)
	}
	cb.Send(darwin.Sel("commit"))
	r.cb = 0
	slot.cb = darwin.Retain(cb)
	if r.timer != nil {
		r.timer.track(cb, r.submission)
	}
	if snap != 0 {
		cb.Send(darwin.Sel("waitUntilCompleted"))
		if darwin.Get[uint64](cb, "status") == cbStatusCompleted {
			f(r.captured(bufferBytes(snap, r.width*r.height*4)))
		}
	}
	return wait, nil
}

// captured makes the snapshot image of the frame's BGRA texels, opaque, as
// the screen shows it over an opaque window.
func (r *Renderer) captured(bgra []byte) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, r.width, r.height))
	toNRGBA(img.Pix, bgra, false)
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	return img
}

// waitSlot waits until the GPU is done with the frame that last used s.
func (r *Renderer) waitSlot(s *frameSlot) error {
	cb := s.cb
	if cb == 0 {
		return nil
	}
	s.cb = 0
	defer darwin.Release(cb)
	status := darwin.Get[uint64](cb, "status")
	if status < cbStatusCompleted {
		cb.Send(darwin.Sel("waitUntilCompleted"))
		status = darwin.Get[uint64](cb, "status")
	}
	if status == cbStatusError {
		if err := commandBufferError(r.ds, cb); errors.Is(err, ErrDeviceLost) {
			return err
		}
	}
	return nil
}

// abandon ends an encoder a failed draw left open, as Metal requires
// before the command buffer goes.
func (r *Renderer) abandon() {
	if r.enc != 0 {
		r.enc.Send(darwin.Sel("endEncoding"))
		r.enc = 0
	}
	r.cb = 0
	r.clearPending = false
}

// stage writes the pending atlas masks, the frame's prims and the scene's
// clips into the slot's upload buffer and encodes the atlas uploads.
func (r *Renderer) stage(slot *frameSlot, clips []ClipRec) error {
	atlasN := r.Atlas.stagingSize()
	r.primsOff = align(atlasN, 256)
	r.clipsOff = r.primsOff + align(max(len(r.frame)*primSize, 1), 256)
	total := r.clipsOff + max(len(clips)*clipSize, clipSize)
	if total > slot.cap {
		n := max(slot.cap, 64<<10)
		for n < total {
			n *= 2
		}
		buf, err := newBuffer(r.ds.dev, n)
		if err != nil {
			return err
		}
		darwin.Release(slot.buf)
		slot.buf, slot.cap = buf, n
	}
	r.buf = slot.buf
	mem := bufferBytes(slot.buf, total)
	if len(r.frame) > 0 {
		copy(unsafe.Slice((*Prim)(unsafe.Pointer(&mem[r.primsOff])), len(r.frame)), r.frame)
	}
	if len(clips) > 0 {
		copy(unsafe.Slice((*ClipRec)(unsafe.Pointer(&mem[r.clipsOff])), len(clips)), clips)
	}
	r.Atlas.flush(r.cb, slot.buf, mem[:atlasN], 0)
	return nil
}

// encoder returns the render encoder drawing into the current target,
// beginning a render pass with the frame's state when none is open.
func (r *Renderer) encoder() id {
	if r.enc != 0 {
		return r.enc
	}
	pass := darwin.SendClass("MTLRenderPassDescriptor", "renderPassDescriptor")
	ca := darwin.Send(darwin.Send(pass, "colorAttachments"), "objectAtIndexedSubscript:", uint64(0))
	ca.Send(darwin.Sel("setTexture:"), r.cur.tex)
	ca.Send(darwin.Sel("setStoreAction:"), uint64(storeActionStore))
	if r.clearPending {
		c := r.clearColor
		ca.Send(darwin.Sel("setLoadAction:"), uint64(loadActionClear))
		ca.Send(darwin.Sel("setClearColor:"), mtlClearColor{float64(c[0]), float64(c[1]), float64(c[2]), float64(c[3])})
		r.clearPending = false
	} else {
		ca.Send(darwin.Sel("setLoadAction:"), uint64(loadActionLoad))
	}
	enc := darwin.Send(r.cb, "renderCommandEncoderWithDescriptor:", pass)
	r.enc = enc
	enc.Send(darwin.Sel("setRenderPipelineState:"), r.pipes.states[r.blend])
	enc.Send(darwin.Sel("setViewport:"), mtlViewport{W: float64(r.cur.w), H: float64(r.cur.h), ZFar: 1})
	gsize := uint64(unsafe.Sizeof(r.glob))
	enc.Send(darwin.Sel("setVertexBytes:length:atIndex:"), unsafe.Pointer(&r.glob), gsize, uint64(0))
	enc.Send(darwin.Sel("setVertexBuffer:offset:atIndex:"), r.buf, uint64(r.primsOff), uint64(1))
	enc.Send(darwin.Sel("setFragmentBytes:length:atIndex:"), unsafe.Pointer(&r.glob), gsize, uint64(0))
	enc.Send(darwin.Sel("setFragmentBuffer:offset:atIndex:"), r.buf, uint64(r.primsOff), uint64(1))
	enc.Send(darwin.Sel("setFragmentBuffer:offset:atIndex:"), r.buf, uint64(r.clipsOff), uint64(2))
	enc.Send(darwin.Sel("setFragmentTexture:atIndex:"), r.Atlas.tex, uint64(0))
	enc.Send(darwin.Sel("setFragmentTexture:atIndex:"), r.image, uint64(1))
	return enc
}

// endPass ends the open render pass, first running a clear that no draw took.
func (r *Renderer) endPass() {
	if r.enc == 0 && r.clearPending {
		r.encoder()
	}
	if r.enc != 0 {
		r.enc.Send(darwin.Sel("endEncoding"))
		r.enc = 0
	}
}

// clearCur clears the current target, premultiplied, before the next draw.
func (r *Renderer) clearCur(c [4]float32) {
	r.endPass()
	r.clearPending, r.clearColor = true, c
}

// clearTexture clears tex to transparent.
func (r *Renderer) clearTexture(tex id) {
	r.endPass()
	darwin.Send(r.cb, "renderCommandEncoderWithDescriptor:", clearPass(tex, [4]float32{})).Send(darwin.Sel("endEncoding"))
}

// copyTexture copies the w x h texels at (sx, sy) of src to (dx, dy) of dst.
func (r *Renderer) copyTexture(src id, sx, sy, w, h int, dst id, dx, dy int) {
	r.endPass()
	blit := darwin.Send(r.cb, "blitCommandEncoder")
	blit.Send(darwin.Sel("copyFromTexture:sourceSlice:sourceLevel:sourceOrigin:sourceSize:toTexture:destinationSlice:destinationLevel:destinationOrigin:"),
		src, uint64(0), uint64(0), mtlOrigin{uint64(sx), uint64(sy), 0}, mtlSize{uint64(w), uint64(h), 1},
		dst, uint64(0), uint64(0), mtlOrigin{uint64(dx), uint64(dy), 0})
	blit.Send(darwin.Sel("endEncoding"))
}

func (r *Renderer) setBlend(mode int) {
	if r.blend == mode {
		return
	}
	r.blend = mode
	if r.enc != 0 {
		r.enc.Send(darwin.Sel("setRenderPipelineState:"), r.pipes.states[mode])
	}
}

func (r *Renderer) setImage(tex id) {
	if r.image == tex {
		return
	}
	r.image = tex
	if r.enc != 0 {
		r.enc.Send(darwin.Sel("setFragmentTexture:atIndex:"), tex, uint64(1))
	}
}

func (r *Renderer) setTarget(t renderTarget) {
	r.endPass()
	r.cur = t
	r.image = r.emptyImage.tex
	r.glob.Viewport = [2]float32{float32(t.w), float32(t.h)}
}

func (r *Renderer) drawBatch(start, end int) {
	if end <= start {
		return
	}
	r.stats.Batches++
	enc := r.encoder()
	b := batchParams{Start: uint32(start), Origin: r.cur.origin}
	enc.Send(darwin.Sel("setVertexBytes:length:atIndex:"), unsafe.Pointer(&b), uint64(unsafe.Sizeof(b)), uint64(3))
	enc.Send(darwin.Sel("drawPrimitives:vertexStart:vertexCount:instanceCount:"),
		uint64(primitiveTriangleStrip), uint64(0), uint64(4), uint64(end-start))
}

// SetGPUTiming enables timing of each frame's command buffer on the GPU,
// reported a few frames later. Call between draws.
func (r *Renderer) SetGPUTiming(enabled bool) {
	if r.timer != nil {
		r.timer.release()
		r.timer = nil
	}
	if enabled {
		r.timer = &gpuTimer{}
	}
	r.stats.GPU, r.stats.GPUFrame, r.stats.GPUValid = 0, 0, false
}

// Stats returns how the last Draw went.
func (r *Renderer) Stats() Stats { return r.stats }

// Occluded reports whether the last Draw got no drawable, as when the
// window is hidden, and so drew nothing.
func (r *Renderer) Occluded() bool { return r.occluded }

// Release frees all GPU resources.
func (r *Renderer) Release() {
	r.SetGPUTiming(false)
	if r.Atlas != nil {
		r.Atlas.release()
	}
	if r.emptyImage != nil {
		r.emptyImage.Release()
	}
	r.pool.release()
	for i := range r.slots {
		// Command buffers keep what they use, so nothing waits here.
		darwin.Release(r.slots[i].cb)
		darwin.Release(r.slots[i].buf)
	}
	if r.target != nil {
		r.target.Release()
	}
	darwin.Release(r.layer)
	if r.ds != nil {
		r.ds.Release()
	}
	*r = Renderer{}
}
