package imaging

import (
	"encoding/binary"
	"math"
)

// colorTransform converts 8-bit pixels coded in another RGB color space to
// sRGB: each channel through its tone curve to linear light, a 3x3 matrix
// between the two sets of primaries, and the sRGB curve back.
type colorTransform struct {
	in  [3][256]float32
	m   [3][3]float32
	out [4096]uint8 // sRGB encoding of linear light in steps of 1/4095
}

// srgbD50 holds sRGB's primaries in XYZ adapted to D50, as ICC profiles
// record them.
var srgbD50 = [3][3]float64{
	{0.4360747, 0.3850649, 0.1430804},
	{0.2225045, 0.7168786, 0.0606169},
	{0.0139322, 0.0971045, 0.7141733},
}

func srgbDecode(v float64) float64 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func srgbEncode(v float64) float64 {
	if v <= 0.0031308 {
		return v * 12.92
	}
	return 1.055*math.Pow(v, 1/2.4) - 0.055
}

// colorTransform returns the conversion of a still image's pixels to sRGB,
// or nil when they are sRGB already or their color space is not one this
// handles: profiles other than RGB matrix/TRC ones, and nclx primaries other
// than BT.709, BT.2020 and Display P3.
func (m imageMeta) colorTransform() *colorTransform {
	switch {
	case m.srgb:
		return nil
	case m.icc != nil:
		return iccTransform(m.icc)
	}
	curve := nclxCurve(m.transfer)
	if curve == nil {
		return nil
	}
	switch m.primaries {
	case 9:
		return primariesTransform(0.708, 0.292, 0.170, 0.797, 0.131, 0.046, curve)
	case 12:
		return primariesTransform(0.680, 0.320, 0.265, 0.690, 0.150, 0.060, curve)
	case 0, 1, 2:
		if m.transfer == 0 || m.transfer == 2 || m.transfer == 13 {
			return nil
		}
		return primariesTransform(0.64, 0.33, 0.30, 0.60, 0.15, 0.06, curve)
	}
	return nil
}

func nclxCurve(transfer int) func(float64) float64 {
	switch transfer {
	case 0, 2, 13:
		return srgbDecode
	case 8:
		return func(v float64) float64 { return v }
	case 1, 6, 14, 15:
		alpha, beta := 1.099, 0.018
		if transfer == 15 {
			alpha, beta = 1.0993, 0.0181
		}
		return func(v float64) float64 {
			if v < 4.5*beta {
				return v / 4.5
			}
			return math.Pow((v+alpha-1)/alpha, 1/0.45)
		}
	}
	return nil
}

// primariesTransform converts D65 RGB primaries through curve to sRGB.
func primariesTransform(xr, yr, xg, yg, xb, yb float64, curve func(float64) float64) *colorTransform {
	src := rgbToXYZ(xr, yr, xg, yg, xb, yb)
	dst := rgbToXYZ(0.64, 0.33, 0.30, 0.60, 0.15, 0.06)
	var curves [3]func(float64) float64
	for c := range curves {
		curves[c] = curve
	}
	return newColorTransform(mul3(inv3(dst), src), curves)
}

// rgbToXYZ is the matrix from linear RGB with the given primaries and a D65
// white to XYZ.
func rgbToXYZ(xr, yr, xg, yg, xb, yb float64) [3][3]float64 {
	const xw, yw = 0.3127, 0.3290
	p := [3][3]float64{
		{xr / yr, xg / yg, xb / yb},
		{1, 1, 1},
		{(1 - xr - yr) / yr, (1 - xg - yg) / yg, (1 - xb - yb) / yb},
	}
	s := apply3(inv3(p), [3]float64{xw / yw, 1, (1 - xw - yw) / yw})
	for r := range 3 {
		for c := range 3 {
			p[r][c] *= s[c]
		}
	}
	return p
}

func newColorTransform(m [3][3]float64, curves [3]func(float64) float64) *colorTransform {
	t := &colorTransform{}
	for c, f := range curves {
		for v := range 256 {
			t.in[c][v] = float32(f(float64(v) / 255))
		}
	}
	for r := range 3 {
		for c := range 3 {
			t.m[r][c] = float32(m[r][c])
		}
	}
	for i := range t.out {
		t.out[i] = uint8(math.Round(srgbEncode(float64(i)/4095) * 255))
	}
	return t
}

// apply converts premultiplied RGBA pixels in place.
func (t *colorTransform) apply(pix []byte) {
	enc := func(v float32) uint8 {
		switch {
		case !(v > 0): // also NaN
			return t.out[0]
		case v >= 1:
			return t.out[len(t.out)-1]
		}
		return t.out[int(v*4095+0.5)]
	}
	for i := 0; i+3 < len(pix); i += 4 {
		a := pix[i+3]
		if a == 0 {
			continue
		}
		var rgb [3]uint8
		copy(rgb[:], pix[i:i+3])
		if a != 255 {
			for c := range rgb {
				rgb[c] = uint8(min(255, (uint32(rgb[c])*255+uint32(a)/2)/uint32(a)))
			}
		}
		r, g, b := t.in[0][rgb[0]], t.in[1][rgb[1]], t.in[2][rgb[2]]
		for c := range 3 {
			v := enc(t.m[c][0]*r + t.m[c][1]*g + t.m[c][2]*b)
			if a != 255 {
				v = uint8((uint32(v)*uint32(a) + 127) / 255)
			}
			pix[i+c] = v
		}
	}
}

// iccTransform reads an RGB matrix/TRC ICC profile, version 2 or 4.
func iccTransform(p []byte) *colorTransform {
	if len(p) < 132 || string(p[36:40]) != "acsp" || string(p[16:20]) != "RGB " || string(p[20:24]) != "XYZ " {
		return nil
	}
	tags := map[string][]byte{}
	n := binary.BigEndian.Uint32(p[128:])
	if uint64(n)*12+132 > uint64(len(p)) {
		return nil
	}
	for i := range int(n) {
		e := p[132+i*12:]
		off, size := uint64(binary.BigEndian.Uint32(e[4:])), uint64(binary.BigEndian.Uint32(e[8:]))
		if off+size > uint64(len(p)) {
			return nil
		}
		tags[string(e[:4])] = p[off : off+size]
	}
	var m [3][3]float64
	var curves [3]func(float64) float64
	for c, sig := range [3]string{"rXYZ", "gXYZ", "bXYZ"} {
		xyz, ok := iccXYZ(tags[sig])
		if !ok {
			return nil
		}
		for r := range 3 {
			m[r][c] = xyz[r]
		}
	}
	for c, sig := range [3]string{"rTRC", "gTRC", "bTRC"} {
		if curves[c] = iccCurve(tags[sig]); curves[c] == nil {
			return nil
		}
	}
	m = mul3(inv3(srgbD50), m)
	if isSRGB(m, curves) {
		return nil
	}
	return newColorTransform(m, curves)
}

// isSRGB reports profiles that differ from sRGB by less than rounding.
func isSRGB(m [3][3]float64, curves [3]func(float64) float64) bool {
	for r := range 3 {
		for c := range 3 {
			want := 0.0
			if r == c {
				want = 1
			}
			if math.Abs(m[r][c]-want) > 0.003 {
				return false
			}
		}
	}
	for _, f := range curves {
		for v := range 256 {
			x := float64(v) / 255
			if math.Abs(srgbEncode(f(x))-srgbEncode(srgbDecode(x))) > 0.5/255 {
				return false
			}
		}
	}
	return true
}

func s15f16(b []byte) float64 { return float64(int32(binary.BigEndian.Uint32(b))) / 65536 }

func iccXYZ(b []byte) ([3]float64, bool) {
	if len(b) < 20 || string(b[:4]) != "XYZ " {
		return [3]float64{}, false
	}
	return [3]float64{s15f16(b[8:]), s15f16(b[12:]), s15f16(b[16:])}, true
}

// iccCurve reads a curv or para tone curve, from coded values to linear
// light, or returns nil.
func iccCurve(b []byte) func(float64) float64 {
	if len(b) < 12 {
		return nil
	}
	switch string(b[:4]) {
	case "curv":
		n := int(binary.BigEndian.Uint32(b[8:]))
		switch {
		case n == 0:
			return func(x float64) float64 { return x }
		case n == 1:
			if len(b) < 14 {
				return nil
			}
			g := float64(binary.BigEndian.Uint16(b[12:])) / 256
			return func(x float64) float64 { return math.Pow(x, g) }
		case len(b) < 12+2*n:
			return nil
		}
		table := make([]float64, n)
		for i := range table {
			table[i] = float64(binary.BigEndian.Uint16(b[12+2*i:])) / 65535
		}
		return func(x float64) float64 {
			pos := x * float64(n-1)
			i := min(int(pos), n-2)
			f := pos - float64(i)
			return table[i]*(1-f) + table[i+1]*f
		}
	case "para":
		kind := binary.BigEndian.Uint16(b[8:])
		count := [...]int{1, 3, 4, 5, 7}
		if int(kind) >= len(count) || len(b) < 12+4*count[kind] {
			return nil
		}
		var v [7]float64
		for i := range count[kind] {
			v[i] = s15f16(b[12+4*i:])
		}
		g, a, bb, c, d, e, f := v[0], v[1], v[2], v[3], v[4], v[5], v[6]
		pow := func(x float64) float64 { return math.Pow(max(x, 0), g) }
		switch kind {
		case 0:
			return pow
		case 1:
			return func(x float64) float64 {
				if a != 0 && x >= -bb/a {
					return pow(a*x + bb)
				}
				return 0
			}
		case 2:
			return func(x float64) float64 {
				if a != 0 && x >= -bb/a {
					return pow(a*x+bb) + c
				}
				return c
			}
		case 3:
			return func(x float64) float64 {
				if x >= d {
					return pow(a*x + bb)
				}
				return c * x
			}
		default:
			return func(x float64) float64 {
				if x >= d {
					return pow(a*x+bb) + e
				}
				return c*x + f
			}
		}
	}
	return nil
}

func mul3(a, b [3][3]float64) [3][3]float64 {
	var out [3][3]float64
	for r := range 3 {
		for c := range 3 {
			for k := range 3 {
				out[r][c] += a[r][k] * b[k][c]
			}
		}
	}
	return out
}

func apply3(m [3][3]float64, v [3]float64) [3]float64 {
	var out [3]float64
	for r := range 3 {
		out[r] = m[r][0]*v[0] + m[r][1]*v[1] + m[r][2]*v[2]
	}
	return out
}

func inv3(m [3][3]float64) [3][3]float64 {
	a, b, c := m[0][0], m[0][1], m[0][2]
	d, e, f := m[1][0], m[1][1], m[1][2]
	g, h, i := m[2][0], m[2][1], m[2][2]
	det := a*(e*i-f*h) - b*(d*i-f*g) + c*(d*h-e*g)
	return [3][3]float64{
		{(e*i - f*h) / det, (c*h - b*i) / det, (b*f - c*e) / det},
		{(f*g - d*i) / det, (a*i - c*g) / det, (c*d - a*f) / det},
		{(d*h - e*g) / det, (b*g - a*h) / det, (a*e - b*d) / det},
	}
}
