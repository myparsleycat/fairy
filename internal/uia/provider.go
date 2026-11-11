//go:build windows

package uia

import (
	"log"
	"math"
	"runtime/debug"
	"sync"
	"syscall"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/com"
)

// Direction is a NavigateDirection.
type Direction int32

const (
	Parent Direction = iota
	NextSibling
	PreviousSibling
	FirstChild
	LastChild
)

// Rect is a UiaRect in screen pixels.
type Rect struct{ Left, Top, Width, Height float64 }

// Element is the Go side of a provider. UI Automation calls it on the UI thread.
type Element interface {
	// Alive reports whether the element still exists. Other methods are not
	// called on an element that is gone.
	Alive() bool
	Navigate(d Direction) *Provider
	// Property returns nil, a string, int32, bool or float64.
	Property(id int32) any
	// Pattern returns nil or a value implementing the pattern's interface
	// (Invoker, Toggler, SelectionItem, ExpandCollapser, Valuer, RangeValuer,
	// ScrollItem).
	Pattern(id int32) any
	Bounds() Rect
	SetFocus()
	// RuntimeID is nil for a fragment root.
	RuntimeID() []int32
}

// RootElement is the Element of a window's fragment root.
type RootElement interface {
	Element
	HWND() uintptr
	// FromPoint returns the element at a screen point, or nil for the root.
	FromPoint(x, y float64) *Provider
	// Focus returns the focused element, or nil when it is the root or none.
	Focus() *Provider
}

type Invoker interface{ Invoke() }

type Toggler interface {
	ToggleState() int32
	Toggle()
}

type SelectionItem interface {
	IsSelected() bool
	Select()
	Container() *Provider
}

type ExpandCollapser interface {
	ExpandCollapseState() int32
	Expand()
	Collapse()
}

type Valuer interface {
	Value() string
	ReadOnly() bool
	SetValue(s string)
}

type RangeValuer interface {
	Range() (value, min, max, small, large float64)
	RangeReadOnly() bool
	SetRangeValue(v float64)
}

type ScrollItem interface{ ScrollIntoView() }

// Dispatch runs f on the UI thread and waits for it; provider calls run f
// directly while it is nil. Calls that already arrive on the UI thread should
// run f directly.
var Dispatch func(f func())

// Provider is a COM object implementing IRawElementProviderSimple and
// IRawElementProviderFragment, and IRawElementProviderFragmentRoot for a root.
// The vtable fields must stay first and in this order: interface pointers
// handed to UI Automation point at them.
type Provider struct {
	vtSimple   *[7]uintptr
	vtFragment *[9]uintptr
	vtRoot     *[5]uintptr
	refs       int32
	el         Element
	root       *Provider
}

const (
	offFragment = unsafe.Sizeof(uintptr(0))
	offRoot     = 2 * unsafe.Sizeof(uintptr(0))
)

var (
	iidUnknown       = com.MustGUID("00000000-0000-0000-c000-000000000046")
	iidSimple        = com.MustGUID("d6dd68d1-86fd-4332-8666-9abedea2d24c")
	iidFragment      = com.MustGUID("f7063da8-8359-439c-9297-bbc5299a7d87")
	iidFragmentRoot  = com.MustGUID("620ce2a5-ab8f-40a9-86cb-de3c75599b58")
	iidInvoke        = com.MustGUID("54fcb24b-e18e-47a2-b4d3-eccbe77599a2")
	iidToggle        = com.MustGUID("56d00bd0-c4f4-433c-a836-1a52a57e0892")
	iidSelectionItem = com.MustGUID("2acad808-b2d4-452d-a407-91ff1ad167b2")
	iidExpand        = com.MustGUID("d847d3a5-cab0-4a98-8c32-ecb45c59ad24")
	iidValue         = com.MustGUID("c7935180-6fb3-4201-b174-7df73adbf64a")
	iidRangeValue    = com.MustGUID("36dc7aef-33e6-4691-afe1-2be7274b3d33")
	iidScrollItem    = com.MustGUID("2360c714-4bf1-4b26-ba65-9b21316127eb")
)

var (
	vtSimple   [7]uintptr
	vtFragment [9]uintptr
	vtRoot     [5]uintptr
	vtOnce     sync.Once

	// liveMu guards refs and live. UI Automation holds pointers to these
	// objects, so they stay reachable from live until their last Release.
	liveMu sync.Mutex
	live   = map[unsafe.Pointer]struct{}{}

	// Targets of the assembly stubs for methods with double arguments.
	fromPointTarget uintptr
	rangeSetTarget  uintptr
)

// fromPointStub and rangeSetStub move double arguments from XMM1/XMM2 into
// RDX/R8, where the Go callback trampoline reads them.
func fromPointStub()
func fromPointStubAddr() uintptr
func rangeSetStub()
func rangeSetStubAddr() uintptr

func initVtables() {
	vtSimple = [7]uintptr{
		syscall.NewCallback(simpleQI),
		syscall.NewCallback(simpleAddRef),
		syscall.NewCallback(simpleRelease),
		syscall.NewCallback(getProviderOptions),
		syscall.NewCallback(getPatternProvider),
		syscall.NewCallback(getPropertyValue),
		syscall.NewCallback(getHostRawElementProvider),
	}
	vtFragment = [9]uintptr{
		syscall.NewCallback(fragmentQI),
		syscall.NewCallback(fragmentAddRef),
		syscall.NewCallback(fragmentRelease),
		syscall.NewCallback(navigate),
		syscall.NewCallback(getRuntimeID),
		syscall.NewCallback(getBoundingRectangle),
		syscall.NewCallback(getEmbeddedFragmentRoots),
		syscall.NewCallback(setFocus),
		syscall.NewCallback(getFragmentRoot),
	}
	fromPointTarget = syscall.NewCallback(elementProviderFromPoint)
	vtRoot = [5]uintptr{
		syscall.NewCallback(rootQI),
		syscall.NewCallback(rootAddRef),
		syscall.NewCallback(rootRelease),
		fromPointStubAddr(),
		syscall.NewCallback(getFocus),
	}
	initPatternVtables()
}

// NewProvider returns a provider for el inside the fragment rooted at root.
// The caller owns one reference.
func NewProvider(el Element, root *Provider) *Provider {
	vtOnce.Do(initVtables)
	p := &Provider{vtSimple: &vtSimple, vtFragment: &vtFragment, vtRoot: &vtRoot, refs: 1, el: el, root: root}
	liveMu.Lock()
	live[unsafe.Pointer(p)] = struct{}{}
	liveMu.Unlock()
	return p
}

// NewRoot returns the fragment root provider of a window.
func NewRoot(el RootElement) *Provider {
	p := NewProvider(el, nil)
	p.root = p
	return p
}

func (p *Provider) isRoot() bool { return p.root == p }

func (p *Provider) simple() uintptr   { return uintptr(unsafe.Pointer(p)) }
func (p *Provider) fragment() uintptr { return uintptr(unsafe.Pointer(p)) + offFragment }
func (p *Provider) rootPtr() uintptr  { return uintptr(unsafe.Pointer(p)) + offRoot }

func (p *Provider) AddRef()  { addRef(p) }
func (p *Provider) Release() { release(p) }

// Disconnect tells UI Automation to drop its references to the provider.
func (p *Provider) Disconnect() {
	if pDisconnectProvider.Find() == nil {
		pDisconnectProvider.Call(p.simple())
	}
}

// RaiseEvent raises an automation event with the provider as its source.
func (p *Provider) RaiseEvent(id int32) {
	pRaiseAutomationEvent.Call(p.simple(), uintptr(id))
}

// RaisePropertyChanged raises a property change; values are as for Element.Property.
func (p *Provider) RaisePropertyChanged(id int32, old, new any) {
	var o, n variant
	setVariant(&o, old)
	setVariant(&n, new)
	// VARIANT arguments are larger than 8 bytes, so the x64 convention passes
	// them by reference.
	pRaisePropertyChanged.Call(p.simple(), uintptr(id), uintptr(unsafe.Pointer(&o)), uintptr(unsafe.Pointer(&n)))
	clearVariant(&o)
	clearVariant(&n)
}

// RaiseChildrenInvalidated reports that the provider's children changed.
func (p *Provider) RaiseChildrenInvalidated() {
	id := p.el.RuntimeID()
	pRaiseStructureChanged.Call(p.simple(), uintptr(StructureChildrenInvalidated), uintptr(unsafe.Pointer(unsafe.SliceData(id))), uintptr(len(id)))
}

// RaiseNotification asks screen readers to announce text (Windows 10 1709 and later).
func (p *Provider) RaiseNotification(text string) {
	if pRaiseNotification.Find() != nil {
		return
	}
	s, a := allocBSTR(text), allocBSTR("fairy.announce")
	pRaiseNotification.Call(p.simple(), uintptr(NotificationOther), uintptr(ProcessingAll), s, a)
	pSysFreeString.Call(s)
	pSysFreeString.Call(a)
}

// call runs f on the UI thread and turns a panic into E_FAIL, since a panic
// cannot unwind through UI Automation's frames.
func call(f func() uintptr) (hr uintptr) {
	run := func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("fairy: UI Automation provider panic: %v\n%s", r, debug.Stack())
				hr = eFail
			}
		}()
		hr = f()
	}
	if Dispatch != nil {
		Dispatch(run)
	} else {
		run()
	}
	return hr
}

func provAt(this uintptr, off uintptr) *Provider { return (*Provider)(com.Ptr(this - off)) }

func setPtr(out uintptr, v uintptr) { *(*uintptr)(com.Ptr(out)) = v }

func qi(p *Provider, riid, out uintptr) uintptr {
	if out == 0 {
		return eInvalidArg
	}
	var v uintptr
	switch *(*com.GUID)(com.Ptr(riid)) {
	case iidUnknown, iidSimple:
		v = p.simple()
	case iidFragment:
		v = p.fragment()
	case iidFragmentRoot:
		if p.isRoot() {
			v = p.rootPtr()
		}
	}
	setPtr(out, v)
	if v == 0 {
		return eNoInterface
	}
	p.AddRef()
	return sOK
}

func addRef(p *Provider) uintptr {
	liveMu.Lock()
	defer liveMu.Unlock()
	p.refs++
	return uintptr(p.refs)
}

func release(p *Provider) uintptr {
	liveMu.Lock()
	defer liveMu.Unlock()
	p.refs--
	if p.refs == 0 {
		delete(live, unsafe.Pointer(p))
	}
	return uintptr(p.refs)
}

func simpleQI(this, riid, out uintptr) uintptr   { return qi(provAt(this, 0), riid, out) }
func simpleAddRef(this uintptr) uintptr          { return addRef(provAt(this, 0)) }
func simpleRelease(this uintptr) uintptr         { return release(provAt(this, 0)) }
func fragmentQI(this, riid, out uintptr) uintptr { return qi(provAt(this, offFragment), riid, out) }
func fragmentAddRef(this uintptr) uintptr        { return addRef(provAt(this, offFragment)) }
func fragmentRelease(this uintptr) uintptr       { return release(provAt(this, offFragment)) }
func rootQI(this, riid, out uintptr) uintptr     { return qi(provAt(this, offRoot), riid, out) }
func rootAddRef(this uintptr) uintptr            { return addRef(provAt(this, offRoot)) }
func rootRelease(this uintptr) uintptr           { return release(provAt(this, offRoot)) }

const (
	optionServerSide   = 0x2
	optionComThreading = 0x20
)

func getProviderOptions(this, out uintptr) uintptr {
	if out == 0 {
		return eInvalidArg
	}
	*(*int32)(com.Ptr(out)) = optionServerSide | optionComThreading
	return sOK
}

func getPatternProvider(this uintptr, id uintptr, out uintptr) uintptr {
	if out == 0 {
		return eInvalidArg
	}
	setPtr(out, 0)
	p := provAt(this, 0)
	return call(func() uintptr {
		if !p.el.Alive() {
			return eElementNotAvailable
		}
		impl := p.el.Pattern(int32(id))
		if impl == nil {
			return sOK
		}
		if obj := newPattern(int32(id), p, impl); obj != 0 {
			setPtr(out, obj)
		}
		return sOK
	})
}

func getPropertyValue(this uintptr, id uintptr, out uintptr) uintptr {
	if out == 0 {
		return eInvalidArg
	}
	v := (*variant)(com.Ptr(out))
	*v = variant{}
	p := provAt(this, 0)
	return call(func() uintptr {
		if !p.el.Alive() {
			return eElementNotAvailable
		}
		setVariant(v, p.el.Property(int32(id)))
		return sOK
	})
}

func getHostRawElementProvider(this, out uintptr) uintptr {
	if out == 0 {
		return eInvalidArg
	}
	setPtr(out, 0)
	p := provAt(this, 0)
	if !p.isRoot() {
		return sOK
	}
	hwnd := p.el.(RootElement).HWND()
	r, _, _ := pHostProviderFromHwnd.Call(hwnd, out)
	return r
}

// setFragmentOut stores q as an IRawElementProviderFragment out parameter.
func setFragmentOut(out uintptr, q *Provider) {
	if q == nil {
		setPtr(out, 0)
		return
	}
	q.AddRef()
	setPtr(out, q.fragment())
}

func navigate(this uintptr, dir uintptr, out uintptr) uintptr {
	if out == 0 {
		return eInvalidArg
	}
	setPtr(out, 0)
	p := provAt(this, offFragment)
	return call(func() uintptr {
		if !p.el.Alive() {
			return eElementNotAvailable
		}
		setFragmentOut(out, p.el.Navigate(Direction(int32(dir))))
		return sOK
	})
}

func getRuntimeID(this, out uintptr) uintptr {
	if out == 0 {
		return eInvalidArg
	}
	setPtr(out, 0)
	p := provAt(this, offFragment)
	return call(func() uintptr {
		if !p.el.Alive() {
			return eElementNotAvailable
		}
		if id := p.el.RuntimeID(); id != nil {
			setPtr(out, int32SafeArray(id))
		}
		return sOK
	})
}

func getBoundingRectangle(this, out uintptr) uintptr {
	if out == 0 {
		return eInvalidArg
	}
	r := (*Rect)(com.Ptr(out))
	*r = Rect{}
	p := provAt(this, offFragment)
	return call(func() uintptr {
		if !p.el.Alive() {
			return eElementNotAvailable
		}
		*r = p.el.Bounds()
		return sOK
	})
}

func getEmbeddedFragmentRoots(this, out uintptr) uintptr {
	if out != 0 {
		setPtr(out, 0)
	}
	return sOK
}

func setFocus(this uintptr) uintptr {
	p := provAt(this, offFragment)
	return call(func() uintptr {
		if !p.el.Alive() {
			return eElementNotAvailable
		}
		p.el.SetFocus()
		return sOK
	})
}

func getFragmentRoot(this, out uintptr) uintptr {
	if out == 0 {
		return eInvalidArg
	}
	p := provAt(this, offFragment)
	r := p.root
	r.AddRef()
	setPtr(out, r.rootPtr())
	return sOK
}

func elementProviderFromPoint(this, x, y, out uintptr) uintptr {
	if out == 0 {
		return eInvalidArg
	}
	setPtr(out, 0)
	p := provAt(this, offRoot)
	fx, fy := math.Float64frombits(uint64(x)), math.Float64frombits(uint64(y))
	return call(func() uintptr {
		if !p.el.Alive() {
			return eElementNotAvailable
		}
		setFragmentOut(out, p.el.(RootElement).FromPoint(fx, fy))
		return sOK
	})
}

func getFocus(this, out uintptr) uintptr {
	if out == 0 {
		return eInvalidArg
	}
	setPtr(out, 0)
	p := provAt(this, offRoot)
	return call(func() uintptr {
		if !p.el.Alive() {
			return eElementNotAvailable
		}
		setFragmentOut(out, p.el.(RootElement).Focus())
		return sOK
	})
}
