//go:build windows

package com

import (
	"math"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// F64 passes a float64 through an integer argument slot; see F32.
func F64(f float64) uintptr { return uintptr(math.Float64bits(f)) }

// f64Code is the machine code of f64Thunk, which calls fn(this, a, b) and returns XMM0 in RAX,
// because the Go syscall trampoline only returns RAX:
//
//	mov rax, rcx; mov rcx, rdx; mov rdx, r8; mov r8, r9
//	sub rsp, 40; call rax; movq rax, xmm0; add rsp, 40; ret
var f64Code = []byte{
	0x48, 0x89, 0xC8, 0x48, 0x89, 0xD1, 0x4C, 0x89, 0xC2, 0x4D, 0x89, 0xC8,
	0x48, 0x83, 0xEC, 0x28, 0xFF, 0xD0, 0x66, 0x48, 0x0F, 0x7E, 0xC0,
	0x48, 0x83, 0xC4, 0x28, 0xC3,
}

// f64Thunk returns the address of an executable copy of f64Code.
var f64Thunk = sync.OnceValue(func() uintptr {
	p, err := windows.VirtualAlloc(0, uintptr(len(f64Code)), windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE)
	if err != nil {
		panic("com: VirtualAlloc: " + err.Error())
	}
	copy(unsafe.Slice((*byte)(Ptr(p)), len(f64Code)), f64Code)
	var old uint32
	if err := windows.VirtualProtect(p, uintptr(len(f64Code)), windows.PAGE_EXECUTE_READ, &old); err != nil {
		panic("com: VirtualProtect: " + err.Error())
	}
	return p
})

// CallF64 invokes vtable slot idx of obj, which returns a double, with at most
// two further arguments.
//
//go:uintptrescapes
func CallF64(obj unsafe.Pointer, idx int, args ...uintptr) float64 {
	if obj == nil {
		panic("com: call on nil interface")
	}
	if len(args) > 2 {
		panic("com: CallF64 takes at most two arguments")
	}
	vtbl := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vtbl, idx*int(unsafe.Sizeof(uintptr(0)))))
	a := [4]uintptr{fn, uintptr(obj)}
	n := copy(a[2:], args)
	r, _, _ := syscall.SyscallN(f64Thunk(), a[:n+2]...)
	return math.Float64frombits(uint64(r))
}
