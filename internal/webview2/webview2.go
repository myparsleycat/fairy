//go:build windows

// Package webview2 hosts Microsoft Edge WebView2 without WebView2Loader.dll:
// it finds the installed runtime in the registry, as the official loader does,
// and calls its client DLL directly. Only the interfaces Fairy needs are bound.
package webview2

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/myparsleycat/fairy/internal/com"
)

// ErrNoRuntime reports that the WebView2 runtime is not installed.
var ErrNoRuntime = errors.New("webview2: runtime not installed")

var channels = []string{
	"{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}", // stable
	"{2CD8A007-E189-409D-A2C8-9AF4EF3C72AA}", // beta
	"{0D50BFEC-CD6A-4F9A-964C-C7416E3ACB10}", // dev
	"{65C35B14-6C1D-4122-AC46-7148CC9D6497}", // canary
}

// ClientDLL returns the path of the installed runtime's EmbeddedBrowserWebView.dll.
func ClientDLL() (string, error) {
	for _, ch := range channels {
		for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
			k, err := registry.OpenKey(root, `Software\Microsoft\EdgeUpdate\ClientState\`+ch, registry.READ|registry.WOW64_32KEY)
			if err != nil {
				continue
			}
			dir, _, err := k.GetStringValue("EBWebView")
			k.Close()
			if err != nil || dir == "" {
				continue
			}
			dll := filepath.Join(dir, "EBWebView", "x64", "EmbeddedBrowserWebView.dll")
			if _, err := os.Stat(dll); err == nil {
				return dll, nil
			}
		}
	}
	return "", ErrNoRuntime
}

// Interface ids of objects WebView2 hands out.
var (
	IID_ICoreWebView2_2   = com.MustGUID("9e8f0cf8-e670-4b5e-b2bc-73e061e3184c")
	iidEnvironmentOptions = com.MustGUID("2fde08a8-1e9a-4766-8c05-95a9ceb9d1c5")
)

// Options are the environment options.
type Options struct {
	UserDataFolder string
	BrowserArgs    string
	Language       string
}

// CreateEnvironment starts creating an environment; done runs on the calling
// (STA) thread with the ICoreWebView2Environment or an error.
func CreateEnvironment(o Options, done func(env unsafe.Pointer, err error)) error {
	dllPath, err := ClientDLL()
	if err != nil {
		return err
	}
	dll, err := windows.LoadDLL(dllPath)
	if err != nil {
		return fmt.Errorf("webview2: %w", err)
	}
	proc, err := dll.FindProc("CreateWebViewEnvironmentWithOptionsInternal")
	if err != nil {
		return fmt.Errorf("webview2: %w", err)
	}
	folder, err := syscall.UTF16PtrFromString(o.UserDataFolder)
	if err != nil {
		return err
	}
	opts := newOptions(o)
	defer com.Release(opts)
	h := com.NewHandler(com.GUID{}, func(a, b, _ uintptr) uintptr {
		hr := com.HRESULT(int32(a))
		if hr.Failed() || b == 0 {
			done(nil, hr.Err("webview2: create environment"))
			return 0
		}
		env := com.Ptr(b)
		com.AddRef(env)
		done(env, nil)
		return 0
	})
	defer com.Release(h)
	// arguments: checkRunningInstance, runtime type (installed), user data
	// folder, options, completion handler
	r, _, _ := proc.Call(1, 0, uintptr(unsafe.Pointer(folder)), uintptr(opts), uintptr(h))
	return com.HRESULT(int32(r)).Err("webview2: CreateWebViewEnvironmentWithOptionsInternal")
}

// options implements ICoreWebView2EnvironmentOptions.
type options struct {
	vtbl          *[11]uintptr
	refs          int32
	args, lang    string
	targetVersion string
}

var (
	optionsVtbl [11]uintptr
	optionsLive = map[*options]struct{}{}
)

func init() {
	nop := syscall.NewCallback(func(this *options, v uintptr) uintptr { return 1 }) // S_FALSE
	optionsVtbl = [11]uintptr{
		syscall.NewCallback(optQueryInterface),
		syscall.NewCallback(optAddRef),
		syscall.NewCallback(optRelease),
		syscall.NewCallback(func(this *options, out *uintptr) uintptr { *out = coString(this.args); return 0 }),
		nop,
		syscall.NewCallback(func(this *options, out *uintptr) uintptr { *out = coString(this.lang); return 0 }),
		nop,
		syscall.NewCallback(func(this *options, out *uintptr) uintptr { *out = coString(this.targetVersion); return 0 }),
		nop,
		syscall.NewCallback(func(this *options, out *int32) uintptr { *out = 0; return 0 }),
		nop,
	}
}

func newOptions(o Options) unsafe.Pointer {
	p := &options{vtbl: &optionsVtbl, refs: 1, args: o.BrowserArgs, lang: o.Language, targetVersion: "86.0.616.0"}
	optionsLive[p] = struct{}{}
	return unsafe.Pointer(p)
}

var iidUnknown = com.MustGUID("00000000-0000-0000-c000-000000000046")

func optQueryInterface(this *options, riid *com.GUID, out *unsafe.Pointer) uintptr {
	if *riid == iidUnknown || *riid == iidEnvironmentOptions {
		this.refs++
		*out = unsafe.Pointer(this)
		return 0
	}
	*out = nil
	return 0x80004002
}

func optAddRef(this *options) uintptr { this.refs++; return uintptr(this.refs) }

func optRelease(this *options) uintptr {
	this.refs--
	if this.refs == 0 {
		delete(optionsLive, this)
	}
	return uintptr(this.refs)
}

var pCoTaskMemAlloc = syscall.NewLazyDLL("ole32.dll").NewProc("CoTaskMemAlloc")
var pCoTaskMemFree = syscall.NewLazyDLL("ole32.dll").NewProc("CoTaskMemFree")

// coString returns s as a CoTaskMemAlloc'ed UTF-16 string, which the callee frees.
func coString(s string) uintptr {
	u := utf16.Encode([]rune(s + "\x00"))
	p, _, _ := pCoTaskMemAlloc.Call(uintptr(len(u) * 2))
	if p == 0 {
		return 0
	}
	copy(unsafe.Slice((*uint16)(com.Ptr(p)), len(u)), u)
	return p
}

// TakeString converts a CoTaskMemAlloc'ed string returned by WebView2 and frees it.
func TakeString(p uintptr) string {
	if p == 0 {
		return ""
	}
	s := windows.UTF16PtrToString((*uint16)(com.Ptr(p)))
	pCoTaskMemFree.Call(p)
	return s
}

// String calls a getter at vtable slot idx that returns an LPWSTR.
func String(obj unsafe.Pointer, idx int) string {
	var p uintptr
	if com.HR(obj, idx, uintptr(unsafe.Pointer(&p))).Failed() {
		return ""
	}
	return TakeString(p)
}

// Bool calls a getter at vtable slot idx that returns a BOOL.
func Bool(obj unsafe.Pointer, idx int) bool {
	var v int32
	com.HR(obj, idx, uintptr(unsafe.Pointer(&v)))
	return v != 0
}

// Object calls a getter at vtable slot idx that returns an interface.
func Object(obj unsafe.Pointer, idx int) unsafe.Pointer {
	var p unsafe.Pointer
	if com.HR(obj, idx, uintptr(unsafe.Pointer(&p))).Failed() {
		return nil
	}
	return p
}

// WStr returns a NUL-terminated UTF-16 copy of s that must stay reachable
// for the duration of the call it is passed to.
func WStr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

// Borrowed converts a UTF-16 string owned by WebView2, such as a handler argument.
func Borrowed(p uintptr) string {
	if p == 0 {
		return ""
	}
	return windows.UTF16PtrToString((*uint16)(com.Ptr(p)))
}
