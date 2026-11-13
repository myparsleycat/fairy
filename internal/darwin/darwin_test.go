//go:build darwin

package darwin

import "testing"

func TestStringRoundTrip(t *testing.T) {
	WithPool(func() {
		for _, s := range []string{"", "hello", "한글 🙂", "\x00", "before\x00한글 🙂\x00after"} {
			if got := GoString(String(s)); got != s {
				t.Errorf("GoString(String(%q)) = %q", s, got)
			}
		}
	})
}

func TestDataRoundTrip(t *testing.T) {
	WithPool(func() {
		b := []byte{1, 2, 3}
		if got := Bytes(Data(b)); string(got) != string(b) {
			t.Errorf("Bytes(Data(%v)) = %v", b, got)
		}
	})
}
