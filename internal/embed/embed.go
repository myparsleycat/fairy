// Package embed lets fairy's subpackages show their own content in an
// element without fairy exporting how elements are painted.
package embed

import (
	"time"

	"github.com/myparsleycat/fairy/internal/geom"
	"github.com/myparsleycat/fairy/internal/render"
	"github.com/myparsleycat/fairy/internal/win"
)

// NewEl returns a *fairy.El that shows e, or nothing for a nil e. fairy
// sets it in init.
var NewEl func(e Element) any

// Element is the content of an element made by NewEl. Paint runs on the UI
// thread each time the element is painted.
type Element interface {
	Paint(c *Context)
}

// Native is an Element shown as a child window instead of drawn into the
// scene. host is the *fairy.Window passed as Context.Window. fairy calls these
// only for windows that have an HWND.
type Native interface {
	Element
	// Unplaced runs after a frame of host that did not paint the element.
	Unplaced(host any)
	// Moved runs when host moves on the screen.
	Moved(host any)
	// HostClosed runs when host is destroyed.
	HostClosed(host any)
}

// Context is what Element.Paint draws with. Positions are in DIPs.
type Context struct {
	Window  any // the *fairy.Window being painted
	HWND    win.HWND
	Scale   float32
	Now     time.Time
	Scene   *render.Scene
	Box     geom.Rect // the element's content box
	Radii   [4]float32
	Clip    [4]float32
	Opacity float32
	Fit     geom.Fit
	// Snap converts a rectangle to device pixels aligned to the pixel grid.
	Snap func(geom.Rect) [4]float32
	// Animate paints the window again on the next display refresh.
	Animate func()
	// WakeAt paints the window again at t.
	WakeAt func(t time.Time)
}
