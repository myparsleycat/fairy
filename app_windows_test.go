package fairy

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"
	"time"

	xwin "golang.org/x/sys/windows"

	"github.com/myparsleycat/fairy/internal/win"
)

func TestDoReturnsWhenMainStartupFails(t *testing.T) {
	startErr := errors.New("startup failed")
	returned, ran := make(chan struct{}), make(chan struct{})
	err := Main(func() error {
		go func() {
			Do(func() { close(ran) })
			close(returned)
		}()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			app.mu.Lock()
			pending := len(app.calls) != 0
			app.mu.Unlock()
			if pending {
				return startErr
			}
			runtime.Gosched()
		}
		return errors.New("Do did not queue its callback")
	})
	if err != startErr {
		t.Fatal(err)
	}
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Error("Do did not return after Main failed")
		runAppCalls()
		return
	}
	select {
	case <-ran:
		t.Error("Do ran its callback after startup failed")
	default:
	}
}

func TestMainQuitDestroysOpenWindow(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var hwnd win.HWND
	defer func() {
		if windows[hwnd] != nil {
			win.DestroyWindow(hwnd)
		}
		var msg win.MSG
		win.PeekMessage(&msg, 0, win.WM_QUIT, win.WM_QUIT, win.PM_REMOVE)
	}()

	err := Main(func() error {
		if err := registerClass(); err != nil {
			return err
		}
		var err error
		hwnd, err = win.CreateWindowEx(0, className, nil, 0, 0, 0, 0, 0, win.HWND(win.HWND_MESSAGE), 0, win.GetModuleHandle(), 0)
		if err != nil {
			return err
		}
		windows[hwnd] = &Window{hwnd: hwnd}
		Quit()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if windows[hwnd] != nil {
		t.Fatal("Main returned without destroying the open window")
	}
	var msg win.MSG
	if win.PeekMessage(&msg, 0, win.WM_QUIT, win.WM_QUIT, win.PM_REMOVE) {
		t.Fatal("Main left a quit message on the UI thread")
	}
}

func TestAppWindowIsTopLevel(t *testing.T) {
	// Message-only windows cannot be enumerated and get no broadcasts, such
	// as TaskbarCreated.
	app.instanceID = fmt.Sprintf("test.%d", os.Getpid())
	defer func() { app.instanceID = "" }()
	var hwnd, found win.HWND
	var listed bool
	enum := syscall.NewCallback(func(h win.HWND, _ uintptr) uintptr {
		listed = listed || h == hwnd
		return 1
	})
	err := Main(func() error {
		hwnd, found = app.hwnd, findInstanceWindow(app.instanceID)
		xwin.EnumWindows(enum, nil)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !listed {
		t.Error("the app window is not a top-level window")
	}
	if found != hwnd {
		t.Errorf("instance lookup found %#x, want the app window %#x", found, hwnd)
	}
}

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
