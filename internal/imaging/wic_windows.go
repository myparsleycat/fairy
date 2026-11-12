package imaging

import (
	"bytes"
	"encoding/hex"
	"image"
	"image/color"
	"math"
	"runtime"
	"sync"
	"unsafe"

	syswindows "golang.org/x/sys/windows"

	"github.com/myparsleycat/fairy/internal/com"
	"github.com/myparsleycat/fairy/internal/win"
)

var (
	wicFactoryClass       = com.MustGUID("cacaf262-9370-4615-a13b-9f5539da4c0a")
	wicFactoryIID         = com.MustGUID("ec5ec8a9-c395-4314-9c77-54d7a935ff70")
	wicJPEGDecoderClass   = com.MustGUID("9456a480-e88b-43ea-9e73-0b2d9b71b1ca")
	wicPNGDecoderClass    = com.MustGUID("e018945b-aa86-4008-9bd4-6777a1e40c11")
	wicHEIFDecoderClass   = com.MustGUID("e9a4a80a-44fe-4de4-8971-7150b10a5199")
	wicBitmapDecoderIID   = com.MustGUID("9edde9e7-8dee-47ea-99df-e6faf2ed44bf")
	wicBitmapTransformIID = com.MustGUID("3b16811b-6a43-4ec9-b713-3d5a0c13b940")
	wicRGBA               = com.MustGUID("f5c7ad2d-6a8d-43dd-a7a8-a29935261ae9")
	wicPRGBA              = com.MustGUID("3cc4a650-a527-4d37-a916-3142c7ebedba")
	wicBGR24              = wicFormat(0x0c)
	wicGray8              = wicFormat(0x08)
	wicCoUninitialize     = syswindows.NewLazySystemDLL("ole32.dll").NewProc("CoUninitialize")
)

// wicFormat returns one of the GUID_WICPixelFormat GUIDs that differ only in
// their last byte.
func wicFormat(last byte) com.GUID {
	return com.GUID{Data1: 0x6fddc324, Data2: 0x4e03, Data3: 0x4bfe,
		Data4: [8]byte{0xb1, 0x85, 0x3d, 0x77, 0x76, 0x8d, 0xc9, last}}
}

// wicOpaque reports pixel formats without alpha. Indexed formats are left
// out because a PNG palette can carry alpha.
func wicOpaque(f com.GUID) bool {
	switch f {
	case wicFormat(0x05), wicFormat(0x06), wicFormat(0x07), wicFormat(0x08), wicFormat(0x0b), // gray
		wicFormat(0x0c), wicFormat(0x0d), wicFormat(0x0e), wicFormat(0x15): // BGR, RGB, BGRX, 48bpp RGB
		return true
	}
	return false
}

// pngEnd is the IEND chunk, which a complete PNG ends with.
var pngEnd = []byte("\x00\x00\x00\x00IEND\xaeB`\x82")

// wicDecoderClass returns the built-in WIC decoder for still JPEG and PNG
// images. Images that need a guard the Go decoders give, or a color
// conversion WIC does differently, are left to Go: truncated files, which
// WIC decodes partially, and CMYK JPEGs.
// wicAccepts reports whether WIC decodes in's format.
func wicAccepts(in Input) bool { return wicDecoderClass(in) != nil }

func wicDecoderClass(in Input) *com.GUID {
	if in.Animation.Count != 0 || in.AnimErr != nil || uint64(len(in.data)) > math.MaxUint32 ||
		in.Config.Width <= 0 || in.Config.Height <= 0 {
		return nil
	}
	switch {
	case in.Format == "jpeg" && bytes.HasPrefix(in.data, []byte{0xff, 0xd8}) &&
		(in.Config.ColorModel == color.YCbCrModel || in.Config.ColorModel == color.GrayModel) &&
		bytes.LastIndex(in.data, []byte{0xff, 0xd9}) >= 2:
		return &wicJPEGDecoderClass
	case in.Format == "png" && bytes.HasPrefix(in.data, []byte("\x89PNG\r\n\x1a\n")) &&
		bytes.Contains(in.data, pngEnd):
		return &wicPNGDecoderClass
	case in.Format == "avif" && in.avif.plain:
		return &wicHEIFDecoderClass
	}
	return nil
}

// jpegDCTSize returns the size of a JPEG decoded at 1/8, 1/4 or 1/2 scale
// that still leaves at least twice the target dimensions, so the final
// resampling keeps its quality.
func jpegDCTSize(sw, sh, dw, dh int) (int, int, bool) {
	for _, f := range [...]int{8, 4, 2} {
		w, h := (sw+f-1)/f, (sh+f-1)/f
		if w >= 2*dw && h >= 2*dh {
			return w, h, true
		}
	}
	return 0, 0, false
}

// wicScratch is the memory reserved for what WIC's decoders, converters and
// scaler allocate besides the pixels DecodeWIC holds, given that wicCopyRows
// requests rows in bands of about 1 MiB.
const wicScratch = 2 << 20

// wicWork estimates the memory DecodeWIC holds: the output, the DCT-scaled
// JPEG it resamples from or the HEIF decoder's full image, and WIC's scratch.
func wicWork(in Input, bound Bound) int64 {
	sw, sh := in.srcSize()
	dw, dh := in.srcBound(bound).Size(sw, sh)
	n := int64(dw)*int64(dh)*4 + wicScratch
	switch in.Format {
	case "jpeg":
		if w, h, ok := jpegDCTSize(sw, sh, dw, dh); ok {
			n += int64(w) * int64(h) * 3
		}
	case "avif":
		// The HEIF decoder decodes the whole image before it hands out
		// rows, so reserve 12 bytes per source pixel whatever the output
		// size.
		n += int64(sw) * int64(sh) * 12
	}
	return min(n, WorkBudget)
}

// DecodeWIC decodes a still JPEG or PNG with the Windows Imaging Component,
// scaled within bound with high-quality cubic resampling, in premultiplied
// alpha for translucent images. JPEGs reduced fourfold or more first decode
// at a DCT scale. WIC reads the bytes in place and hands out rows on
// request, so neither the full-size source nor a resampling buffer of
// source height is held. It returns nil for images WIC does not take or
// when a call fails, such as in a COM single-threaded apartment.
func DecodeWIC(in Input, bound Bound) *Bitmap {
	img := decodeWICPixels(in, bound)
	if img == nil {
		return nil
	}
	if in.Format == "avif" && in.avif.bt601 {
		switch heifMatrix() {
		case heifMatrixBT709:
			rec709To601(img.Pix)
		case heifMatrixFollowsFile:
		default:
			return nil
		}
	}
	return New(img)
}

func decodeWICPixels(in Input, bound Bound) *image.RGBA {
	class := wicDecoderClass(in)
	if class == nil {
		return nil
	}
	sw, sh := in.srcSize()
	dw, dh := in.srcBound(bound).Size(sw, sh)
	if dw <= 0 || dh <= 0 {
		return nil
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := win.CoInitializeEx(win.COINIT_MULTITHREADED); err != nil {
		return nil
	}
	defer wicCoUninitialize.Call()
	factory, hr := win.CoCreateInstance(unsafe.Pointer(&wicFactoryClass), unsafe.Pointer(&wicFactoryIID))
	if com.HRESULT(hr).Failed() {
		return nil
	}
	w := wicSession{factory: factory}
	defer w.close()

	var pin runtime.Pinner
	pin.Pin(unsafe.SliceData(in.data))
	defer pin.Unpin()
	const (
		createStream         = 14
		initializeFromMemory = 16
	)
	stream := w.out(func(p *unsafe.Pointer) com.HRESULT { return com.HR(factory, createStream, uintptr(unsafe.Pointer(p))) })
	if stream == nil || com.HR(stream, initializeFromMemory,
		uintptr(unsafe.Pointer(unsafe.SliceData(in.data))), uintptr(len(in.data))).Failed() {
		return nil
	}
	dec, hr := win.CoCreateInstance(unsafe.Pointer(class), unsafe.Pointer(&wicBitmapDecoderIID))
	if com.HRESULT(hr).Failed() {
		return nil
	}
	w.own(dec)
	const (
		initialize     = 4
		getFrame       = 13
		getSize        = 3
		getPixelFormat = 4
	)
	if com.HR(dec, initialize, uintptr(stream), 0).Failed() {
		return nil
	}
	frame := w.out(func(p *unsafe.Pointer) com.HRESULT { return com.HR(dec, getFrame, 0, uintptr(unsafe.Pointer(p))) })
	if frame == nil {
		return nil
	}
	var fw, fh uint32
	var format com.GUID
	if com.HR(frame, getSize, uintptr(unsafe.Pointer(&fw)), uintptr(unsafe.Pointer(&fh))).Failed() ||
		int(fw) != sw || int(fh) != sh ||
		com.HR(frame, getPixelFormat, uintptr(unsafe.Pointer(&format))).Failed() {
		return nil
	}

	src, srcW, srcH := frame, sw, sh
	if class == &wicJPEGDecoderClass {
		if s, w2, h2 := w.dctScaled(frame, sw, sh, dw, dh); s != nil {
			src, srcW, srcH = s, w2, h2
		}
	}
	// Scaled translucent images are resampled premultiplied and copied out
	// as they are. Otherwise the straight pixels are premultiplied here, so
	// that unscaled images match the Go decoders exactly.
	out, premul := &wicRGBA, false
	if srcW != dw || srcH != dh {
		if !wicOpaque(format) {
			if src = w.convert(src, &wicPRGBA); src == nil {
				return nil
			}
			out, premul = &wicPRGBA, true
		}
		if src = w.scale(src, dw, dh); src == nil {
			return nil
		}
	}
	if src = w.convert(src, out); src == nil {
		return nil
	}
	img := image.NewRGBA(image.Rect(0, 0, dw, dh))
	if !wicCopyRows(src, img.Pix, dw, dh, img.Stride) {
		return nil
	}
	if !premul && !wicOpaque(format) {
		premultiplyRGBA(img.Pix, img.Pix)
	}
	runtime.KeepAlive(in.data)
	return img
}

const (
	heifMatrixUnknown = iota
	heifMatrixFollowsFile
	heifMatrixBT709
	heifMatrixOther
)

var (
	heifMatrixMu    sync.Mutex
	heifMatrixState = heifMatrixUnknown
)

// heifMatrixProbe is a lossless 8 x 8 AVIF of Y 110, Cb 100, Cr 200 in full
// range with BT.601 matrix coefficients: RGB 211 68 60, or 223 82 58 when
// decoded with BT.709's.
const heifMatrixProbe = "00000020667479706176696600000000617669666d6966316d6961664d413141000000f9" +
	"6d657461000000000000002f68646c7200000000000000007069637400000000000000000000000050696374757265" +
	"48616e646c6572000000000e7069746d0000000000010000001e696c6f6300000000440000010001000000010000" +
	"01210000001e0000002869696e660000000000010000001a696e6665020000000001000061763031436f6c6f7200" +
	"0000006a697072700000004b6970636f0000001469737065000000000000000800000008000000107069786900000000" +
	"030808080000000c617631438120000000000013636f6c726e636c78000200020006800000001769706d6100000000" +
	"0000000100010401028304000000266d6461740a083808bf6d020206903212100000000ffa4aaf931ea7557f3f6bb6" +
	"4740"

// heifMatrix reports which matrix coefficients the HEIF decoder uses for
// AVIF images coded with BT.601 ones; version 1.2 of the HEIF Bitmap
// Extensions uses BT.709's whatever the file says. A probe that fails to
// decode is retried on a later call.
func heifMatrix() int {
	heifMatrixMu.Lock()
	defer heifMatrixMu.Unlock()
	if heifMatrixState != heifMatrixUnknown {
		return heifMatrixState
	}
	data, _ := hex.DecodeString(heifMatrixProbe)
	in, err := Inspect(data)
	if err != nil {
		return heifMatrixUnknown
	}
	img := decodeWICPixels(in, Bound{})
	if img == nil || len(img.Pix) < 4 {
		return heifMatrixUnknown
	}
	near := func(r, g, b int) bool {
		for c, want := range [3]int{r, g, b} {
			if d := int(img.Pix[c]) - want; d < -4 || d > 4 {
				return false
			}
		}
		return true
	}
	switch {
	case near(211, 68, 60):
		heifMatrixState = heifMatrixFollowsFile
	case near(223, 82, 58):
		heifMatrixState = heifMatrixBT709
	default:
		heifMatrixState = heifMatrixOther
	}
	return heifMatrixState
}

// rec709To601 recolors pixels that were converted from BT.601 YCbCr with
// BT.709 matrix coefficients; the HEIF decoder returns opaque pixels. Both
// conversions are linear, so this is the
// BT.601 YCbCr-to-RGB matrix times the inverse of BT.709's, in 16-bit fixed
// point with rows summing to one so that grays stay unchanged. Colors that
// BT.709's conversion pushed out of range were clipped and stay slightly off.
func rec709To601(pix []byte) {
	m := [3][3]int32{{59874, 5143, 519}, {-6884, 76819, -4399}, {628, 2112, 62796}}
	for i := 0; i+4 <= len(pix); i += 4 {
		r, g, b := int32(pix[i]), int32(pix[i+1]), int32(pix[i+2])
		for c := range 3 {
			v := (m[c][0]*r + m[c][1]*g + m[c][2]*b + 1<<15) >> 16
			pix[i+c] = uint8(max(0, min(255, v)))
		}
	}
}

// wicSession releases the COM objects of one decode in reverse order.
type wicSession struct {
	factory unsafe.Pointer
	objs    []unsafe.Pointer
}

func (w *wicSession) own(p unsafe.Pointer) unsafe.Pointer {
	w.objs = append(w.objs, p)
	return p
}

func (w *wicSession) out(call func(*unsafe.Pointer) com.HRESULT) unsafe.Pointer {
	var p unsafe.Pointer
	if call(&p).Failed() || p == nil {
		return nil
	}
	return w.own(p)
}

func (w *wicSession) close() {
	for i := len(w.objs) - 1; i >= 0; i-- {
		com.Release(w.objs[i])
	}
	com.Release(w.factory)
}

func (w *wicSession) convert(src unsafe.Pointer, format *com.GUID) unsafe.Pointer {
	const (
		createFormatConverter = 10
		initialize            = 8
	)
	c := w.out(func(p *unsafe.Pointer) com.HRESULT {
		return com.HR(w.factory, createFormatConverter, uintptr(unsafe.Pointer(p)))
	})
	// WICBitmapDitherTypeNone, no palette, alpha threshold 0, WICBitmapPaletteTypeCustom.
	if c == nil || com.HR(c, initialize, uintptr(src), uintptr(unsafe.Pointer(format)), 0, 0, 0, 0).Failed() {
		return nil
	}
	return c
}

func (w *wicSession) scale(src unsafe.Pointer, dw, dh int) unsafe.Pointer {
	const (
		createBitmapScaler = 11
		initialize         = 8
		highQualityCubic   = 4 // WICBitmapInterpolationModeHighQualityCubic
	)
	s := w.out(func(p *unsafe.Pointer) com.HRESULT {
		return com.HR(w.factory, createBitmapScaler, uintptr(unsafe.Pointer(p)))
	})
	if s == nil || com.HR(s, initialize, uintptr(src), uintptr(dw), uintptr(dh), highQualityCubic).Failed() {
		return nil
	}
	return s
}

// dctScaled decodes a JPEG frame at the DCT scale jpegDCTSize picks into a
// WIC bitmap, or returns nil to resample the full-size frame instead.
func (w *wicSession) dctScaled(frame unsafe.Pointer, sw, sh, dw, dh int) (unsafe.Pointer, int, int) {
	want, wantH, ok := jpegDCTSize(sw, sh, dw, dh)
	if !ok {
		return nil, 0, 0
	}
	t, err := com.QueryInterface(frame, &wicBitmapTransformIID)
	if err != nil {
		return nil, 0, 0
	}
	w.own(t)
	const (
		copyPixels            = 3
		getClosestSize        = 4
		getClosestPixelFormat = 5
	)
	format := wicBGR24
	if com.HR(t, getClosestPixelFormat, uintptr(unsafe.Pointer(&format))).Failed() ||
		(format != wicBGR24 && format != wicGray8) {
		return nil, 0, 0
	}
	bpp := 3
	if format == wicGray8 {
		bpp = 1
	}
	rw, rh := uint32(want), uint32(wantH)
	if com.HR(t, getClosestSize, uintptr(unsafe.Pointer(&rw)), uintptr(unsafe.Pointer(&rh))).Failed() ||
		int(rw) < 2*dw || int(rh) < 2*dh || int(rw) >= sw || int(rh) >= sh ||
		int(rw) > want || int(rh) > wantH {
		return nil, 0, 0
	}
	const (
		createBitmap   = 17
		cacheOnLoad    = 1 // WICBitmapCacheOnLoad
		lock           = 8
		lockWrite      = 2 // WICBitmapLockWrite
		getStride      = 4
		getDataPointer = 5
	)
	bmp := w.out(func(p *unsafe.Pointer) com.HRESULT {
		return com.HR(w.factory, createBitmap, uintptr(rw), uintptr(rh), uintptr(unsafe.Pointer(&format)),
			cacheOnLoad, uintptr(unsafe.Pointer(p)))
	})
	if bmp == nil {
		return nil, 0, 0
	}
	var l unsafe.Pointer
	if com.HR(bmp, lock, 0, lockWrite, uintptr(unsafe.Pointer(&l))).Failed() || l == nil {
		return nil, 0, 0
	}
	var stride, size uint32
	var data uintptr
	ok = !com.HR(l, getStride, uintptr(unsafe.Pointer(&stride))).Failed() &&
		!com.HR(l, getDataPointer, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&data))).Failed() &&
		int(stride) >= int(rw)*bpp && uint64(size) >= uint64(stride)*uint64(rh) &&
		!com.HR(t, copyPixels, 0, uintptr(rw), uintptr(rh), uintptr(unsafe.Pointer(&format)), 0,
			uintptr(stride), uintptr(size), data).Failed()
	// The bitmap can be read only once its lock is released.
	com.Release(l)
	if !ok {
		return nil, 0, 0
	}
	return bmp, int(rw), int(rh)
}

// wicCopyRows copies src in bands of rows. A request for the whole image
// makes WIC decode and convert it in one piece, which holds a full-size
// intermediate; bands keep that to a few rows.
func wicCopyRows(src unsafe.Pointer, pix []byte, w, h, stride int) bool {
	const copyPixels = 7
	band := max(1, (1<<20)/stride)
	for y := 0; y < h; y += band {
		n := min(band, h-y)
		rect := [4]int32{0, int32(y), int32(w), int32(n)}
		chunk := pix[y*stride : (y+n)*stride]
		if com.HR(src, copyPixels, uintptr(unsafe.Pointer(&rect)), uintptr(stride), uintptr(len(chunk)),
			uintptr(unsafe.Pointer(unsafe.SliceData(chunk)))).Failed() {
			return false
		}
	}
	return true
}
