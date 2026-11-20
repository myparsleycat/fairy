package fairy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	xwin "golang.org/x/sys/windows"

	"github.com/myparsleycat/fairy/internal/com"
	"github.com/myparsleycat/fairy/internal/win"
)

const (
	launchMagic        = 0x6775696c // "guil"
	msgInstanceRelease = win.WM_APP + 13
)

// instanceWait bounds how long a further process waits for the first one to take its launch or to give up its claim.
var instanceWait = 30 * time.Second

// instance is the claim of the first process. One goroutine locked to its own thread owns the mutex and runs the
// instance window, because Windows abandons a mutex when the thread that owns it ends, and the window then answers
// launches whether or not Main is running.
var instance struct {
	mu   sync.Mutex
	hwnd win.HWND
	done chan struct{} // closed once the claim is released

	// accepting is false after Main returns, so a further process waits for this one to exit instead of sending its
	// launch to a process that will not handle it.
	accepting atomic.Bool

	onLaunch func(Launch) // read and written only on the instance thread
}

var instanceProcPtr = syscall.NewCallback(instanceProc)

func instanceClass(id string) string { return "nahida.fairy.instance." + id }

func findInstanceWindow(id string) win.HWND { return win.FindWindowByClass(instanceClass(id)) }

// claimMu serializes SingleInstance and ReleaseSingleInstance.
var claimMu sync.Mutex

// SingleInstance makes the program run once per user session. Call it before Main. In the first process it returns
// true, and onLaunch receives the arguments of every further start on the UI thread; starts that arrive before Main
// runs are delivered once it does, and are lost if it never runs. In a further process it forwards the launch to the
// first one, allows it to take the foreground, and returns false; the caller should then exit. After Main has
// returned in the first process, a further process waits until the first one exits or calls ReleaseSingleInstance,
// and then returns true in its place.
func SingleInstance(id string, onLaunch func(Launch)) (bool, error) {
	if id == "" {
		return false, errors.New("fairy: SingleInstance needs an id")
	}
	claimMu.Lock()
	defer claimMu.Unlock()
	instance.mu.Lock()
	held := instance.done != nil
	instance.mu.Unlock()
	if held {
		return false, errors.New("fairy: SingleInstance already holds a claim")
	}

	dir, _ := os.Getwd()
	data, _ := json.Marshal(Launch{Args: os.Args, Dir: dir})
	result := make(chan claimResult, 1)
	go claimInstance(id, data, onLaunch, result)
	r := <-result
	return r.first, r.err
}

// ReleaseSingleInstance gives up the claim SingleInstance took, so a later start runs as the first instance instead
// of waiting for this process to exit. Call it once the program no longer needs to be the only instance, for example
// before it shows a final error and exits. It does nothing in a process that holds no claim.
func ReleaseSingleInstance() {
	claimMu.Lock()
	defer claimMu.Unlock()
	instance.mu.Lock()
	hwnd, done := instance.hwnd, instance.done
	instance.mu.Unlock()
	if done == nil || !win.PostMessage(hwnd, msgInstanceRelease, 0, 0) {
		return
	}
	<-done
}

type claimResult struct {
	first bool
	err   error
}

// claimInstance runs on its own thread for as long as this process holds the claim.
func claimInstance(id string, data []byte, onLaunch func(Launch), result chan<- claimResult) {
	runtime.LockOSThread()
	fail := func(h win.HANDLE, err error) {
		win.CloseHandle(h)
		runtime.UnlockOSThread()
		result <- claimResult{err: err}
	}

	h, existed, err := win.CreateMutex(`Local\nahida.fairy.`+id, true)
	if err != nil {
		fail(0, fmt.Errorf("fairy: single instance mutex: %w", err))
		return
	}
	if existed {
		forwarded, err := forwardLaunch(id, h, data)
		if forwarded || err != nil {
			fail(h, err)
			return
		}
	}

	instance.onLaunch = onLaunch
	instance.accepting.Store(true)
	hwnd, err := createInstanceWindow(id)
	if err != nil {
		instance.accepting.Store(false)
		instance.onLaunch = nil
		_ = xwin.ReleaseMutex(h)
		fail(h, err)
		return
	}
	done := make(chan struct{})
	instance.mu.Lock()
	instance.hwnd, instance.done = hwnd, done
	instance.mu.Unlock()
	result <- claimResult{first: true}

	var m win.MSG
	for {
		r := win.GetMessage(&m)
		if r == 0 || r == -1 {
			break
		}
		win.DispatchMessage(&m)
	}

	instance.accepting.Store(false)
	instance.onLaunch = nil
	_ = xwin.ReleaseMutex(h)
	win.CloseHandle(h)
	instance.mu.Lock()
	instance.hwnd, instance.done = 0, nil
	instance.mu.Unlock()
	runtime.UnlockOSThread()
	close(done)
}

// forwardLaunch sends the launch to the first process, or waits until this thread owns the mutex h because the
// first process exited or released its claim. It reports whether the launch was forwarded.
func forwardLaunch(id string, h win.HANDLE, data []byte) (bool, error) {
	deadline := time.Now().Add(instanceWait)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false, errors.New("fairy: the running instance did not answer")
		}
		if target := findInstanceWindow(id); target != 0 {
			win.AllowSetForegroundWindow(win.WindowProcessID(target))
			timeoutMS := uint32((remaining + time.Millisecond - 1) / time.Millisecond)
			cds := win.COPYDATASTRUCT{Data: launchMagic, Size: uint32(len(data)), Ptr: uintptr(unsafe.Pointer(&data[0]))}
			reply, ok := win.SendMessageTimeout(target, win.WM_COPYDATA, 0, unsafe.Pointer(&cds), timeoutMS)
			runtime.KeepAlive(data)
			if ok && reply != 0 {
				return true, nil
			}
		}

		wait := max(min(100*time.Millisecond, time.Until(deadline)), 0)
		event, err := xwin.WaitForSingleObject(h, uint32(wait/time.Millisecond))
		if err != nil {
			return false, fmt.Errorf("fairy: single instance mutex: %w", err)
		}
		// An abandoned mutex belonged to a process that exited; this thread owns it either way.
		if event == xwin.WAIT_OBJECT_0 || event == xwin.WAIT_ABANDONED {
			return false, nil
		}
	}
}

// createInstanceWindow creates a hidden top-level window rather than a message-only one, because further processes,
// including ones built with older versions of fairy, find it by class among top-level windows.
func createInstanceWindow(id string) (win.HWND, error) {
	hwnd, err := createHiddenWindow(instanceClass(id), instanceProcPtr)
	if err != nil {
		return 0, fmt.Errorf("fairy: instance window: %w", err)
	}
	return hwnd, nil
}

type ownWindowSearch struct {
	pid   uint32
	found win.HWND
}

var ownWindowSearchPtr = syscall.NewCallback(func(h win.HWND, lp uintptr) uintptr {
	s := (*ownWindowSearch)(com.Ptr(lp))
	if win.IsWindowVisible(h) && win.WindowProcessID(h) == s.pid {
		s.found = h
		return 0
	}
	return 1
})

// foregroundOwnWindow brings this process's topmost visible window forward, such as a dialog shown before Main, so a
// launch that reaches a process without its UI still shows the user what it is waiting for. The sending process
// allowed this process to take the foreground.
func foregroundOwnWindow() {
	s := ownWindowSearch{pid: uint32(os.Getpid())}
	_ = xwin.EnumWindows(ownWindowSearchPtr, unsafe.Pointer(&s))
	if s.found != 0 {
		win.SetForegroundWindow(s.found)
	}
}

func instanceProc(hwnd win.HWND, msg uint32, wp, lp uintptr) uintptr {
	switch msg {
	case win.WM_COPYDATA:
		cds := (*win.COPYDATASTRUCT)(com.Ptr(lp))
		if cds.Data != launchMagic || !instance.accepting.Load() {
			return 0
		}
		var l Launch
		if json.Unmarshal(unsafe.Slice((*byte)(com.Ptr(cds.Ptr)), cds.Size), &l) != nil {
			// Taken anyway, so the sender exits instead of retrying a launch this process cannot read.
			return 1
		}
		// Reply first so the sending process can exit; the launch runs on the UI thread, or once Main starts. A
		// launch that arrives while Main shuts down cannot be queued, so the sender waits for this process to exit.
		if f := instance.onLaunch; f != nil && !enqueueAppCall(appCall{run: func() { f(l) }}, false) {
			return 0
		}
		if app.thread.Load() == 0 {
			foregroundOwnWindow()
		}
		return 1
	case win.WM_CLOSE:
		// Sent to top-level windows by taskkill and installers; the claim lasts until the process exits or releases it.
		return 0
	case msgInstanceRelease:
		win.DestroyWindow(hwnd)
		win.PostQuitMessage(0)
		return 0
	}
	return win.DefWindowProc(hwnd, msg, wp, lp)
}
