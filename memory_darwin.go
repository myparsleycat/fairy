package fairy

import (
	"runtime/debug"
	"sync"
	"sync/atomic"

	"github.com/myparsleycat/fairy/internal/darwin"
)

var (
	memoryPressureOnce sync.Once
	trimming           atomic.Bool
)

// watchMemoryPressure trims image memory whenever macOS reports that memory
// pressure rose to the warning or critical level.
func watchMemoryPressure() {
	memoryPressureOnce.Do(func() {
		darwin.OnMemoryPressure(func() {
			if trimming.CompareAndSwap(false, true) {
				go func() {
					defer trimming.Store(false)
					trimImageMemory()
				}()
			}
		})
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
}

// releaseIdleTextures releases the textures not drawn in the last painted
// frame. Run outside painting, where w.frame is the next frame.
func (w *Window) releaseIdleTextures() {
	w.sweepTextures(w.frame-1, 0)
}
