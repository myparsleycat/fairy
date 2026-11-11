package imaging

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"testing"
)

// exifTIFF returns a little-endian TIFF structure whose first IFD holds
// only the Orientation tag.
func exifTIFF(orient uint16) []byte {
	return []byte{'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 1, 3, 0, 1, 0, 0, 0, byte(orient), 0, 0, 0, 0, 0, 0, 0}
}

func withJPEGSegment(jpg []byte, marker byte, body []byte) []byte {
	seg := []byte{0xff, marker, byte((len(body) + 2) >> 8), byte(len(body) + 2)}
	return append(append(append(append([]byte(nil), jpg[:2]...), seg...), body...), jpg[2:]...)
}

// withPNGChunk inserts a chunk after IHDR.
func withPNGChunk(p []byte, typ string, body []byte) []byte {
	chunk := binary.BigEndian.AppendUint32(nil, uint32(len(body)))
	chunk = append(append(chunk, typ...), body...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))
	at := 8 + 25 // signature, IHDR
	return append(append(append([]byte(nil), p[:at]...), chunk...), p[at:]...)
}

func TestDecodeImageAppliesEXIFOrientation(t *testing.T) {
	// 40 x 20 quadrants: red, green over blue, white.
	red, green, blue, white := color.NRGBA{255, 0, 0, 255}, color.NRGBA{0, 255, 0, 255},
		color.NRGBA{0, 0, 255, 255}, color.NRGBA{255, 255, 255, 255}
	src := image.NewNRGBA(image.Rect(0, 0, 40, 20))
	for y := range 20 {
		for x := range 40 {
			c := [2][2]color.NRGBA{{red, green}, {blue, white}}[y/10][x/20]
			src.SetNRGBA(x, y, c)
		}
	}
	var jb, pb bytes.Buffer
	if err := jpeg.Encode(&jb, src, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(&pb, src); err != nil {
		t.Fatal(err)
	}
	// The quadrants as shown: top left, top right, bottom left, bottom right.
	want := map[uint16][4]color.NRGBA{
		1: {red, green, blue, white},
		2: {green, red, white, blue},
		3: {white, blue, green, red},
		4: {blue, white, red, green},
		5: {red, blue, green, white},
		6: {blue, red, white, green},
		7: {white, green, blue, red},
		8: {green, white, red, blue},
	}
	near := func(a, b color.NRGBA) bool {
		d := func(x, y uint8) bool { return x-y < 24 || y-x < 24 }
		return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B) && a.A == b.A
	}
	check := func(t *testing.T, data []byte, o uint16) {
		t.Helper()
		w, h := 40, 20
		if o >= 5 {
			w, h = 20, 40
		}
		img, err := DecodeImage(data)
		if err != nil {
			t.Fatal(err)
		}
		if img.W != w || img.H != h {
			t.Fatalf("decoded %dx%d, want %dx%d", img.W, img.H, w, h)
		}
		for i, p := range [4]image.Point{{w / 4, h / 4}, {3 * w / 4, h / 4}, {w / 4, 3 * h / 4}, {3 * w / 4, 3 * h / 4}} {
			if got := straightAt(img.Pixels, (p.Y*w+p.X)*4); !near(got, want[o][i]) {
				t.Errorf("quadrant %d is %v, want %v", i, got, want[o][i])
			}
		}
		// A bound applies to the image as shown.
		small, err := Decode(data, Bound{W: w / 2}, maxAnimBytes)
		if err != nil {
			t.Fatal(err)
		}
		if small.W != w/2 || small.H != h/2 {
			t.Fatalf("bound to width %d: decoded %dx%d", w/2, small.W, small.H)
		}
	}
	for o := range uint16(8) {
		o++
		t.Run("jpeg "+string(rune('0'+o)), func(t *testing.T) {
			check(t, withJPEGSegment(jb.Bytes(), 0xe1, append([]byte("Exif\x00\x00"), exifTIFF(o)...)), o)
		})
	}
	t.Run("png eXIf", func(t *testing.T) { check(t, withPNGChunk(pb.Bytes(), "eXIf", exifTIFF(6)), 6) })
}

// iccProfile returns an RGB matrix/TRC profile with the given D50 primaries
// and the sRGB tone curve.
func iccProfile(primaries [3][3]float64) []byte {
	s15 := func(v float64) []byte { return binary.BigEndian.AppendUint32(nil, uint32(int32(math.Round(v*65536)))) }
	var tags [][2][]byte
	for i, sig := range []string{"rXYZ", "gXYZ", "bXYZ"} {
		body := append([]byte("XYZ \x00\x00\x00\x00"), s15(primaries[i][0])...)
		body = append(append(body, s15(primaries[i][1])...), s15(primaries[i][2])...)
		tags = append(tags, [2][]byte{[]byte(sig), body})
	}
	trc := []byte("para\x00\x00\x00\x00\x00\x03\x00\x00")
	for _, v := range []float64{2.4, 1 / 1.055, 0.055 / 1.055, 1 / 12.92, 0.04045} {
		trc = append(trc, s15(v)...)
	}
	for _, sig := range []string{"rTRC", "gTRC", "bTRC"} {
		tags = append(tags, [2][]byte{[]byte(sig), trc})
	}
	p := make([]byte, 128)
	copy(p[16:], "RGB XYZ ")
	copy(p[36:], "acsp")
	p = binary.BigEndian.AppendUint32(p, uint32(len(tags)))
	off := 128 + 4 + 12*len(tags)
	var data []byte
	for _, t := range tags {
		p = append(p, t[0]...)
		p = binary.BigEndian.AppendUint32(p, uint32(off+len(data)))
		p = binary.BigEndian.AppendUint32(p, uint32(len(t[1])))
		data = append(data, t[1]...)
	}
	p = append(p, data...)
	binary.BigEndian.PutUint32(p, uint32(len(p)))
	return p
}

// Display P3's primaries adapted to D50, as Apple's profile records them.
var displayP3D50 = [3][3]float64{
	{0.515121, 0.241196, -0.001053},
	{0.291977, 0.692245, 0.041885},
	{0.157104, 0.066574, 0.784073},
}

// p3ToSRGB converts a Display P3 color with the published linear matrix.
func p3ToSRGB(c [3]uint8) [3]float64 {
	m := [3][3]float64{{1.2249, -0.2247, 0}, {-0.0420, 1.0419, 0}, {-0.0197, -0.0786, 1.0979}}
	var lin [3]float64
	for i, v := range c {
		lin[i] = srgbDecode(float64(v) / 255)
	}
	var out [3]float64
	for r := range 3 {
		out[r] = srgbEncode(min(max(m[r][0]*lin[0]+m[r][1]*lin[1]+m[r][2]*lin[2], 0), 1)) * 255
	}
	return out
}

func TestColorTransformConvertsToSRGB(t *testing.T) {
	colors := [][3]uint8{{200, 100, 50}, {30, 180, 90}, {128, 128, 128}, {250, 240, 10}}
	check := func(t *testing.T, xf *colorTransform) {
		t.Helper()
		if xf == nil {
			t.Fatal("no transform")
		}
		for _, c := range colors {
			want := p3ToSRGB(c)
			px := []byte{c[0], c[1], c[2], 255}
			xf.apply(px)
			for i := range 3 {
				if math.Abs(float64(px[i])-want[i]) > 1.01 {
					t.Errorf("%v: got %v, want %.1f", c, px[:3], want)
				}
			}
			// Translucent pixels convert their straight color.
			pre := make([]byte, 4)
			premultiplyRGBA(pre, []byte{c[0], c[1], c[2], 160})
			xf.apply(pre)
			got := straightAt(pre, 0)
			for i, v := range [3]uint8{got.R, got.G, got.B} {
				if math.Abs(float64(v)-want[i]) > 3 {
					t.Errorf("%v at alpha 160: got %v, want %.1f", c, got, want)
				}
			}
		}
	}
	t.Run("ICC", func(t *testing.T) { check(t, imageMeta{icc: iccProfile(displayP3D50)}.colorTransform()) })
	t.Run("nclx", func(t *testing.T) { check(t, imageMeta{primaries: 12}.colorTransform()) })

	t.Run("sRGB", func(t *testing.T) {
		var srgb [3][3]float64
		for r := range 3 {
			for c := range 3 {
				srgb[c][r] = srgbD50[r][c]
			}
		}
		for name, m := range map[string]imageMeta{
			"profile": {icc: iccProfile(srgb)},
			"BT.709":  {primaries: 1},
			"chunk":   {icc: iccProfile(displayP3D50), srgb: true},
			"broken":  {icc: iccProfile(displayP3D50)[:200]},
		} {
			if m.colorTransform() != nil {
				t.Errorf("%s: converted", name)
			}
		}
	})

	t.Run("PNG iCCP", func(t *testing.T) {
		src := image.NewNRGBA(image.Rect(0, 0, 2, 1))
		src.SetNRGBA(0, 0, color.NRGBA{200, 100, 50, 255})
		var buf, z bytes.Buffer
		if err := png.Encode(&buf, src); err != nil {
			t.Fatal(err)
		}
		zw := zlib.NewWriter(&z)
		zw.Write(iccProfile(displayP3D50))
		zw.Close()
		data := withPNGChunk(buf.Bytes(), "iCCP", append([]byte("Display P3\x00\x00"), z.Bytes()...))
		img, err := DecodeImage(data)
		if err != nil {
			t.Fatal(err)
		}
		want := p3ToSRGB([3]uint8{200, 100, 50})
		for i := range 3 {
			if math.Abs(float64(img.Pixels[i])-want[i]) > 1.01 {
				t.Fatalf("got %v, want %.1f", img.Pixels[:3], want)
			}
		}
	})

	t.Run("AVIF nclx", func(t *testing.T) {
		info, ok := inspectAVIF((avifFixture{brand: "avif", matrix: 1, transfer: 13, primaries: 12}).bytes())
		if !ok || info.primaries != 12 || !info.plain {
			t.Fatalf("%+v, %v", info, ok)
		}
	})
}

func TestAVIFSDRTransfer(t *testing.T) {
	for _, tc := range []struct {
		name     string
		transfer uint16
		linear   float64
	}{
		{"sRGB", 13, srgbDecode(128.0 / 255)},
		{"linear", 8, 128.0 / 255},
		{"BT709", 1, math.Pow((128.0/255+0.099)/1.099, 1/0.45)},
		{"SMPTE170", 6, math.Pow((128.0/255+0.099)/1.099, 1/0.45)},
		{"BT2020-10", 14, math.Pow((128.0/255+0.099)/1.099, 1/0.45)},
		{"BT2020-12", 15, math.Pow((128.0/255+0.0993)/1.0993, 1/0.45)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, err := Inspect((avifFixture{brand: "avif", matrix: 1, transfer: tc.transfer}).bytes())
			if err != nil {
				t.Fatal(err)
			}
			src := image.NewNRGBA(image.Rect(0, 0, 1, 1))
			src.SetNRGBA(0, 0, color.NRGBA{128, 128, 128, 255})
			got := in.finish(New(src))
			want := srgbEncode(tc.linear) * 255
			for _, v := range got.Pixels[:3] {
				if math.Abs(float64(v)-want) > 1 {
					t.Fatalf("encoded gray %d, want %.2f", v, want)
				}
			}
		})
	}
}
