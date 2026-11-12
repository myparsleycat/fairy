package fairy

import (
	"math"
	"sync"

	"github.com/myparsleycat/fairy/internal/render"
)

// stillTextures holds the textures of still images on the shared device, so
// that the windows and captures showing an Image upload it once. Each of
// their texEntry values holds one reference.
var stillTextures = struct {
	sync.Mutex
	m map[stillTexKey]*stillTexture
}{m: map[stillTexKey]*stillTexture{}}

type stillTexKey struct{ id, gen uint64 }

type stillTexture struct {
	tex  *render.Texture
	refs int
}

// acquireStill returns a reference to img's texture on r's device. Unless
// upload is set it does not upload one that no window holds, and returns nil.
func acquireStill(r *render.Renderer, img *Image, upload bool) (tex *render.Texture, uploaded bool, err error) {
	k := stillTexKey{img.id, r.Device().Gen}
	stillTextures.Lock()
	defer stillTextures.Unlock()
	if s := stillTextures.m[k]; s != nil {
		s.refs++
		return s.tex, false, nil
	}
	if !upload {
		return nil, false, nil
	}
	if tex, err = r.NewTexture(img.data.W, img.data.H, img.data.Pixels, false); err != nil {
		return nil, false, err
	}
	stillTextures.m[k] = &stillTexture{tex: tex, refs: 1}
	return tex, true, nil
}

// releaseStill drops a reference from acquireStill, releasing the texture
// with the last one.
func releaseStill(id, gen uint64) {
	k := stillTexKey{id, gen}
	stillTextures.Lock()
	defer stillTextures.Unlock()
	s := stillTextures.m[k]
	if s == nil {
		return
	}
	if s.refs--; s.refs == 0 {
		s.tex.Release()
		delete(stillTextures.m, k)
	}
}

// maxUploadBytesPerFrame bounds upload, update and mip writes after the first
// operation in a frame. A single larger operation can always make progress.
const maxUploadBytesPerFrame = 32 << 20

type imageWorkKey struct {
	id uint64
	op uint8
}

const (
	imageUpload uint8 = iota
	imageUpdate
	imageMips
)

type imageWorkPending struct {
	seq, epoch uint64
}

func (w *Window) allowImageWork(key imageWorkKey, cost int64) bool {
	if w.capture != nil || cost == 0 {
		delete(w.imageWork, key)
		return true
	}
	wait, queued := w.imageWork[key]
	priority := true
	for other, pending := range w.imageWork {
		if other != key && (!queued || pending.seq < wait.seq) {
			priority = false
			break
		}
	}
	if priority && (w.uploaded == 0 || cost <= maxUploadBytesPerFrame-w.uploaded) {
		w.uploaded += cost
		delete(w.imageWork, key)
		return true
	}
	if w.imageWork == nil {
		w.imageWork = make(map[imageWorkKey]imageWorkPending)
	}
	if !queued {
		w.imageWorkSeq++
		wait.seq = w.imageWorkSeq
	}
	wait.epoch = w.imageWorkEpoch
	w.imageWork[key] = wait
	w.frameAt(w.now())
	return false
}

func (w *Window) finishImageWork() {
	for key, wait := range w.imageWork {
		if wait.epoch != w.imageWorkEpoch {
			delete(w.imageWork, key)
		}
	}
}

func (w *Window) promoteImageMips(id uint64, tex *render.Texture) error {
	cost := tex.MipWork()
	if !w.allowImageWork(imageWorkKey{id, imageMips}, cost) {
		return nil
	}
	written, err := tex.PromoteMips(w.r.Device())
	if w.capture == nil {
		w.uploaded -= cost - written
	}
	return err
}

// dropTexture releases the window's reference to the texture of image id.
func (w *Window) dropTexture(id uint64, t *texEntry) {
	if t.shared {
		releaseStill(id, t.gen)
	} else {
		t.tex.Release()
	}
	delete(w.textures, id)
}

// wantsMips reports whether a texture drawn into bounds (device pixels) from
// the uv rectangle and then scaled by xs is reduced to half or less on an
// axis, where bilinear filtering skips texels and aliases.
func wantsMips(tex *render.Texture, bounds, uv [4]float32, xs float32) bool {
	if !tex.CanMip() || !(xs > 0) {
		return false
	}
	return float32(tex.Width)*uv[2] >= 2*bounds[2]*xs || float32(tex.Height)*uv[3] >= 2*bounds[3]*xs
}

// scaleOf returns the smaller axis scale of m.
func (m xform) scaleOf() float32 {
	hypot := func(x, y float32) float32 { return float32(math.Hypot(float64(x), float64(y))) }
	return min(hypot(m.a, m.b), hypot(m.c, m.d))
}
