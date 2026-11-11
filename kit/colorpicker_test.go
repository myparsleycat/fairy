package kit

import (
	"testing"

	"github.com/myparsleycat/fairy"
)

func TestParseHexColor(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want fairy.Color
		ok   bool
	}{
		{"#ff8000", fairy.Hex(0xff8000), true},
		{"FF8000", fairy.Hex(0xff8000), true},
		{"#f80", fairy.Hex(0xff8800), true},
		{"#ff800080", fairy.HexA(0xff800080), true},
		{"#f808", fairy.HexA(0xff880088), true},
		{"#ff80", fairy.HexA(0xffff8800), true},
		{"#ff800", fairy.Color{}, false},
		{"#gg0000", fairy.Color{}, false},
		{"", fairy.Color{}, false},
	} {
		got, ok := ParseHexColor(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("ParseHexColor(%q) = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
	if s := HexString(fairy.HexA(0x0a1b2c80), true); s != "#0A1B2C80" {
		t.Errorf("HexString = %s", s)
	}
}

// Every 8-bit color survives the trip through hue, saturation and value, and
// grays and black keep the hue the picker had, so the hue slider does not jump.
func TestHSVRoundTrip(t *testing.T) {
	for _, v := range []uint32{0xff0000, 0x00ff00, 0x0000ff, 0x123456, 0xfedcba, 0x808080, 0x000000, 0xffffff, 0x7f3f1f, 0xff00fe} {
		c := fairy.Hex(v)
		if got := toHSV(c, hsv{}).color(); got != c {
			t.Errorf("%06x: round trip = %s", v, HexString(got, false))
		}
	}
	keep := hsv{h: 200, s: 0.7}
	if got := toHSV(fairy.Hex(0x808080), keep); got.h != 200 || got.s != 0 {
		t.Errorf("gray = %+v, want hue 200 and saturation 0", got)
	}
	if got := toHSV(fairy.Hex(0x000000), keep); got.h != 200 || got.s != 0.7 || got.v != 0 {
		t.Errorf("black = %+v, want hue 200, saturation 0.7, value 0", got)
	}
}
