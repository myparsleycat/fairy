package fairy

import (
	"strings"
	"testing"

	"github.com/myparsleycat/fairy/internal/darwin"
)

func TestDarwinSystemQueries(t *testing.T) {
	if ms := doubleClickMs(); ms < 100 || ms > 5000 {
		t.Errorf("doubleClickMs = %d", ms)
	}
	if l := systemLocale(); l == "" || len(l) < 2 {
		t.Errorf("systemLocale = %q", l)
	}
	if n := screenPixels(); n <= 0 {
		t.Errorf("screenPixels = %d", n)
	}
	if currentThreadID() == 0 {
		t.Error("currentThreadID = 0")
	}
	t.Logf("locale %s, screen %d px, dark %v, reduced motion %v", systemLocale(), screenPixels(), SystemDark(), ReducedMotion())
}

// The menu bar holds the app menu, with Hide and Quit, and the Window menu.
func TestDarwinMainMenu(t *testing.T) {
	var titles []string
	var quitKey string
	var windowsMenu bool
	Do(func() {
		bar := darwin.Send(nsApp, "mainMenu")
		for i := range darwin.Get[int64](bar, "numberOfItems") {
			sub := darwin.Send(darwin.Send(bar, "itemAtIndex:", i), "submenu")
			for j := range darwin.Get[int64](sub, "numberOfItems") {
				it := darwin.Send(sub, "itemAtIndex:", j)
				title := darwin.GoString(darwin.Send(it, "title"))
				titles = append(titles, title)
				if strings.HasPrefix(title, "Quit ") {
					quitKey = darwin.GoString(darwin.Send(it, "keyEquivalent"))
				}
			}
		}
		windowsMenu = darwin.Send(nsApp, "windowsMenu") != 0
	})
	joined := strings.Join(titles, "|")
	for _, want := range []string{"Hide ", "Hide Others", "Quit ", "Minimize", "Zoom"} {
		if !strings.Contains(joined, want) {
			t.Errorf("menus %q lack %q", joined, want)
		}
	}
	if quitKey != "q" || !windowsMenu {
		t.Errorf("Quit key %q, Window menu set %v", quitKey, windowsMenu)
	}
}
