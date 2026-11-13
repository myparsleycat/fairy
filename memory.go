package fairy

import (
	"log"
	"runtime/debug"
)

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
