package kit

import (
	"errors"
	"fmt"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/shortcut"
)

type recorderState struct {
	recording bool
	err       string
	refused   keyboard.Chord // the chord onChange refused, shown with err while recording
	refusing  bool
}

// errNeedsModifier refuses a chord without Ctrl, Alt or Win before onChange.
var errNeedsModifier = errors.New("kit: shortcut needs Ctrl, Alt or Win")

// ShortcutRecorder edits a shortcut. A click, Enter or Space starts recording;
// the next key with its modifiers proposes a chord, Escape cancels and
// Backspace clears. value is the current chord, the zero Chord for none.
// onChange receives the proposed chord (the zero Chord to clear) and should
// register it before releasing the old one; an error keeps value and shows the
// refused chord with a message (shortcut.ErrTaken reads "Ctrl+K is already
// in use."). Recording the current chord again is a no-op; pass the zero Chord
// as value for a shortcut that failed to register, so recording it proposes it
// again. A chord without Ctrl, Alt or Win other than F1–F24 is refused before
// onChange unless AllowUnmodified is given. Label names what the shortcut
// does.
func ShortcutRecorder(id string, value keyboard.Chord, onChange func(keyboard.Chord) error, opts ...Option) *fairy.El {
	o := build(opts)
	t := &current
	st := fairy.Local[recorderState]("kit.shortcut:" + id)
	key := "kit.shortcut:" + id
	if st.recording {
		// Recording ends when focus leaves, as by Tab or a click elsewhere.
		if w := fairy.CurrentWindow(); w == nil || !w.Focused(key) {
			*st = recorderState{}
		}
	}
	var caps []string
	switch {
	case st.recording && st.refusing:
		caps = st.refused.Keycaps()
	case st.recording:
	case value.Key != 0:
		caps = value.Keycaps()
	}
	control := fairy.Row().Key(key).W(240).H(controlHeight(o.size)).PadXY(8, 0).Gap(4).Align(fairy.Center).
		Role(fairy.RoleButton)
	fieldBox(control, options{invalid: st.err != "", disabled: o.disabled})
	if st.recording && st.err == "" {
		control.Border(1, t.Ring).Ring(3, alpha(t.Ring, 0.5))
	}
	if len(caps) == 0 {
		ph := Messages.RecordShortcut
		if st.recording {
			ph = Messages.PressShortcut
		}
		control.Child(fairy.Text(ph).Color(t.MutedForeground).Role(fairy.RoleHidden))
		caps = []string{ph}
	} else {
		for _, c := range caps {
			control.Child(Kbd(c).Role(fairy.RoleHidden))
		}
	}
	name := strings.Join(caps, "+")
	if o.label != "" {
		name = o.label + ": " + name
	}
	if st.recording {
		name += ". " + Messages.ShortcutHelp
	}
	control.Label(name)
	if o.disabled {
		control.Disabled(true)
	} else {
		control.Focusable().Cursor(fairy.CursorPointer).
			OnClick(func() { *st = recorderState{recording: true} }).
			OnKey(func(ev keyboard.KeyEvent) bool { return recordKey(st, ev, value, onChange, o.unmodified) })
	}
	if st.err == "" {
		return control
	}
	msg := fairy.Text(st.err).Color(t.Destructive)
	return fairy.Col(control, msg).Gap(8)
}

func recordKey(st *recorderState, ev keyboard.KeyEvent, value keyboard.Chord, onChange func(keyboard.Chord) error, unmodified bool) bool {
	if !st.recording {
		if (ev.Is(keyboard.KeyEnter, 0) || ev.Is(keyboard.KeySpace, 0)) && !ev.Repeat {
			*st = recorderState{recording: true}
			return true
		}
		return false
	}
	if modifierKey(ev.Key) {
		// Pressing another modifier starts a new chord: the refused one and its error go.
		st.err, st.refusing = "", false
		return true
	}
	if ev.Repeat {
		return true
	}
	if ev.Is(keyboard.KeyEscape, 0) {
		*st = recorderState{}
		return true
	}
	var next keyboard.Chord
	if !ev.Is(keyboard.KeyBackspace, 0) {
		next = keyboard.ChordOf(ev)
	}
	fkey := next.Key >= keyboard.KeyF1 && next.Key < keyboard.KeyF1+24
	var err error
	switch {
	case next.Key != 0 && !unmodified && !fkey && next.Mods&(keyboard.ModCtrl|keyboard.ModAlt|keyboard.ModSuper) == 0:
		err = errNeedsModifier
	case next == value:
	case onChange != nil:
		err = onChange(next)
	}
	if err != nil {
		st.err = shortcutError(err, next)
		st.refused, st.refusing = next, next.Key != 0
		fairy.Announce(st.err)
		return true
	}
	*st = recorderState{}
	return true
}

// modifierKey reports whether k is Shift, Ctrl, Alt or Win on its own.
func modifierKey(k keyboard.Key) bool {
	switch {
	case k >= 0x10 && k <= 0x12, k == 0x5B, k == 0x5C, k >= 0xA0 && k <= 0xA5:
		return true
	}
	return false
}

func shortcutError(err error, c keyboard.Chord) string {
	switch {
	case errors.Is(err, errNeedsModifier):
		return Messages.ShortcutNeedsModifier
	case c.Key == 0:
		return Messages.ShortcutClearFailed
	case errors.Is(err, shortcut.ErrTaken):
		return fmt.Sprintf(Messages.ShortcutTaken, c.Label())
	}
	return fmt.Sprintf(Messages.ShortcutFailed, c.Label())
}
