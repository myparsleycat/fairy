package taskbar

import (
	"runtime"
	"testing"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/internal/com"
)

func TestTaskbarAcrossMainRuns(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for range 2 {
		err := fairy.Main(func() error {
			if tb := taskbar(); tb != nil {
				com.HR(tb, 10, 0, 0) // SetProgressState(NULL, TBPF_NOPROGRESS)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
