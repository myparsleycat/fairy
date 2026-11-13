package fairy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/myparsleycat/fairy/internal/darwin"
)

// The first process holds an flock on a file in the user's temporary
// folder, which macOS keeps per user, and listens on a Unix socket beside
// it. A further process sends its Launch over the socket.

// instanceWait bounds how long a further process waits for the first one,
// which answers only while it runs Main.
const instanceWait = 30 * time.Second

// instancePaths returns the lock file and socket of id. Socket paths are
// short on macOS, so the id is hashed.
func instancePaths(id string) (lock, sock string) {
	sum := sha256.Sum256([]byte("nahida.fairy.instance:" + id))
	base := filepath.Join(os.TempDir(), "fairy-"+hex.EncodeToString(sum[:8]))
	return base + ".lock", base + ".sock"
}

// SingleInstance makes the program run once per user session. Call it
// before Main. In the first process it returns true, and onLaunch later
// receives the arguments of every further start on the UI thread. In a
// further process it forwards the launch to the first one, which comes to
// the front, and returns false; the caller should then exit. A further
// process waits while the first one is not running Main, and returns true in
// its place once it has exited.
func SingleInstance(id string, onLaunch func(Launch)) (bool, error) {
	if id == "" {
		return false, errors.New("fairy: SingleInstance needs an id")
	}
	lockPath, sockPath := instancePaths(id)
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return false, fmt.Errorf("fairy: single instance lock: %w", err)
	}
	dir, _ := os.Getwd()
	data, _ := json.Marshal(Launch{Args: os.Args, Dir: dir})
	deadline := time.Now().Add(instanceWait)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			f.Close()
			return false, fmt.Errorf("fairy: single instance lock: %w", err)
		}
		if sendLaunch(sockPath, data, time.Until(deadline)) {
			f.Close()
			return false, nil
		}
		if time.Until(deadline) <= 0 {
			f.Close()
			return false, errors.New("fairy: the running instance did not answer")
		}
		time.Sleep(min(100*time.Millisecond, time.Until(deadline)))
	}
	// This process holds the lock, so a socket left behind is stale.
	os.Remove(sockPath)
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		f.Close()
		return false, fmt.Errorf("fairy: single instance socket: %w", err)
	}
	app.mu.Lock()
	app.onLaunch = onLaunch
	app.mu.Unlock()
	go serveLaunches(ln)
	instanceLock = f
	return true, nil
}

// instanceLock keeps the lock file open, and so locked.
var instanceLock *os.File

// sendLaunch hands a launch to the first process and reports whether it
// took it.
func sendLaunch(sock string, data []byte, wait time.Duration) bool {
	c, err := net.DialTimeout("unix", sock, max(wait, time.Millisecond))
	if err != nil {
		return false
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(max(wait, time.Second)))
	if _, err := c.Write(data); err != nil {
		return false
	}
	c.(*net.UnixConn).CloseWrite()
	var reply [1]byte
	_, err = io.ReadFull(c, reply[:])
	return err == nil && reply[0] == 1
}

// serveLaunches answers further processes. A launch is taken only while
// Main runs, as the further process waits for it otherwise.
func serveLaunches(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer c.Close()
			c.SetDeadline(time.Now().Add(5 * time.Second))
			data, err := io.ReadAll(io.LimitReader(c, 1<<20))
			var l Launch
			if err != nil || json.Unmarshal(data, &l) != nil {
				return
			}
			app.mu.Lock()
			f, running := app.onLaunch, app.running && !app.closing
			app.mu.Unlock()
			if !running {
				c.Write([]byte{0})
				return
			}
			// Reply first so the sending process can exit, then handle it.
			c.Write([]byte{1})
			Go(func() {
				darwin.Send(nsApp, "activateIgnoringOtherApps:", true)
				if f != nil {
					f(l)
				}
			})
		}()
	}
}
