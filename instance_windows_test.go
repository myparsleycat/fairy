package fairy

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
	"unsafe"

	xwin "golang.org/x/sys/windows"

	"github.com/myparsleycat/fairy/internal/win"
)

const (
	instanceChildEnv = "FAIRY_TEST_INSTANCE_CHILD"
	instanceWaitEnv  = "FAIRY_TEST_INSTANCE_WAIT"
)

// TestInstanceChild is the helper process of the single-instance tests. It claims the instance given as "mode:id" in
// instanceChildEnv, prints the result, and keeps what it got until its stdin closes. In mode "main" a first instance
// runs a Main that returns at once; in mode "release" it releases its claim.
func TestInstanceChild(t *testing.T) {
	spec := os.Getenv(instanceChildEnv)
	if spec == "" {
		return
	}
	mode, id, _ := strings.Cut(spec, ":")
	if d, err := time.ParseDuration(os.Getenv(instanceWaitEnv)); err == nil {
		instanceWait = d
	}
	first, err := SingleInstance(id, nil)
	if first {
		switch mode {
		case "main":
			err = Main(func() error { return nil })
		case "release":
			ReleaseSingleInstance()
		}
	}
	fmt.Println(first, err)
	_, _ = io.Copy(io.Discard, os.Stdin)
	os.Exit(0)
}

type instanceChild struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	line  chan string
}

func startInstanceChild(t *testing.T, mode, id string, env ...string) *instanceChild {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestInstanceChild$")
	cmd.Env = append(os.Environ(), append(env, instanceChildEnv+"="+mode+":"+id)...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c := &instanceChild{cmd: cmd, stdin: stdin, line: make(chan string, 1)}
	go func() {
		line, _ := bufio.NewReader(stdout).ReadString('\n')
		c.line <- strings.TrimSpace(line)
	}()
	t.Cleanup(c.exit)
	return c
}

func (c *instanceChild) result(t *testing.T) string {
	t.Helper()
	select {
	case line := <-c.line:
		return line
	case <-time.After(time.Minute):
		t.Fatal("the helper process did not report")
		return ""
	}
}

func (c *instanceChild) exit() {
	_ = c.stdin.Close()
	_ = c.cmd.Wait()
}

func holdInstance(t *testing.T, mode string) (string, *instanceChild) {
	t.Helper()
	id := fmt.Sprintf("test.%s.%d", t.Name(), os.Getpid())
	holder := startInstanceChild(t, mode, id)
	if got := holder.result(t); got != "true <nil>" {
		t.Fatalf("first process = %q", got)
	}
	return id, holder
}

func claimInstanceForTest(t *testing.T, id string) (bool, error) {
	first, err := SingleInstance(id, nil)
	if first {
		t.Cleanup(ReleaseSingleInstance)
	}
	return first, err
}

// A first process that has not reached Main, such as one showing a dialog while it starts, still takes launches.
func TestSingleInstanceForwardsBeforeMain(t *testing.T) {
	id, _ := holdInstance(t, "hold")

	first, err := claimInstanceForTest(t, id)
	if err != nil || first {
		t.Fatalf("SingleInstance = %v, %v, want the launch forwarded", first, err)
	}
}

// A launch the first process cannot decode is still taken, so its sender exits instead of retrying until it gives up.
func TestSingleInstanceTakesUndecodableLaunch(t *testing.T) {
	id, _ := holdInstance(t, "hold")
	target := findInstanceWindow(id)
	if target == 0 {
		t.Fatal("the first process has no instance window")
	}

	data := []byte("not json")
	cds := win.COPYDATASTRUCT{Data: launchMagic, Size: uint32(len(data)), Ptr: uintptr(unsafe.Pointer(&data[0]))}
	reply, ok := win.SendMessageTimeout(target, win.WM_COPYDATA, 0, unsafe.Pointer(&cds), 5000)
	runtime.KeepAlive(data)
	if !ok || reply != 1 {
		t.Fatalf("reply = %d, %v, want the launch taken", reply, ok)
	}
}

// After Main returns, the first process no longer takes launches; a further start waits for it to exit and then runs
// as the first instance.
func TestSingleInstanceTakesOverAfterMainReturns(t *testing.T) {
	id, holder := holdInstance(t, "main")

	claimed := make(chan claimResult, 1)
	go func() {
		first, err := SingleInstance(id, nil)
		claimed <- claimResult{first: first, err: err}
	}()
	wait := func() claimResult {
		select {
		case r := <-claimed:
			if r.first {
				t.Cleanup(ReleaseSingleInstance)
			}
			return r
		case <-time.After(time.Minute):
			t.Fatal("SingleInstance did not return")
			return claimResult{}
		}
	}
	select {
	case <-claimed:
		t.Fatal("SingleInstance returned while the first process was still running")
	case <-time.After(500 * time.Millisecond):
	}

	holder.exit()
	if r := wait(); r.err != nil || !r.first {
		t.Fatalf("SingleInstance after the first process exited = %v, %v", r.first, r.err)
	}
}

// A first process that released its claim, for example to show a final error, does not hold up later starts.
func TestSingleInstanceTakesOverAfterRelease(t *testing.T) {
	id, _ := holdInstance(t, "release")

	first, err := claimInstanceForTest(t, id)
	if err != nil || !first {
		t.Fatalf("SingleInstance after the first process released its claim = %v, %v", first, err)
	}
}

// A start that gave up waiting and stays open, as one showing its error, keeps no handle to the mutex, which
// installers check to tell whether the program runs.
func TestSingleInstanceFailedStartKeepsNoMutex(t *testing.T) {
	id, holder := holdInstance(t, "main")
	failed := startInstanceChild(t, "hold", id, instanceWaitEnv+"=300ms")
	if got := failed.result(t); got != "false fairy: the running instance did not answer" {
		t.Fatalf("waiting start = %q", got)
	}

	holder.exit()
	name, _ := xwin.UTF16PtrFromString(`Local\nahida.fairy.` + id)
	h, err := xwin.OpenMutex(xwin.SYNCHRONIZE, false, name)
	if err == nil {
		_ = xwin.CloseHandle(h)
		t.Fatal("the mutex outlived the first process while the failed start was still open")
	}
}

// Starts that wait for the same first process end with one first instance, which takes the other's launch.
func TestSingleInstanceElectsOneOfSeveralWaitingStarts(t *testing.T) {
	id, holder := holdInstance(t, "main")
	a := startInstanceChild(t, "hold", id)
	b := startInstanceChild(t, "hold", id)
	time.Sleep(500 * time.Millisecond)

	holder.exit()
	results := []string{a.result(t), b.result(t)}
	slices.Sort(results)
	if !slices.Equal(results, []string{"false <nil>", "true <nil>"}) {
		t.Fatalf("waiting starts = %q, want one forwarded and one first", results)
	}
}
