package fairy

import (
	"image"
	"log"
	"runtime/debug"
	"sync"
	"time"

	"github.com/myparsleycat/fairy/internal/geom"
	"github.com/myparsleycat/fairy/internal/imagecache"
	"github.com/myparsleycat/fairy/internal/imaging"
	"github.com/myparsleycat/fairy/internal/win"
)

type Fit uint8

const (
	FitCover Fit = iota
	FitContain
	FitFill
)

// geom.Fit is converted from Fit by value; these fail to compile when the
// values differ.
var (
	_ = [1]struct{}{}[geom.FitCover-geom.Fit(FitCover)]
	_ = [1]struct{}{}[geom.FitContain-geom.Fit(FitContain)]
	_ = [1]struct{}{}[geom.FitFill-geom.Fit(FitFill)]
)

// Image holds decoded pixels and the state used to display them.
type Image struct {
	c     *imagecache.Image
	gpu   *gpuImage
	start time.Time
}

// noPixels stands in for the pixels of a zero Image.
var noPixels imagecache.Image

// core returns the pixels behind i.
func (i *Image) core() *imagecache.Image {
	if i.c != nil {
		return i.c
	}
	return &noPixels
}

// wrapCore returns the Image presenting core, making it on first use, or nil
// for nil.
func wrapCore(core *imagecache.Image) *Image {
	if core == nil {
		return nil
	}
	if o := core.Owner(); o != nil {
		return o.(*Image)
	}
	img := &Image{c: core}
	if !core.SetOwner(img) {
		return core.Owner().(*Image)
	}
	return img
}

func wrapImage(data *imaging.Bitmap) *Image { return wrapCore(imagecache.NewImage(data)) }

// NewImage converts any image.Image.
func NewImage(src image.Image) *Image { return wrapImage(imaging.New(src)) }

// DecodeImage decodes PNG, JPEG, GIF or WebP bytes. Still images are rotated
// as their EXIF orientation says, and colors with an embedded ICC profile
// are converted to sRGB. Animated GIF and WebP images keep every frame and
// play when drawn; when their frames would take more than 64 MiB, the
// frames are decoded as they play. Images larger than 64 megapixels are
// rejected. Register more formats (AVIF, for one) with image.RegisterFormat.
func DecodeImage(data []byte) (*Image, error) {
	decoded, err := imaging.DecodeImage(data)
	return wrapImage(decoded), err
}

// Size returns the image size in pixels.
func (i *Image) Size() (int, int) { return i.core().Size() }

// ImageEl draws img. Without an explicit size it takes the image's pixel size.
func ImageEl(img *Image) *El {
	e := newEl(kindImage)
	e.image = img
	return e
}

// Fit sets how an image element fills its box.
func (e *El) Fit(f Fit) *El { e.fit = f; return e }

// NewImageFrames makes an animated Image from frames shown in turn, each for
// its delay. The frames are drawn over a transparent canvas the size of the
// first one, at their bounds' positions. plays is how many times the
// animation runs before it stops on the last frame; 0 repeats forever.
func NewImageFrames(frames []image.Image, delays []time.Duration, plays int) (*Image, error) {
	decoded, err := imaging.NewFrames(frames, delays, plays)
	return wrapImage(decoded), err
}

// Frames returns the number of frames: 1 for a still image.
func (i *Image) Frames() int { return i.core().Frames() }

// Duration returns the length of one run of an animated image, 0 for a
// still one.
func (i *Image) Duration() time.Duration { return i.core().Duration() }

func (i *Image) animated() bool { return i.core().Animated() }

const streamRetry = 8 * time.Millisecond

// Blurred returns i blurred by a Gaussian with a standard deviation of sigma
// pixels, extending the edge pixels past the border so the edges do not fade.
// The result is computed on the calling goroutine and weakly memoized with
// i, so blur a small image, such as one loaded scaled down to 64 pixels, and
// draw it stretched with FitFill. Animated images blur their first frame.
// An image an ImageCache loaded without a size limit is decoded at its
// natural size for this. Images without pixels in memory return nil.
// Results belong to the caller, outside ImageCache budgets. Use ImageCache.Blurred
// to bound the output size and run the operation on a cache worker.
func (i *Image) Blurred(sigma float32) *Image {
	if i == nil {
		return nil
	}
	return wrapCore(i.core().Blurred(sigma))
}

func init() { imagecache.Wake = wakeWindows }

// ImageCache decodes images on background goroutines and keeps the decoded
// pixels in memory up to a byte budget, evicting the least recently used.
type ImageCache struct {
	c *imagecache.Cache
}

// Images is the default cache, with a budget of two screens of pixels for
// the monitors attached when it is first used, between 64 and 256 MiB.
var Images = NewImageCache(0, 0)

// NewImageCache creates a cache holding up to maxBytes of decoded pixels and
// decoding with up to workers goroutines (0 picks a number from the CPU count).
// Up to twice as many loads read their sources at once, apart from decoding,
// and the loads requested last go first.
// A maxBytes of 0 or less sizes the budget as Images does.
// Each cache also limits overlapping decodes to an estimated 96 MiB of working
// memory. Larger images and registered decoders run alone; this is not a limit
// on process memory or on bytes returned by read.
// Results are also limited to 1,024 entries, including lazy images and blur
// variants, and failures to 256 entries. Evicted failures may be retried.
func NewImageCache(maxBytes int64, workers int) *ImageCache {
	return &ImageCache{imagecache.New(maxBytes, workers)}
}

// SetNativeDecoders sets whether the cache decodes still JPEG, PNG and AVIF
// images with the Windows Imaging Component, as it does by default. WIC and
// the codecs it loads, such as the HEIF Image Extensions, run in this
// process; a cache for images from untrusted sources can turn them off to
// use only the Go decoders. AVIF images then need a registered decoder.
func (c *ImageCache) SetNativeDecoders(on bool) { c.c.SetNativeDecoders(on) }

// Load returns the image identified by src, decoded from the bytes returned
// by read and scaled down to fit maxW×maxH. While the image is loading, or
// when it failed, Load returns nil; open windows update when it is ready. src
// identifies the image in the cache, together with the size.
//
// With maxW and maxH both 0, Load reads only the image's header and returns
// an Image of its natural size that ImageEl decodes at the size it is drawn,
// one decode per drawn size. A 0 for one of them leaves that axis unlimited.
func (c *ImageCache) Load(src string, maxW, maxH int, read func() ([]byte, error)) *Image {
	return wrapCore(c.c.Load(src, maxW, maxH, read))
}

// Sized returns src decoded for a w x h box of logical pixels that ImageEl
// draws with fit. Unlike Load it decodes at the display scale of the window
// being built (1 outside View) and, for FitCover and FitFill, to the smallest
// size that covers the box, so the image is not stretched up on screen. The
// size is rounded up to steps of about √2, so that resizing the box reloads
// the image only now and then; until a new size is ready, Sized returns the
// size of src that finished loading last, other than one loaded only as the
// source of Blurred. A zero w or h is unlimited; with
// both zero it returns an image decoded at the size it is drawn, as Load does.
func (c *ImageCache) Sized(src string, w, h float32, fit Fit, read func() ([]byte, error)) *Image {
	return wrapCore(c.c.Sized(src, sizedBound(w, h, fit), read))
}

// SizedErr returns the error of a failed Sized load with the same arguments.
func (c *ImageCache) SizedErr(src string, w, h float32, fit Fit) error {
	return c.c.SizedErr(src, sizedBound(w, h, fit))
}

func sizedBound(w, h float32, fit Fit) imaging.Bound {
	scale := float32(1)
	if win := CurrentWindow(); win != nil {
		scale = win.Scale()
	}
	return imaging.Bound{W: imagecache.SizeStep(w * scale), H: imagecache.SizeStep(h * scale), Cover: fit != FitContain}
}

// File loads an image file; see Load.
func (c *ImageCache) File(path string, maxW, maxH int) *Image {
	return wrapCore(c.c.File(path, maxW, maxH))
}

// Err returns the error of a failed load of src at this size.
func (c *ImageCache) Err(src string, maxW, maxH int) error { return c.c.Err(src, maxW, maxH) }

// Forget drops cached results and invalidates pending reads for src at every
// size, so that the next Load reads it again.
func (c *ImageCache) Forget(src string) { c.c.Forget(src) }

// Blurred loads src within maxW by maxH pixels and blurs its first frame on
// a cache worker. sigma is measured in output pixels. At least one size must
// be positive; zero leaves the other axis unlimited. Nonpositive sigma loads
// without blur. Pending and failed requests return nil; windows update when
// ready. The source decode and blurred result share the cache's pixel budget.
func (c *ImageCache) Blurred(src string, maxW, maxH int, sigma float32, read func() ([]byte, error)) *Image {
	return wrapCore(c.c.Blurred(src, maxW, maxH, sigma, read))
}

// BlurredErr reports an invalid or failed Blurred request with the same arguments.
func (c *ImageCache) BlurredErr(src string, maxW, maxH int, sigma float32) error {
	return c.c.BlurredErr(src, maxW, maxH, sigma)
}

// memoryPressureInterval is the least time between trims: the notification
// stays signaled for as long as memory is low.
const memoryPressureInterval = 30 * time.Second

// watchMemoryPressure trims image memory whenever Windows reports that
// physical memory is low.
func watchMemoryPressure() {
	memoryPressureOnce.Do(func() { win.WatchLowMemory(memoryPressureInterval, trimImageMemory) })
}

var memoryPressureOnce sync.Once

// trimImageMemory halves every image cache, releases the textures of images
// no window drew in its last frame and returns freed memory to the system.
func trimImageMemory() {
	imagecache.TrimAll()
	// Finish releasing textures before trimming, without rebuilding views.
	doQuiet(func() {
		liveMu.Lock()
		defer liveMu.Unlock()
		for w := range live {
			w.releaseIdleTextures()
		}
	})
	debug.FreeOSMemory()
	if err := TrimDevice(); err != nil {
		log.Printf("fairy: trim graphics device: %v", err)
	}
}

// releaseIdleTextures releases the textures not drawn in the last painted
// frame. Run outside painting, where w.frame is the next frame.
func (w *Window) releaseIdleTextures() {
	w.sweepTextures(w.frame-1, 0)
}
