package spell

import (
	"reflect"
	"sync"
	"testing"
	"time"
)

// The right-click menu must not wait for a background check to finish.
func TestSpellUrgentDuringCheck(t *testing.T) {
	w := worker{initialize: func(*engine) {}}
	defer func() {
		close(w.calls)
		<-w.done
	}()
	release := make(chan struct{})
	started := make(chan struct{})
	var long, urgent sync.WaitGroup
	long.Add(1)
	go func() {
		defer long.Done()
		w.send(false, func(s *engine) {
			close(started)
			for {
				select {
				case <-release:
					return
				default:
					s.runUrgent()
					time.Sleep(time.Millisecond)
				}
			}
		})
	}()
	defer long.Wait()
	<-started

	done := make(chan struct{})
	urgent.Add(1)
	defer urgent.Wait()
	defer close(release)
	go func() {
		defer urgent.Done()
		w.send(true, func(*engine) {})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("urgent request waited for the running background check")
	}
}

func TestUTF16ToBytes(t *testing.T) {
	text := "😀 helo wrld"
	got := utf16ToBytes(text, []Range{{3, 7}, {8, 12}})
	want := []Range{{5, 9}, {10, 14}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("utf16ToBytes = %v, want %v", got, want)
	}
}
