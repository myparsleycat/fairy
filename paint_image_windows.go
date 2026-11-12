package fairy

import (
	"cmp"
	"log"
	"math"
	"runtime"
	"slices"
	"weak"

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
		if img := t.img.Value(); img == nil || img.retired.Load() {
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
	t, ok := w.textures[img.id]
	if !ok {
		if img.data.W == 0 || img.data.H == 0 {
			return nil, nil
		}
		bytes := int64(img.data.W) * int64(img.data.H) * 4
		still := !img.animated()
		var tex *render.Texture
		var err error
		shown := 0
		if still {
			tex, _, err = acquireStill(w.r, img, false)
		}
		key := imageWorkKey{img.id, imageUpload}
		if tex == nil && err == nil && w.allowImageWork(key, bytes) {
			var uploaded bool
			if still {
				tex, uploaded, err = acquireStill(w.r, img, true)
			} else {
				create := func(pix []byte) { tex, err = w.r.NewTexture(img.data.W, img.data.H, pix, true) }
				switch {
				case n < len(img.data.FrameData):
					create(img.data.FrameData[n].Pixels)
					shown = n
				case img.data.Stream != nil && n != 0:
					if img.data.Stream.WithFrame(n, create) {
						shown = n
					} else {
						create(img.data.Pixels)
						w.frameAt(w.now().Add(streamRetry))
					}
				default:
					create(img.data.Pixels)
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
		w.textures[img.id] = t
		return t.tex, nil
	}
	t.frame = w.frame
	if t.shown != n {
		update := func(pix []byte) {
			if !w.allowImageWork(imageWorkKey{img.id, imageUpdate}, t.tex.ByteSize()) {
				return
			}
			dev := w.r.Device()
			runtime.LockOSThread()
			dev.Lock()
			t.tex.Update(dev, pix)
			dev.Unlock()
			runtime.UnlockOSThread()
			t.shown = n
		}
		switch {
		case n < len(img.data.FrameData):
			update(img.data.FrameData[n].Pixels)
		case img.data.Stream != nil:
			if !img.data.Stream.WithFrame(n, update) {
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
	n, wait := img.data.FrameAt(now.Sub(clock.start))
	if wait > 0 {
		w.frameAt(now.Add(wait))
	}
	return n
}

func (w *Window) paintImage(e *El, box rectF, radii [4]float32, clip [4]float32, opacity float32) {
	img := e.image
	if img == nil || box.empty() || !(e.xf.scaleOf() > 0) {
		return
	}
	dst, uv := fitImage(box, float32(img.data.W), float32(img.data.H), e.fit)
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
	case img.lazy != nil:
		bound := imageDrawBound(box, dst, w.scale, e.fit, e.xf)
		prev := img.lazy.last()
		if v := img.lazy.variant(img.data.W, img.data.H, bound); v != nil {
			tex = w.texture(v, w.imageFrame(v, img))
			if tex == nil && prev != nil && prev != v {
				// v waits for its upload; the size drawn last stays.
				if t, ok := w.textures[prev.id]; ok {
					t.frame = w.frame
					tex = t.tex
					img.lazy.show(prev)
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
		if err := w.promoteImageMips(img.id, tex); err != nil {
			log.Printf("fairy: image mipmaps: %v", err)
		}
	}
	if tex.Premultiplied {
		flags = render.FlagPremultiplied
	}
	prim.Flags = flags
	w.scene.AddImage(prim, tex)
}

func imageDrawBound(box, dst rectF, scale float32, fit Fit, transform xform) imaging.Bound {
	if fit == FitContain {
		box = dst
	}
	sx := float32(math.Hypot(float64(transform.a), float64(transform.b)))
	sy := float32(math.Hypot(float64(transform.c), float64(transform.d)))
	return imaging.Bound{W: sizeStep(box.w * scale * sx), H: sizeStep(box.h * scale * sy), Cover: fit != FitContain}
}

// fitImage places an iw x ih image in box: the destination rectangle and the
// UV rectangle (x, y, w, h) to sample.
func fitImage(box rectF, iw, ih float32, fit Fit) (rectF, [4]float32) {
	dst := box
	uv := [4]float32{0, 0, 1, 1}
	switch fit {
	case FitCover:
		boxAR, imgAR := box.w/box.h, iw/ih
		if imgAR > boxAR {
			f := boxAR / imgAR
			uv = [4]float32{(1 - f) / 2, 0, f, 1}
		} else if imgAR < boxAR {
			f := imgAR / boxAR
			uv = [4]float32{0, (1 - f) / 2, 1, f}
		}
	case FitContain:
		sc := min(box.w/iw, box.h/ih)
		dw, dh := iw*sc, ih*sc
		dst = rectF{box.x + (box.w-dw)/2, box.y + (box.h-dh)/2, dw, dh}
	}
	return dst, uv
}
