package fairy

import (
	"os"
	"testing"

	"github.com/myparsleycat/fairy/internal/apphost"
)

// TestMain runs the tests with animations on, whatever the machine's
// accessibility setting is, and with the UI loop on the main thread, which
// AppKit needs: tests run on other goroutines and reach windows through Do.
func TestMain(m *testing.M) {
	reducedMotion.Store(false)
	code := 0
	err := Main(func() error {
		apphost.Hold()
		go func() {
			code = m.Run()
			Go(apphost.Release)
		}()
		return nil
	})
	if err != nil {
		panic(err)
	}
	os.Exit(code)
}
