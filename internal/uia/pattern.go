//go:build windows

package uia

import (
	"math"
	"syscall"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/com"
)

// patternObj is a tear-off COM object for one control pattern of a provider.
type patternObj struct {
	vtbl *uintptr
	refs int32
	iid  com.GUID
	p    *Provider
	impl any
}

var (
	vtInvoke     [4]uintptr
	vtToggle     [5]uintptr
	vtSelItem    [8]uintptr
	vtExpand     [6]uintptr
	vtValue      [6]uintptr
	vtRangeValue [10]uintptr
	vtScrollItem [4]uintptr
)

func initPatternVtables() {
	unk := [3]uintptr{
		syscall.NewCallback(patternQI),
		syscall.NewCallback(patternAddRef),
		syscall.NewCallback(patternRelease),
	}
	vtInvoke = [4]uintptr{unk[0], unk[1], unk[2], syscall.NewCallback(invoke)}
	vtToggle = [5]uintptr{unk[0], unk[1], unk[2], syscall.NewCallback(toggle), syscall.NewCallback(getToggleState)}
	vtSelItem = [8]uintptr{unk[0], unk[1], unk[2],
		syscall.NewCallback(selectItem),
		syscall.NewCallback(selectItem), // AddToSelection: single selection only
		syscall.NewCallback(removeFromSelection),
		syscall.NewCallback(getIsSelected),
		syscall.NewCallback(getSelectionContainer),
	}
	vtExpand = [6]uintptr{unk[0], unk[1], unk[2],
		syscall.NewCallback(expand),
		syscall.NewCallback(collapse),
		syscall.NewCallback(getExpandCollapseState),
	}
	vtValue = [6]uintptr{unk[0], unk[1], unk[2],
		syscall.NewCallback(setValue),
		syscall.NewCallback(getValue),
		syscall.NewCallback(getValueReadOnly),
	}
	rangeSetTarget = syscall.NewCallback(setRangeValue)
	vtRangeValue = [10]uintptr{unk[0], unk[1], unk[2],
		rangeSetStubAddr(),
		syscall.NewCallback(func(this, out uintptr) uintptr { return rangeField(this, out, 0) }),
		syscall.NewCallback(getRangeReadOnly),
		syscall.NewCallback(func(this, out uintptr) uintptr { return rangeField(this, out, 2) }),
		syscall.NewCallback(func(this, out uintptr) uintptr { return rangeField(this, out, 1) }),
		syscall.NewCallback(func(this, out uintptr) uintptr { return rangeField(this, out, 4) }),
		syscall.NewCallback(func(this, out uintptr) uintptr { return rangeField(this, out, 3) }),
	}
	vtScrollItem = [4]uintptr{unk[0], unk[1], unk[2], syscall.NewCallback(scrollIntoView)}
}

// newPattern returns a tear-off owning one reference, or 0 when impl does not
// implement the pattern.
func newPattern(id int32, p *Provider, impl any) uintptr {
	var vt *uintptr
	var iid com.GUID
	ok := false
	switch id {
	case PatternInvoke:
		_, ok = impl.(Invoker)
		vt, iid = &vtInvoke[0], iidInvoke
	case PatternToggle:
		_, ok = impl.(Toggler)
		vt, iid = &vtToggle[0], iidToggle
	case PatternSelectionItem:
		_, ok = impl.(SelectionItem)
		vt, iid = &vtSelItem[0], iidSelectionItem
	case PatternExpandCollapse:
		_, ok = impl.(ExpandCollapser)
		vt, iid = &vtExpand[0], iidExpand
	case PatternValue:
		_, ok = impl.(Valuer)
		vt, iid = &vtValue[0], iidValue
	case PatternRangeValue:
		_, ok = impl.(RangeValuer)
		vt, iid = &vtRangeValue[0], iidRangeValue
	case PatternScrollItem:
		_, ok = impl.(ScrollItem)
		vt, iid = &vtScrollItem[0], iidScrollItem
	}
	if !ok {
		return 0
	}
	p.AddRef()
	o := &patternObj{vtbl: vt, refs: 1, iid: iid, p: p, impl: impl}
	liveMu.Lock()
	live[unsafe.Pointer(o)] = struct{}{}
	liveMu.Unlock()
	return uintptr(unsafe.Pointer(o))
}

func patAt(this uintptr) *patternObj { return (*patternObj)(com.Ptr(this)) }

func patternQI(this, riid, out uintptr) uintptr {
	if out == 0 {
		return eInvalidArg
	}
	o := patAt(this)
	if g := *(*com.GUID)(com.Ptr(riid)); g == iidUnknown || g == o.iid {
		patternAddRef(this)
		setPtr(out, this)
		return sOK
	}
	setPtr(out, 0)
	return eNoInterface
}

func patternAddRef(this uintptr) uintptr {
	o := patAt(this)
	liveMu.Lock()
	defer liveMu.Unlock()
	o.refs++
	return uintptr(o.refs)
}

func patternRelease(this uintptr) uintptr {
	o := patAt(this)
	liveMu.Lock()
	o.refs--
	n := o.refs
	if n == 0 {
		delete(live, unsafe.Pointer(o))
	}
	liveMu.Unlock()
	if n == 0 {
		o.p.Release()
	}
	return uintptr(n)
}

// patCall runs f on the UI thread if the element still exists.
func patCall(this uintptr, f func(impl any) uintptr) uintptr {
	o := patAt(this)
	return call(func() uintptr {
		if !o.p.el.Alive() {
			return eElementNotAvailable
		}
		return f(o.impl)
	})
}

func setBool(out uintptr, b bool) {
	var v int32
	if b {
		v = 1
	}
	*(*int32)(com.Ptr(out)) = v
}

func invoke(this uintptr) uintptr {
	return patCall(this, func(impl any) uintptr { impl.(Invoker).Invoke(); return sOK })
}

func toggle(this uintptr) uintptr {
	return patCall(this, func(impl any) uintptr { impl.(Toggler).Toggle(); return sOK })
}

func getToggleState(this, out uintptr) uintptr {
	if out == 0 {
		return eInvalidArg
	}
	return patCall(this, func(impl any) uintptr {
		*(*int32)(com.Ptr(out)) = impl.(Toggler).ToggleState()
		return sOK
	})
}

func selectItem(this uintptr) uintptr {
	return patCall(this, func(impl any) uintptr { impl.(SelectionItem).Select(); return sOK })
}

func removeFromSelection(this uintptr) uintptr {
	return patCall(this, func(impl any) uintptr {
		if impl.(SelectionItem).IsSelected() {
			return eInvalidOperation
		}
		return sOK
	})
}

func getIsSelected(this, out uintptr) uintptr {
	if out == 0 {
		return eInvalidArg
	}
	return patCall(this, func(impl any) uintptr {
		setBool(out, impl.(SelectionItem).IsSelected())
		return sOK
	})
}

func getSelectionContainer(this, out uintptr) uintptr {
	if out == 0 {
		return eInvalidArg
	}
	setPtr(out, 0)
	return patCall(this, func(impl any) uintptr {
		if c := impl.(SelectionItem).Container(); c != nil {
			c.AddRef()
			setPtr(out, c.simple())
		}
		return sOK
	})
}

func expand(this uintptr) uintptr {
	return patCall(this, func(impl any) uintptr {
		e := impl.(ExpandCollapser)
		if e.ExpandCollapseState() == LeafNode {
			return eInvalidOperation
		}
		e.Expand()
		return sOK
	})
}

func collapse(this uintptr) uintptr {
	return patCall(this, func(impl any) uintptr {
		e := impl.(ExpandCollapser)
		if e.ExpandCollapseState() == LeafNode {
			return eInvalidOperation
		}
		e.Collapse()
		return sOK
	})
}

func getExpandCollapseState(this, out uintptr) uintptr {
	if out == 0 {
		return eInvalidArg
	}
	return patCall(this, func(impl any) uintptr {
		*(*int32)(com.Ptr(out)) = impl.(ExpandCollapser).ExpandCollapseState()
		return sOK
	})
}

func setValue(this, s uintptr) uintptr {
	str := utf16At(s)
	return patCall(this, func(impl any) uintptr {
		v := impl.(Valuer)
		if v.ReadOnly() {
			return eElementNotEnabled
		}
		v.SetValue(str)
		return sOK
	})
}

func getValue(this, out uintptr) uintptr {
	if out == 0 {
		return eInvalidArg
	}
	setPtr(out, 0)
	return patCall(this, func(impl any) uintptr {
		setPtr(out, allocBSTR(impl.(Valuer).Value()))
		return sOK
	})
}

func getValueReadOnly(this, out uintptr) uintptr {
	if out == 0 {
		return eInvalidArg
	}
	return patCall(this, func(impl any) uintptr {
		setBool(out, impl.(Valuer).ReadOnly())
		return sOK
	})
}

func setRangeValue(this, bits uintptr) uintptr {
	v := math.Float64frombits(uint64(bits))
	return patCall(this, func(impl any) uintptr {
		r := impl.(RangeValuer)
		if r.RangeReadOnly() {
			return eElementNotEnabled
		}
		_, lo, hi, _, _ := r.Range()
		if v < lo || v > hi || v != v {
			return eInvalidArg
		}
		r.SetRangeValue(v)
		return sOK
	})
}

// rangeField writes one of value, min, max, small and large change.
func rangeField(this, out uintptr, i int) uintptr {
	if out == 0 {
		return eInvalidArg
	}
	return patCall(this, func(impl any) uintptr {
		v, lo, hi, small, large := impl.(RangeValuer).Range()
		*(*float64)(com.Ptr(out)) = [5]float64{v, lo, hi, small, large}[i]
		return sOK
	})
}

func getRangeReadOnly(this, out uintptr) uintptr {
	if out == 0 {
		return eInvalidArg
	}
	return patCall(this, func(impl any) uintptr {
		setBool(out, impl.(RangeValuer).RangeReadOnly())
		return sOK
	})
}

func scrollIntoView(this uintptr) uintptr {
	return patCall(this, func(impl any) uintptr { impl.(ScrollItem).ScrollIntoView(); return sOK })
}
