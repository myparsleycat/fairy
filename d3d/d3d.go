//go:build windows

// Package d3d is a Direct3D 11 binding that needs no cgo, together with the
// D3DCompile HLSL compiler. The Fairy renderer draws with it, and apps use it
// to draw 3D content into a Fairy Surface or an offscreen texture: buffers,
// textures and their views, input layouts, shaders, pipeline state objects,
// draws, queries and readback.
//
// Undocumented Device and Context methods mirror the ID3D11Device and
// ID3D11DeviceContext methods of the same name.
//
// Every handle is a COM interface pointer in its P field. Every handle a
// Create* method returns holds one reference, and the caller must free it
// with Release. Handles built from fairy.SurfaceFrame or fairy.GPU fields are
// borrowed and must not be released.
//
// The ID3D11Device is free threaded. The immediate ID3D11DeviceContext is not:
// use it only inside a Surface draw function or fairy.UseDevice, which hold the
// device's lock.
package d3d

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/myparsleycat/fairy/internal/com"
)

var (
	d3d11       = windows.NewLazySystemDLL("d3d11.dll")
	d3dcompiler = windows.NewLazySystemDLL("d3dcompiler_47.dll")

	pD3D11CreateDevice = d3d11.NewProc("D3D11CreateDevice")
	pD3DCompile        = d3dcompiler.NewProc("D3DCompile")
)

var (
	iidIDXGIDevice       = com.MustGUID("54ec77fa-1377-44e6-8c32-88fd5f44c84c")
	iidIDXGIAdapter1     = com.MustGUID("29038f61-3839-4626-91fd-086879011a05")
	iidID3D10Multithread = com.MustGUID("9b7e4e00-342c-4106-a19f-4f2704f689f0")
)

// sdkVersion is D3D11_SDK_VERSION.
const sdkVersion = 7

const (
	DRIVER_TYPE_HARDWARE = 1
	DRIVER_TYPE_WARP     = 5

	CREATE_DEVICE_BGRA_SUPPORT  = 0x20
	CREATE_DEVICE_VIDEO_SUPPORT = 0x800

	FEATURE_LEVEL_10_0 = 0xa000
	FEATURE_LEVEL_10_1 = 0xa100
	FEATURE_LEVEL_11_0 = 0xb000
	FEATURE_LEVEL_11_1 = 0xb100
)

// DXGI_FORMAT values.
const (
	FORMAT_UNKNOWN               = 0
	FORMAT_R32G32B32A32_FLOAT    = 2
	FORMAT_R32G32B32A32_UINT     = 3
	FORMAT_R32G32B32_FLOAT       = 6
	FORMAT_R16G16B16A16_FLOAT    = 10
	FORMAT_R16G16B16A16_UNORM    = 11
	FORMAT_R32G32_FLOAT          = 16
	FORMAT_R10G10B10A2_UNORM     = 24
	FORMAT_R8G8B8A8_UNORM        = 28
	FORMAT_R8G8B8A8_UNORM_SRGB   = 29
	FORMAT_R8G8B8A8_UINT         = 30
	FORMAT_R8G8B8A8_SNORM        = 31
	FORMAT_R16G16_FLOAT          = 34
	FORMAT_R16G16_UNORM          = 35
	FORMAT_R32_TYPELESS          = 39
	FORMAT_D32_FLOAT             = 40
	FORMAT_R32_FLOAT             = 41
	FORMAT_R32_UINT              = 42
	FORMAT_R24G8_TYPELESS        = 44
	FORMAT_D24_UNORM_S8_UINT     = 45
	FORMAT_R24_UNORM_X8_TYPELESS = 46
	FORMAT_R8G8_UNORM            = 49
	FORMAT_R16_FLOAT             = 54
	FORMAT_R16_UINT              = 57
	FORMAT_R8_UNORM              = 61
	FORMAT_B8G8R8A8_UNORM        = 87
	FORMAT_B8G8R8A8_UNORM_SRGB   = 91
)

const (
	USAGE_DEFAULT   = 0
	USAGE_IMMUTABLE = 1
	USAGE_DYNAMIC   = 2
	USAGE_STAGING   = 3

	BIND_VERTEX_BUFFER    = 0x1
	BIND_INDEX_BUFFER     = 0x2
	BIND_CONSTANT_BUFFER  = 0x4
	BIND_SHADER_RESOURCE  = 0x8
	BIND_RENDER_TARGET    = 0x20
	BIND_DEPTH_STENCIL    = 0x40
	BIND_UNORDERED_ACCESS = 0x80

	CPU_ACCESS_WRITE = 0x10000
	CPU_ACCESS_READ  = 0x20000

	RESOURCE_MISC_GENERATE_MIPS     = 0x1
	RESOURCE_MISC_TEXTURECUBE       = 0x4
	RESOURCE_MISC_BUFFER_STRUCTURED = 0x40

	MAP_READ               = 1
	MAP_WRITE              = 2
	MAP_READ_WRITE         = 3
	MAP_WRITE_DISCARD      = 4
	MAP_WRITE_NO_OVERWRITE = 5

	CLEAR_DEPTH   = 0x1
	CLEAR_STENCIL = 0x2

	BLEND_ZERO             = 1
	BLEND_ONE              = 2
	BLEND_SRC_COLOR        = 3
	BLEND_INV_SRC_COLOR    = 4
	BLEND_SRC_ALPHA        = 5
	BLEND_INV_SRC_ALPHA    = 6
	BLEND_DEST_ALPHA       = 7
	BLEND_INV_DEST_ALPHA   = 8
	BLEND_DEST_COLOR       = 9
	BLEND_INV_DEST_COLOR   = 10
	BLEND_SRC_ALPHA_SAT    = 11
	BLEND_BLEND_FACTOR     = 14
	BLEND_INV_BLEND_FACTOR = 15

	BLEND_OP_ADD          = 1
	BLEND_OP_SUBTRACT     = 2
	BLEND_OP_REV_SUBTRACT = 3
	BLEND_OP_MIN          = 4
	BLEND_OP_MAX          = 5

	COLOR_WRITE_ALL = 0xF

	FILL_WIREFRAME = 2
	FILL_SOLID     = 3

	CULL_NONE  = 1
	CULL_FRONT = 2
	CULL_BACK  = 3

	COMPARISON_NEVER         = 1
	COMPARISON_LESS          = 2
	COMPARISON_EQUAL         = 3
	COMPARISON_LESS_EQUAL    = 4
	COMPARISON_GREATER       = 5
	COMPARISON_NOT_EQUAL     = 6
	COMPARISON_GREATER_EQUAL = 7
	COMPARISON_ALWAYS        = 8

	DEPTH_WRITE_MASK_ZERO = 0
	DEPTH_WRITE_MASK_ALL  = 1

	STENCIL_OP_KEEP     = 1
	STENCIL_OP_ZERO     = 2
	STENCIL_OP_REPLACE  = 3
	STENCIL_OP_INCR_SAT = 4
	STENCIL_OP_DECR_SAT = 5
	STENCIL_OP_INVERT   = 6
	STENCIL_OP_INCR     = 7
	STENCIL_OP_DECR     = 8

	FILTER_MIN_MAG_MIP_POINT             = 0
	FILTER_MIN_MAG_MIP_LINEAR            = 0x15
	FILTER_ANISOTROPIC                   = 0x55
	FILTER_COMPARISON_MIN_MAG_MIP_LINEAR = 0x95

	TEXTURE_ADDRESS_WRAP   = 1
	TEXTURE_ADDRESS_MIRROR = 2
	TEXTURE_ADDRESS_CLAMP  = 3
	TEXTURE_ADDRESS_BORDER = 4

	PRIMITIVE_TOPOLOGY_POINTLIST     = 1
	PRIMITIVE_TOPOLOGY_LINELIST      = 2
	PRIMITIVE_TOPOLOGY_LINESTRIP     = 3
	PRIMITIVE_TOPOLOGY_TRIANGLELIST  = 4
	PRIMITIVE_TOPOLOGY_TRIANGLESTRIP = 5

	INPUT_PER_VERTEX_DATA   = 0
	INPUT_PER_INSTANCE_DATA = 1
	APPEND_ALIGNED_ELEMENT  = 0xffffffff

	SRV_DIMENSION_BUFFER      = 1
	SRV_DIMENSION_TEXTURE2D   = 4
	SRV_DIMENSION_TEXTURE2DMS = 6
	SRV_DIMENSION_TEXTURECUBE = 9

	RTV_DIMENSION_TEXTURE2D   = 4
	RTV_DIMENSION_TEXTURE2DMS = 6

	DSV_DIMENSION_TEXTURE2D   = 3
	DSV_DIMENSION_TEXTURE2DMS = 5

	DXGI_ERROR_DEVICE_HUNG           = 0x887A0006
	DXGI_ERROR_DEVICE_REMOVED        = 0x887A0005
	DXGI_ERROR_DEVICE_RESET          = 0x887A0007
	DXGI_ERROR_DRIVER_INTERNAL_ERROR = 0x887A0020

	COMPILE_DEBUG                    = 1 << 0
	COMPILE_SKIP_OPTIMIZATION        = 1 << 2
	COMPILE_PACK_MATRIX_ROW_MAJOR    = 1 << 3
	COMPILE_PACK_MATRIX_COLUMN_MAJOR = 1 << 4
	COMPILE_ENABLE_STRICTNESS        = 1 << 11
	COMPILE_OPTIMIZATION_LEVEL3      = 1 << 15
)

// SampleDesc is DXGI_SAMPLE_DESC: samples per pixel and their quality level.
type SampleDesc struct{ Count, Quality uint32 }

// BufferDesc is D3D11_BUFFER_DESC.
type BufferDesc struct {
	ByteWidth           uint32
	Usage               uint32
	BindFlags           uint32
	CPUAccessFlags      uint32
	MiscFlags           uint32
	StructureByteStride uint32
}

// Texture2DDesc is D3D11_TEXTURE2D_DESC.
type Texture2DDesc struct {
	Width, Height  uint32
	MipLevels      uint32
	ArraySize      uint32
	Format         uint32
	SampleDesc     SampleDesc
	Usage          uint32
	BindFlags      uint32
	CPUAccessFlags uint32
	MiscFlags      uint32
}

// SubresourceData is D3D11_SUBRESOURCE_DATA: initial contents and their row
// and slice pitch in bytes.
type SubresourceData struct {
	SysMem     unsafe.Pointer
	Pitch      uint32
	SlicePitch uint32
}

// SRVDesc is D3D11_SHADER_RESOURCE_VIEW_DESC. U holds the union for
// ViewDimension; for SRV_DIMENSION_TEXTURE2D it is {MostDetailedMip, MipLevels}.
type SRVDesc struct {
	Format        uint32
	ViewDimension uint32
	U             [4]uint32
}

// RTVDesc is D3D11_RENDER_TARGET_VIEW_DESC; for RTV_DIMENSION_TEXTURE2D U is {MipSlice}.
type RTVDesc struct {
	Format        uint32
	ViewDimension uint32
	U             [3]uint32
}

// DSVDesc is D3D11_DEPTH_STENCIL_VIEW_DESC; for DSV_DIMENSION_TEXTURE2D U is {MipSlice}.
type DSVDesc struct {
	Format        uint32
	ViewDimension uint32
	Flags         uint32
	U             [3]uint32
}

// MappedSubresource is D3D11_MAPPED_SUBRESOURCE: the mapped memory and its
// row and depth pitch in bytes.
type MappedSubresource struct {
	Data       unsafe.Pointer
	RowPitch   uint32
	DepthPitch uint32
}

// RenderTargetBlendDesc is D3D11_RENDER_TARGET_BLEND_DESC.
type RenderTargetBlendDesc struct {
	BlendEnable           int32
	SrcBlend              uint32
	DestBlend             uint32
	BlendOp               uint32
	SrcBlendAlpha         uint32
	DestBlendAlpha        uint32
	BlendOpAlpha          uint32
	RenderTargetWriteMask uint8
	_                     [3]byte
}

// BlendDesc is D3D11_BLEND_DESC.
type BlendDesc struct {
	AlphaToCoverageEnable  int32
	IndependentBlendEnable int32
	RenderTarget           [8]RenderTargetBlendDesc
}

// RasterizerDesc is D3D11_RASTERIZER_DESC.
type RasterizerDesc struct {
	FillMode              uint32
	CullMode              uint32
	FrontCounterClockwise int32
	DepthBias             int32
	DepthBiasClamp        float32
	SlopeScaledDepthBias  float32
	DepthClipEnable       int32
	ScissorEnable         int32
	MultisampleEnable     int32
	AntialiasedLineEnable int32
}

// DepthStencilOpDesc is D3D11_DEPTH_STENCILOP_DESC.
type DepthStencilOpDesc struct {
	StencilFailOp      uint32
	StencilDepthFailOp uint32
	StencilPassOp      uint32
	StencilFunc        uint32
}

// DepthStencilDesc is D3D11_DEPTH_STENCIL_DESC.
type DepthStencilDesc struct {
	DepthEnable      int32
	DepthWriteMask   uint32
	DepthFunc        uint32
	StencilEnable    int32
	StencilReadMask  uint8
	StencilWriteMask uint8
	_                [2]byte
	FrontFace        DepthStencilOpDesc
	BackFace         DepthStencilOpDesc
}

// SamplerDesc is D3D11_SAMPLER_DESC.
type SamplerDesc struct {
	Filter         uint32
	AddressU       uint32
	AddressV       uint32
	AddressW       uint32
	MipLODBias     float32
	MaxAnisotropy  uint32
	ComparisonFunc uint32
	BorderColor    [4]float32
	MinLOD, MaxLOD float32
}

// InputElement describes one vertex attribute. Unlike
// D3D11_INPUT_ELEMENT_DESC it takes the semantic name as a Go string.
type InputElement struct {
	SemanticName         string
	SemanticIndex        uint32
	Format               uint32
	InputSlot            uint32
	AlignedByteOffset    uint32
	InputSlotClass       uint32
	InstanceDataStepRate uint32
}

type inputElementDesc struct {
	SemanticName         *byte
	SemanticIndex        uint32
	Format               uint32
	InputSlot            uint32
	AlignedByteOffset    uint32
	InputSlotClass       uint32
	InstanceDataStepRate uint32
}

// Viewport is D3D11_VIEWPORT: a rectangle in pixels and its depth range.
type Viewport struct {
	TopLeftX, TopLeftY, Width, Height, MinDepth, MaxDepth float32
}

// Rect is a scissor rectangle in pixels.
type Rect struct{ Left, Top, Right, Bottom int32 }

// Box is D3D11_BOX, a region of a resource in texels, or bytes for a
// buffer; Right, Bottom and Back are exclusive.
type Box struct{ Left, Top, Front, Right, Bottom, Back uint32 }

// Device wraps ID3D11Device.
type Device struct{ P unsafe.Pointer }

// Context wraps ID3D11DeviceContext.
type Context struct{ P unsafe.Pointer }

type (
	// Buffer wraps ID3D11Buffer.
	Buffer struct{ P unsafe.Pointer }
	// Texture2D wraps ID3D11Texture2D.
	Texture2D struct{ P unsafe.Pointer }
	// ShaderResourceView wraps ID3D11ShaderResourceView.
	ShaderResourceView struct{ P unsafe.Pointer }
	// RenderTargetView wraps ID3D11RenderTargetView.
	RenderTargetView struct{ P unsafe.Pointer }
	// DepthStencilView wraps ID3D11DepthStencilView.
	DepthStencilView struct{ P unsafe.Pointer }
	// InputLayout wraps ID3D11InputLayout.
	InputLayout struct{ P unsafe.Pointer }
	// VertexShader wraps ID3D11VertexShader.
	VertexShader struct{ P unsafe.Pointer }
	// PixelShader wraps ID3D11PixelShader.
	PixelShader struct{ P unsafe.Pointer }
	// BlendState wraps ID3D11BlendState.
	BlendState struct{ P unsafe.Pointer }
	// RasterizerState wraps ID3D11RasterizerState.
	RasterizerState struct{ P unsafe.Pointer }
	// DepthStencilState wraps ID3D11DepthStencilState.
	DepthStencilState struct{ P unsafe.Pointer }
	// SamplerState wraps ID3D11SamplerState.
	SamplerState struct{ P unsafe.Pointer }
)

func (d Device) Release()             { com.Release(d.P) }
func (c Context) Release()            { com.Release(c.P) }
func (b Buffer) Release()             { com.Release(b.P) }
func (t Texture2D) Release()          { com.Release(t.P) }
func (v ShaderResourceView) Release() { com.Release(v.P) }
func (v RenderTargetView) Release()   { com.Release(v.P) }
func (v DepthStencilView) Release()   { com.Release(v.P) }
func (l InputLayout) Release()        { com.Release(l.P) }
func (s VertexShader) Release()       { com.Release(s.P) }
func (s PixelShader) Release()        { com.Release(s.P) }
func (s BlendState) Release()         { com.Release(s.P) }
func (s RasterizerState) Release()    { com.Release(s.P) }
func (s DepthStencilState) Release()  { com.Release(s.P) }
func (s SamplerState) Release()       { com.Release(s.P) }

// AddRef takes another reference to the texture, to keep it beyond a
// borrowed handle's lifetime.
func (t Texture2D) AddRef() { com.AddRef(t.P) }

// Resource is a buffer or texture.
type Resource interface{ resource() unsafe.Pointer }

func (b Buffer) resource() unsafe.Pointer    { return b.P }
func (t Texture2D) resource() unsafe.Pointer { return t.P }

// Desc returns the buffer's description.
func (b Buffer) Desc() BufferDesc {
	var d BufferDesc
	com.Call(b.P, 10, uintptr(unsafe.Pointer(&d)))
	return d
}

// Desc returns the texture's description.
func (t Texture2D) Desc() Texture2DDesc {
	var d Texture2DDesc
	com.Call(t.P, 10, uintptr(unsafe.Pointer(&d)))
	return d
}

// CreateDevice makes a hardware device, falling back to WARP. The device
// supports video decoding when the driver allows it, and its immediate context
// is multithread protected so Media Foundation can share it. Apps normally use
// the device Fairy shares among its windows instead (fairy.UseDevice).
func CreateDevice() (Device, Context, error) {
	dev, ctx, _, err := CreateDeviceWithOptions(DeviceOptions{})
	return dev, ctx, err
}

// FeatureLevel returns the device's D3D_FEATURE_LEVEL, such as FEATURE_LEVEL_11_0.
func (d Device) FeatureLevel() uint32 { return uint32(com.Call(d.P, 37)) }

// RemovedReason returns nil while the device works, or the reason it was removed.
func (d Device) RemovedReason() error {
	return com.HRESULT(int32(com.Call(d.P, 39))).Err("device removed")
}

func (d Device) CreateBuffer(desc *BufferDesc, init *SubresourceData) (Buffer, error) {
	var b unsafe.Pointer
	err := com.HR(d.P, 3, uintptr(unsafe.Pointer(desc)), uintptr(unsafe.Pointer(init)), uintptr(unsafe.Pointer(&b))).Err("CreateBuffer")
	return Buffer{b}, err
}

// CreateBufferData makes a buffer that starts with the bytes of data, a slice
// of vertices, indices or a constant block (any fixed-size element type).
func CreateBufferData[T any](d Device, bind, usage uint32, data []T) (Buffer, error) {
	if len(data) == 0 {
		return Buffer{}, errors.New("d3d: CreateBufferData with no data")
	}
	size := uint32(len(data)) * uint32(unsafe.Sizeof(data[0]))
	desc := BufferDesc{ByteWidth: size, Usage: usage, BindFlags: bind}
	if usage == USAGE_DYNAMIC {
		desc.CPUAccessFlags = CPU_ACCESS_WRITE
	}
	return d.CreateBuffer(&desc, &SubresourceData{SysMem: unsafe.Pointer(&data[0])})
}

func (d Device) CreateTexture2D(desc *Texture2DDesc, init *SubresourceData) (Texture2D, error) {
	var t unsafe.Pointer
	err := com.HR(d.P, 5, uintptr(unsafe.Pointer(desc)), uintptr(unsafe.Pointer(init)), uintptr(unsafe.Pointer(&t))).Err("CreateTexture2D")
	return Texture2D{t}, err
}

// CreateShaderResourceView makes a view of res; a nil desc views the whole resource.
func (d Device) CreateShaderResourceView(res Resource, desc *SRVDesc) (ShaderResourceView, error) {
	var v unsafe.Pointer
	err := com.HR(d.P, 7, uintptr(res.resource()), uintptr(unsafe.Pointer(desc)), uintptr(unsafe.Pointer(&v))).Err("CreateShaderResourceView")
	return ShaderResourceView{v}, err
}

// CreateRenderTargetView makes a view of tex; a nil desc views mip 0.
func (d Device) CreateRenderTargetView(tex Texture2D, desc *RTVDesc) (RenderTargetView, error) {
	var v unsafe.Pointer
	err := com.HR(d.P, 9, uintptr(tex.P), uintptr(unsafe.Pointer(desc)), uintptr(unsafe.Pointer(&v))).Err("CreateRenderTargetView")
	return RenderTargetView{v}, err
}

// CreateDepthStencilView makes a view of a texture bound with BIND_DEPTH_STENCIL.
func (d Device) CreateDepthStencilView(tex Texture2D, desc *DSVDesc) (DepthStencilView, error) {
	var v unsafe.Pointer
	err := com.HR(d.P, 10, uintptr(tex.P), uintptr(unsafe.Pointer(desc)), uintptr(unsafe.Pointer(&v))).Err("CreateDepthStencilView")
	return DepthStencilView{v}, err
}

// CreateInputLayout matches elements against the input signature of the
// vertex shader bytecode vsCode.
func (d Device) CreateInputLayout(elements []InputElement, vsCode []byte) (InputLayout, error) {
	if len(elements) == 0 || len(vsCode) == 0 {
		return InputLayout{}, errors.New("d3d: CreateInputLayout needs elements and shader code")
	}
	descs := make([]inputElementDesc, len(elements))
	for i, e := range elements {
		name, err := windows.BytePtrFromString(e.SemanticName)
		if err != nil {
			return InputLayout{}, err
		}
		descs[i] = inputElementDesc{name, e.SemanticIndex, e.Format, e.InputSlot, e.AlignedByteOffset, e.InputSlotClass, e.InstanceDataStepRate}
	}
	var l unsafe.Pointer
	err := com.HR(d.P, 11, uintptr(unsafe.Pointer(&descs[0])), uintptr(len(descs)),
		uintptr(unsafe.Pointer(&vsCode[0])), uintptr(len(vsCode)), uintptr(unsafe.Pointer(&l))).Err("CreateInputLayout")
	return InputLayout{l}, err
}

func (d Device) CreateVertexShader(code []byte) (VertexShader, error) {
	if len(code) == 0 {
		return VertexShader{}, errors.New("d3d: empty shader code")
	}
	var s unsafe.Pointer
	err := com.HR(d.P, 12, uintptr(unsafe.Pointer(&code[0])), uintptr(len(code)), 0, uintptr(unsafe.Pointer(&s))).Err("CreateVertexShader")
	return VertexShader{s}, err
}

func (d Device) CreatePixelShader(code []byte) (PixelShader, error) {
	if len(code) == 0 {
		return PixelShader{}, errors.New("d3d: empty shader code")
	}
	var s unsafe.Pointer
	err := com.HR(d.P, 15, uintptr(unsafe.Pointer(&code[0])), uintptr(len(code)), 0, uintptr(unsafe.Pointer(&s))).Err("CreatePixelShader")
	return PixelShader{s}, err
}

func (d Device) CreateBlendState(desc *BlendDesc) (BlendState, error) {
	var s unsafe.Pointer
	err := com.HR(d.P, 20, uintptr(unsafe.Pointer(desc)), uintptr(unsafe.Pointer(&s))).Err("CreateBlendState")
	return BlendState{s}, err
}

func (d Device) CreateDepthStencilState(desc *DepthStencilDesc) (DepthStencilState, error) {
	var s unsafe.Pointer
	err := com.HR(d.P, 21, uintptr(unsafe.Pointer(desc)), uintptr(unsafe.Pointer(&s))).Err("CreateDepthStencilState")
	return DepthStencilState{s}, err
}

func (d Device) CreateRasterizerState(desc *RasterizerDesc) (RasterizerState, error) {
	var s unsafe.Pointer
	err := com.HR(d.P, 22, uintptr(unsafe.Pointer(desc)), uintptr(unsafe.Pointer(&s))).Err("CreateRasterizerState")
	return RasterizerState{s}, err
}

func (d Device) CreateSamplerState(desc *SamplerDesc) (SamplerState, error) {
	var s unsafe.Pointer
	err := com.HR(d.P, 23, uintptr(unsafe.Pointer(desc)), uintptr(unsafe.Pointer(&s))).Err("CreateSamplerState")
	return SamplerState{s}, err
}

// MultisampleQualityLevels returns how many quality levels the device has
// for format at count samples per pixel; 0 means unsupported.
func (d Device) MultisampleQualityLevels(format, count uint32) uint32 {
	var n uint32
	if com.HR(d.P, 30, uintptr(format), uintptr(count), uintptr(unsafe.Pointer(&n))).Failed() {
		return 0
	}
	return n
}

// DeviceLost reports whether err comes from a removed, reset or hung device.
// Resources made on that device are unusable; make them again on the device
// fairy.UseDevice hands out next.
func DeviceLost(err error) bool {
	var hr com.HRESULT
	if !errors.As(err, &hr) {
		return false
	}
	switch uint32(hr) {
	case DXGI_ERROR_DEVICE_REMOVED, DXGI_ERROR_DEVICE_RESET, DXGI_ERROR_DEVICE_HUNG, DXGI_ERROR_DRIVER_INTERNAL_ERROR:
		return true
	}
	return false
}

// HResult returns the HRESULT carried by an error from this package.
func HResult(err error) (uint32, bool) {
	var hr com.HRESULT
	if !errors.As(err, &hr) {
		return 0, false
	}
	return uint32(hr), true
}
