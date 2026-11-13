package imaging

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"testing"
)

// cameraJPEG returns a JPEG with an EXIF thumbnail, as cameras write, and
// the same file cut off in the main image's data: the thumbnail's end
// marker is all that is left of one.
func cameraJPEG(t testing.TB) (full, truncated []byte) {
	t.Helper()
	encode := func(w, h int) []byte {
		img := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
		for i := range img.Y {
			img.Y[i] = uint8(i * 7)
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	main, thumb := encode(160, 120), encode(16, 12)
	// A big-endian TIFF with an empty IFD0 and an IFD1 that points at the
	// thumbnail, which follows it.
	be := binary.BigEndian
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08")
	tiff = be.AppendUint16(tiff, 0)
	tiff = be.AppendUint32(tiff, 14) // IFD1
	tiff = be.AppendUint16(tiff, 2)
	tiff = append(be.AppendUint32(be.AppendUint16(be.AppendUint16(tiff, 0x201), 4), 1), 0, 0, 0, 44)
	tiff = be.AppendUint32(be.AppendUint32(be.AppendUint16(be.AppendUint16(tiff, 0x202), 4), 1), uint32(len(thumb)))
	tiff = be.AppendUint32(tiff, 0)
	tiff = append(tiff, thumb...)
	app1 := append([]byte("Exif\x00\x00"), tiff...)
	seg := be.AppendUint16([]byte{0xff, 0xe1}, uint16(len(app1)+2))
	full = append(append(append([]byte{0xff, 0xd8}, seg...), app1...), main[2:]...)
	truncated = full[:len(full)-len(main)/3]
	if !bytes.Contains(truncated, []byte{0xff, 0xd9}) {
		t.Fatal("truncated file lost the thumbnail's end marker")
	}
	return full, truncated
}

func TestJPEGComplete(t *testing.T) {
	full, truncated := cameraJPEG(t)
	progressive, err := os.ReadFile(imageFixturePath("progressive.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	padded := append(append([]byte(nil), full...), 0, 0, 0)
	for _, c := range []struct {
		name string
		data []byte
		want bool
	}{
		{"camera jpeg", full, true},
		{"camera jpeg with trailing bytes", padded, true},
		{"progressive jpeg", progressive, true},
		{"truncated camera jpeg", truncated, false},
		{"camera jpeg without its end marker", full[:len(full)-2], false},
		{"truncated progressive jpeg", progressive[:len(progressive)*2/3], false},
		{"headers only", full[:200], false},
		{"not a jpeg", []byte("\x89PNG\r\n\x1a\n"), false},
	} {
		if got := jpegComplete(c.data); got != c.want {
			t.Errorf("%s: jpegComplete = %v, want %v", c.name, got, c.want)
		}
	}
	if _, err := jpeg.Decode(bytes.NewReader(full)); err != nil {
		t.Errorf("camera jpeg does not decode: %v", err)
	}
}

// apng returns a PNG with an acTL chunk before its image data, making it an
// APNG whose frames, absent here, do not include the default image.
func apng(t testing.TB) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	body := binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(nil, 1), 0)
	c := append(binary.BigEndian.AppendUint32(nil, uint32(len(body))), "acTL"...)
	c = append(c, body...)
	c = binary.BigEndian.AppendUint32(c, crc32.ChecksumIEEE(c[4:]))
	const ihdrEnd = 8 + 25
	out := append(append(append([]byte(nil), data[:ihdrEnd]...), c...), data[ihdrEnd:]...)
	if _, err := png.Decode(bytes.NewReader(out)); err != nil {
		t.Fatalf("APNG: %v", err)
	}
	return out
}
