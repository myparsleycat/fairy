package imaging

import (
	"bytes"
	"image/color"
	"math"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/myparsleycat/fairy/internal/darwin"
)

// ImageIO decodes still JPEG, PNG and AVIF images. DecodeNative draws the
// decoded image into a bitmap context of the image's own color space, so the
// pixels keep the values coded in the file: Input.finish converts them to
// sRGB and applies the EXIF orientation afterwards, as it does for the Go
// decoders' pixels.

var (
	cfDataCreateWithBytesNoCopy func(alloc, bytes unsafe.Pointer, n int, dealloc uintptr) uintptr
	cfDictionaryCreate          func(alloc unsafe.Pointer, keys, values *uintptr, n int, keyCB, valueCB uintptr) uintptr
	cfNumberCreate              func(alloc unsafe.Pointer, typ int, value unsafe.Pointer) uintptr
	cfStringCreateWithCString   func(alloc unsafe.Pointer, s *byte, encoding uint32) uintptr
	cfEqual                     func(a, b uintptr) bool
	cfGetRetainCount            func(p uintptr) int

	cgImageSourceCreateWithData     func(data, options uintptr) uintptr
	cgImageSourceGetType            func(src uintptr) uintptr
	cgImageSourceGetStatus          func(src uintptr) int32
	cgImageSourceGetCount           func(src uintptr) int
	cgImageSourceCreateImageAtIndex func(src uintptr, i int, options uintptr) uintptr

	cgImageGetWidth                  func(img uintptr) int
	cgImageGetHeight                 func(img uintptr) int
	cgImageGetColorSpace             func(img uintptr) uintptr
	cgImageGetAlphaInfo              func(img uintptr) uint32
	cgImageRelease                   func(img uintptr)
	cgColorSpaceGetModel             func(space uintptr) int32
	cgColorSpaceGetBaseColorSpace    func(space uintptr) uintptr
	cgColorSpaceSupportsOutput       func(space uintptr) bool
	cgBitmapContextCreate            func(data unsafe.Pointer, w, h, bitsPerComponent, bytesPerRow int, space uintptr, info uint32) uintptr
	cgContextSetInterpolationQuality func(ctx uintptr, q int32)
	cgContextSetBlendMode            func(ctx uintptr, mode int32)
	cgContextDrawImage               func(ctx uintptr, r darwin.CGRect, img uintptr)
	cgContextRelease                 func(ctx uintptr)

	kCFAllocatorNull                uintptr
	kCFBooleanFalse                 uintptr
	kCFTypeDictionaryKeyCallBacks   uintptr
	kCFTypeDictionaryValueCallBacks uintptr

	kCGImageSourceShouldCache     uintptr
	kCGImageSourceSubsampleFactor uintptr

	// The type identifiers ImageIO reports for the formats it decodes here.
	imageIOTypes map[string]uintptr

	imageIOHeldMu sync.Mutex
	imageIOHeld   [][]byte
)

const (
	cgModelGray    = 0 // kCGColorSpaceModelMonochrome
	cgModelRGB     = 1
	cgModelIndexed = 5

	cgAlphaNone              = 0
	cgAlphaPremultipliedLast = 1
	cgAlphaNoneSkipLast      = 5
	cgAlphaNoneSkipFirst     = 6
	cgAlphaOnly              = 7
	cgInterpolationHigh      = 3
	cgBlendModeNormal        = 0
	cgBlendModeCopy          = 17
	cfNumberIntType          = 9 // kCFNumberIntType
	cfStringEncodingUTF8     = 0x08000100
	cgImageStatusComplete    = 0
)

func init() {
	imageIO, err := purego.Dlopen("/System/Library/Frameworks/ImageIO.framework/ImageIO", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		panic("imaging: " + err.Error())
	}
	cf, cg := darwin.CoreFoundation, darwin.CoreGraphics
	for _, f := range []struct {
		ptr  any
		lib  uintptr
		name string
	}{
		{&cfDataCreateWithBytesNoCopy, cf, "CFDataCreateWithBytesNoCopy"},
		{&cfDictionaryCreate, cf, "CFDictionaryCreate"},
		{&cfNumberCreate, cf, "CFNumberCreate"},
		{&cfStringCreateWithCString, cf, "CFStringCreateWithCString"},
		{&cfEqual, cf, "CFEqual"},
		{&cfGetRetainCount, cf, "CFGetRetainCount"},
		{&cgImageSourceCreateWithData, imageIO, "CGImageSourceCreateWithData"},
		{&cgImageSourceGetType, imageIO, "CGImageSourceGetType"},
		{&cgImageSourceGetStatus, imageIO, "CGImageSourceGetStatus"},
		{&cgImageSourceGetCount, imageIO, "CGImageSourceGetCount"},
		{&cgImageSourceCreateImageAtIndex, imageIO, "CGImageSourceCreateImageAtIndex"},
		{&cgImageGetWidth, cg, "CGImageGetWidth"},
		{&cgImageGetHeight, cg, "CGImageGetHeight"},
		{&cgImageGetColorSpace, cg, "CGImageGetColorSpace"},
		{&cgImageGetAlphaInfo, cg, "CGImageGetAlphaInfo"},
		{&cgImageRelease, cg, "CGImageRelease"},
		{&cgColorSpaceGetModel, cg, "CGColorSpaceGetModel"},
		{&cgColorSpaceGetBaseColorSpace, cg, "CGColorSpaceGetBaseColorSpace"},
		{&cgColorSpaceSupportsOutput, cg, "CGColorSpaceSupportsOutput"},
		{&cgBitmapContextCreate, cg, "CGBitmapContextCreate"},
		{&cgContextSetInterpolationQuality, cg, "CGContextSetInterpolationQuality"},
		{&cgContextSetBlendMode, cg, "CGContextSetBlendMode"},
		{&cgContextDrawImage, cg, "CGContextDrawImage"},
		{&cgContextRelease, cg, "CGContextRelease"},
	} {
		darwin.Func(f.ptr, f.lib, f.name)
	}
	kCFAllocatorNull = uintptr(darwin.SymbolID(cf, "kCFAllocatorNull"))
	kCFBooleanFalse = uintptr(darwin.SymbolID(cf, "kCFBooleanFalse"))
	kCFTypeDictionaryKeyCallBacks = darwin.Symbol(cf, "kCFTypeDictionaryKeyCallBacks")
	kCFTypeDictionaryValueCallBacks = darwin.Symbol(cf, "kCFTypeDictionaryValueCallBacks")
	kCGImageSourceShouldCache = uintptr(darwin.SymbolID(imageIO, "kCGImageSourceShouldCache"))
	kCGImageSourceSubsampleFactor = uintptr(darwin.SymbolID(imageIO, "kCGImageSourceSubsampleFactor"))
	imageIOTypes = map[string]uintptr{}
	for format, uti := range map[string]string{"jpeg": "public.jpeg", "png": "public.png", "avif": "public.avif"} {
		b := append([]byte(uti), 0)
		imageIOTypes[format] = cfStringCreateWithCString(nil, &b[0], cfStringEncodingUTF8)
	}
}

// nativeAccepts reports whether ImageIO decodes in's format. It takes the
// images WIC does on Windows: still JPEGs in YCbCr or gray with their start
// and end markers, complete PNGs, and plain AVIF images. Truncated files,
// which ImageIO decodes partially, and CMYK JPEGs are left to Go.
func nativeAccepts(in Input) bool {
	if in.Animation.Count != 0 || in.AnimErr != nil || in.Config.Width <= 0 || in.Config.Height <= 0 ||
		uint64(len(in.data)) > math.MaxInt32 {
		return false
	}
	switch {
	case in.Format == "jpeg":
		return bytes.HasPrefix(in.data, []byte{0xff, 0xd8}) &&
			(in.Config.ColorModel == color.YCbCrModel || in.Config.ColorModel == color.GrayModel) &&
			bytes.LastIndex(in.data, []byte{0xff, 0xd9}) >= 2
	case in.Format == "png":
		return bytes.HasPrefix(in.data, []byte("\x89PNG\r\n\x1a\n")) && bytes.Contains(in.data, pngEnd)
	case in.Format == "avif":
		return in.avif.plain
	}
	return false
}

// imageIOScratch is the memory reserved for what ImageIO and CoreGraphics
// allocate besides the decoded source and the output.
const imageIOScratch = 2 << 20

// nativeWork estimates the memory DecodeNative holds: the output, the image
// ImageIO decodes to draw it from, at a DCT scale for reduced JPEGs, and
// ImageIO's scratch. AVIF decodes through YCbCr planes first, so it reserves
// 12 bytes per source pixel like the HEIF decoder on Windows.
func nativeWork(in Input, bound Bound) int64 {
	sw, sh := in.srcSize()
	dw, dh := in.srcBound(bound).Size(sw, sh)
	n := int64(dw)*int64(dh)*4 + imageIOScratch
	switch in.Format {
	case "jpeg":
		if w, h, ok := jpegDCTSize(sw, sh, dw, dh); ok {
			n += int64(w) * int64(h) * 4
		} else {
			n += int64(sw) * int64(sh) * 4
		}
	case "png":
		per := int64(4)
		switch in.Config.ColorModel {
		case color.RGBA64Model, color.NRGBA64Model, color.Gray16Model:
			per = 8
		}
		n += int64(sw) * int64(sh) * per
	case "avif":
		n += int64(sw) * int64(sh) * 12
	}
	return min(n, WorkBudget)
}

// DecodeNative decodes a still JPEG, PNG or AVIF with ImageIO, scaled within
// bound with CoreGraphics' high-quality resampling, in premultiplied alpha.
// The pixels keep the image's color space and coded orientation; reduced
// JPEGs first decode at a DCT scale. ImageIO reads the bytes in place. It
// returns nil for images ImageIO does not take or when a call fails, and is
// safe to call from several goroutines at once.
//
// ImageIO converts AVIF images coded with BT.601 matrix coefficients with
// those coefficients, so unlike the HEIF decoder on Windows it needs no
// correction.
func DecodeNative(in Input, bound Bound) (img *Bitmap) {
	if !nativeAccepts(in) {
		return nil
	}
	sw, sh := in.srcSize()
	dw, dh := in.srcBound(bound).Size(sw, sh)
	if dw <= 0 || dh <= 0 {
		return nil
	}
	var pin runtime.Pinner
	pin.Pin(unsafe.SliceData(in.data))
	defer pin.Unpin()
	defer runtime.KeepAlive(in.data)
	data := cfDataCreateWithBytesNoCopy(nil, unsafe.Pointer(unsafe.SliceData(in.data)), len(in.data), kCFAllocatorNull)
	if data == 0 {
		return nil
	}
	darwin.WithPool(func() { img = decodeImageIO(data, in, sw, sh, dw, dh) })
	// The source and images, released with the pool drained, were all that
	// held the data. Should ImageIO keep it past them, the bytes stay
	// reachable for good rather than be collected under it.
	if cfGetRetainCount(data) != 1 {
		imageIOHeldMu.Lock()
		imageIOHeld = append(imageIOHeld, in.data)
		imageIOHeldMu.Unlock()
	}
	darwin.CFRelease(data)
	return img
}

func decodeImageIO(data uintptr, in Input, sw, sh, dw, dh int) *Bitmap {
	noCache := cfDict([]uintptr{kCGImageSourceShouldCache}, []uintptr{kCFBooleanFalse})
	defer darwin.CFRelease(noCache)
	src := cgImageSourceCreateWithData(data, noCache)
	if src == 0 {
		return nil
	}
	defer darwin.CFRelease(src)
	if t := cgImageSourceGetType(src); t == 0 || !cfEqual(t, imageIOTypes[in.Format]) ||
		cgImageSourceGetStatus(src) != cgImageStatusComplete || cgImageSourceGetCount(src) < 1 {
		return nil
	}
	img := cgImageSourceCreateImageAtIndex(src, 0, noCache)
	if img == 0 {
		return nil
	}
	defer cgImageRelease(img)
	if cgImageGetWidth(img) != sw || cgImageGetHeight(img) != sh {
		return nil
	}
	space := cgImageGetColorSpace(img)
	if space == 0 {
		return nil
	}
	if in.Format == "jpeg" {
		if s := jpegReduced(src, space, sw, sh, dw, dh); s != 0 {
			defer cgImageRelease(s)
			img = s
		}
	}
	return drawImageIO(img, space, dw, dh)
}

// jpegReduced decodes a JPEG at the DCT scale jpegDCTSize picks, or returns
// 0 to draw from the full-size image instead. ImageIO's subsampling decodes
// at the DCT scale itself, where a thumbnail would resample to the size it
// is asked for, and like the full image it is neither rotated nor color
// converted; the checks on its size and color space make sure of that.
func jpegReduced(src, space uintptr, sw, sh, dw, dh int) uintptr {
	w, h, f, ok := jpegDCTScale(sw, sh, dw, dh)
	if !ok {
		return 0
	}
	factor := int32(f)
	n := cfNumberCreate(nil, cfNumberIntType, unsafe.Pointer(&factor))
	if n == 0 {
		return 0
	}
	opts := cfDict([]uintptr{kCGImageSourceShouldCache, kCGImageSourceSubsampleFactor}, []uintptr{kCFBooleanFalse, n})
	darwin.CFRelease(n)
	defer darwin.CFRelease(opts)
	img := cgImageSourceCreateImageAtIndex(src, 0, opts)
	if img == 0 {
		return 0
	}
	rw, rh := cgImageGetWidth(img), cgImageGetHeight(img)
	// ImageIO rounds the reduced size down, where jpegDCTSize rounds up.
	if rw < w-1 || rh < h-1 || rw > w || rh > h || !cfEqual(cgImageGetColorSpace(img), space) {
		cgImageRelease(img)
		return 0
	}
	return img
}

// drawImageIO draws img scaled to dw x dh into premultiplied RGBA of its own
// color space.
func drawImageIO(img, space uintptr, dw, dh int) *Bitmap {
	switch cgColorSpaceGetModel(space) {
	case cgModelRGB:
	case cgModelIndexed:
		space = cgColorSpaceGetBaseColorSpace(space)
		if space == 0 {
			return nil
		}
		switch cgColorSpaceGetModel(space) {
		case cgModelRGB:
		case cgModelGray:
			return drawGrayImageIO(img, space, dw, dh)
		default:
			return nil
		}
	case cgModelGray:
		return drawGrayImageIO(img, space, dw, dh)
	default:
		return nil
	}
	if !cgColorSpaceSupportsOutput(space) {
		return nil
	}
	pix := make([]byte, dw*dh*4)
	if !drawInto(img, pix, dw, dh, dw*4, space, cgAlphaPremultipliedLast, cgBlendModeCopy) {
		return nil
	}
	return &Bitmap{W: dw, H: dh, Pixels: pix}
}

// drawGrayImageIO draws a gray image into a gray context, which takes no
// alpha: over black, which leaves the gray premultiplied, and its alpha
// into an alpha-only context.
func drawGrayImageIO(img, space uintptr, dw, dh int) *Bitmap {
	if !cgColorSpaceSupportsOutput(space) {
		return nil
	}
	gray := make([]byte, dw*dh)
	if !drawInto(img, gray, dw, dh, dw, space, cgAlphaNone, cgBlendModeNormal) {
		return nil
	}
	pix := make([]byte, dw*dh*4)
	for i, g := range gray {
		pix[4*i], pix[4*i+1], pix[4*i+2], pix[4*i+3] = g, g, g, 255
	}
	switch cgImageGetAlphaInfo(img) {
	case cgAlphaNone, cgAlphaNoneSkipLast, cgAlphaNoneSkipFirst:
		return &Bitmap{W: dw, H: dh, Pixels: pix}
	}
	clear(gray)
	if !drawInto(img, gray, dw, dh, dw, 0, cgAlphaOnly, cgBlendModeCopy) {
		return nil
	}
	for i, a := range gray {
		pix[4*i+3] = a
		// The drawing over black kept gray at most alpha, but resampling
		// the two separately can leave it a step above.
		for c := range 3 {
			pix[4*i+c] = min(pix[4*i+c], a)
		}
	}
	return &Bitmap{W: dw, H: dh, Pixels: pix}
}

// drawInto draws img scaled to w x h into the zeroed pix with blend mode
// mode: copy, so that the drawing replaces the pixels rather than blends
// over them, or for a context without alpha normal, over black.
func drawInto(img uintptr, pix []byte, w, h, stride int, space uintptr, info uint32, mode int32) bool {
	var pin runtime.Pinner
	pin.Pin(unsafe.SliceData(pix))
	defer pin.Unpin()
	ctx := cgBitmapContextCreate(unsafe.Pointer(unsafe.SliceData(pix)), w, h, 8, stride, space, info)
	if ctx == 0 {
		return false
	}
	defer cgContextRelease(ctx)
	cgContextSetInterpolationQuality(ctx, cgInterpolationHigh)
	cgContextSetBlendMode(ctx, mode)
	cgContextDrawImage(ctx, darwin.Rect(0, 0, float64(w), float64(h)), img)
	return true
}

// cfDict returns a new dictionary of keys and values.
func cfDict(keys, values []uintptr) uintptr {
	return cfDictionaryCreate(nil, unsafe.SliceData(keys), unsafe.SliceData(values), len(keys),
		kCFTypeDictionaryKeyCallBacks, kCFTypeDictionaryValueCallBacks)
}
