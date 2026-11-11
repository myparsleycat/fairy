package kit

import (
	"testing"

	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/shortcut"
)

func TestShortcutRecorderKeys(t *testing.T) {
	ctrlK := keyboard.MustChord("ctrl+k")
	var proposed []keyboard.Chord
	var refuse error
	onChange := func(c keyboard.Chord) error {
		proposed = append(proposed, c)
		return refuse
	}
	st := &recorderState{recording: true}
	press := func(ev keyboard.KeyEvent, value keyboard.Chord) { recordKey(st, ev, value, onChange, false) }

	press(keyboard.KeyEvent{Key: 'K'}, keyboard.Chord{})
	if len(proposed) != 0 || st.err != Messages.ShortcutNeedsModifier || !st.recording {
		t.Fatalf("K alone: proposed %v, error %q, recording %v", proposed, st.err, st.recording)
	}
	press(keyboard.KeyEvent{Key: 0x11, Mods: keyboard.ModCtrl}, keyboard.Chord{})
	if st.err != "" {
		t.Fatalf("pressing Ctrl kept the error %q", st.err)
	}

	refuse = shortcut.ErrTaken
	press(keyboard.KeyEvent{Key: 'K', Mods: keyboard.ModCtrl}, keyboard.Chord{})
	if len(proposed) != 1 || proposed[0] != ctrlK || !st.recording || !st.refusing || st.err == "" {
		t.Fatalf("refused Ctrl+K: proposed %v, recording %v, error %q", proposed, st.recording, st.err)
	}

	refuse = nil
	press(keyboard.KeyEvent{Key: 'K', Mods: keyboard.ModCtrl}, keyboard.Chord{})
	if len(proposed) != 2 || st.recording || st.err != "" {
		t.Fatalf("accepted Ctrl+K: proposed %v, recording %v, error %q", proposed, st.recording, st.err)
	}

	*st = recorderState{recording: true}
	press(keyboard.KeyEvent{Key: 'K', Mods: keyboard.ModCtrl}, ctrlK)
	if len(proposed) != 2 || st.recording {
		t.Fatalf("recording the current chord again proposed it (%v) or kept recording", proposed)
	}

	*st = recorderState{recording: true}
	press(keyboard.KeyEvent{Key: keyboard.KeyBackspace}, ctrlK)
	if len(proposed) != 3 || proposed[2] != (keyboard.Chord{}) {
		t.Fatalf("Backspace proposed %v, want the zero chord", proposed)
	}

	*st = recorderState{recording: true}
	press(keyboard.KeyEvent{Key: keyboard.KeyEscape}, ctrlK)
	if len(proposed) != 3 || st.recording {
		t.Fatalf("Escape proposed %v or kept recording", proposed)
	}
}
