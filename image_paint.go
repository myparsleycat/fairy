//go:build windows

package fairy

import (
	"cmp"
	"log"
	"math"
	"runtime"
	"slices"
	"sync"
	"weak"

	"github.com/myparsleycat/fairy/internal/geom"
	"github.com/myparsleycat/fairy/internal/imagecache"
	"github.com/myparsleycat/fairy/internal/imaging"
	"github.com/myparsleycat/fairy/internal/render"
)

// maxIdleTextureBytes is the texture memory a window keeps for images it did
// not draw in its last frame, so that scrolling back does not upload them
// again.
const maxIdleTextureBytes = 32 << 20

// sweepTextures releases the textures not drawn in frame whose Image was
// garbage collected or dropped from its cache, then the least recently drawn
// of the rest beyond keep bytes.
func (w *Window) sweepTextures(frame int, keep int64) {
	var idle int64
	for id, t := range w.textures {
		if t.frame == frame {
			continue
		}
		if img := t.img.Value(); img == nil || img.core().Retired.Load() {
			w.dropTexture(id, t)
			continue
		}
		idle += t.tex.ByteSize()
	}
	if idle <= keep {
		return
	}
	var ids []uint64
	for id, t := range w.textures {
		if t.frame != frame {
			ids = append(ids, id)
		}
	}
	slices.SortFunc(ids, func(a, b uint64) int { return cmp.Compare(w.textures[a].frame, w.textures[b].frame) })
	for _, id := range ids {
		if idle <= keep {
			break
		}
		t := w.textures[id]
		idle -= t.tex.ByteSize()
		w.dropTexture(id, t)
	}
}

func (w *Window) releaseTextures() {
	for id, t := range w.textures {
		w.dropTexture(id, t)
	}
}

// texture returns the window's texture of img showing frame n. An animated
// image has one texture whose pixels are replaced when the frame changes.
func (w *Window) texture(img *Image, n int) *render.Texture {
	tex, _ := w.uploadImage(img, n)
	return tex
}

// uploadImage returns the window's texture of img, uploading it when needed.
// Past maxUploadBytesPerFrame it returns nil and asks for another frame,
// except in captures, which must be complete.
func (w *Window) uploadImage(img *Image, n int) (*render.Texture, error) {
	t, ok := w.textures[img.core().ID]
	if !ok {
		if img.core().Data.W == 0 || img.core().Data.H == 0 {
			return nil, nil
		}
		bytes := int64(img.core().Data.W) * int64(img.core().Data.H) * 4
		still := !img.animated()
		var tex *render.Texture
		var err error
		shown := 0
		if still {
			tex, _, err = acquireStill(w.r, img, false)
		}
		key := imageWorkKey{img.core().ID, imageUpload}
		if tex == nil && err == nil && w.allowImageWork(key, bytes) {
			var uploaded bool
			if still {
				tex, uploaded, err = acquireStill(w.r, img, true)
			} else {
				create := func(pix []byte) { tex, err = w.r.NewTexture(img.core().Data.W, img.core().Data.H, pix, true) }
				switch {
				case n < len(img.core().Data.FrameData):
					create(img.core().Data.FrameData[n].Pixels)
					shown = n
				case img.core().Data.Stream != nil && n != 0:
					if img.core().Data.Stream.WithFrame(n, create) {
						shown = n
					} else {
						create(img.core().Data.Pixels)
						w.frameAt(w.now().Add(streamRetry))
					}
				default:
					create(img.core().Data.Pixels)
				}
				uploaded = err == nil
			}
			if !uploaded && w.capture == nil {
				w.uploaded -= bytes
			}
		}
		if err != nil {
			return nil, err
		}
		if tex == nil {
			w.frameAt(w.now())
			return nil, nil
		}
		delete(w.imageWork, key)
		t = &texEntry{tex: tex, img: weak.Make(img), shared: still, shown: shown, gen: w.r.Device().Gen, frame: w.frame}
		w.textures[img.core().ID] = t
		return t.tex, nil
	}
	t.frame = w.frame
	if t.shown != n {
		update := func(pix []byte) {
			if !w.allowImageWork(imageWorkKey{img.core().ID, imageUpdate}, t.tex.ByteSize()) {
				return
			}
			dev := w.r.Device()
			runtime.LockOSThread()
			dev.Lock()
			t.tex.Update(dev.Ctx, pix)
			dev.Unlock()
			runtime.UnlockOSThread()
			t.shown = n
		}
		switch {
		case n < len(img.core().Data.FrameData):
			update(img.core().Data.FrameData[n].Pixels)
		case img.core().Data.Stream != nil:
			if !img.core().Data.Stream.WithFrame(n, update) {
				// Keep the frame shown until the stream has decoded n.
				w.frameAt(w.now().Add(streamRetry))
			}
		}
	}
	return t.tex, nil
}

// imageFrame picks the frame of an animated image to paint now and asks for
// a frame when it changes. Animations start when first painted, pause with
// the window while it is hidden or the element is scrolled out of view
// (the clock keeps running), and show their first frame when motion is
// reduced. The clock is kept on clock, the image the element shows, so that
// a lazy image keeps its place when a decode of another size replaces img.
func (w *Window) imageFrame(img, clock *Image) int {
	if !img.animated() || reducedMotion.Load() {
		return 0
	}
	now := w.now()
	if clock.start.IsZero() {
		clock.start = now
	}
	n, wait := img.core().Data.FrameAt(now.Sub(clock.start))
	if wait > 0 {
		w.frameAt(now.Add(wait))
	}
	return n
}

func (w *Window) paintImage(e *El, box geom.Rect, radii [4]float32, clip [4]float32, opacity float32) {
	img := e.image
	if img == nil || box.Empty() || !(e.xf.scaleOf() > 0) {
		return
	}
	dst, uv := geom.FitImage(box, float32(img.core().Data.W), float32(img.core().Data.H), geom.Fit(e.fit))
	prim := render.Prim{
		Bounds: w.snap(dst),
		Clip:   clip,
		Color:  [4]float32{1, 1, 1, opacity},
		Radii:  radii,
		UV:     uv,
	}
	if w.capture != nil {
		w.capture.addImage(prim, img, imageDrawBound(box, dst, w.scale, e.fit, e.xf), e.xf.scaleOf())
		return
	}
	var tex *render.Texture
	var flags uint32
	switch {
	case img.gpu != nil:
		tex = img.gpu.texture(w.r.Device().Gen)
		if img.gpu.premul {
			flags = render.FlagPremultiplied
		}
	case img.core().Lazy != nil:
		bound := imageDrawBound(box, dst, w.scale, e.fit, e.xf)
		prev := img.core().Lazy.Last()
		if vc := img.core().Lazy.Variant(img.core().Data.W, img.core().Data.H, bound); vc != nil {
			v := wrapCore(vc)
			tex = w.texture(v, w.imageFrame(v, img))
			if tex == nil && prev != nil && prev != vc {
				// v waits for its upload; the size drawn last stays.
				if t, ok := w.textures[prev.ID]; ok {
					t.frame = w.frame
					tex = t.tex
					img.core().Lazy.Show(prev)
				}
			}
		}
	default:
		tex = w.texture(img, w.imageFrame(img, img))
	}
	if tex == nil {
		return
	}
	if wantsMips(tex, prim.Bounds, prim.UV, e.xf.scaleOf()) {
		if err := w.promoteImageMips(img.core().ID, tex); err != nil {
			log.Printf("fairy: image mipmaps: %v", err)
		}
	}
	if tex.Premultiplied {
		flags = render.FlagPremultiplied
	}
	prim.Flags = flags
	w.scene.AddImage(prim, tex)
}

func imageDrawBound(box, dst geom.Rect, scale float32, fit Fit, transform xform) imaging.Bound {
	if fit == FitContain {
		box = dst
	}
	sx := float32(math.Hypot(float64(transform.a), float64(transform.b)))
	sy := float32(math.Hypot(float64(transform.c), float64(transform.d)))
	return imaging.Bound{W: imagecache.SizeStep(box.W * scale * sx), H: imagecache.SizeStep(box.H * scale * sy), Cover: fit != FitContain}
}

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
	k := stillTexKey{img.core().ID, r.Device().Gen}
	stillTextures.Lock()
	defer stillTextures.Unlock()
	if s := stillTextures.m[k]; s != nil {
		s.refs++
		return s.tex, false, nil
	}
	if !upload {
		return nil, false, nil
	}
	if tex, err = r.NewTexture(img.core().Data.W, img.core().Data.H, img.core().Data.Pixels, false); err != nil {
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
