package fairy

import (
	"testing"

	"github.com/myparsleycat/fairy/internal/darwin"
)

func TestDarwinDockProgress(t *testing.T) {
	w := newTestWindow(t, func() *El { return Box() })
	var shown darwin.ID
	var value float64
	w.SetProgress(ProgressNormal, 0.4)
	Do(func() {
		shown = darwin.Send(darwin.Send(nsApp, "dockTile"), "contentView")
		value = darwin.Get[float64](dockProgress.bar, "doubleValue")
	})
	if shown == 0 || value != 0.4 {
		t.Fatalf("Dock tile content %v, bar at %v", shown, value)
	}
	w.SetProgress(ProgressNone, 0)
	Do(func() { shown = darwin.Send(darwin.Send(nsApp, "dockTile"), "contentView") })
	if shown != 0 {
		t.Error("the bar stayed on the Dock tile")
	}
}
