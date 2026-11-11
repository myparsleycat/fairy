package fairy

import (
	"time"
	"unsafe"
)

// SurfaceFrame describes the texture a Surface draws into for one frame.
type SurfaceFrame struct {
	// Device and Context are the ID3D11Device and its immediate
	// ID3D11DeviceContext, shared by every window. Wrap them with
	// d3d.Device{P: f.Device} and d3d.Context{P: f.Context}, or use GPU.
	Device, Context unsafe.Pointer
	// Texture is an ID3D11Texture2D in DXGI_FORMAT_B8G8R8A8_UNORM and Target
	// its ID3D11RenderTargetView. The texture lives until the element leaves
	// the tree or changes size, so keep views of it only for one frame.
	Texture, Target unsafe.Pointer
	Width, Height   int     // device pixels
	Scale           float32 // device pixels per logical pixel
	// Time is the time since the surface first appeared.
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

// Retain makes a Surface keep its pixels between paints. The window then
// composites the previous texture and calls draw only when the surface first
// appears, when its pixel size or scale changes, after the device was
// recreated, when version differs from the version of the last draw, after
// Window.InvalidateSurface with the element's keyboard.Key, and on the frame after a
// draw that returned true. Derive version from the state the picture shows,
// such as a camera and model revision, so that other repaints (a hover
// elsewhere in the window) skip the draw.
func (e *El) Retain(version uint64) *El {
	e.retain = &version
	return e
}
