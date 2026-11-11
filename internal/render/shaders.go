//go:build windows

package render

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"errors"

	"github.com/myparsleycat/fairy/d3d"
)

//go:generate go run ./shadergen

//go:embed shaders.hlsl
var shaderSource []byte

// compiledShaders is shaders.bin, the shaders compiled from shaders.hlsl by go
// generate, so that windows open without running the HLSL compiler.
//
//go:embed shaders.bin
var compiledShaders []byte

func sourceHash(src []byte) [32]byte {
	return sha256.Sum256(bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n")))
}

// CompileShaders compiles src into the shaders.bin format: the source hash,
// the vertex shader length and code, then the pixel shader code.
func CompileShaders(src []byte) ([]byte, error) {
	vs, err := d3d.Compile(src, "shaders.hlsl", "vs_main", "vs_4_1")
	if err != nil {
		return nil, err
	}
	ps, err := d3d.Compile(src, "shaders.hlsl", "ps_main", "ps_4_1")
	if err != nil {
		return nil, err
	}
	h := sourceHash(src)
	out := append([]byte(nil), h[:]...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(vs)))
	out = append(out, vs...)
	return append(out, ps...), nil
}

func splitShaders(bin []byte) (vs, ps []byte, err error) {
	if len(bin) < 36 {
		return nil, nil, errors.New("render: short shader blob")
	}
	n := int(binary.LittleEndian.Uint32(bin[32:]))
	if 36+n > len(bin) {
		return nil, nil, errors.New("render: corrupt shader blob")
	}
	return bin[36 : 36+n], bin[36+n:], nil
}

// shaders returns the compiled shaders, compiling at run time when
// shaders.hlsl changed since shaders.bin was generated.
func shaders() (vs, ps []byte, err error) {
	h := sourceHash(shaderSource)
	bin := compiledShaders
	if len(bin) < 32 || !bytes.Equal(bin[:32], h[:]) {
		if bin, err = CompileShaders(shaderSource); err != nil {
			return nil, nil, err
		}
	}
	return splitShaders(bin)
}
