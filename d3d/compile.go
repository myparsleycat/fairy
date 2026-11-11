//go:build windows

package d3d

import (
	"errors"
	"fmt"
	"image"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/myparsleycat/fairy/internal/com"
)

// CompileOptions adjusts Compile.
type CompileOptions struct {
	// Flags are D3DCOMPILE_* flags. Zero uses COMPILE_OPTIMIZATION_LEVEL3 |
	// COMPILE_ENABLE_STRICTNESS.
	Flags uint32
	// Defines are preprocessor macros as name, value pairs.
	Defines [][2]string
}

// Compile compiles HLSL source with d3dcompiler_47 for target, such as
// "vs_5_0" or "ps_4_1", and returns the bytecode. #include is not supported.
func Compile(src []byte, name, entry, target string) ([]byte, error) {
	return CompileWith(src, name, entry, target, CompileOptions{})
}

// CompileWith is Compile with flags and macros.
func CompileWith(src []byte, name, entry, target string, o CompileOptions) ([]byte, error) {
	if len(src) == 0 {
		return nil, errors.New("d3d: empty shader source")
	}
	cname, err := windows.BytePtrFromString(name)
	if err != nil {
		return nil, err
	}
	centry, err := windows.BytePtrFromString(entry)
	if err != nil {
		return nil, err
	}
	ctarget, err := windows.BytePtrFromString(target)
	if err != nil {
		return nil, err
	}
	var macros []*byte
	for _, d := range o.Defines {
		n, err := windows.BytePtrFromString(d[0])
		if err != nil {
			return nil, err
		}
		v, err := windows.BytePtrFromString(d[1])
		if err != nil {
			return nil, err
		}
		macros = append(macros, n, v)
	}
	var pmacros uintptr
	if len(macros) > 0 {
		macros = append(macros, nil, nil)
		pmacros = uintptr(unsafe.Pointer(&macros[0]))
	}
	flags := o.Flags
	if flags == 0 {
		flags = COMPILE_OPTIMIZATION_LEVEL3 | COMPILE_ENABLE_STRICTNESS
	}
	var code, errs unsafe.Pointer
	r, _, _ := pD3DCompile.Call(uintptr(unsafe.Pointer(&src[0])), uintptr(len(src)), uintptr(unsafe.Pointer(cname)), pmacros, 0,
		uintptr(unsafe.Pointer(centry)), uintptr(unsafe.Pointer(ctarget)), uintptr(flags), 0,
		uintptr(unsafe.Pointer(&code)), uintptr(unsafe.Pointer(&errs)))
	runtime.KeepAlive(macros)
	if errs != nil {
		defer com.Release(errs)
	}
	if hr := com.HRESULT(int32(r)); hr.Failed() {
		msg := ""
		if errs != nil {
			msg = string(blobBytes(errs))
		}
		return nil, fmt.Errorf("D3DCompile %s/%s: %w\n%s", name, entry, hr, msg)
	}
	defer com.Release(code)
	return append([]byte(nil), blobBytes(code)...), nil
}

func blobBytes(blob unsafe.Pointer) []byte {
	p := com.Call(blob, 3)
	n := com.Call(blob, 4)
	if p == 0 || n == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(com.Ptr(p)), n)
}

// ReadPixels copies subresource 0 of a single-sampled texture with four
// bytes per texel, such as FORMAT_B8G8R8A8_UNORM or FORMAT_R8G8B8A8_UNORM,
// into memory through a staging texture. Rows are packed: Width*4 bytes each.
// Resolve multisampled textures with ResolveSubresource first.
func (c Context) ReadPixels(tex Texture2D) ([]byte, Texture2DDesc, error) {
	desc := tex.Desc()
	if desc.SampleDesc.Count > 1 {
		return nil, desc, errors.New("d3d: ReadPixels of a multisampled texture")
	}
	if !fourByteFormat(desc.Format) {
		return nil, desc, fmt.Errorf("d3d: ReadPixels of format %d", desc.Format)
	}
	dev := c.Device()
	defer dev.Release()
	staging, err := dev.CreateTexture2D(&Texture2DDesc{
		Width:          desc.Width,
		Height:         desc.Height,
		MipLevels:      1,
		ArraySize:      1,
		Format:         desc.Format,
		SampleDesc:     SampleDesc{Count: 1},
		Usage:          USAGE_STAGING,
		CPUAccessFlags: CPU_ACCESS_READ,
	}, nil)
	if err != nil {
		return nil, desc, err
	}
	defer staging.Release()
	c.CopySubresourceRegion(staging, 0, 0, 0, 0, tex, 0, nil)
	m, err := c.Map(staging, MAP_READ)
	if err != nil {
		return nil, desc, err
	}
	defer c.Unmap(staging)
	w, h := int(desc.Width), int(desc.Height)
	out := make([]byte, w*h*4)
	for y := range h {
		copy(out[y*w*4:(y+1)*w*4], unsafe.Slice((*byte)(unsafe.Add(m.Data, y*int(m.RowPitch))), w*4))
	}
	return out, desc, nil
}

func fourByteFormat(f uint32) bool {
	switch f {
	case FORMAT_B8G8R8A8_UNORM, FORMAT_B8G8R8A8_UNORM_SRGB, FORMAT_R8G8B8A8_UNORM, FORMAT_R8G8B8A8_UNORM_SRGB:
		return true
	}
	return false
}

// ReadImage reads a B8G8R8A8 or R8G8B8A8 texture like ReadPixels and converts
// it to straight alpha. Set premultiplied when the texture holds
// premultiplied color, as Surface targets do.
func (c Context) ReadImage(tex Texture2D, premultiplied bool) (*image.NRGBA, error) {
	pix, desc, err := c.ReadPixels(tex)
	if err != nil {
		return nil, err
	}
	bgra := desc.Format == FORMAT_B8G8R8A8_UNORM || desc.Format == FORMAT_B8G8R8A8_UNORM_SRGB
	img := image.NewNRGBA(image.Rect(0, 0, int(desc.Width), int(desc.Height)))
	toNRGBA(img.Pix, pix, bgra, premultiplied)
	return img, nil
}

// toNRGBA converts four-byte texels in src to straight-alpha RGBA in dst.
func toNRGBA(dst, src []byte, bgra, premultiplied bool) {
	for i := 0; i+3 < len(src) && i+3 < len(dst); i += 4 {
		r, g, b, a := src[i], src[i+1], src[i+2], src[i+3]
		if bgra {
			r, b = b, r
		}
		if premultiplied && a != 0 && a != 255 {
			r = unpremul(r, a)
			g = unpremul(g, a)
			b = unpremul(b, a)
		} else if premultiplied && a == 0 {
			r, g, b = 0, 0, 0
		}
		dst[i], dst[i+1], dst[i+2], dst[i+3] = r, g, b, a
	}
}

func unpremul(c, a byte) byte {
	return byte(min((uint32(c)*255+uint32(a)/2)/uint32(a), 255))
}
