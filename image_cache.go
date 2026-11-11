package fairy

import (
	"container/list"
	"errors"
	"fmt"
	"image"
	"maps"
	"math"
	"os"
	"runtime"
	"slices"
	"sync"
	"time"
	"weak"

	"github.com/myparsleycat/fairy/internal/imaging"
)

// ImageCache decodes images on background goroutines and keeps the decoded
// pixels in memory up to a byte budget, evicting the least recently used.
type ImageCache struct {
	mu       sync.Mutex
	maxBytes int64
	auto     bool // maxBytes is set from the monitors on first use
	bytes    int64
	entries  map[imageKey]*list.Element
	lru      list.List // of *imageEntry, most recently used first
	evicted  map[imageKey]evictedImage
	loading  map[imageKey]*imageLoad
	reusable map[string]map[*imageEntry]struct{}
	seq      uint64 // numbers loads and direct requests, in order
	failed   map[imageKey]error
	failLRU  list.List
	failKeys map[imageKey]*list.Element
	latest   map[string]imageKey // the size of each src that finished loading last
	reads    slotQueue           // loads reading their source
	decodes  slotQueue           // loads decoding or resampling
	work     int64
	waiting  map[*imageLoad]*imageWorkWait
	wic      func(imaging.Input, imaging.Bound) *imaging.Bitmap
}

var errImageLoadInvalidated = errors.New("fairy: image load invalidated")

// An imageLoad publishes its result once, before closing done. Captures can
// hold that result even if the cache evicts it before they draw.
type imageLoad struct {
	id   uint64
	key  imageKey
	done chan struct{}
	img  *Image
	err  error

	// req and reqAt are when the load was last requested (c.mu): windows
	// request what they show each time they paint.
	req        uint64
	reqAt      time.Time
	queuedAt   time.Time
	started    bool
	dependency *imageLoad
	dependents map[*imageLoad]struct{}
	// direct is false while the load was asked for only as the source of a blur (c.mu).
	direct bool

	mu       sync.Mutex
	watchers map[chan<- *imageLoad]struct{}
}

func (p *imageLoad) finish(img *Image, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.img, p.err = img, err
	close(p.done)
	for ch := range p.watchers {
		ch <- p
	}
	p.watchers = nil
}

// watch reports this load once. The caller reserves one channel slot per
// distinct load so publishing never blocks a cache worker.
func (p *imageLoad) watch(ch chan<- *imageLoad) {
	p.mu.Lock()
	defer p.mu.Unlock()
	select {
	case <-p.done:
		ch <- p
	default:
		if p.watchers == nil {
			p.watchers = make(map[chan<- *imageLoad]struct{})
		}
		p.watchers[ch] = struct{}{}
	}
}

func (p *imageLoad) unwatch(ch chan<- *imageLoad) {
	p.mu.Lock()
	delete(p.watchers, ch)
	p.mu.Unlock()
}

func (p *imageLoad) watched() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.watchers) > 0
}

// imageLoadExpiry is how long a load waits for a slot without being
// requested again before it is dropped. Windows request the images they
// show whenever they paint, and every finished load makes them paint, so
// a load nobody asked for in that time is for an image no longer shown.
var imageLoadExpiry = 2 * time.Second

// slotQueue hands a limited number of slots to the waiting loads that were
// requested last, so that images on screen go before those scrolled past.
type slotQueue struct {
	limit, used int
	waiting     map[*imageLoad]chan struct{}
}

func newSlotQueue(limit int) slotQueue {
	return slotQueue{limit: limit, waiting: map[*imageLoad]chan struct{}{}}
}

// acquire waits for a slot of q, and reports false without one when the
// load was dropped while waiting.
func (c *ImageCache) acquire(q *slotQueue, p *imageLoad) bool {
	c.mu.Lock()
	if c.loading[p.key] != p {
		c.mu.Unlock()
		return false
	}
	granted := make(chan struct{}, 1)
	p.queuedAt = time.Now()
	q.waiting[p] = granted
	c.dispatch(q)
	c.mu.Unlock()
	got := false
	select {
	case <-granted:
		got = true
	case <-p.done:
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(q.waiting, p)
	if !got {
		select {
		case <-granted:
			got = true
		default:
		}
	}
	if got {
		if c.loading[p.key] == p {
			return true
		}
		q.used--
		c.dispatch(q)
	}
	return false
}

func (c *ImageCache) releaseSlot(q *slotQueue) {
	c.mu.Lock()
	q.used--
	c.dispatch(q)
	c.mu.Unlock()
}

// dispatch hands free slots of q to the loads requested last, first
// dropping loads nobody has requested for imageLoadExpiry. Loads a capture
// waits for are kept. c.mu is held.
func (c *ImageCache) dispatch(q *slotQueue) {
	if q.used >= q.limit || len(q.waiting) == 0 {
		return
	}
	now := time.Now()
	dropped := false
	for p := range q.waiting {
		if c.loading[p.key] != p {
			delete(q.waiting, p)
		} else if c.loadExpired(p, now) {
			delete(q.waiting, p)
			c.invalidateLoad(p)
			dropped = true
		}
	}
	if dropped {
		// Elements still showing a dropped image request it again.
		go wakeWindows()
	}
	for q.used < q.limit && len(q.waiting) > 0 {
		var best *imageLoad
		for p := range q.waiting {
			if best == nil || p.req > best.req {
				best = p
			}
		}
		q.waiting[best] <- struct{}{}
		delete(q.waiting, best)
		q.used++
	}
}

type imageKey struct {
	src string
	imaging.Bound
	sigma float32
}

// lazy reports a load without a size limit, which yields a lazy Image.
func (k imageKey) lazy() bool { return k.W == 0 && k.H == 0 && k.sigma == 0 }

type imageEntry struct {
	key        imageKey
	img        *Image
	start      uint64      // seq when its load started
	hit        uint64      // seq of the last request for exactly this key
	sourceSize image.Point // nonzero for directly decoded, reusable stills
	// direct is false for a blur's source that nothing else asked for. Sized
	// does not stand it in for another size, as it is often far smaller.
	direct bool
}

// evictedImage remembers an evicted entry while something else may still
// hold its image, as Glide keeps active resources apart from its memory cache.
type evictedImage struct {
	img        weak.Pointer[Image]
	sourceSize image.Point
	direct     bool
}

// Images is the default cache, with a budget of two screens of pixels for
// the monitors attached when it is first used, between 64 and 256 MiB.
var Images = NewImageCache(0, 0)

const (
	minAutoImageBudget = 64 << 20
	maxAutoImageBudget = 256 << 20
	maxImageEntries    = 1024
	maxImageFailures   = 256
)

// maxAnimationKeepBytes caps the decoded frames one animation keeps, as
// Firefox's image.animated.decode-on-demand.threshold-kb does.
const maxAnimationKeepBytes = 20 << 20

// animationKeepBytes is how much of the budget the frames of one animation may
// take before it decodes them as it plays instead. Allowing the whole budget
// let one animation evict every other image on screen.
func animationKeepBytes(budget int64) int64 {
	return min(budget/4, maxAnimationKeepBytes)
}

// autoImageBudget is two screens of RGBA pixels, as Android image loaders
// size their memory caches.
func autoImageBudget(screenPixels int64) int64 {
	if screenPixels <= 0 {
		return maxAutoImageBudget
	}
	return min(max(2*screenPixels*4, minAutoImageBudget), maxAutoImageBudget)
}

// imagePruneDelay is how long after a new size of an image is stored the
// other sizes nobody asked for since are dropped. Loads wake every window,
// so by then each window has asked again for the sizes it still shows.
var imagePruneDelay = 500 * time.Millisecond

var (
	cachesMu sync.Mutex
	caches   []weak.Pointer[ImageCache]
)

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
	if workers <= 0 {
		workers = max(2, runtime.NumCPU()/2)
	}
	c := &ImageCache{
		maxBytes: maxBytes,
		auto:     maxBytes <= 0,
		entries:  map[imageKey]*list.Element{},
		evicted:  map[imageKey]evictedImage{},
		loading:  map[imageKey]*imageLoad{},
		reusable: map[string]map[*imageEntry]struct{}{},
		failed:   map[imageKey]error{},
		failKeys: map[imageKey]*list.Element{},
		latest:   map[string]imageKey{},
		reads:    newSlotQueue(2 * workers),
		decodes:  slotQueue{limit: workers},
		wic:      imaging.DecodeWIC,
		waiting:  map[*imageLoad]*imageWorkWait{},
	}
	cachesMu.Lock()
	caches = append(caches, weak.Make(c))
	cachesMu.Unlock()
	return c
}

// SetNativeDecoders sets whether the cache decodes still JPEG, PNG and AVIF
// images with the Windows Imaging Component, as it does by default. WIC and
// the codecs it loads, such as the HEIF Image Extensions, run in this
// process; a cache for images from untrusted sources can turn them off to
// use only the Go decoders. AVIF images then need a registered decoder.
func (c *ImageCache) SetNativeDecoders(on bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.wic = nil
	if on {
		c.wic = imaging.DecodeWIC
	}
}

// resolveBudget sets an automatic budget once monitors are known, which they
// are with the right DPI awareness only after the app has started.
func (c *ImageCache) resolveBudget() {
	if c.auto {
		c.auto = false
		c.maxBytes = autoImageBudget(screenPixels())
	}
}

// Load returns the image identified by src, decoded from the bytes returned
// by read and scaled down to fit maxW×maxH. While the image is loading, or
// when it failed, Load returns nil; open windows update when it is ready. src
// identifies the image in the cache, together with the size.
//
// With maxW and maxH both 0, Load reads only the image's header and returns
// an Image of its natural size that ImageEl decodes at the size it is drawn,
// one decode per drawn size. A 0 for one of them leaves that axis unlimited.
func (c *ImageCache) Load(src string, maxW, maxH int, read func() ([]byte, error)) *Image {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.load(imageKey{src: src, Bound: imaging.Bound{W: maxW, H: maxH}}, read)
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
	c.mu.Lock()
	defer c.mu.Unlock()
	if img := c.load(imageKey{src: src, Bound: sizedBound(w, h, fit)}, read); img != nil {
		return img
	}
	if k, ok := c.latest[src]; ok {
		el := c.entries[k]
		c.lru.MoveToFront(el)
		return el.Value.(*imageEntry).img
	}
	return nil
}

// SizedErr returns the error of a failed Sized load with the same arguments.
func (c *ImageCache) SizedErr(src string, w, h float32, fit Fit) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	k := imageKey{src: src, Bound: sizedBound(w, h, fit)}
	c.touchFailure(k)
	return c.failed[k]
}

func sizedBound(w, h float32, fit Fit) imaging.Bound {
	scale := float32(1)
	if win := CurrentWindow(); win != nil {
		scale = win.Scale()
	}
	return imaging.Bound{W: sizeStep(w * scale), H: sizeStep(h * scale), Cover: fit != FitContain}
}

// sizeStep rounds n up to the nearest power of √2, or 0 for no limit.
func sizeStep(n float32) int {
	if !(n > 0) {
		return 0
	}
	return int(math.Ceil(math.Pow(2, math.Ceil(2*math.Log2(float64(n)))/2)))
}

func (c *ImageCache) load(k imageKey, read func() ([]byte, error)) *Image {
	img, _, _ := c.request(k, read)
	return img
}

// request returns the cached image, its pending load, or its error. c.mu is held.
func (c *ImageCache) request(k imageKey, read func() ([]byte, error)) (*Image, *imageLoad, error) {
	return c.requestAs(k, read, true)
}

// requestAs is request; direct is false when only a blur asks for k as its source.
func (c *ImageCache) requestAs(k imageKey, read func() ([]byte, error), direct bool) (*Image, *imageLoad, error) {
	c.resolveBudget()
	c.seq++
	if el, ok := c.entries[k]; ok {
		c.lru.MoveToFront(el)
		e := el.Value.(*imageEntry)
		e.hit = c.seq
		if direct && !e.direct {
			e.direct = true
			if _, ok := c.latest[k.src]; !ok && k.sigma == 0 {
				c.latest[k.src] = k
			}
		}
		return e.img, nil, nil
	}
	if img := c.revive(k, direct); img != nil {
		return img, nil, nil
	}
	if p := c.loading[k]; p != nil {
		for current := p; current != nil; current = current.dependency {
			current.req, current.reqAt = c.seq, time.Now()
		}
		p.direct = p.direct || direct
		return nil, p, nil
	}
	if err := c.failed[k]; err != nil {
		c.touchFailure(k)
		return nil, nil, err
	}
	p := &imageLoad{id: c.seq, key: k, done: make(chan struct{}), req: c.seq, reqAt: time.Now(), direct: direct}
	c.loading[k] = p
	go c.decode(k, p, read)
	return nil, p, nil
}

// File loads an image file; see Load.
func (c *ImageCache) File(path string, maxW, maxH int) *Image {
	return c.Load("file:"+path, maxW, maxH, func() ([]byte, error) { return os.ReadFile(path) })
}

// Err returns the error of a failed load of src at this size.
func (c *ImageCache) Err(src string, maxW, maxH int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	k := imageKey{src: src, Bound: imaging.Bound{W: maxW, H: maxH}}
	c.touchFailure(k)
	return c.failed[k]
}

// Forget drops cached results and invalidates pending reads for src at every
// size, so that the next Load reads it again.
func (c *ImageCache) Forget(src string) {
	c.mu.Lock()
	removed := false
	for k, el := range c.entries {
		if k.src == src {
			c.remove(el)
			removed = true
		}
	}
	for k := range c.evicted {
		if k.src == src {
			delete(c.evicted, k)
		}
	}
	for k := range c.failed {
		if k.src == src {
			c.removeFailure(k)
		}
	}
	for k, p := range c.loading {
		if k.src == src {
			c.invalidateLoad(p)
		}
	}
	c.dispatchWork()
	c.mu.Unlock()
	if removed {
		wakeWindows()
	}
}

func (c *ImageCache) remove(el *list.Element) {
	e := c.lru.Remove(el).(*imageEntry)
	delete(c.entries, e.key)
	if e.sourceSize != (image.Point{}) {
		stills := c.reusable[e.key.src]
		delete(stills, e)
		if len(stills) == 0 {
			delete(c.reusable, e.key.src)
		}
	}
	if c.latest[e.key.src] == e.key {
		delete(c.latest, e.key.src)
		for cur := c.lru.Front(); cur != nil; cur = cur.Next() {
			other := cur.Value.(*imageEntry)
			if other.key.src == e.key.src && other.key.sigma == 0 && other.direct {
				c.latest[e.key.src] = other.key
				break
			}
		}
	}
	c.bytes -= e.img.data.ByteSize()
	e.img.retired.Store(true)
	c.evicted[e.key] = evictedImage{img: weak.Make(e.img), sourceSize: e.sourceSize, direct: e.direct}
	if len(c.evicted) > maxImageEntries {
		maps.DeleteFunc(c.evicted, func(_ imageKey, ev evictedImage) bool { return ev.img.Value() == nil })
		for k := range c.evicted {
			if len(c.evicted) <= maxImageEntries {
				break
			}
			delete(c.evicted, k)
		}
	}
}

// evictTo drops the least recently used decodes until the cache holds at
// most limit bytes. The separate entry limit also bounds zero-byte metadata.
func (c *ImageCache) evictTo(limit int64) bool {
	removed := false
	for el := c.lru.Back(); el != nil && c.bytes > limit; {
		prev := el.Prev()
		if el.Value.(*imageEntry).img.data.ByteSize() > 0 {
			c.remove(el)
			removed = true
		}
		el = prev
	}
	return removed
}

// trim halves the cache, for when the system runs low on memory.
func (c *ImageCache) trim() {
	c.mu.Lock()
	c.resolveBudget()
	removed := c.evictTo(c.maxBytes / 2)
	for target := c.lru.Len() / 2; c.lru.Len() > target; {
		c.remove(c.lru.Back())
		removed = true
	}
	for target := len(c.failed) / 2; len(c.failed) > target; {
		c.removeFailure(c.failLRU.Back().Value.(imageKey))
	}
	c.mu.Unlock()
	if removed {
		wakeWindows()
	}
}

func trimImageCaches() {
	cachesMu.Lock()
	live := caches[:0]
	for _, p := range caches {
		if p.Value() != nil {
			live = append(live, p)
		}
	}
	clear(caches[len(live):])
	caches = live
	snapshot := slices.Clone(live)
	cachesMu.Unlock()
	for _, p := range snapshot {
		if c := p.Value(); c != nil {
			c.trim()
		}
	}
}

// pruneSizes drops the other decoded sizes of k.src that nothing has asked
// for since k started loading.
func (c *ImageCache) pruneSizes(k imageKey) {
	c.mu.Lock()
	removed := false
	if el, ok := c.entries[k]; ok {
		since := el.Value.(*imageEntry).start
		for k2, el2 := range c.entries {
			if k2.src == k.src && k2.sigma == k.sigma && k2 != k && !k2.lazy() && el2.Value.(*imageEntry).hit < since {
				c.remove(el2)
				removed = true
			}
		}
	}
	c.mu.Unlock()
	if removed {
		wakeWindows()
	}
}

func (c *ImageCache) decode(k imageKey, p *imageLoad, read func() ([]byte, error)) {
	img, sourceSize, err := c.decodeFit(k, p, read)

	c.mu.Lock()
	if c.loading[k] != p {
		c.mu.Unlock()
		return
	}
	delete(c.loading, k)
	var size int64
	decodeFailed := err != nil
	if err == nil {
		size = img.data.ByteSize()
		if size > c.maxBytes {
			err = fmt.Errorf("fairy: decoded image size %d bytes exceeds cache budget of %d bytes", size, c.maxBytes)
		}
	}
	if err != nil {
		c.recordFailure(k, err)
		if decodeFailed && !k.lazy() && k.sigma == 0 {
			c.failLazy(k.src, err)
		}
	} else {
		c.insert(&imageEntry{key: k, img: img, start: p.id, hit: p.id, sourceSize: sourceSize, direct: p.direct})
		if !k.lazy() {
			time.AfterFunc(imagePruneDelay, func() { c.pruneSizes(k) })
		}
	}
	if err != nil {
		img = nil
	}
	p.finish(img, err)
	c.mu.Unlock()
	wakeWindows()
}

// insert makes room for e within the budget and adds it as the most recently
// used entry. c.mu is held.
func (c *ImageCache) insert(e *imageEntry) {
	size := e.img.data.ByteSize()
	c.evictTo(c.maxBytes - size)
	for c.lru.Len() >= maxImageEntries {
		c.remove(c.lru.Back())
	}
	delete(c.evicted, e.key)
	c.entries[e.key] = c.lru.PushFront(e)
	if e.sourceSize != (image.Point{}) {
		stills := c.reusable[e.key.src]
		if stills == nil {
			stills = make(map[*imageEntry]struct{})
			c.reusable[e.key.src] = stills
		}
		stills[e] = struct{}{}
	}
	if e.key.sigma == 0 && e.direct {
		c.latest[e.key.src] = e.key
	}
	c.bytes += size
}

// revive puts back an evicted image that is still referenced, so that an image
// on screen is not decoded again when the images shown together do not fit
// the budget. Evicting it freed nothing while the UI holds it. c.mu is held.
func (c *ImageCache) revive(k imageKey, direct bool) *Image {
	ev, ok := c.evicted[k]
	if !ok {
		return nil
	}
	img := ev.img.Value()
	if img == nil {
		delete(c.evicted, k)
		return nil
	}
	img.retired.Store(false)
	c.insert(&imageEntry{key: k, img: img, start: c.seq, hit: c.seq, sourceSize: ev.sourceSize, direct: ev.direct || direct})
	return img
}

// failLazy records a failed decode of src on its lazy images too, so that
// Load reports the error instead of an image that never draws.
func (c *ImageCache) failLazy(src string, err error) {
	for _, cover := range []bool{false, true} {
		k := imageKey{src: src, Bound: imaging.Bound{Cover: cover}}
		if el, ok := c.entries[k]; ok {
			c.remove(el)
			delete(c.evicted, k)
			c.recordFailure(k, err)
		}
	}
}

func (c *ImageCache) decodeFit(k imageKey, p *imageLoad, read func() ([]byte, error)) (img *Image, sourceSize image.Point, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("fairy: decoding image: %v", r)
		}
	}()
	if k.sigma > 0 {
		img, err = c.decodeBlur(k, p, read)
		return img, image.Point{}, err
	}
	c.mu.Lock()
	active := c.loading[k] == p
	var candidate *Image
	var dw, dh int
	if active && !k.lazy() {
		candidate, dw, dh = c.reuseCandidate(k)
	}
	wic := c.wic
	c.mu.Unlock()
	if !active {
		return nil, image.Point{}, nil
	}
	run := func(weight int64, f func()) bool { return c.withWork(k, p, weight, f) }
	if candidate != nil {
		weight := min(imaging.WorkBudget, int64(dw)*int64(dh)*4+imaging.ResampleWork(candidate.data.W, candidate.data.H, dw, dh))
		run(weight, func() {
			src := image.RGBA{Pix: candidate.data.Pixels, Stride: candidate.data.W * 4, Rect: image.Rect(0, 0, candidate.data.W, candidate.data.H)}
			img = wrapImage(imaging.ResizeTo(&src, dw, dh))
		})
		return img, image.Point{}, nil
	}
	// The source is read in a slot of its own, so that slow reads do not
	// hold up decoding, and kept until a decode slot is free.
	if !c.acquire(&c.reads, p) {
		return nil, image.Point{}, nil
	}
	reading := true
	defer func() {
		if reading {
			c.releaseSlot(&c.reads)
		}
	}()
	data, err := read()
	if err != nil {
		return nil, image.Point{}, err
	}
	in, err := imaging.Inspect(data)
	if err != nil {
		return nil, image.Point{}, err
	}
	if k.lazy() {
		img, err = newLazyImage(c, k.src, read, in)
		return img, image.Point{}, err
	}
	run = func(weight int64, f func()) bool {
		return c.withWork(k, p, weight, func() {
			if reading {
				c.releaseSlot(&c.reads)
				reading = false
			}
			f()
		})
	}
	decoded, usedWIC, err := in.DecodePixels(k.Bound, animationKeepBytes(c.maxBytes), c.maxBytes, false, wic, run)
	img = wrapImage(decoded)
	if img != nil && len(img.data.FrameData) == 0 && in.Animation.Count == 0 && !in.AnimatedWebP && in.AnimErr == nil {
		switch in.Format {
		case "jpeg", "png", "webp":
			sourceSize = image.Pt(in.Config.Width, in.Config.Height)
		case "avif":
			if usedWIC {
				sourceSize = image.Pt(in.Config.Width, in.Config.Height)
			}
		}
	}
	return img, sourceSize, err
}

func (c *ImageCache) reuseCandidate(k imageKey) (candidate *Image, dw, dh int) {
	var best *imageEntry
	for e := range c.reusable[k.src] {
		w, h := k.Bound.Size(e.sourceSize.X, e.sourceSize.Y)
		if e.img.data.W < w || e.img.data.H < h || e.img.data.W == w && e.img.data.H == h {
			continue
		}
		size := e.img.data.ByteSize()
		if best == nil || size < best.img.data.ByteSize() || size == best.img.data.ByteSize() && e.start > best.start {
			best, dw, dh = e, w, h
		}
	}
	if best != nil {
		return best.img, dw, dh
	}
	return nil, 0, 0
}

// withWork runs decode holding weight of the working-memory budget, and
// reports false without running it when the load was dropped while waiting.
func (c *ImageCache) withWork(k imageKey, p *imageLoad, weight int64, decode func()) bool {
	if !c.reserveWork(k, p, weight) {
		return false
	}
	defer c.releaseWork(weight)
	decode()
	return true
}

func (c *ImageCache) reserveWork(k imageKey, p *imageLoad, weight int64) bool {
	c.mu.Lock()
	if c.loading[k] != p {
		c.mu.Unlock()
		return false
	}
	wait := &imageWorkWait{weight: weight, ready: make(chan struct{})}
	p.queuedAt = time.Now()
	c.waiting[p] = wait
	c.dispatchWork()
	c.mu.Unlock()
	select {
	case <-wait.ready:
	case <-p.done:
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.waiting, p)
	if wait.granted && c.loading[k] == p {
		return true
	}
	if wait.granted {
		c.work -= weight
		c.decodes.used--
	}
	c.dispatchWork()
	return false
}

func (c *ImageCache) releaseWork(weight int64) {
	c.mu.Lock()
	c.work -= weight
	c.decodes.used--
	c.dispatchWork()
	c.mu.Unlock()
}

type imageWorkWait struct {
	weight  int64
	ready   chan struct{}
	granted bool
}

func (c *ImageCache) dispatchWork() {
	now := time.Now()
	dropped := false
	for p := range c.waiting {
		if c.loading[p.key] != p {
			delete(c.waiting, p)
		} else if c.loadExpired(p, now) {
			delete(c.waiting, p)
			c.invalidateLoad(p)
			dropped = true
		}
	}
	if dropped {
		go wakeWindows()
	}
	for c.decodes.used < c.decodes.limit && len(c.waiting) > 0 {
		var best *imageLoad
		for p := range c.waiting {
			if best == nil || p.req > best.req {
				best = p
			}
		}
		wait := c.waiting[best]
		if wait.weight > imaging.WorkBudget-c.work {
			return
		}
		delete(c.waiting, best)
		c.work += wait.weight
		c.decodes.used++
		best.started, wait.granted = true, true
		close(wait.ready)
	}
}

func (c *ImageCache) loadExpired(p *imageLoad, now time.Time) bool {
	if p.started || now.Sub(p.reqAt) <= imageLoadExpiry || now.Sub(p.queuedAt) <= imageLoadExpiry || p.watched() {
		return false
	}
	for dependent := range p.dependents {
		if c.loading[dependent.key] == dependent && !c.loadExpired(dependent, now) {
			return false
		}
	}
	return true
}

func (c *ImageCache) invalidateLoad(p *imageLoad) {
	if c.loading[p.key] != p {
		return
	}
	delete(c.loading, p.key)
	delete(c.reads.waiting, p)
	delete(c.waiting, p)
	if p.dependency != nil {
		delete(p.dependency.dependents, p)
		p.dependency = nil
	}
	for dependent := range p.dependents {
		c.invalidateLoad(dependent)
	}
	p.finish(nil, errImageLoadInvalidated)
}

func (c *ImageCache) touchFailure(k imageKey) {
	if el := c.failKeys[k]; el != nil {
		c.failLRU.MoveToFront(el)
	}
}

func (c *ImageCache) removeFailure(k imageKey) {
	delete(c.failed, k)
	if el := c.failKeys[k]; el != nil {
		c.failLRU.Remove(el)
		delete(c.failKeys, k)
	}
}

func (c *ImageCache) recordFailure(k imageKey, err error) {
	c.failed[k] = err
	if el := c.failKeys[k]; el != nil {
		c.failLRU.MoveToFront(el)
		return
	}
	c.failKeys[k] = c.failLRU.PushFront(k)
	for len(c.failed) > maxImageFailures {
		c.removeFailure(c.failLRU.Back().Value.(imageKey))
	}
}
