//go:build windows

package render

import "testing"

func backdropRenderer(t *testing.T, w, h int) *Renderer {
	t.Helper()
	d := warpDevice(t)
	r := &Renderer{ds: d, dev: d.Dev, ctx: d.Ctx, width: w, height: h}
	t.Cleanup(func() { r.ds = nil; r.Release() })
	var err error
	if r.target, err = NewTarget(d, w, h); err != nil {
		t.Fatal(err)
	}
	r.rtv = r.target.rtv
	if err := r.init(); err != nil {
		t.Fatal(err)
	}
	return r
}
