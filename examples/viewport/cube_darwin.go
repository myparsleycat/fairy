package main

import (
	"errors"
	"unsafe"

	"github.com/ebitengine/purego/objc"

	"github.com/myparsleycat/fairy"
)

// The vertex shader reads the vertices straight from buffer 0, so the
// pipeline needs no vertex descriptor, and the camera from buffer 1.
const cubeMSL = `
#include <metal_stdlib>
using namespace metal;
struct Vertex { packed_float3 pos; packed_float3 color; };
struct Camera { float4 row0, row1, row2, row3; };
struct VSOut { float4 pos [[position]]; float3 color; };
vertex VSOut vs_main(uint id [[vertex_id]], const device Vertex *v [[buffer(0)]], constant Camera &cam [[buffer(1)]]) {
	float4 p = float4(float3(v[id].pos), 1);
	VSOut o;
	o.pos = float4(dot(cam.row0, p), dot(cam.row1, p), dot(cam.row2, p), dot(cam.row3, p));
	o.color = float3(v[id].color);
	return o;
}
fragment float4 ps_main(VSOut i [[stage_in]]) { return float4(i.color, 1); }
`

// Metal enumeration values used here.
const (
	pixelDepth32Float  = 252 // MTLPixelFormatDepth32Float
	usageRenderTarget  = 4   // MTLTextureUsageRenderTarget
	storagePrivate     = 2   // MTLStorageModePrivate
	loadClear          = 2   // MTLLoadActionClear
	storeDontCare      = 0   // MTLStoreActionDontCare
	storeStore         = 1   // MTLStoreActionStore
	compareLess        = 1   // MTLCompareFunctionLess
	cullBack           = 2   // MTLCullModeBack
	windingCCW         = 1   // MTLWindingCounterClockwise
	primitiveTriangle  = 3   // MTLPrimitiveTypeTriangle
	indexUInt16        = 0   // MTLIndexTypeUInt16
	resourceSharedMode = 0   // MTLResourceStorageModeShared
)

// mtlClearColor is MTLClearColor.
type mtlClearColor struct{ R, G, B, A float64 }

// msg sends the message name to obj. Objects that new, alloc or copy
// methods return belong to the caller and need release; the others are
// autoreleased, and the pool fairy runs Surface draw functions and
// SurfaceImage in frees them.
func msg(obj objc.ID, name string, args ...any) objc.ID {
	return obj.Send(objc.RegisterName(name), args...)
}

func class(name string) objc.ID { return objc.ID(objc.GetClass(name)) }

func release(objs ...objc.ID) {
	for _, o := range objs {
		if o != 0 {
			msg(o, "release")
		}
	}
}

// nsError returns the description of an NSError, or a fallback.
func nsError(what string, e objc.ID) error {
	if e == 0 {
		return errors.New("cube: could not " + what)
	}
	desc := msg(e, "localizedDescription")
	return errors.New("cube: could not " + what + ": " + objc.Send[string](desc, objc.RegisterName("UTF8String")))
}

// cubeRenderer draws a shaded cube with a depth buffer through Metal,
// standing in for an app's 3D renderer. Its resources belong to one device
// generation and are made again after the device is lost.
type cubeRenderer struct {
	gen        uint64
	lib        objc.ID            // id<MTLLibrary>
	vs, fs     objc.ID            // id<MTLFunction>
	vb, ib     objc.ID            // id<MTLBuffer>
	depthState objc.ID            // id<MTLDepthStencilState>
	pipelines  map[uint64]objc.ID // id<MTLRenderPipelineState> by target pixel format
	depths     map[[2]int]objc.ID // id<MTLTexture> by size
	err        error
}

func (c *cubeRenderer) release() {
	for _, p := range c.pipelines {
		release(p)
	}
	for _, d := range c.depths {
		release(d)
	}
	release(c.depthState, c.ib, c.vb, c.fs, c.vs, c.lib)
	*c = cubeRenderer{}
}

func (c *cubeRenderer) init(g fairy.GPU) error {
	if c.gen == g.Generation && c.err == nil {
		return nil
	}
	c.release()
	c.gen, c.pipelines, c.depths = g.Generation, map[uint64]objc.ID{}, map[[2]int]objc.ID{}
	c.err = c.create(objc.ID(g.Device))
	return c.err
}

func (c *cubeRenderer) create(dev objc.ID) error {
	var e objc.ID
	src := msg(class("NSString"), "stringWithUTF8String:", cubeMSL)
	if c.lib = msg(dev, "newLibraryWithSource:options:error:", src, objc.ID(0), unsafe.Pointer(&e)); c.lib == 0 {
		return nsError("compile the shaders", e)
	}
	c.vs = msg(c.lib, "newFunctionWithName:", msg(class("NSString"), "stringWithUTF8String:", "vs_main"))
	c.fs = msg(c.lib, "newFunctionWithName:", msg(class("NSString"), "stringWithUTF8String:", "ps_main"))
	if c.vs == 0 || c.fs == 0 {
		return errors.New("cube: shader function missing")
	}
	verts, indices := cubeMesh()
	c.vb = msg(dev, "newBufferWithBytes:length:options:",
		unsafe.Pointer(&verts[0]), uint64(len(verts))*uint64(unsafe.Sizeof(vertex{})), uint64(resourceSharedMode))
	c.ib = msg(dev, "newBufferWithBytes:length:options:",
		unsafe.Pointer(&indices[0]), uint64(len(indices))*2, uint64(resourceSharedMode))
	if c.vb == 0 || c.ib == 0 {
		return errors.New("cube: could not make the buffers")
	}
	desc := msg(msg(class("MTLDepthStencilDescriptor"), "alloc"), "init")
	defer release(desc)
	msg(desc, "setDepthCompareFunction:", uint64(compareLess))
	msg(desc, "setDepthWriteEnabled:", true)
	if c.depthState = msg(dev, "newDepthStencilStateWithDescriptor:", desc); c.depthState == 0 {
		return errors.New("cube: could not make the depth state")
	}
	return nil
}

// pipeline returns the pipeline that draws into targets of pixel format.
func (c *cubeRenderer) pipeline(dev objc.ID, format uint64) (objc.ID, error) {
	if p, ok := c.pipelines[format]; ok {
		return p, nil
	}
	desc := msg(msg(class("MTLRenderPipelineDescriptor"), "alloc"), "init")
	defer release(desc)
	msg(desc, "setVertexFunction:", c.vs)
	msg(desc, "setFragmentFunction:", c.fs)
	msg(msg(msg(desc, "colorAttachments"), "objectAtIndexedSubscript:", uint64(0)), "setPixelFormat:", format)
	msg(desc, "setDepthAttachmentPixelFormat:", uint64(pixelDepth32Float))
	var e objc.ID
	p := msg(dev, "newRenderPipelineStateWithDescriptor:error:", desc, unsafe.Pointer(&e))
	if p == 0 {
		return 0, nsError("make the pipeline", e)
	}
	c.pipelines[format] = p
	return p, nil
}

func (c *cubeRenderer) depth(dev objc.ID, w, h int) (objc.ID, error) {
	if d, ok := c.depths[[2]int{w, h}]; ok {
		return d, nil
	}
	desc := msg(class("MTLTextureDescriptor"), "texture2DDescriptorWithPixelFormat:width:height:mipmapped:",
		uint64(pixelDepth32Float), uint64(w), uint64(h), false)
	msg(desc, "setUsage:", uint64(usageRenderTarget))
	msg(desc, "setStorageMode:", uint64(storagePrivate))
	tex := msg(dev, "newTextureWithDescriptor:", desc)
	if tex == 0 {
		return 0, errors.New("cube: could not make the depth buffer")
	}
	// Keep the surface's buffer and one thumbnail size.
	if len(c.depths) >= 2 {
		for k, d := range c.depths {
			release(d)
			delete(c.depths, k)
		}
	}
	c.depths[[2]int{w, h}] = tex
	return tex, nil
}

// draw renders the cube into the frame's target as cam sees it, matching
// viewer.project for a w x h logical-pixel view, with background bg. It
// encodes one render pass on the frame's queue and commits it.
func (c *cubeRenderer) draw(f fairy.SurfaceFrame, cam camera, bg [4]float32) error {
	g := f.GPU()
	if err := c.init(g); err != nil {
		return err
	}
	dev, target := objc.ID(g.Device), objc.ID(f.Texture)
	pipe, err := c.pipeline(dev, objc.Send[uint64](target, objc.RegisterName("pixelFormat")))
	if err != nil {
		return err
	}
	depth, err := c.depth(dev, f.Width, f.Height)
	if err != nil {
		return err
	}

	pass := msg(class("MTLRenderPassDescriptor"), "renderPassDescriptor")
	color := msg(msg(pass, "colorAttachments"), "objectAtIndexedSubscript:", uint64(0))
	msg(color, "setTexture:", target)
	msg(color, "setLoadAction:", uint64(loadClear))
	msg(color, "setStoreAction:", uint64(storeStore))
	msg(color, "setClearColor:", mtlClearColor{float64(bg[0]), float64(bg[1]), float64(bg[2]), float64(bg[3])})
	da := msg(pass, "depthAttachment")
	msg(da, "setTexture:", depth)
	msg(da, "setLoadAction:", uint64(loadClear))
	msg(da, "setStoreAction:", uint64(storeDontCare))
	msg(da, "setClearDepth:", 1.0)

	lw, lh := float64(f.Width)/float64(f.Scale), float64(f.Height)/float64(f.Scale)
	m := cameraMatrix(cam, lw, lh)
	cb := msg(objc.ID(g.Queue), "commandBuffer")
	enc := msg(cb, "renderCommandEncoderWithDescriptor:", pass)
	msg(enc, "setRenderPipelineState:", pipe)
	msg(enc, "setDepthStencilState:", c.depthState)
	// cubeMesh winds the faces toward the viewer counter-clockwise on screen.
	msg(enc, "setFrontFacingWinding:", uint64(windingCCW))
	msg(enc, "setCullMode:", uint64(cullBack))
	msg(enc, "setVertexBuffer:offset:atIndex:", c.vb, uint64(0), uint64(0))
	// The camera is small, so Metal copies it with the commands: no buffer
	// that a frame still in flight might be reading is overwritten.
	msg(enc, "setVertexBytes:length:atIndex:", unsafe.Pointer(&m), uint64(unsafe.Sizeof(m)), uint64(1))
	msg(enc, "drawIndexedPrimitives:indexCount:indexType:indexBuffer:indexBufferOffset:",
		uint64(primitiveTriangle), uint64(36), uint64(indexUInt16), c.ib, uint64(0))
	msg(enc, "endEncoding")
	msg(cb, "commit")
	return nil
}
