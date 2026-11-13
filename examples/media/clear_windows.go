package main

import "github.com/myparsleycat/fairy"

// clearView fills the surface target, standing in for an app's own Direct3D
// renderer.
func clearView(f fairy.SurfaceFrame, rgba [4]float32) {
	_, rtv := f.RenderTarget()
	f.GPU().Context.ClearRenderTargetView(rtv, rgba)
}
