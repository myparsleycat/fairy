//go:build darwin

package mackey

import (
	"testing"

	"github.com/myparsleycat/fairy/keyboard"
)

func TestLayoutKeysRoundTrip(t *testing.T) {
	// Whatever the user's layout, every letter maps to a key and back.
	for k := 'A'; k <= 'Z'; k++ {
		code, ok := Code(keyboardKey(k))
		if !ok {
			t.Errorf("no key code for %c", k)
			continue
		}
		if got := Key(code); got != keyboardKey(k) {
			t.Errorf("Key(Code(%c)) = %v", k, got)
		}
	}
}

func TestDeadKeysTypeLetters(t *testing.T) {
	var d DeadKeys
	code, _ := Code(keyboardKey('A'))
	s, ok := d.Type(code, 0)
	if !ok || s == "" {
		t.Fatalf("Type(A) = %q, %v", s, ok)
	}
}

func keyboardKey(r rune) keyboard.Key { return keyboard.Key(r) }

func TestDeadKeysCompose(t *testing.T) {
	var d DeadKeys
	const kE = 0x0E // kVK_ANSI_E
	if s, _ := d.Type(kE, 1<<19); s != "" {
		d.Reset()
		t.Skipf("Option-E is not a dead key on this layout (typed %q)", s)
	}
	if s, _ := d.Type(kE, 0); s != "é" {
		t.Errorf("Option-E then E typed %q, want é", s)
	}
	if s, _ := d.Type(kE, 0); s != "e" {
		t.Errorf("dead-key state was kept: %q", s)
	}
}
