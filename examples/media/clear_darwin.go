package main

import (
	"github.com/ebitengine/purego/objc"

	"github.com/myparsleycat/fairy"
)

var (
	selRenderPassDescriptor = objc.RegisterName("renderPassDescriptor")
	selColorAttachments     = objc.RegisterName("colorAttachments")
	selObjectAtIndex        = objc.RegisterName("objectAtIndexedSubscript:")
	selSetTexture           = objc.RegisterName("setTexture:")
	selSetLoadAction        = objc.RegisterName("setLoadAction:")
	selSetStoreAction       = objc.RegisterName("setStoreAction:")
	selSetClearColor        = objc.RegisterName("setClearColor:")
	selCommandBuffer        = objc.RegisterName("commandBuffer")
	selRenderEncoder        = objc.RegisterName("renderCommandEncoderWithDescriptor:")
	selEndEncoding          = objc.RegisterName("endEncoding")
	selCommit               = objc.RegisterName("commit")
)

// mtlClearColor is MTLClearColor.
type mtlClearColor struct{ R, G, B, A float64 }

// clearView fills the surface target, standing in for an app's own Metal
// renderer: a render pass that clears the texture and draws nothing. The
// Surface draw function runs inside an autorelease pool, which frees the
// autoreleased descriptor, command buffer and encoder.
func clearView(f fairy.SurfaceFrame, rgba [4]float32) {
	pass := objc.ID(objc.GetClass("MTLRenderPassDescriptor")).Send(selRenderPassDescriptor)
	color := pass.Send(selColorAttachments).Send(selObjectAtIndex, uint64(0))
	color.Send(selSetTexture, objc.ID(f.Texture))
	color.Send(selSetLoadAction, uint64(2))  // MTLLoadActionClear
	color.Send(selSetStoreAction, uint64(1)) // MTLStoreActionStore
	color.Send(selSetClearColor, mtlClearColor{float64(rgba[0]), float64(rgba[1]), float64(rgba[2]), float64(rgba[3])})
	cb := objc.ID(f.GPU().Queue).Send(selCommandBuffer)
	cb.Send(selRenderEncoder, pass).Send(selEndEncoding)
	cb.Send(selCommit)
}
