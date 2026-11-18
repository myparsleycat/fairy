//go:build windows

package fairy

import (
	"time"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/geom"
	"github.com/myparsleycat/fairy/internal/render"
)

// SurfaceFrame describes the texture a Surface draws into for one frame.
type SurfaceFrame struct {
	// Device and Context are the ID3D11Device and its immediate
	// ID3D11DeviceContext, shared by every window. Wrap them with
	// d3d.Device{P: f.Device} and d3d.Context{P: f.Context}, or use GPU.
	Device, Context unsafe.Pointer
	// Texture is an ID3D11Texture2D in DXGI_FORMAT_B8G8R8A8_UNORM and Target
	// its ID3D11RenderTargetView. The texture is freed on the first frame the
	// element is not painted, and replaced when its size changes or the device
	// is made again, so keep views of it only for one frame.
	Texture, Target unsafe.Pointer
	Width, Height   int     // device pixels
	Scale           float32 // device pixels per logical pixel
	// Time is the time since the surface was first painted, or painted again
	// after a frame that left it out.
	Time time.Duration
	// Generation numbers the shared device. It changes when the device was
	// lost and made again; resources the app made on an older generation are
	// unusable and must be made again.
	Generation uint64
}

// Surface is an element whose pixels the app renders with Direct3D 11, such
// as a 3D view. draw runs on the UI thread whenever the window paints, after
// the target is cleared to transparent, and returns true to be called again
// on the next frame. It must leave premultiplied-alpha color in the target;
// any pipeline state it sets is cleared afterwards. Size the element like a
// box; corner radii and clipping apply. Use Retain to keep the pixels between
// paints instead of drawing every time.
//
// draw holds the shared device's lock: it must not wait for another goroutine
// that uses UseDevice.
func Surface(draw func(SurfaceFrame) bool) *El {
	e := newEl(kindSurface)
	e.surface = draw
	return e
}

// Retain makes a Surface keep its pixels, or a Canvas its drawing, between
// paints; see Canvas for when a canvas draws again. A Surface's window then
// composites the previous texture and calls draw only when the surface first
// appears, when its pixel size or scale changes, after the device was
// recreated, when version differs from the version of the last draw, after
// Window.InvalidateSurface with the key set by El.Key, and on the frame after
// a draw that returned true. Derive version from the state the picture shows,
// such as a camera and model revision, so that other repaints (a hover
// elsewhere in the window) skip the draw.
func (e *El) Retain(version uint64) *El {
	e.retain = &version
	return e
}

type surfaceEntry struct {
	target *render.Target
	start  time.Time
	frame  int

	drawn   bool    // target holds a finished draw
	again   bool    // the last draw asked to be called on the next frame
	version uint64  // Retain version of the last draw
	scale   float32 // window scale at the last draw
	gen     uint64  // device generation of the target
}

// surfaceNeedsDraw decides whether a surface runs its draw function this
// paint. A surface without Retain draws every time.
func surfaceNeedsDraw(se *surfaceEntry, retain *uint64, scale float32, gen uint64, invalidated bool) bool {
	return retain == nil || !se.drawn || se.again || invalidated ||
		se.version != *retain || se.scale != scale || se.gen != gen
}

func (w *Window) paintSurface(e *El, box geom.Rect, radii, clip [4]float32, opacity float32) {
	dst := w.snap(box)
	width, height := int(dst[2]), int(dst[3])
	if width <= 0 || height <= 0 || e.surface == nil {
		return
	}
	se := w.surfaces[e.id]
	if se == nil {
		se = &surfaceEntry{start: time.Now()}
		w.surfaces[e.id] = se
	}
	se.frame = w.frame
	dev := w.r.Device()
	if se.target == nil || se.target.Width != width || se.target.Height != height || se.gen != dev.Gen {
		se.target.Release()
		se.drawn = false
		t, err := render.NewTarget(dev, width, height)
		if err != nil {
			se.target = nil
			return
		}
		se.target = t
		se.gen = dev.Gen
	}
	invalidated := e.key != "" && w.invalidSurfaces[e.key]
	if surfaceNeedsDraw(se, e.retain, w.scale, dev.Gen, invalidated) {
		w.drawSurface(e, se, dev, width, height)
	}
	if se.again {
		w.animating = true
	}
	w.scene.AddImage(render.Prim{
		Bounds: dst,
		Clip:   clip,
		Color:  [4]float32{1, 1, 1, opacity},
		Radii:  radii,
		UV:     [4]float32{0, 0, 1, 1},
		Flags:  render.FlagPremultiplied,
	}, &se.target.Texture)
}

// sweepSurfaces frees targets of surfaces that were not painted this frame
// and forgets invalidations of surfaces that are gone.
func (w *Window) sweepSurfaces() {
	for id, se := range w.surfaces {
		if se.frame != w.frame {
			se.target.Release()
			delete(w.surfaces, id)
		}
	}
	clear(w.invalidSurfaces)
}

// InvalidateSurface makes the retained Surface or Canvas with key draw again
// on the next paint, and asks for that paint. Use it when what the surface shows
// changed without a new Retain version, as when a model finishes loading.
// Call it on the UI thread; from other goroutines use Post.
func (w *Window) InvalidateSurface(key string) {
	if w.invalidSurfaces == nil {
		w.invalidSurfaces = map[string]bool{}
	}
	w.invalidSurfaces[key] = true
	w.invalidate()
}
