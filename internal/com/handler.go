//go:build windows

package com

import (
	"slices"
	"sync"
	"syscall"
	"unsafe"
)

// handler is a COM object implemented in Go with one method after IUnknown,
// the shape of WebView2 completion and event handlers and of
// IMFMediaEngineNotify.
type handler struct {
	vtbl *[4]uintptr
	refs int32
	iid  GUID
	fn   func(a, b, c uintptr) uintptr
}

var (
	handlerVtbl  [4]uintptr
	handlerOnce  sync.Once
	handlersMu   sync.Mutex
	handlersLive = map[*handler]struct{}{} // foreign code holds these pointers
	iidUnknown   = MustGUID("00000000-0000-0000-c000-000000000046")
	iidAgile     = MustGUID("94ea2b94-e9cc-49e0-c0ff-ee64ca8f5b90")
)

// NewHandler returns a COM object that implements the interface iid by
// calling fn with the method's arguments; unused arguments hold garbage. A
// zero iid accepts any interface except the marshaling ones. The
// object starts with one reference owned by the caller. fn may run on any
// thread the caller of the interface uses.
func NewHandler(iid GUID, fn func(a, b, c uintptr) uintptr) unsafe.Pointer {
	handlerOnce.Do(func() {
		handlerVtbl = [4]uintptr{
			syscall.NewCallback(handlerQueryInterface),
			syscall.NewCallback(handlerAddRef),
			syscall.NewCallback(handlerRelease),
			syscall.NewCallback(handlerInvoke),
		}
	})
	h := &handler{vtbl: &handlerVtbl, refs: 1, iid: iid, fn: fn}
	handlersMu.Lock()
	handlersLive[h] = struct{}{}
	handlersMu.Unlock()
	return unsafe.Pointer(h)
}

// marshalIIDs are interfaces COM probes to decide how to marshal an object.
// A handler created with a zero IID answers every other interface with
// itself, as WebView2 handlers are only ever called through their one method.
var marshalIIDs = []GUID{
	MustGUID("00000003-0000-0000-c000-000000000046"), // IMarshal
	MustGUID("000001cf-0000-0000-c000-000000000046"), // IMarshal2
	MustGUID("00000018-0000-0000-c000-000000000046"), // IStdMarshalInfo
	MustGUID("00000019-0000-0000-c000-000000000046"), // IExternalConnection
}

func handlerQueryInterface(this *handler, riid *GUID, out *unsafe.Pointer) uintptr {
	if this.iid == (GUID{}) && !slices.Contains(marshalIIDs, *riid) ||
		*riid == iidUnknown || *riid == this.iid || *riid == iidAgile {
		handlerAddRef(this)
		*out = unsafe.Pointer(this)
		return 0
	}
	*out = nil
	return 0x80004002 // E_NOINTERFACE
}

func handlerAddRef(this *handler) uintptr {
	handlersMu.Lock()
	defer handlersMu.Unlock()
	this.refs++
	return uintptr(this.refs)
}

func handlerRelease(this *handler) uintptr {
	handlersMu.Lock()
	defer handlersMu.Unlock()
	this.refs--
	if this.refs == 0 {
		delete(handlersLive, this)
	}
	return uintptr(this.refs)
}

func handlerInvoke(this *handler, a, b, c uintptr) uintptr { return this.fn(a, b, c) }
