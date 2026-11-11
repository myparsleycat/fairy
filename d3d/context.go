//go:build windows

package d3d

import (
	"fmt"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/com"
)

// Device returns the device that owns the context, with a reference the
// caller releases.
func (c Context) Device() Device {
	var d unsafe.Pointer
	com.Call(c.P, 3, uintptr(unsafe.Pointer(&d)))
	return Device{d}
}

// Slice addresses are converted inline in each com.Call so that its
// go:uintptrescapes keeps the backing array alive and in place.

func (c Context) VSSetConstantBuffers(slot uint32, bufs ...Buffer) {
	com.Call(c.P, 7, uintptr(slot), uintptr(len(bufs)), uintptr(unsafe.Pointer(unsafe.SliceData(bufs))))
}

func (c Context) PSSetConstantBuffers(slot uint32, bufs ...Buffer) {
	com.Call(c.P, 16, uintptr(slot), uintptr(len(bufs)), uintptr(unsafe.Pointer(unsafe.SliceData(bufs))))
}

func (c Context) VSSetShaderResources(slot uint32, views ...ShaderResourceView) {
	com.Call(c.P, 25, uintptr(slot), uintptr(len(views)), uintptr(unsafe.Pointer(unsafe.SliceData(views))))
}

func (c Context) PSSetShaderResources(slot uint32, views ...ShaderResourceView) {
	com.Call(c.P, 8, uintptr(slot), uintptr(len(views)), uintptr(unsafe.Pointer(unsafe.SliceData(views))))
}

func (c Context) VSSetSamplers(slot uint32, samplers ...SamplerState) {
	com.Call(c.P, 26, uintptr(slot), uintptr(len(samplers)), uintptr(unsafe.Pointer(unsafe.SliceData(samplers))))
}

func (c Context) PSSetSamplers(slot uint32, samplers ...SamplerState) {
	com.Call(c.P, 10, uintptr(slot), uintptr(len(samplers)), uintptr(unsafe.Pointer(unsafe.SliceData(samplers))))
}

func (c Context) VSSetShader(s VertexShader) { com.Call(c.P, 11, uintptr(s.P), 0, 0) }
func (c Context) PSSetShader(s PixelShader)  { com.Call(c.P, 9, uintptr(s.P), 0, 0) }

func (c Context) IASetInputLayout(l InputLayout) { com.Call(c.P, 17, uintptr(l.P)) }

// IASetVertexBuffers binds bufs from slot on, each with its stride and offset in bytes.
func (c Context) IASetVertexBuffers(slot uint32, bufs []Buffer, strides, offsets []uint32) {
	if len(bufs) == 0 {
		return
	}
	if len(strides) != len(bufs) || len(offsets) != len(bufs) {
		panic("d3d: IASetVertexBuffers needs a stride and an offset per buffer")
	}
	com.Call(c.P, 18, uintptr(slot), uintptr(len(bufs)), uintptr(unsafe.Pointer(&bufs[0])),
		uintptr(unsafe.Pointer(&strides[0])), uintptr(unsafe.Pointer(&offsets[0])))
}

// IASetIndexBuffer binds an index buffer of FORMAT_R16_UINT or FORMAT_R32_UINT indices.
func (c Context) IASetIndexBuffer(buf Buffer, format, offset uint32) {
	com.Call(c.P, 19, uintptr(buf.P), uintptr(format), uintptr(offset))
}

func (c Context) IASetPrimitiveTopology(t uint32) { com.Call(c.P, 24, uintptr(t)) }

// OMSetRenderTargets binds the render targets and depth buffer; an empty
// rtvs and zero dsv unbind them.
func (c Context) OMSetRenderTargets(rtvs []RenderTargetView, dsv DepthStencilView) {
	com.Call(c.P, 33, uintptr(len(rtvs)), uintptr(unsafe.Pointer(unsafe.SliceData(rtvs))), uintptr(dsv.P))
}

// OMSetBlendState sets the blend state; a zero state is the default
// (blending off).
func (c Context) OMSetBlendState(s BlendState, factor [4]float32, sampleMask uint32) {
	com.Call(c.P, 35, uintptr(s.P), uintptr(unsafe.Pointer(&factor[0])), uintptr(sampleMask))
}

func (c Context) OMSetDepthStencilState(s DepthStencilState, stencilRef uint32) {
	com.Call(c.P, 36, uintptr(s.P), uintptr(stencilRef))
}

func (c Context) RSSetState(s RasterizerState) { com.Call(c.P, 43, uintptr(s.P)) }

func (c Context) RSSetViewports(v ...Viewport) {
	if len(v) == 0 {
		return
	}
	com.Call(c.P, 44, uintptr(len(v)), uintptr(unsafe.Pointer(&v[0])))
}

// RSSetScissorRects sets the scissor rectangles, used when the rasterizer
// state enables ScissorEnable.
func (c Context) RSSetScissorRects(r ...Rect) {
	if len(r) == 0 {
		com.Call(c.P, 45, 0, 0)
		return
	}
	com.Call(c.P, 45, uintptr(len(r)), uintptr(unsafe.Pointer(&r[0])))
}

func (c Context) Draw(vertexCount, startVertex uint32) {
	com.Call(c.P, 13, uintptr(vertexCount), uintptr(startVertex))
}

func (c Context) DrawIndexed(indexCount, startIndex uint32, baseVertex int32) {
	com.Call(c.P, 12, uintptr(indexCount), uintptr(startIndex), uintptr(baseVertex))
}

func (c Context) DrawInstanced(vertsPerInstance, instances, startVertex, startInstance uint32) {
	com.Call(c.P, 21, uintptr(vertsPerInstance), uintptr(instances), uintptr(startVertex), uintptr(startInstance))
}

func (c Context) DrawIndexedInstanced(indicesPerInstance, instances, startIndex uint32, baseVertex int32, startInstance uint32) {
	com.Call(c.P, 20, uintptr(indicesPerInstance), uintptr(instances), uintptr(startIndex), uintptr(baseVertex), uintptr(startInstance))
}

// Map maps subresource 0 of res, as for writing a USAGE_DYNAMIC buffer with
// MAP_WRITE_DISCARD or reading a USAGE_STAGING texture with MAP_READ.
func (c Context) Map(res Resource, mapType uint32) (MappedSubresource, error) {
	return c.MapSubresource(res, 0, mapType)
}

func (c Context) MapSubresource(res Resource, sub, mapType uint32) (MappedSubresource, error) {
	var m MappedSubresource
	err := com.HR(c.P, 14, uintptr(res.resource()), uintptr(sub), uintptr(mapType), 0, uintptr(unsafe.Pointer(&m))).Err("Map")
	return m, err
}

func (c Context) Unmap(res Resource) { c.UnmapSubresource(res, 0) }

func (c Context) UnmapSubresource(res Resource, sub uint32) {
	com.Call(c.P, 15, uintptr(res.resource()), uintptr(sub))
}

// UpdateSubresource copies memory into subresource 0 of a USAGE_DEFAULT
// resource; a nil box writes all of it.
func (c Context) UpdateSubresource(res Resource, box *Box, data unsafe.Pointer, rowPitch, depthPitch uint32) {
	com.Call(c.P, 48, uintptr(res.resource()), 0, uintptr(unsafe.Pointer(box)), uintptr(data), uintptr(rowPitch), uintptr(depthPitch))
}

// UpdateData writes a slice into subresource 0 of a USAGE_DEFAULT buffer.
func UpdateData[T any](c Context, buf Buffer, data []T) {
	if len(data) == 0 {
		return
	}
	c.UpdateSubresource(buf, nil, unsafe.Pointer(&data[0]), 0, 0)
}

// WriteData replaces the contents of a USAGE_DYNAMIC buffer with data, by
// mapping it with MAP_WRITE_DISCARD.
func WriteData[T any](c Context, buf Buffer, data []T) error {
	if len(data) == 0 {
		return nil
	}
	var zero T
	if n := uint64(len(data)) * uint64(unsafe.Sizeof(zero)); n > uint64(buf.Desc().ByteWidth) {
		return fmt.Errorf("d3d: WriteData: %d bytes into a %d-byte buffer", n, buf.Desc().ByteWidth)
	}
	m, err := c.Map(buf, MAP_WRITE_DISCARD)
	if err != nil {
		return err
	}
	copy(unsafe.Slice((*T)(m.Data), len(data)), data)
	c.Unmap(buf)
	return nil
}

func (c Context) CopyResource(dst, src Resource) {
	com.Call(c.P, 47, uintptr(dst.resource()), uintptr(src.resource()))
}

// CopySubresourceRegion copies box of src subresource srcSub to (x, y, z)
// of dst subresource dstSub; a nil box copies all of it.
func (c Context) CopySubresourceRegion(dst Resource, dstSub, x, y, z uint32, src Resource, srcSub uint32, box *Box) {
	com.Call(c.P, 46, uintptr(dst.resource()), uintptr(dstSub), uintptr(x), uintptr(y), uintptr(z),
		uintptr(src.resource()), uintptr(srcSub), uintptr(unsafe.Pointer(box)))
}

// ResolveSubresource resolves a multisampled texture into a single-sampled one.
func (c Context) ResolveSubresource(dst Texture2D, dstSub uint32, src Texture2D, srcSub, format uint32) {
	com.Call(c.P, 57, uintptr(dst.P), uintptr(dstSub), uintptr(src.P), uintptr(srcSub), uintptr(format))
}

func (c Context) ClearRenderTargetView(rtv RenderTargetView, color [4]float32) {
	com.Call(c.P, 50, uintptr(rtv.P), uintptr(unsafe.Pointer(&color[0])))
}

// ClearDepthStencilView clears depth (CLEAR_DEPTH) and stencil (CLEAR_STENCIL).
func (c Context) ClearDepthStencilView(dsv DepthStencilView, flags uint32, depth float32, stencil uint8) {
	com.Call(c.P, 53, uintptr(dsv.P), uintptr(flags), com.F32(depth), uintptr(stencil))
}

// GenerateMips fills the lower mips of a texture made with
// RESOURCE_MISC_GENERATE_MIPS and bound as a render target.
func (c Context) GenerateMips(v ShaderResourceView) { com.Call(c.P, 54, uintptr(v.P)) }

func (c Context) ClearState() { com.Call(c.P, 110) }
func (c Context) Flush()      { com.Call(c.P, 111) }
