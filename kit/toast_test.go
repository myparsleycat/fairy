package kit

import (
	"testing"
	"time"
)

func TestToastLoadingToSuccess(t *testing.T) {
	ts := &Toaster{Duration: time.Hour}
	ts.put(1, ToastLoading, "Saving", "")
	it := ts.find(1)
	if it == nil || it.timed {
		t.Fatalf("loading toast %+v, want shown without a timer", it)
	}
	gen := it.gen
	ts.put(1, ToastSuccess, "Saved", "a.json")
	it = ts.find(1)
	if len(ts.items) != 1 || it.kind != ToastSuccess || it.title != "Saved" || it.body != "a.json" {
		t.Fatalf("after update %+v", ts.items)
	}
	if !it.timed || it.remaining != time.Hour || it.gen <= gen {
		t.Fatalf("updated toast timed=%v remaining=%v gen=%d (was %d); want a fresh timer", it.timed, it.remaining, it.gen, gen)
	}
}

func TestToastUpdateWhilePausedAndAfterDismiss(t *testing.T) {
	ts := &Toaster{Duration: time.Hour}
	ts.put(1, ToastInfo, "a", "")
	ts.hold(true)
	ts.put(1, ToastError, "b", "")
	if it := ts.find(1); !it.timed || it.remaining != time.Hour {
		t.Fatalf("paused update %+v, want the full duration held", it)
	}
	ts.dismiss(1)
	if ts.find(1) != nil {
		t.Fatal("dismissed toast still shown")
	}
	ts.put(1, ToastSuccess, "c", "")
	if it := ts.find(1); it == nil || it.title != "c" {
		t.Fatal("update of a dismissed toast did not show it again")
	}
}

func TestToastLimitAndForever(t *testing.T) {
	ts := &Toaster{Duration: -1}
	for i := ToastID(1); i <= 7; i++ {
		ts.put(i, ToastInfo, "x", "")
	}
	if len(ts.items) != maxToasts || ts.items[0].id != 3 {
		t.Fatalf("items %v, want the newest %d", ts.items, maxToasts)
	}
	if ts.items[0].timed {
		t.Fatal("negative Duration armed a timer")
	}
}

func TestToastIDs(t *testing.T) {
	ts := &Toaster{}
	a, b := ts.Show(ToastInfo, "a", ""), ts.Loading("b", "")
	if a == b || a == 0 || b == 0 {
		t.Fatalf("ids %d, %d", a, b)
	}
}
