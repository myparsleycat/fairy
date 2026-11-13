package fairy

import (
	"os"
	"testing"
	"time"
)

// A second start of the same id hands its launch to the first and returns
// false. Flocks belong to open files, so a second open in this process
// stands in for a second process.
func TestDarwinSingleInstance(t *testing.T) {
	id := "test." + time.Now().Format("150405.000000")
	got := make(chan Launch, 1)
	first, err := SingleInstance(id, func(l Launch) { got <- l })
	if err != nil || !first {
		t.Fatalf("first SingleInstance = %v, %v", first, err)
	}
	t.Cleanup(func() {
		lock, sock := instancePaths(id)
		os.Remove(lock)
		os.Remove(sock)
	})
	again, err := SingleInstance(id, nil)
	if err != nil || again {
		t.Fatalf("second SingleInstance = %v, %v", again, err)
	}
	select {
	case l := <-got:
		if len(l.Args) == 0 || l.Dir == "" {
			t.Errorf("launch = %+v", l)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the first instance did not receive the launch")
	}
}
