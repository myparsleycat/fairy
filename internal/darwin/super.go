//go:build darwin

package darwin

import (
	"reflect"
	"runtime"
	"structs"

	"github.com/ebitengine/purego"
)

// objcSuper is struct objc_super for objc_msgSendSuper2: the receiver and
// the class whose superclass the method lookup starts from.
type objcSuper struct {
	_     structs.HostLayout
	self  ID
	class Class
}

var (
	msgSendSuper2Fn      uintptr
	msgSendSuper2StretFn uintptr
	msgSendSuper2        func(super *objcSuper, cmd SEL, args ...any) ID
)

func init() {
	msgSendSuper2Fn = Symbol(Objc, "objc_msgSendSuper2")
	if runtime.GOARCH == "amd64" {
		msgSendSuper2StretFn = Symbol(Objc, "objc_msgSendSuper2_stret")
	}
	purego.RegisterFunc(&msgSendSuper2, msgSendSuper2Fn)
}

// SendSuper sends sel to self as [super sel] does in a method of cls, the
// class that defines the calling method. objc's ID.SendSuper starts from
// self's class instead, which is a runtime subclass of cls once key-value
// observing swizzles self, and would call the same method again forever.
func SendSuper(self ID, cls Class, sel SEL, args ...any) ID {
	return msgSendSuper2(&objcSuper{self: self, class: cls}, sel, args...)
}

// GetSuper is SendSuper for a result of type T.
func GetSuper[T any](self ID, cls Class, sel SEL, args ...any) T {
	var fn func(super *objcSuper, cmd SEL, args ...any) T
	var zero T
	// Structs larger than two registers are returned through memory on x86-64.
	if t := reflect.TypeOf(zero); runtime.GOARCH == "amd64" && t != nil && t.Kind() == reflect.Struct && t.Size() > 16 {
		purego.RegisterFunc(&fn, msgSendSuper2StretFn)
	} else {
		purego.RegisterFunc(&fn, msgSendSuper2Fn)
	}
	return fn(&objcSuper{self: self, class: cls}, sel, args...)
}
