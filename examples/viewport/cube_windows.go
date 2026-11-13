package main

import (
	"unsafe"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/d3d"
)

const cubeHLSL = `
cbuffer Camera : register(b0) {
	float4 row0, row1, row2, row3;
};
struct VSIn { float3 pos : POSITION; float3 color : COLOR; };
struct VSOut { float4 pos : SV_Position; float3 color : COLOR; };
VSOut vs_main(VSIn i) {
	float4 p = float4(i.pos, 1);
	VSOut o;
	o.pos = float4(dot(row0, p), dot(row1, p), dot(row2, p), dot(row3, p));
	o.color = i.color;
	return o;
}
float4 ps_main(VSOut i) : SV_Target { return float4(i.color, 1); }
`

// cubeRenderer draws a shaded cube with a depth buffer through the d3d
// package, standing in for an app's 3D renderer. Its resources belong to one
// device generation and are made again after the device is lost.
type cubeRenderer struct {
	gen     uint64
	vs      d3d.VertexShader
	ps      d3d.PixelShader
	layout  d3d.InputLayout
	vb, ib  d3d.Buffer
	cb      d3d.Buffer
	depthSt d3d.DepthStencilState
	raster  d3d.RasterizerState
	depths  map[[2]int]depthBuffer
	err     error
}

type depthBuffer struct {
	tex d3d.Texture2D
	dsv d3d.DepthStencilView
}

func (c *cubeRenderer) release() {
	for _, d := range c.depths {
		d.dsv.Release()
		d.tex.Release()
	}
	c.raster.Release()
	c.depthSt.Release()
	c.cb.Release()
	c.ib.Release()
	c.vb.Release()
	c.layout.Release()
	c.ps.Release()
	c.vs.Release()
	*c = cubeRenderer{}
}

func (c *cubeRenderer) init(g fairy.GPU) error {
	if c.gen == g.Generation && c.err == nil {
		return nil
	}
	c.release()
	c.gen, c.depths = g.Generation, map[[2]int]depthBuffer{}
	c.err = c.create(g.Device)
	return c.err
}

func (c *cubeRenderer) create(dev d3d.Device) error {
	vsCode, err := d3d.Compile([]byte(cubeHLSL), "cube.hlsl", "vs_main", "vs_4_0")
	if err != nil {
		return err
	}
	psCode, err := d3d.Compile([]byte(cubeHLSL), "cube.hlsl", "ps_main", "ps_4_0")
	if err != nil {
		return err
	}
	if c.vs, err = dev.CreateVertexShader(vsCode); err != nil {
		return err
	}
	if c.ps, err = dev.CreatePixelShader(psCode); err != nil {
		return err
	}
	if c.layout, err = dev.CreateInputLayout([]d3d.InputElement{
		{SemanticName: "POSITION", Format: d3d.FORMAT_R32G32B32_FLOAT},
		{SemanticName: "COLOR", Format: d3d.FORMAT_R32G32B32_FLOAT, AlignedByteOffset: 12},
	}, vsCode); err != nil {
		return err
	}
	verts, indices := cubeMesh()
	if c.vb, err = d3d.CreateBufferData(dev, d3d.BIND_VERTEX_BUFFER, d3d.USAGE_IMMUTABLE, verts); err != nil {
		return err
	}
	if c.ib, err = d3d.CreateBufferData(dev, d3d.BIND_INDEX_BUFFER, d3d.USAGE_IMMUTABLE, indices); err != nil {
		return err
	}
	if c.cb, err = dev.CreateBuffer(&d3d.BufferDesc{
		ByteWidth:      uint32(unsafe.Sizeof(cameraRows{})),
		Usage:          d3d.USAGE_DYNAMIC,
		BindFlags:      d3d.BIND_CONSTANT_BUFFER,
		CPUAccessFlags: d3d.CPU_ACCESS_WRITE,
	}, nil); err != nil {
		return err
	}
	if c.depthSt, err = dev.CreateDepthStencilState(&d3d.DepthStencilDesc{
		DepthEnable:    1,
		DepthWriteMask: d3d.DEPTH_WRITE_MASK_ALL,
		DepthFunc:      d3d.COMPARISON_LESS,
	}); err != nil {
		return err
	}
	c.raster, err = dev.CreateRasterizerState(&d3d.RasterizerDesc{FillMode: d3d.FILL_SOLID, CullMode: d3d.CULL_BACK, DepthClipEnable: 1})
	return err
}

func (c *cubeRenderer) depth(dev d3d.Device, w, h int) (d3d.DepthStencilView, error) {
	if d, ok := c.depths[[2]int{w, h}]; ok {
		return d.dsv, nil
	}
	tex, err := dev.CreateTexture2D(&d3d.Texture2DDesc{
		Width: uint32(w), Height: uint32(h), MipLevels: 1, ArraySize: 1,
		Format:     d3d.FORMAT_D24_UNORM_S8_UINT,
		SampleDesc: d3d.SampleDesc{Count: 1},
		BindFlags:  d3d.BIND_DEPTH_STENCIL,
	}, nil)
	if err != nil {
		return d3d.DepthStencilView{}, err
	}
	dsv, err := dev.CreateDepthStencilView(tex, nil)
	if err != nil {
		tex.Release()
		return d3d.DepthStencilView{}, err
	}
	// Keep the surface's buffer and one thumbnail size.
	if len(c.depths) >= 2 {
		for k, d := range c.depths {
			d.dsv.Release()
			d.tex.Release()
			delete(c.depths, k)
		}
	}
	c.depths[[2]int{w, h}] = depthBuffer{tex, dsv}
	return dsv, nil
}

// draw renders the cube into the frame's target as cam sees it, matching
// viewer.project for a w x h logical-pixel view, with background bg.
func (c *cubeRenderer) draw(f fairy.SurfaceFrame, cam camera, bg [4]float32) error {
	g := f.GPU()
	if err := c.init(g); err != nil {
		return err
	}
	dsv, err := c.depth(g.Device, f.Width, f.Height)
	if err != nil {
		return err
	}
	ctx := g.Context
	_, rtv := f.RenderTarget()
	ctx.ClearRenderTargetView(rtv, bg)
	ctx.ClearDepthStencilView(dsv, d3d.CLEAR_DEPTH, 1, 0)
	lw, lh := float64(f.Width)/float64(f.Scale), float64(f.Height)/float64(f.Scale)
	m := cameraMatrix(cam, lw, lh)
	if err := d3d.WriteData(ctx, c.cb, []cameraRows{m}); err != nil {
		return err
	}
	ctx.OMSetRenderTargets([]d3d.RenderTargetView{rtv}, dsv)
	ctx.OMSetDepthStencilState(c.depthSt, 0)
	ctx.RSSetState(c.raster)
	ctx.RSSetViewports(d3d.Viewport{Width: float32(f.Width), Height: float32(f.Height), MaxDepth: 1})
	ctx.IASetInputLayout(c.layout)
	ctx.IASetPrimitiveTopology(d3d.PRIMITIVE_TOPOLOGY_TRIANGLELIST)
	ctx.IASetVertexBuffers(0, []d3d.Buffer{c.vb}, []uint32{uint32(unsafe.Sizeof(vertex{}))}, []uint32{0})
	ctx.IASetIndexBuffer(c.ib, d3d.FORMAT_R16_UINT, 0)
	ctx.VSSetShader(c.vs)
	ctx.VSSetConstantBuffers(0, c.cb)
	ctx.PSSetShader(c.ps)
	ctx.DrawIndexed(36, 0, 0)
	return nil
}
