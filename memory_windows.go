package fairy

import (
	"sync"
	"time"

	"github.com/myparsleycat/fairy/internal/win"
)

// memoryPressureInterval is the least time between trims: the notification
// stays signaled for as long as memory is low.
const memoryPressureInterval = 30 * time.Second

// watchMemoryPressure trims image memory whenever Windows reports that
// physical memory is low.
func watchMemoryPressure() {
	memoryPressureOnce.Do(func() { win.WatchLowMemory(memoryPressureInterval, trimImageMemory) })
}

var memoryPressureOnce sync.Once
