//go:build windows

// Package com calls COM interfaces through their vtables without cgo.
package com

import (
	"fmt"
	"syscall"
	"unsafe"
)

// GUID matches the Win32 GUID layout.
type GUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

// MustGUID parses "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx".
func MustGUID(s string) GUID {
	var g GUID
	var d4 [8]uint32
	_, err := fmt.Sscanf(s, "%08x-%04x-%04x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		&g.Data1, &g.Data2, &g.Data3, &d4[0], &d4[1], &d4[2], &d4[3], &d4[4], &d4[5], &d4[6], &d4[7])
	if err != nil {
		panic("com: bad GUID " + s)
	}
	for i, v := range d4 {
		g.Data4[i] = byte(v)
	}
	return g
}

// HRESULT is a COM status code.
type HRESULT int32

func (h HRESULT) Failed() bool { return h < 0 }

func (h HRESULT) Error() string {
	return fmt.Sprintf("HRESULT 0x%08X", uint32(h))
}

// Err returns nil for success codes.
func (h HRESULT) Err(op string) error {
	if h >= 0 {
		return nil
	}
	return fmt.Errorf("%s: %w", op, h)
}

// Call invokes vtable slot idx of obj with obj as the implicit this argument.
//
//go:uintptrescapes
func Call(obj unsafe.Pointer, idx int, args ...uintptr) uintptr {
	if obj == nil {
		panic("com: call on nil interface")
	}
	vtbl := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vtbl, idx*int(unsafe.Sizeof(uintptr(0)))))
	var a [18]uintptr
	a[0] = uintptr(obj)
	n := copy(a[1:], args)
	r, _, _ := syscall.SyscallN(fn, a[:n+1]...)
	return r
}

// HR invokes a vtable slot that returns HRESULT.
//
//go:uintptrescapes
func HR(obj unsafe.Pointer, idx int, args ...uintptr) HRESULT {
	return HRESULT(int32(Call(obj, idx, args...)))
}

const (
	slotQueryInterface = 0
	slotAddRef         = 1
	slotRelease        = 2
)

// Release decrements the reference count of a non-nil interface.
func Release(obj unsafe.Pointer) {
	if obj != nil {
		Call(obj, slotRelease)
	}
}

// AddRef increments the reference count of a non-nil interface.
func AddRef(obj unsafe.Pointer) {
	if obj != nil {
		Call(obj, slotAddRef)
	}
}

// QueryInterface asks obj for another interface.
func QueryInterface(obj unsafe.Pointer, iid *GUID) (unsafe.Pointer, error) {
	var out unsafe.Pointer
	hr := HR(obj, slotQueryInterface, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	if err := hr.Err("QueryInterface"); err != nil {
		return nil, err
	}
	return out, nil
}

// F32 passes a float32 through an integer argument slot. The Go Windows syscall
// trampoline mirrors the first four integer registers into XMM0-3, and later
// arguments are read from the stack, so the bit pattern lands where the callee
// expects it in either case.
func F32(f float32) uintptr {
	return uintptr(*(*uint32)(unsafe.Pointer(&f)))
}

// Ptr converts an address owned by foreign code into a pointer.
func Ptr(addr uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&addr))
}
