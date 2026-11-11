package fairy

import (
	"context"
	"errors"
	"fmt"
	"image"
	"math"
	"sync/atomic"

	"github.com/myparsleycat/fairy/internal/imaging"
	"github.com/myparsleycat/fairy/internal/render"
	"github.com/myparsleycat/fairy/internal/text"
)

// maxRenderSize is the largest texture edge Direct3D 11 guarantees.
const maxRenderSize = 16384

// ErrImageNotReady means a visible lazy Image is still decoding at the size
// requested by an offscreen render.
var ErrImageNotReady = errors.New("fairy: image is still loading")

// RenderImage draws el into an image without a window, with the same
// renderer, DirectWrite text, font fallback and color emoji as the screen.
// The tree is laid out in a width x height logical-pixel box and drawn at
// scale device pixels per logical pixel, so the image is
// ceil(width*scale) x ceil(height*scale) pixels. The background is
// transparent and the result has straight (not premultiplied) alpha.
//
// Text inherits Segoe UI at 14 logical pixels in near-black unless el sets
// Font, FontSize or Color. Animations and transitions are drawn at their
// resting state; Surfaces draw once; WebViews are left out. el is built as if
// by a View, so components that keep state with Local start from their
// initial state.
//
// While Main runs, drawing happens on the UI thread. A lazy Image that is
// still decoding causes ErrImageNotReady; use RenderImageContext to wait
// from outside the UI thread.
func RenderImage(el *El, width, height, scale float32) (*image.NRGBA, error) {
	return RenderViewImage(func(*Cx) *El { return el }, width, height, scale)
}

// RenderViewImage is RenderImage for a view that needs its client size, local state or overlays.
func RenderViewImage(view func(*Cx) *El, width, height, scale float32) (*image.NRGBA, error) {
	return captureView(context.Background(), view, width, height, scale, false)
}

// RenderImageContext is RenderImage with a cancelable wait for lazy images
// from outside the UI thread. It cannot wait while the caller holds the
// graphics device lock, as in a UseDevice or Surface callback.
func RenderImageContext(ctx context.Context, el *El, width, height, scale float32) (*image.NRGBA, error) {
	return RenderViewImageContext(ctx, func(*Cx) *El { return el }, width, height, scale)
}

// RenderViewImageContext is RenderViewImage with a cancelable wait for lazy
// images from outside the UI thread. Only ImageEl elements with a non-nil
// Image can be awaited. Cancelling does not stop shared image loads.
func RenderViewImageContext(ctx context.Context, view func(*Cx) *El, width, height, scale float32) (*image.NRGBA, error) {
	return captureView(ctx, view, width, height, scale, true)
}

func captureView(ctx context.Context, view func(*Cx) *El, width, height, scale float32, wait bool) (*image.NRGBA, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !(width > 0) || !(height > 0) || !(scale > 0) {
		return nil, errors.New("fairy: RenderImage needs a positive size and scale")
	}
	pw := int(math.Ceil(float64(width * scale)))
	ph := int(math.Ceil(float64(height * scale)))
	if pw > maxRenderSize || ph > maxRenderSize {
		return nil, errors.New("fairy: RenderImage size exceeds 16384 pixels")
	}
	app.mu.Lock()
	session := app.done
	ui := onUIThread()
	inline := session == nil || ui
	app.mu.Unlock()
	locked := render.DeviceLockedByCurrentThread()
	if locked && !inline {
		return nil, errors.New("fairy: cannot dispatch an image capture while holding the graphics device lock")
	}
	wait = wait && !ui && !locked

	var c offscreenCapture
	defer c.release()
	var img *image.NRGBA
	var pending bool
	err := runCapturePhase(ctx, session, inline, func() error {
		if err := c.prepare(view, pw, ph, scale); err != nil {
			return err
		}
		var err error
		if pending, err = c.imagesReady(); err != nil {
			return err
		}
		if pending {
			if !wait {
				return ErrImageNotReady
			}
			return nil
		}
		img, err = c.draw()
		return err
	})
	if err != nil {
		return nil, err
	}
	if !pending {
		return img, nil
	}
	if err := c.waitImages(ctx, session); err != nil {
		return nil, err
	}
	err = runCapturePhase(ctx, session, inline, func() error {
		var err error
		img, err = c.draw()
		return err
	})
	if err != nil {
		return nil, err
	}
	return img, nil
}

func runCapturePhase(ctx context.Context, session <-chan struct{}, inline bool, f func() error) error {
	run := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := f()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	if inline {
		return run()
	}
	result := make(chan error, 1)
	var claimed atomic.Bool
	if !enqueueAppCall(appCall{
		session: session,
		run: func() {
			if claimed.CompareAndSwap(false, true) {
				err := errUIUnavailable
				defer func() { result <- err }()
				err = run()
			}
		},
		cancel: func() { result <- errUIUnavailable },
	}, msgAppCall) {
		return errUIUnavailable
	}
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		if !claimed.CompareAndSwap(false, true) {
			// A phase already owns the capture. Release its resources only
			// after it stops using them.
			<-result
		}
		return ctx.Err()
	}
}

type offscreenCapture struct {
	w      *Window
	images []captureImage
}

type captureImage struct {
	img        *Image
	bound      imaging.Bound
	load       *imageLoad
	bounds, uv [4]float32 // where the prim draws, to decide on mipmaps
	xs         float32    // the element's transform scale
}

func (c *offscreenCapture) addImage(prim render.Prim, img *Image, bound imaging.Bound, xs float32) {
	c.w.scene.AddDeferredImage(prim, len(c.images))
	c.images = append(c.images, captureImage{img: img, bound: bound, bounds: prim.Bounds, uv: prim.UV, xs: xs})
}

func (c *offscreenCapture) imagesReady() (pending bool, err error) {
	c.w.scene.DeferredImages(func(slot int) bool {
		ref := &c.images[slot]
		if ref.img.lazy == nil {
			return true
		}
		img, load, loadErr := ref.img.lazy.requestVariant(ref.img.data.W, ref.img.data.H, ref.bound)
		if loadErr != nil {
			err = loadErr
			return false
		}
		ref.img, ref.load = img, load
		if load != nil {
			select {
			case <-load.done:
				ref.img, ref.load, err = load.img, nil, load.err
			default:
				pending = true
			}
		}
		return err == nil
	})
	return pending, err
}

func (c *offscreenCapture) waitImages(ctx context.Context, session <-chan struct{}) error {
	loads := make(map[*imageLoad]struct{})
	for _, ref := range c.images {
		if ref.load != nil {
			loads[ref.load] = struct{}{}
		}
	}
	ready := make(chan *imageLoad, len(loads))
	for load := range loads {
		load.watch(ready)
	}
	defer func() {
		for load := range loads {
			load.unwatch(ready)
		}
	}()
	for range len(loads) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-session:
			return errUIUnavailable
		case load := <-ready:
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := load.err; err != nil {
				return err
			}
		}
	}
	for i := range c.images {
		ref := &c.images[i]
		if ref.load != nil {
			ref.img, ref.load = ref.load.img, nil
		}
	}
	return nil
}

func (c *offscreenCapture) prepare(view func(*Cx) *El, pw, ph int, scale float32) error {
	r, err := render.NewOffscreen(pw, ph)
	if err != nil {
		return err
	}
	ts, err := text.NewSystem(r.Atlas)
	if err != nil {
		r.Release()
		return err
	}
	w := &Window{
		opts: Options{
			View:      view,
			Font:      "Segoe UI",
			FontSize:  14,
			TextColor: Hex(0x1a1a1a),
		},
		r:         r,
		ts:        ts,
		scale:     scale,
		width:     pw,
		height:    ph,
		states:    map[uint64]*elState{},
		hoverIDs:  map[uint64]bool{},
		textures:  map[uint64]*texEntry{},
		capture:   c,
		surfaces:  map[uint64]*surfaceEntry{},
		webviews:  map[*WebView]bool{},
		iconTiles: map[iconKey]render.Tile{},
		byID:      map[uint64]*El{},
		prevByID:  map[uint64]*El{},
		locals:    map[string]*localEntry{},
		dirty:     true,
		// Keeps invalidate from calling InvalidateRect with hwnd 0, which
		// invalidates every top-level window.
		needPaint: true,
	}
	c.w = w
	ts.BeginBuild()
	w.build()
	ts.EndBuild()
	w.placeAll()
	// A failed Grow leaves the atlas the scene was painted against, so the
	// scene is drawn without the glyphs that did not fit.
	for {
		c.images = c.images[:0]
		if w.paintScene() || r.Atlas.Grow() != nil {
			break
		}
	}
	return nil
}

func (c *offscreenCapture) draw() (*image.NRGBA, error) {
	w := c.w
	r := w.r
	if err := w.scene.BindDeferredImages(func(slot int) (*render.Texture, bool, error) {
		ci := c.images[slot]
		img := ci.img
		var tex *render.Texture
		premul := false
		if img.gpu != nil {
			tex, premul = img.gpu.texture(r.Device().Gen), img.gpu.premul
		} else {
			var err error
			if tex, err = w.uploadImage(img, 0); err != nil {
				return nil, false, fmt.Errorf("fairy: upload capture image: %w", err)
			}
			premul = tex != nil && tex.Premultiplied
		}
		if tex != nil && wantsMips(tex, ci.bounds, ci.uv, ci.xs) {
			if err := tex.EnsureMips(r.Device()); err != nil {
				return nil, false, fmt.Errorf("fairy: capture image mipmaps: %w", err)
			}
		}
		return tex, premul, nil
	}); err != nil {
		return nil, err
	}
	if err := r.Draw(&w.scene, [4]float32{}); err != nil {
		return nil, err
	}
	w.ts.EndFrame()
	return r.ReadTarget()
}

func (c *offscreenCapture) release() {
	if w := c.w; w != nil {
		w.releaseTextures()
		for _, se := range w.surfaces {
			se.target.Release()
		}
		w.ts.Release()
		w.r.Release()
	}
}
