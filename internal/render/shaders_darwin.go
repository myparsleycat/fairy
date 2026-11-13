//go:build darwin

package render

import (
	_ "embed"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/darwin"
)

// shaderSource is the Metal port of shaders.hlsl. Metal compiles it when the
// first renderer starts; the system caches the result between launches.
//
//go:embed shaders.metal
var shaderSource string

// Blend modes, indexing pipelines.states.
const (
	blendOver  = iota // premultiplied source over the target
	blendErase        // scales the target by one less the prim's alpha
	blendAdd          // adds the premultiplied source
	blendModes
)

// pipelines are the render pipeline states of a device, one per blend mode,
// all drawing into BGRA8Unorm targets.
type pipelines struct {
	states [blendModes]id
}

func (p *pipelines) release() {
	if p == nil {
		return
	}
	for i, s := range p.states {
		darwin.Release(s)
		p.states[i] = 0
	}
}

// pipelines compiles the shaders and makes the pipeline states the first
// time a renderer on d needs them.
func (d *Device) pipelines() (*pipelines, error) {
	d.pipeMu.Lock()
	defer d.pipeMu.Unlock()
	if d.pipes != nil {
		return d.pipes, nil
	}
	var p pipelines
	var err error
	withPool(func() {
		opts := darwin.New("MTLCompileOptions")
		defer darwin.Release(opts)
		var e id
		lib := darwin.Send(d.dev, "newLibraryWithSource:options:error:", darwin.String(shaderSource), opts, unsafe.Pointer(&e))
		if lib == 0 {
			err = nsError("compile shaders.metal", e)
			return
		}
		defer darwin.Release(lib)
		vs := darwin.Send(lib, "newFunctionWithName:", darwin.String("vs_main"))
		defer darwin.Release(vs)
		fs := darwin.Send(lib, "newFunctionWithName:", darwin.String("ps_main"))
		defer darwin.Release(fs)
		for mode := range blendModes {
			desc := darwin.New("MTLRenderPipelineDescriptor")
			desc.Send(darwin.Sel("setVertexFunction:"), vs)
			desc.Send(darwin.Sel("setFragmentFunction:"), fs)
			ca := darwin.Send(darwin.Send(desc, "colorAttachments"), "objectAtIndexedSubscript:", uint64(0))
			ca.Send(darwin.Sel("setPixelFormat:"), uint64(pixelBGRA8Unorm))
			ca.Send(darwin.Sel("setBlendingEnabled:"), true)
			src, dst := uint64(blendOne), uint64(blendOneMinusSourceAlpa)
			switch mode {
			case blendErase:
				src = blendZero
			case blendAdd:
				dst = blendOne
			}
			ca.Send(darwin.Sel("setSourceRGBBlendFactor:"), src)
			ca.Send(darwin.Sel("setSourceAlphaBlendFactor:"), src)
			ca.Send(darwin.Sel("setDestinationRGBBlendFactor:"), dst)
			ca.Send(darwin.Sel("setDestinationAlphaBlendFactor:"), dst)
			ca.Send(darwin.Sel("setRgbBlendOperation:"), uint64(blendOpAdd))
			ca.Send(darwin.Sel("setAlphaBlendOperation:"), uint64(blendOpAdd))
			var e id
			p.states[mode] = darwin.Send(d.dev, "newRenderPipelineStateWithDescriptor:error:", desc, unsafe.Pointer(&e))
			darwin.Release(desc)
			if p.states[mode] == 0 {
				err = nsError("create render pipeline", e)
				return
			}
		}
	})
	if err != nil {
		p.release()
		return nil, err
	}
	d.pipes = &p
	return d.pipes, nil
}
