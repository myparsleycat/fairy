package frameprof

import (
	"time"

	"github.com/myparsleycat/fairy/internal/win"
)

// ticks is a monotonic high-resolution time for phase timing.
func ticks() time.Duration { return win.Ticks() }
