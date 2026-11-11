//go:build windows

package dwrite

import (
	"math"
	"sync"
	"syscall"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/com"
)

// GlyphRunSink receives the glyph runs of an IDWriteTextLayout.Draw call.
// x and y are the run's baseline origin; effect is the value set with SetDrawingEffect.
type GlyphRunSink func(x, y float32, run *GlyphRun, desc *GlyphRunDescription, effect unsafe.Pointer)

// DecorationSink receives underlines and strikethroughs: a line of width
// starting at (x, y+offset), thickness tall, where y is the baseline.
type DecorationSink func(x, y, width, thickness, offset float32, effect unsafe.Pointer)

var (
	iidUnknown       = com.MustGUID("00000000-0000-0000-c000-000000000046")
	iidPixelSnapping = com.MustGUID("eaf3a2da-ecf4-4d24-b644-b34f6842024b")
	iidTextRenderer  = com.MustGUID("ef8a8135-5cc6-45fe-8825-c5a0724eb819")
)

// textRenderer is a static IDWriteTextRenderer implementation. DirectWrite calls it
// synchronously from Draw on the calling thread, so the active sink is a package
// variable, owned by the DrawLayout call that holds drawMu.
type textRenderer struct {
	vtbl *[10]uintptr
}

var (
	rendererVtbl [10]uintptr
	rendererObj  textRenderer
	drawMu       sync.Mutex
	activeSink   GlyphRunSink
	activeDeco   DecorationSink

	// glyphRunTarget, decorationTarget and inlineObjectTarget are the Go callbacks the assembly stubs jump to.
	glyphRunTarget     uintptr
	decorationTarget   uintptr
	inlineObjectTarget uintptr
)

// glyphRunStub is the DrawGlyphRun vtable entry. Go callbacks cannot receive
// float arguments in XMM registers, so the stub copies the baseline origin from
// XMM2/XMM3 into R8/R9 before jumping to glyphRunTarget.
func glyphRunStub()

func glyphRunStubAddr() uintptr

func decorationStub()

func decorationStubAddr() uintptr

func inlineObjectStub()

func inlineObjectStubAddr() uintptr

func init() {
	rendererVtbl[0] = syscall.NewCallback(trQueryInterface)
	rendererVtbl[1] = syscall.NewCallback(trAddRef)
	rendererVtbl[2] = syscall.NewCallback(trRelease)
	rendererVtbl[3] = syscall.NewCallback(trIsPixelSnappingDisabled)
	rendererVtbl[4] = syscall.NewCallback(trGetCurrentTransform)
	rendererVtbl[5] = syscall.NewCallback(trGetPixelsPerDip)
	glyphRunTarget = syscall.NewCallback(trDrawGlyphRun)
	rendererVtbl[6] = glyphRunStubAddr()
	decorationTarget = syscall.NewCallback(trDrawDecoration)
	rendererVtbl[7] = decorationStubAddr()
	rendererVtbl[8] = decorationStubAddr()
	inlineObjectTarget = syscall.NewCallback(trDrawInlineObject)
	rendererVtbl[9] = inlineObjectStubAddr()
	rendererObj.vtbl = &rendererVtbl
}

// DrawLayout runs layout.Draw and forwards each glyph run to sink and each
// underline or strikethrough to deco (which may be nil). It may be called from
// any goroutine; calls are serialized, so sink and deco must not call it.
func DrawLayout(l TextLayout, x, y float32, sink GlyphRunSink, deco DecorationSink) error {
	drawMu.Lock()
	defer drawMu.Unlock()
	activeSink, activeDeco = sink, deco
	defer func() { activeSink, activeDeco = nil, nil }()
	return l.Draw(unsafe.Pointer(&rendererObj), x, y)
}

const (
	sOK          = 0
	eNoInterface = 0x80004002
)

func trQueryInterface(this, riid, out uintptr) uintptr {
	iid := *(*com.GUID)(unsafe.Pointer(riid))
	if iid == iidUnknown || iid == iidPixelSnapping || iid == iidTextRenderer {
		*(*uintptr)(unsafe.Pointer(out)) = this
		return sOK
	}
	*(*uintptr)(unsafe.Pointer(out)) = 0
	return eNoInterface
}

func trAddRef(this uintptr) uintptr  { return 1 }
func trRelease(this uintptr) uintptr { return 1 }

func trIsPixelSnappingDisabled(this, ctx, out uintptr) uintptr {
	*(*int32)(unsafe.Pointer(out)) = 0
	return sOK
}

func trGetCurrentTransform(this, ctx, out uintptr) uintptr {
	m := (*[6]float32)(unsafe.Pointer(out))
	*m = [6]float32{1, 0, 0, 1, 0, 0}
	return sOK
}

func trGetPixelsPerDip(this, ctx, out uintptr) uintptr {
	*(*float32)(unsafe.Pointer(out)) = 1
	return sOK
}

func trDrawGlyphRun(this, ctx, x, y, mode, run, desc, effect uintptr) uintptr {
	if activeSink != nil {
		activeSink(math.Float32frombits(uint32(x)), math.Float32frombits(uint32(y)),
			(*GlyphRun)(unsafe.Pointer(run)), (*GlyphRunDescription)(unsafe.Pointer(desc)), unsafe.Pointer(effect))
	}
	return sOK
}

func trDrawDecoration(this, ctx, x, y, deco, effect uintptr) uintptr {
	if activeDeco != nil && deco != 0 {
		// DWRITE_UNDERLINE and DWRITE_STRIKETHROUGH start with width, thickness, offset.
		d := (*[3]float32)(unsafe.Pointer(deco))
		activeDeco(math.Float32frombits(uint32(x)), math.Float32frombits(uint32(y)), d[0], d[1], d[2], unsafe.Pointer(effect))
	}
	return sOK
}

// trDrawInlineObject draws inline objects such as the ellipsis trimming sign by
// calling IDWriteInlineObject::Draw, which renders through this renderer again.
func trDrawInlineObject(this, ctx, x, y, obj, sideways, rtl, effect uintptr) uintptr {
	if obj != 0 {
		com.Call(com.Ptr(obj), 3, ctx, this, uintptr(uint32(x)), uintptr(uint32(y)), sideways, rtl, effect)
	}
	return sOK
}
