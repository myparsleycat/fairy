//go:build darwin

package darwin

import (
	"testing"
	"unsafe"
)

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

// An NSString with an unpaired surrogate, which UTF8String cannot encode,
// keeps the rest of its text.
func TestGoStringLoneSurrogate(t *testing.T) {
	WithPool(func() {
		for _, c := range []struct {
			u    []uint16
			want string
		}{
			{[]uint16{'a', 0xd800, 'b'}, "a\ufffdb"},
			{[]uint16{0xdc00}, "\ufffd"},
			{[]uint16{'x', 0xd83d, 0xde42, 0xd83d}, "x🙂\ufffd"},
		} {
			s := Send(SendClass("NSString", "alloc"), "initWithCharacters:length:", unsafe.Pointer(&c.u[0]), uint64(len(c.u)))
			if got := GoString(s); got != c.want {
				t.Errorf("GoString(%x) = %q, want %q", c.u, got, c.want)
			}
			Release(s)
		}
	})
}
