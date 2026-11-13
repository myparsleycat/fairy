package fairy

import (
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
