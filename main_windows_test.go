package fairy

import (
	"os"
	"testing"
)

// TestMain runs the tests with animations on, whatever the machine's
// "Animation effects" setting is; tests of reduced motion turn it on themselves.
func TestMain(m *testing.M) {
	reducedMotion.Store(false)
	os.Exit(m.Run())
}
