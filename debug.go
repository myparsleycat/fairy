package fairy

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/myparsleycat/fairy/internal/render"
)

// dirtyCause records why a view is built again, for FAIRY_DEBUG_FRAMES.
type dirtyCause uint16

const (
	dirtyUpdate dirtyCause = 1 << iota
	dirtyPost
	dirtyGo
	dirtyHover
	dirtyPointer
	dirtyDismiss
	dirtyScroll
	dirtyWheel
	dirtyResize
	dirtyDevice
	dirtyLayout
)

var dirtyNames = [...]string{"update", "post", "go", "hover", "pointer", "dismiss", "scroll", "wheel", "resize", "device", "layout"}

func (c dirtyCause) String() string {
	var parts []string
	for i, n := range dirtyNames {
		if c&(1<<i) != 0 {
			parts = append(parts, n)
		}
	}
	return strings.Join(parts, ",")
}

var (
	// FAIRY_DEBUG_GPU adds GPU timing of the renderer's commands to the frame log.
	debugGPU = os.Getenv("FAIRY_DEBUG_GPU") != ""
	// FAIRY_DEBUG_FRAMES logs each frame's time by phase and why it was built.
	debugFrames = os.Getenv("FAIRY_DEBUG_FRAMES") != "" || debugGPU
	// FAIRY_DEBUG_SLOW=ms logs views, posted functions, Go calls and input
	// messages that keep the UI thread longer than ms.
	debugSlow = func() time.Duration {
		ms, _ := strconv.Atoi(os.Getenv("FAIRY_DEBUG_SLOW"))
		return time.Duration(ms) * time.Millisecond
	}()
)

func init() { render.Verbose = debugFrames }

// frameTimes is the time a frame spent in each phase.
type frameTimes struct {
	at                                 time.Duration // monoTicks
	view, prepare, layout, rows, place time.Duration
	paint, record, present             time.Duration
	builds                             int
	causes                             dirtyCause
}

// mark starts timing a phase; lap adds the time since to d.
func (w *Window) mark() {
	if debugFrames {
		w.ft.at = monoTicks()
	}
}

func (w *Window) lap(d *time.Duration) {
	if debugFrames {
		now := monoTicks()
		*d += now - w.ft.at
		w.ft.at = now
	}
}

func (w *Window) logFrame(total time.Duration, batches int) {
	t := &w.ft
	kind := "paint only"
	if t.builds > 0 {
		cause := t.causes.String()
		if cause == "" {
			cause = "start"
		}
		kind = fmt.Sprintf("built %dx for %s", t.builds, cause)
	}
	ms := func(d time.Duration) string { return strconv.FormatFloat(float64(d.Microseconds())/1000, 'f', 2, 64) }
	gpu := ""
	if debugGPU {
		st := w.r.Stats()
		backend := "hardware"
		if w.r.Device().Info.Software {
			backend = "software"
		}
		gpu = " gpu_render=n/a backend=" + backend
		if st.GPUValid {
			gpu = fmt.Sprintf(" gpu_render=%sms gpu_submit=%d backend=%s", ms(st.GPU), st.GPUFrame, backend)
		}
	}
	log.Printf("fairy: frame %d: %sms [view %s prepare %s layout %s rows %s place %s paint %s record %s present %s] %d prims %d batches %d hits, %s, animating=%v%s",
		w.frame, ms(total), ms(t.view), ms(t.prepare), ms(t.layout), ms(t.rows), ms(t.place), ms(t.paint), ms(t.record), ms(t.present),
		len(w.scene.Prims), batches, len(w.hits), kind, w.animating, gpu)
}

// slow logs what that took longer than FAIRY_DEBUG_SLOW.
func slow(what string, start time.Time) {
	if d := time.Since(start); d > debugSlow {
		log.Printf("fairy: slow %s: %v on the UI thread", what, d.Round(time.Microsecond))
	}
}
