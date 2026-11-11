//go:build windows

package dwrite

import (
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/myparsleycat/fairy/internal/com"
)

// effectChunk is the number of drawing effects allocated at a time. Drawing
// effects are minimal static COM objects used as SetDrawingEffect values.
// DirectWrite only AddRefs and Releases them and hands them back to
// DrawGlyphRun, where EffectIndex recovers which one tagged the run. They are
// allocated outside the Go heap in chunks that are never freed: layouts keep
// pointers to them that the garbage collector cannot see, and checkptr rejects
// heap pointers that come back from DirectWrite as uintptr.
const effectChunk = 256

type effectObj struct {
	vtbl  *[3]uintptr
	index uintptr
}

var (
	effectVtbl   [3]uintptr
	effectMu     sync.Mutex
	effectChunks []*[effectChunk]effectObj
)

func init() {
	effectVtbl = [3]uintptr{
		syscall.NewCallback(func(this, riid, out uintptr) uintptr {
			*(*uintptr)(unsafe.Pointer(out)) = this
			return 0
		}),
		syscall.NewCallback(func(this uintptr) uintptr { return 1 }),
		syscall.NewCallback(func(this uintptr) uintptr { return 1 }),
	}
}

// Effect returns the drawing effect object for index i >= 0.
func Effect(i int) unsafe.Pointer {
	effectMu.Lock()
	defer effectMu.Unlock()
	for len(effectChunks) <= i/effectChunk {
		p, err := windows.VirtualAlloc(0, unsafe.Sizeof([effectChunk]effectObj{}), windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE)
		if err != nil {
			panic("dwrite: VirtualAlloc: " + err.Error())
		}
		c := (*[effectChunk]effectObj)(com.Ptr(p))
		base := len(effectChunks) * effectChunk
		for j := range c {
			c[j] = effectObj{vtbl: &effectVtbl, index: uintptr(base + j)}
		}
		effectChunks = append(effectChunks, c)
	}
	return unsafe.Pointer(&effectChunks[i/effectChunk][i%effectChunk])
}

// EffectIndex returns the index of an effect from Effect, or -1 for nil or
// another COM object.
func EffectIndex(p unsafe.Pointer) int {
	if p == nil {
		return -1
	}
	// Every COM object starts with its vtable pointer, so this read is safe
	// for any effect DirectWrite hands back.
	e := (*effectObj)(p)
	if e.vtbl != &effectVtbl {
		return -1
	}
	return int(e.index)
}

func (l TextLayout) SetStrikethrough(on bool, r TextRange) { com.HR(l.P, 37, b2u(on), r.arg()) }
