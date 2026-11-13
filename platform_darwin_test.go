package fairy

import "testing"

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
