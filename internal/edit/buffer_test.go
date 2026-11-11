package edit

import (
	"strconv"
	"strings"
	"testing"
)

func TestUndoReleasesEvictedSnapshots(t *testing.T) {
	for _, limit := range []string{"steps", "bytes"} {
		t.Run(limit, func(t *testing.T) {
			backing := make([]Snap, maxUndoSteps+1)
			s := &Buffer{Undo: backing[:0]}
			count, value := maxUndoSteps, "before"
			if limit == "bytes" {
				count, value = 1, strings.Repeat("a", maxUndoBytes/2+1)
			}
			for i := range count {
				s.Value = value + strconv.Itoa(i)
				s.PushUndo(Other)
			}
			retained := append([]Snap(nil), s.Undo[1:]...)
			s.Value, s.Caret, s.Anchor = value+"next", 3, 1
			want := s.Snap()
			s.PushUndo(Other)
			retained = append(retained, want)
			if backing[0] != (Snap{}) {
				t.Errorf("evicted snapshot still retains %d bytes", len(backing[0].Value))
			}
			if len(s.Undo) != len(retained) {
				t.Fatalf("undo length = %d, want %d", len(s.Undo), len(retained))
			}
			bytes := 0
			for i, snap := range s.Undo {
				if snap != retained[i] {
					t.Errorf("retained snapshot %d changed", i)
				}
				bytes += len(snap.Value)
			}
			if s.UndoBytes != bytes {
				t.Errorf("undoBytes = %d, want %d", s.UndoBytes, bytes)
			}
		})
	}
}

func TestUndoInvalidatesRedoSnapshots(t *testing.T) {
	for _, merge := range []bool{false, true} {
		t.Run(strconv.FormatBool(merge), func(t *testing.T) {
			backing := []Snap{{"redo one", 3, 1}, {"redo two", 2, 0}, {}}
			s := &Buffer{Value: "current", Caret: 4, Anchor: 2, Redo: backing[:2]}
			if merge {
				s.Undo = []Snap{{Value: "previous"}}
				s.UndoBytes = len("previous")
				s.LastKind, s.Last = Typing, s.Snap()
			}
			s.PushUndo(Typing)
			if len(s.Redo) != 0 {
				t.Errorf("redo length = %d, want 0", len(s.Redo))
			}
			for i, snap := range backing {
				if snap != (Snap{}) {
					t.Errorf("discarded redo slot %d still retains %q", i, snap.Value)
				}
			}
			want, bytes := s.Snap(), len(s.Value)
			if merge {
				want, bytes = Snap{Value: "previous"}, len("previous")
			}
			if len(s.Undo) != 1 || s.Undo[0] != want || s.UndoBytes != bytes {
				t.Errorf("undo changed unexpectedly: %+v, %d bytes", s.Undo, s.UndoBytes)
			}
		})
	}
}

func TestUndoRetainsOversizedNewestSnapshot(t *testing.T) {
	backing := make([]Snap, 0, 4)
	s := &Buffer{Undo: backing}
	for _, value := range []string{"first", "second", "third"} {
		s.Value = value
		s.PushUndo(Other)
	}
	s.Value = strings.Repeat("x", maxUndoBytes+1)
	s.PushUndo(Other)
	if len(s.Undo) != 1 || s.Undo[0] != s.Snap() || s.UndoBytes != len(s.Value) {
		t.Fatal("newest snapshot was lost when it exceeded the byte budget")
	}
	for i, snap := range backing[:3] {
		if snap != (Snap{}) {
			t.Errorf("evicted snapshot %d still retains %q", i, snap.Value)
		}
	}
}
