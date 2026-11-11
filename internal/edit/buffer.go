// Package edit holds the text editing state of an input field: its value,
// caret, selection and undo history.
package edit

import (
	"strings"
	"unicode/utf8"
)

// Buffer is the text, caret and undo history of an editable field.
type Buffer struct {
	Value    string
	Reported string // the value last reported to the app or taken from it
	Caret    int    // byte offsets
	Anchor   int
	Undo     []Snap
	Redo     []Snap

	PreferredX    float32
	HasPreferredX bool
	// HandlerRan is set by the owner when app code that may set the value
	// ran since the last Sync. A value the app sets then is an undo step of
	// its own instead of a rewrite of the edit it was sent.
	HandlerRan bool
	// LastKind and Last describe the most recent recorded edit, so that
	// typing or deleting continued from where it left off joins its undo step.
	LastKind  Kind
	Last      Snap
	UndoBytes int
}

type Snap struct {
	Value         string
	Caret, Anchor int
}

type Kind uint8

const (
	Other Kind = iota
	Typing
	Deleting
)

const (
	maxUndoSteps = 200
	maxUndoBytes = 1 << 20
)

// Sync shows the app's value whenever it differs from the last one the field
// reported, so that an app rejecting or normalizing an edit, or clearing the
// field from a handler before the next paint, is never out of step with it.
func (s *Buffer) Sync(value string) {
	if value != s.Reported {
		s.Reported = value
		if value != s.Value {
			// An app that rewrites what was typed keeps the typing run going.
			cont := !s.HandlerRan && s.Snap() == s.Last
			if s.HandlerRan {
				s.PushUndo(Other)
			}
			s.Value = value
			if cont {
				s.ClampCaret()
				s.Edited()
			}
		}
	}
	s.HandlerRan = false
	s.ClampCaret()
}

func (s *Buffer) ClampCaret() {
	s.Caret = ClampOffset(s.Value, s.Caret)
	s.Anchor = ClampOffset(s.Value, s.Anchor)
}

func ClampOffset(s string, i int) int {
	if i < 0 {
		return 0
	}
	if i > len(s) {
		return len(s)
	}
	for i > 0 && i < len(s) && !utf8.RuneStart(s[i]) {
		i--
	}
	return i
}

func (s *Buffer) Selection() (int, int) {
	if s.Caret < s.Anchor {
		return s.Caret, s.Anchor
	}
	return s.Anchor, s.Caret
}

func (s *Buffer) HasSelection() bool { return s.Caret != s.Anchor }

func (s *Buffer) SelectedText() string {
	a, b := s.Selection()
	return s.Value[a:b]
}

func (s *Buffer) Snap() Snap { return Snap{s.Value, s.Caret, s.Anchor} }

// PushUndo records the state before an edit of the given kind. Typing or
// deleting that continues the previous edit of the same kind, with the caret
// where that edit left it, stays in that edit's undo step.
func (s *Buffer) PushUndo(kind Kind) {
	clear(s.Redo)
	s.Redo = s.Redo[:0]
	if kind != Other && kind == s.LastKind && len(s.Undo) > 0 && s.Snap() == s.Last {
		return
	}
	s.LastKind = kind
	s.Undo = append(s.Undo, s.Snap())
	s.UndoBytes += len(s.Value)
	for len(s.Undo) > 1 && (len(s.Undo) > maxUndoSteps || s.UndoBytes > maxUndoBytes) {
		s.UndoBytes -= len(s.Undo[0].Value)
		s.Undo[0] = Snap{}
		s.Undo = s.Undo[1:]
	}
}

// Edited marks the current state as the result of the last recorded edit.
func (s *Buffer) Edited() { s.Last = s.Snap() }

// BreakUndo ends a typing or deleting run, so the next edit starts a new step.
func (s *Buffer) BreakUndo() { s.LastKind = Other }

// ReplaceSelection inserts str in place of the selection.
func (s *Buffer) ReplaceSelection(str string) {
	a, b := s.Selection()
	s.Value = s.Value[:a] + str + s.Value[b:]
	s.Caret = a + len(str)
	s.Anchor = s.Caret
	s.HasPreferredX = false
}

func Sanitize(str string, multiline bool) string {
	str = strings.ReplaceAll(str, "\r\n", "\n")
	str = strings.ReplaceAll(str, "\r", "\n")
	if !multiline {
		str = strings.ReplaceAll(str, "\n", " ")
	}
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, str)
}

// UndoStep restores the state before the last recorded edit and reports
// whether there was one.
func (s *Buffer) UndoStep() bool {
	if len(s.Undo) == 0 {
		return false
	}
	snap := s.Undo[len(s.Undo)-1]
	s.Undo[len(s.Undo)-1] = Snap{}
	s.Undo = s.Undo[:len(s.Undo)-1]
	s.UndoBytes -= len(snap.Value)
	s.Redo = append(s.Redo, s.Snap())
	s.Value, s.Caret, s.Anchor = snap.Value, snap.Caret, snap.Anchor
	s.BreakUndo()
	return true
}

// RedoStep reapplies the last undone edit and reports whether there was one.
func (s *Buffer) RedoStep() bool {
	if len(s.Redo) == 0 {
		return false
	}
	snap := s.Redo[len(s.Redo)-1]
	s.Redo[len(s.Redo)-1] = Snap{}
	s.Redo = s.Redo[:len(s.Redo)-1]
	s.Undo = append(s.Undo, s.Snap())
	s.UndoBytes += len(s.Value)
	s.Value, s.Caret, s.Anchor = snap.Value, snap.Caret, snap.Anchor
	s.BreakUndo()
	return true
}
