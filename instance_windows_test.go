package fairy

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/myparsleycat/fairy/internal/win"
)

const holdInstanceEnv = "FAIRY_TEST_HOLD_INSTANCE"

// TestSingleInstanceTakesOverAfterFirstExits covers a first instance that never reaches Main, as one still starting
// up or already shutting down: a further start waits for it instead of failing, and runs as the first instance once
// it has exited.
func TestSingleInstanceTakesOverAfterFirstExits(t *testing.T) {
	if id := os.Getenv(holdInstanceEnv); id != "" {
		first, err := SingleInstance(id, nil)
		fmt.Println("held", first, err)
		time.Sleep(time.Second)
		os.Exit(0)
	}

	id := fmt.Sprintf("test.takeover.%d", os.Getpid())
	child := exec.Command(os.Args[0], "-test.run=^TestSingleInstanceTakesOverAfterFirstExits$")
	child.Env = append(os.Environ(), holdInstanceEnv+"="+id)
	out, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Wait() }()
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil || line != "held true <nil>\n" {
		t.Fatalf("child = %q, %v", line, err)
	}

	defer func() {
		win.CloseHandle(app.mutex)
		app.mutex, app.instanceID, app.onLaunch = 0, "", nil
	}()
	first, err := SingleInstance(id, nil)
	if err != nil || !first {
		t.Fatalf("SingleInstance after the first instance exited = %v, %v", first, err)
	}
}

func TestOnSessionEndRunsOnlyWhenTheSessionEnds(t *testing.T) {
	calls := 0
	OnSessionEnd(func() { calls++ })
	defer OnSessionEnd(nil)
	err := Main(func() error {
		// wParam 0 reports that another application cancelled the shutdown.
		win.SendMessage(app.hwnd, win.WM_ENDSESSION, 0, 0)
		if calls != 0 {
			return fmt.Errorf("cancelled session end ran the hook %d times", calls)
		}
		win.SendMessage(app.hwnd, win.WM_ENDSESSION, 1, 0)
		if calls != 1 {
			return fmt.Errorf("session end ran the hook %d times", calls)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
