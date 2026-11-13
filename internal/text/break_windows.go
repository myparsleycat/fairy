package text

import (
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// icu loads the ICU that Windows ships: icu.dll since 1903, icuuc.dll since
// 1703. It returns nil when neither has the break iterator functions.
var icu = sync.OnceValue(func() *icuAPI {
	for _, name := range []string{"icu.dll", "icuuc.dll"} {
		dll := windows.NewLazySystemDLL(name)
		if dll.Load() != nil {
			continue
		}
		open := dll.NewProc("ubrk_open")
		setText := dll.NewProc("ubrk_setText")
		preceding := dll.NewProc("ubrk_preceding")
		following := dll.NewProc("ubrk_following")
		ruleStatus := dll.NewProc("ubrk_getRuleStatus")
		ok := true
		for _, f := range []*windows.LazyProc{open, setText, preceding, following, ruleStatus} {
			if f.Find() != nil {
				ok = false
			}
		}
		if !ok {
			continue
		}
		return &icuAPI{
			open: func(kind int32, locale *byte, text *uint16, n int32, status *int32) uintptr {
				r, _, _ := open.Call(uintptr(kind), uintptr(unsafe.Pointer(locale)), uintptr(unsafe.Pointer(text)), uintptr(n), uintptr(unsafe.Pointer(status)))
				return r
			},
			setText: func(it uintptr, text *uint16, n int32, status *int32) {
				setText.Call(it, uintptr(unsafe.Pointer(text)), uintptr(n), uintptr(unsafe.Pointer(status)))
			},
			preceding: func(it uintptr, offset int32) int32 {
				r, _, _ := preceding.Call(it, uintptr(offset))
				return int32(r)
			},
			following: func(it uintptr, offset int32) int32 {
				r, _, _ := following.Call(it, uintptr(offset))
				return int32(r)
			},
			ruleStatus: func(it uintptr) int32 {
				r, _, _ := ruleStatus.Call(it)
				return int32(r)
			},
		}
	}
	return nil
})
