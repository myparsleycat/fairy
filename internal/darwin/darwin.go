//go:build darwin

// Package darwin calls the macOS frameworks without cgo: Objective-C messages
// through the objc runtime, and C functions loaded with dlopen. It holds what
// several packages share; each package registers the functions it needs from
// the library handles here with purego.RegisterLibFunc.
package darwin

import (
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// ID, SEL and Class are Objective-C object, selector and class references.
type (
	ID    = objc.ID
	SEL   = objc.SEL
	Class = objc.Class
)

// Library handles, loaded when the package initializes.
var (
	System          uintptr
	CoreFoundation  uintptr
	Foundation      uintptr
	CoreGraphics    uintptr
	CoreText        uintptr
	QuartzCore      uintptr
	Metal           uintptr
	AppKit          uintptr
	ApplicationSvcs uintptr
)

func load(path string) uintptr {
	h, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		panic("darwin: " + err.Error())
	}
	return h
}

func init() {
	const fw = "/System/Library/Frameworks/"
	System = load("/usr/lib/libSystem.B.dylib")
	CoreFoundation = load(fw + "CoreFoundation.framework/CoreFoundation")
	Foundation = load(fw + "Foundation.framework/Foundation")
	CoreGraphics = load(fw + "CoreGraphics.framework/CoreGraphics")
	CoreText = load(fw + "CoreText.framework/CoreText")
	QuartzCore = load(fw + "QuartzCore.framework/QuartzCore")
	Metal = load(fw + "Metal.framework/Metal")
	AppKit = load(fw + "AppKit.framework/AppKit")
	ApplicationSvcs = load(fw + "ApplicationServices.framework/ApplicationServices")

	purego.RegisterLibFunc(&CFRetain, CoreFoundation, "CFRetain")
	purego.RegisterLibFunc(&CFRelease, CoreFoundation, "CFRelease")
}

// Func registers the C function name from lib in fptr, a pointer to a
// variable of func type, as purego.RegisterLibFunc does.
func Func(fptr any, lib uintptr, name string) { purego.RegisterLibFunc(fptr, lib, name) }

// Symbol returns the address of the global name in lib.
func Symbol(lib uintptr, name string) uintptr {
	p, err := purego.Dlsym(lib, name)
	if err != nil {
		panic("darwin: " + err.Error())
	}
	return p
}

// SymbolID returns the object a global pointer variable in lib points to,
// such as an NSString constant.
func SymbolID(lib uintptr, name string) ID {
	return *(*ID)(unsafe.Add(nil, Symbol(lib, name)))
}

var (
	CFRetain  func(p uintptr) uintptr
	CFRelease func(p uintptr)
)

var (
	selMu    sync.Mutex
	sels     = map[string]SEL{}
	classMu  sync.Mutex
	classMap = map[string]Class{}
)

// Sel returns the selector for name.
func Sel(name string) SEL {
	selMu.Lock()
	s, ok := sels[name]
	if !ok {
		s = objc.RegisterName(name)
		sels[name] = s
	}
	selMu.Unlock()
	return s
}

// Cls returns the class named name, or 0.
func Cls(name string) Class {
	classMu.Lock()
	c, ok := classMap[name]
	if !ok {
		c = objc.GetClass(name)
		classMap[name] = c
	}
	classMu.Unlock()
	return c
}

// Send sends the message sel with args to id and returns an object result.
func Send(id ID, sel string, args ...any) ID { return id.Send(Sel(sel), args...) }

// SendClass sends a message to the class named cls.
func SendClass(cls, sel string, args ...any) ID { return ID(Cls(cls)).Send(Sel(sel), args...) }

// Get sends the message sel with args to id and returns a result of type T,
// such as bool, float64, a struct or a pointer.
func Get[T any](id ID, sel string, args ...any) T { return objc.Send[T](id, Sel(sel), args...) }

// New returns [[cls alloc] init].
func New(cls string) ID { return SendClass(cls, "new") }

// Retain and Release adjust an object's reference count.
func Retain(id ID) ID { return id.Send(Sel("retain")) }
func Release(id ID) {
	if id != 0 {
		id.Send(Sel("release"))
	}
}

// Pool is an autorelease pool. Every message sent off the main run loop
// that returns autoreleased objects runs inside one.
type Pool ID

func NewPool() Pool     { return Pool(New("NSAutoreleasePool")) }
func (p Pool) Drain()   { ID(p).Send(Sel("drain")) }
func WithPool(f func()) { p := NewPool(); defer p.Drain(); f() }
