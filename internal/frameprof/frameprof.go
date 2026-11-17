// Package frameprof holds the FAIRY_DEBUG_* switches and the frame log they
// turn on.
package frameprof

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/myparsleycat/fairy/internal/render"
)

// Cause records why a view is built again, for FAIRY_DEBUG_FRAMES.
type Cause uint16

const (
	CauseUpdate Cause = 1 << iota
	CausePost
	CauseGo
	CauseHover
	CausePointer
	CauseDismiss
	CauseScroll
	CauseWheel
	CauseResize
	CauseDevice
	CauseLayout
)

var causeNames = [...]string{"update", "post", "go", "hover", "pointer", "dismiss", "scroll", "wheel", "resize", "device", "layout"}

func (c Cause) String() string {
	var parts []string
	for i, n := range causeNames {
		if c&(1<<i) != 0 {
			parts = append(parts, n)
		}
	}
	return strings.Join(parts, ",")
}

var (
	// GPU is FAIRY_DEBUG_GPU: it adds GPU timing of the renderer's commands
	// to the frame log.
	GPU = os.Getenv("FAIRY_DEBUG_GPU") != ""
	// Frames is FAIRY_DEBUG_FRAMES: it logs each frame's time by phase and
	// why it was built.
	Frames = os.Getenv("FAIRY_DEBUG_FRAMES") != "" || GPU
	// Slow is FAIRY_DEBUG_SLOW=ms: views, posted functions, Go calls and input
	// messages that keep the UI thread longer than ms are logged.
	Slow = func() time.Duration {
		ms, _ := strconv.Atoi(os.Getenv("FAIRY_DEBUG_SLOW"))
		return time.Duration(ms) * time.Millisecond
	}()
	// damage is FAIRY_DEBUG_DAMAGE: full turns damage tracking off, so every
	// frame is drawn whole; show tints the part of each frame that was
	// redrawn; verify logs when a window's buffer does not hold the frame
	// presented from it.
	damage = os.Getenv("FAIRY_DEBUG_DAMAGE")
)

// SetDamageTracking applies FAIRY_DEBUG_DAMAGE to a new renderer.
func SetDamageTracking(r *render.Renderer) {
	if damage == "full" || !r.SetDamageTracking(true) {
		return
	}
	switch damage {
	case "show":
		r.SetDamageDebug(render.DamageDebugShow)
	case "verify":
		r.SetDamageDebug(render.DamageDebugVerify)
	}
}

func init() { render.Verbose = Frames }

// Times is the time a frame spent in each phase.
type Times struct {
	at                                 time.Duration // ticks
	View, Prepare, Layout, Rows, Place time.Duration
	Paint, Record, Present             time.Duration
	Builds                             int
	Causes                             Cause
}

// Mark starts timing a phase; Lap adds the time since to d.
func (t *Times) Mark() {
	if Frames {
		t.at = ticks()
	}
}

func (t *Times) Lap(d *time.Duration) {
	if Frames {
		now := ticks()
		*d += now - t.at
		t.at = now
	}
}

// Frame is what the frame log reports of a frame besides its Times.
type Frame struct {
	N             int
	Total         time.Duration
	Prims         int
	Batches       int
	Hits          int
	Animating     bool
	Width, Height int // device pixels
}

// Log writes a line of the frame log for frame f, drawn by r.
func (t *Times) Log(f Frame, r *render.Renderer) {
	kind := "paint only"
	if t.Builds > 0 {
		cause := t.Causes.String()
		if cause == "" {
			cause = "start"
		}
		kind = fmt.Sprintf("built %dx for %s", t.Builds, cause)
	}
	ms := func(d time.Duration) string { return strconv.FormatFloat(float64(d.Microseconds())/1000, 'f', 2, 64) }
	gpu := ""
	if GPU {
		st := r.Stats()
		backend := "hardware"
		if r.Device().Info.Software {
			backend = "software"
		}
		gpu = " gpu_render=n/a backend=" + backend
		if st.GPUValid {
			gpu = fmt.Sprintf(" gpu_render=%sms gpu_submit=%d backend=%s", ms(st.GPU), st.GPUFrame, backend)
		}
	}
	log.Printf("fairy: frame %d: %sms [view %s prepare %s layout %s rows %s place %s paint %s record %s present %s] %d prims %d batches %d hits, %s, animating=%v, damage=%s%s",
		f.N, ms(f.Total), ms(t.View), ms(t.Prepare), ms(t.Layout), ms(t.Rows), ms(t.Place), ms(t.Paint), ms(t.Record), ms(t.Present),
		f.Prims, f.Batches, f.Hits, kind, f.Animating, damageString(r.Stats(), f.Width, f.Height), gpu)
}

func damageString(st render.Stats, width, height int) string {
	if st.DamageReason != "" {
		return "full(" + st.DamageReason + ")"
	}
	d := st.Damage
	if d.Empty() {
		return "none"
	}
	pct := 100 * float64(st.DamageArea) / float64(max(width*height, 1))
	return fmt.Sprintf("%d,%d,%dx%d/%d(%.1f%%)", d.Min.X, d.Min.Y, d.Dx(), d.Dy(), st.DamageRects, pct)
}

// LogSlow logs what when it took longer than Slow since start.
func LogSlow(what string, start time.Time) {
	if d := time.Since(start); d > Slow {
		log.Printf("fairy: slow %s: %v on the UI thread", what, d.Round(time.Microsecond))
	}
}
