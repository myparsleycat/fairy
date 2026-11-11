package fairy

import (
	"log"
	"runtime/debug"
	"sync"
	"time"

	syswin "golang.org/x/sys/windows"

	"github.com/myparsleycat/fairy/internal/win"
)

// screenPixels is the area of all attached monitors in device pixels.
func screenPixels() int64 {
	var n int64
	for _, r := range win.MonitorRects() {
		n += int64(r.Right-r.Left) * int64(r.Bottom-r.Top)
	}
	return n
}

// memoryPressureInterval is the least time between trims: the notification
// stays signaled for as long as memory is low.
const memoryPressureInterval = 30 * time.Second

var memoryPressureOnce sync.Once

// watchMemoryPressure trims image memory whenever Windows reports that
// physical memory is low.
func watchMemoryPressure() {
	memoryPressureOnce.Do(func() {
		h := win.CreateMemoryResourceNotification(win.LowMemoryResourceNotification)
		if h == 0 {
			return
		}
		go func() {
			for {
				if ev, err := syswin.WaitForSingleObject(h, syswin.INFINITE); err != nil || ev != syswin.WAIT_OBJECT_0 {
					return
				}
				trimImageMemory()
				time.Sleep(memoryPressureInterval)
			}
		}()
	})
}

// trimImageMemory halves every image cache, releases the textures of images
// no window drew in its last frame and returns freed memory to the system.
func trimImageMemory() {
	trimImageCaches()
	// Finish releasing textures before trimming, without rebuilding views.
	doQuiet(func() {
		liveMu.Lock()
		defer liveMu.Unlock()
		for w := range live {
			w.releaseIdleTextures()
		}
	})
	debug.FreeOSMemory()
	if err := TrimDevice(); err != nil {
		log.Printf("fairy: trim graphics device: %v", err)
	}
}

// releaseIdleTextures releases the textures not drawn in the last painted
// frame. Run outside painting, where w.frame is the next frame.
func (w *Window) releaseIdleTextures() {
	w.sweepTextures(w.frame-1, 0)
}
