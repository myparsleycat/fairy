// Command viewport drives a Direct3D surface the way a 3D model viewer does:
// left drag turns the camera, right or middle drag pans, Shift+left drag
// scales the model, the wheel zooms about the pointer, the pointer picks the
// corner under it, and a right click without moving opens a context menu.
//
// The cube is drawn with the d3d package into a retained surface with a
// depth buffer: it draws again only when the camera changes, so hovering the
// buttons below leaves the draw count alone. "Thumbnail" renders the cube
// offscreen into an Image shown beside them.
//
//	go run ./examples/viewport
package main

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"log"
	"math"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/motion"
)

type camera struct {
	yaw, pitch float64 // radians
	panX, panY float32 // logical pixels
	zoom       float32
	scale      float64 // model scale
}

// version identifies what the camera shows, for Retain.
func (c camera) version() uint64 {
	h := fnv.New64a()
	binary.Write(h, binary.LittleEndian, []float64{c.yaw, c.pitch, float64(c.panX), float64(c.panY), float64(c.zoom), c.scale})
	return h.Sum64()
}

var background = [4]float32{0.12, 0.12, 0.15, 1}

type viewer struct {
	cube      cubeRenderer
	draws     int           // times the surface drew the cube
	spin      bool          // the cube turns by itself; the surface draws every frame
	spinFrom  time.Duration // SurfaceFrame.Time when spinning started; -1 until the first frame
	spinYaw   float64       // yaw the spin has added to cam
	thumb     *fairy.Image
	thumbErr  error
	cam, base camera // base is the camera when the drag began
	mode      string // what the current drag does
	w, h      float32
	hover     bool
	px, py    float32
	picked    int // corner under the pointer, or -1
	last      string
}

var corners = [8][3]float64{
	{-1, -1, -1}, {1, -1, -1}, {1, 1, -1}, {-1, 1, -1},
	{-1, -1, 1}, {1, -1, 1}, {1, 1, 1}, {-1, 1, 1},
}

var edges = [12][2]int{
	{0, 1}, {1, 2}, {2, 3}, {3, 0}, {4, 5}, {5, 6}, {6, 7}, {7, 4}, {0, 4}, {1, 5}, {2, 6}, {3, 7},
}

// project places corner i in surface coordinates.
func (v *viewer) project(i int) (float32, float32) {
	c := v.cam
	x, y, z := corners[i][0]*c.scale, corners[i][1]*c.scale, corners[i][2]*c.scale
	x, z = x*math.Cos(c.yaw)+z*math.Sin(c.yaw), -x*math.Sin(c.yaw)+z*math.Cos(c.yaw)
	y, z = y*math.Cos(c.pitch)-z*math.Sin(c.pitch), y*math.Sin(c.pitch)+z*math.Cos(c.pitch)
	f := float64(min(v.w, v.h)) * 0.9 / (z + 5)
	return v.w/2 + c.panX + c.zoom*float32(x*f), v.h/2 + c.panY + c.zoom*float32(y*f)
}

func (v *viewer) pick(x, y float32) int {
	best, bestD := -1, float32(12*12)
	for i := range corners {
		cx, cy := v.project(i)
		if d := (cx-x)*(cx-x) + (cy-y)*(cy-y); d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

func (v *viewer) drag(d fairy.DragEvent) {
	if d.Phase == fairy.DragStart {
		v.base = v.cam
		switch {
		case d.Button != 0:
			v.mode = "pan"
		case d.Mods&keyboard.ModShift != 0:
			v.mode = "scale"
		default:
			v.mode = "orbit"
		}
	}
	c := v.base
	switch v.mode {
	case "pan":
		c.panX, c.panY = c.panX+d.DX, c.panY+d.DY
	case "scale":
		c.scale = max(c.scale*math.Exp(float64(-d.DY)/150), 0.1)
	default:
		c.yaw += float64(d.DX) / 150
		c.pitch = max(min(c.pitch+float64(d.DY)/150, 1.5), -1.5)
	}
	if d.Canceled {
		c = v.base
	}
	v.cam = c
	v.last = fmt.Sprintf("%s drag, button %d, mods %d, phase %d, canceled %v", v.mode, d.Button, d.Mods, d.Phase, d.Canceled)
}

// wheel zooms about the pointer: the point under it stays where it is.
func (v *viewer) wheel(e fairy.WheelEvent) bool {
	k := float32(math.Pow(1.15, float64(-e.DY)))
	zoom := min(max(v.cam.zoom*k, 0.1), 20)
	k = zoom / v.cam.zoom
	qx, qy := e.LocalX-v.w/2, e.LocalY-v.h/2
	v.cam.panX = qx - (qx-v.cam.panX)*k
	v.cam.panY = qy - (qy-v.cam.panY)*k
	v.cam.zoom = zoom
	v.last = fmt.Sprintf("wheel %.3f at %.0f,%.0f, precise %v", e.DY, e.LocalX, e.LocalY, e.Precise)
	return true
}

func (v *viewer) view(cx *fairy.Cx) *fairy.El {
	kit.Use(kit.Dark())
	t := kit.T()
	cam, spin := v.cam, v.spin
	surface := fairy.Surface(func(f fairy.SurfaceFrame) bool {
		c := cam
		if spin {
			if v.spinFrom < 0 {
				v.spinFrom = f.Time
			}
			v.spinYaw = (f.Time - v.spinFrom).Seconds() * 0.8
			c.yaw += v.spinYaw
		}
		if err := v.cube.draw(f, c, background); err != nil {
			log.Print(err)
		}
		v.draws++
		return spin
	}).Retain(cam.version()).Key("cube").Absolute().Inset(0).Radius(12).Cursor(fairy.CursorCrosshair).
		DragButtons(fairy.MouseLeft | fairy.MouseRight | fairy.MouseMiddle).
		OnDrag(v.drag).OnWheel(v.wheel).
		OnPointerMove(func(p fairy.PointerEvent) bool {
			v.px, v.py, v.picked = p.LocalX, p.LocalY, v.pick(p.LocalX, p.LocalY)
			return true // the crosshair follows the pointer
		}).
		OnHover(func(in bool) {
			v.hover = in
			if !in {
				v.picked = -1
			}
		})
	kit.ContextMenu("viewport", surface, func() []kit.MenuItem {
		return []kit.MenuItem{{Label: "Reset view", OnClick: func() { v.cam = camera{zoom: 1, scale: 1, yaw: 0.6, pitch: -0.4} }}}
	})

	scene := fairy.Box().Absolute().Inset(0).PassThrough()
	if v.w > 0 && !v.spin {
		for _, e := range edges {
			ax, ay := v.project(e[0])
			bx, by := v.project(e[1])
			l := float32(math.Hypot(float64(bx-ax), float64(by-ay)))
			scene.Child(fairy.Box().Size(l, 2).Left((ax+bx)/2 - l/2).Top((ay+by)/2 - 1).Fill(t.Primary).PassThrough().
				Rotate(float32(math.Atan2(float64(by-ay), float64(bx-ax)))).ScaleMotion(motion.Instant))
		}
		for i := range corners {
			x, y := v.project(i)
			r, c := float32(4), t.Foreground
			if i == v.picked {
				r, c = 7, t.Warning
			}
			scene.Child(fairy.Box().Size(2*r, 2*r).Radius(r).Left(x - r).Top(y - r).Fill(c).PassThrough())
		}
		if v.hover {
			scene.Child(fairy.Box().Size(1, v.h).Left(v.px).Top(0).Fill(t.MutedForeground.Alpha(0.4)).PassThrough(),
				fairy.Box().Size(v.w, 1).Left(0).Top(v.py).Fill(t.MutedForeground.Alpha(0.4)).PassThrough())
		}
	}

	pointer := "outside"
	if v.hover {
		pointer = fmt.Sprintf("%.0f, %.0f", v.px, v.py)
		if v.picked >= 0 {
			pointer += fmt.Sprintf(" · corner %d", v.picked)
		}
	}
	c := v.cam
	hud := fmt.Sprintf("yaw %.0f°  pitch %.0f°  zoom %.2f  pan %.0f, %.0f  scale %.2f  ·  pointer %s",
		c.yaw*180/math.Pi, c.pitch*180/math.Pi, c.zoom, c.panX, c.panY, c.scale, pointer)
	thumb := fairy.Box().Size(160, 120).Radius(8).Border(1, t.Border).Clip()
	if v.thumb != nil {
		thumb.Child(fairy.ImageEl(v.thumb).WFull().HFull())
	} else if v.thumbErr != nil {
		thumb.Child(fairy.Text(v.thumbErr.Error()).FontSize(11).Color(t.Destructive).Pad(6))
	}
	controls := fairy.Row(
		kit.Button("Thumbnail", v.capture).Key("thumb-button"),
		kit.Button("Redraw", func() { cx.Window().InvalidateSurface("cube") }).Key("redraw-button"),
		fairy.Row(kit.Switch(v.spin, func(on bool) {
			v.spin = on
			if on {
				v.spinFrom = -1
			} else {
				// Keep the angle the cube turned to.
				v.cam.yaw += v.spinYaw
				v.spinYaw = 0
			}
			// The camera did not change, so the retained surface needs telling.
			cx.Window().InvalidateSurface("cube")
		}), fairy.Text("Spin")).Gap(8).Align(fairy.Center),
		fairy.Box().Grow(1),
		thumb,
	).Gap(8).Align(fairy.Center)
	return fairy.Col(
		fairy.Box(surface, scene).Grow(1).OnLayout(func(w, h float32) { v.w, v.h = w, h }),
		controls,
		fairy.Text(hud).Color(t.Foreground).Font(t.MonoFont).FontSize(12),
		fairy.Text(fmt.Sprintf("surface draws %d", v.draws)).Color(t.Foreground).Font(t.MonoFont).FontSize(12),
		fairy.Text(v.last).Color(t.MutedForeground).Font(t.MonoFont).FontSize(12),
	).Gap(8).Pad(16).Fill(t.Background).Color(t.Foreground)
}

// capture renders the cube as the camera shows it into an offscreen texture
// and keeps it as an Image.
func (v *viewer) capture() {
	const w, h, scale = 160, 120, 2
	cam := v.cam
	cam.panX, cam.panY = cam.panX*w/max(v.w, 1), cam.panY*h/max(v.h, 1)
	img, err := fairy.SurfaceImage(w*scale, h*scale, scale, func(f fairy.SurfaceFrame) {
		if err := v.cube.draw(f, cam, background); err != nil {
			log.Print(err)
		}
	})
	v.thumb.Release()
	v.thumb, v.thumbErr = img, err
}

func main() {
	v := &viewer{cam: camera{zoom: 1, scale: 1, yaw: 0.6, pitch: -0.4}, picked: -1}
	v.capture()
	if err := fairy.Run(fairy.Options{Title: "Viewport", Width: 800, Height: 640, View: v.view}); err != nil {
		log.Fatal(err)
	}
}
