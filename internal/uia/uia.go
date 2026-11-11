//go:build windows

// Package uia implements UI Automation providers as COM objects without cgo.
package uia

import (
	"math"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/myparsleycat/fairy/internal/com"
)

// Property IDs.
const (
	PropBoundingRectangle   int32 = 30001
	PropControlType         int32 = 30003
	PropName                int32 = 30005
	PropHasKeyboardFocus    int32 = 30008
	PropIsKeyboardFocusable int32 = 30009
	PropIsEnabled           int32 = 30010
	PropAutomationID        int32 = 30011
	PropHelpText            int32 = 30013
	PropIsControlElement    int32 = 30016
	PropIsContentElement    int32 = 30017
	PropIsPassword          int32 = 30019
	PropIsOffscreen         int32 = 30022
	PropFrameworkID         int32 = 30024
	PropValueValue          int32 = 30045
	PropRangeValueValue     int32 = 30047
	PropExpandCollapseState int32 = 30070
	PropSelectionIsSelected int32 = 30079
	PropToggleState         int32 = 30086
	PropLiveSetting         int32 = 30135
	PropPositionInSet       int32 = 30152
	PropSizeOfSet           int32 = 30153
	PropLevel               int32 = 30154
	PropHeadingLevel        int32 = 30173
	PropIsDialog            int32 = 30174
)

// Pattern IDs.
const (
	PatternInvoke         int32 = 10000
	PatternValue          int32 = 10002
	PatternRangeValue     int32 = 10003
	PatternExpandCollapse int32 = 10005
	PatternSelectionItem  int32 = 10010
	PatternToggle         int32 = 10015
	PatternScrollItem     int32 = 10017
)

// Event IDs.
const (
	EventMenuOpened              int32 = 20003
	EventFocusChanged            int32 = 20005
	EventWindowOpened            int32 = 20016
	EventLiveRegionChanged       int32 = 20024
	EventNotification            int32 = 20035
	StructureChildrenInvalidated int32 = 2
)

// Control type IDs.
const (
	ControlButton      int32 = 50000
	ControlCheckBox    int32 = 50002
	ControlComboBox    int32 = 50003
	ControlEdit        int32 = 50004
	ControlHyperlink   int32 = 50005
	ControlImage       int32 = 50006
	ControlListItem    int32 = 50007
	ControlList        int32 = 50008
	ControlMenu        int32 = 50009
	ControlMenuBar     int32 = 50010
	ControlMenuItem    int32 = 50011
	ControlProgressBar int32 = 50012
	ControlRadioButton int32 = 50013
	ControlSlider      int32 = 50015
	ControlStatusBar   int32 = 50017
	ControlTab         int32 = 50018
	ControlTabItem     int32 = 50019
	ControlText        int32 = 50020
	ControlToolBar     int32 = 50021
	ControlTree        int32 = 50023
	ControlTreeItem    int32 = 50024
	ControlGroup       int32 = 50026
	ControlDataGrid    int32 = 50028
	ControlDataItem    int32 = 50029
	ControlDocument    int32 = 50030
	ControlPane        int32 = 50033
	ControlHeaderItem  int32 = 50035
	ControlSeparator   int32 = 50038
)

// Toggle and expand/collapse states.
const (
	ToggleOff           int32 = 0
	ToggleOn            int32 = 1
	ToggleIndeterminate int32 = 2

	Collapsed int32 = 0
	Expanded  int32 = 1
	LeafNode  int32 = 3

	HeadingNone int32 = 80050
	Heading1    int32 = 80051
)

// RootObjectID is the WM_GETOBJECT object ID UI Automation asks for.
const RootObjectID = -25

// Notification kinds and processing for RaiseNotification.
const (
	NotificationOther int32 = 4
	ProcessingAll     int32 = 2
)

const (
	sOK                  = 0
	eNoInterface         = 0x80004002
	eFail                = 0x80004005
	eInvalidArg          = 0x80070057
	eElementNotAvailable = 0x80040201
	eElementNotEnabled   = 0x80040200
	eInvalidOperation    = 0x80131509
)

var (
	core                   = windows.NewLazySystemDLL("uiautomationcore.dll")
	pReturnRawElement      = core.NewProc("UiaReturnRawElementProvider")
	pHostProviderFromHwnd  = core.NewProc("UiaHostProviderFromHwnd")
	pRaiseAutomationEvent  = core.NewProc("UiaRaiseAutomationEvent")
	pRaisePropertyChanged  = core.NewProc("UiaRaiseAutomationPropertyChangedEvent")
	pRaiseStructureChanged = core.NewProc("UiaRaiseStructureChangedEvent")
	pRaiseNotification     = core.NewProc("UiaRaiseNotificationEvent")
	pClientsAreListening   = core.NewProc("UiaClientsAreListening")
	pDisconnectProvider    = core.NewProc("UiaDisconnectProvider")
	oleaut                 = windows.NewLazySystemDLL("oleaut32.dll")
	pSysAllocStringLen     = oleaut.NewProc("SysAllocStringLen")
	pSysFreeString         = oleaut.NewProc("SysFreeString")
	pSafeArrayCreateVector = oleaut.NewProc("SafeArrayCreateVector")
	pSafeArrayAccessData   = oleaut.NewProc("SafeArrayAccessData")
	pSafeArrayUnaccessData = oleaut.NewProc("SafeArrayUnaccessData")
)

// Available reports whether UIAutomationCore.dll can be loaded.
func Available() bool { return core.Load() == nil }

// ClientsAreListening reports whether any UI Automation client wants events.
func ClientsAreListening() bool {
	if pClientsAreListening.Find() != nil {
		return false
	}
	r, _, _ := pClientsAreListening.Call()
	return r != 0
}

// ReturnRawElementProvider answers WM_GETOBJECT. A nil p tells UI Automation
// that the window is going away.
func ReturnRawElementProvider(hwnd, wp, lp uintptr, p *Provider) uintptr {
	if pReturnRawElement.Find() != nil {
		return 0
	}
	var simple uintptr
	if p != nil {
		simple = p.simple()
	}
	r, _, _ := pReturnRawElement.Call(hwnd, wp, lp, simple)
	return r
}

const (
	vtEmpty = 0
	vtI4    = 3
	vtR8    = 5
	vtBSTR  = 8
	vtBool  = 11
)

// variant matches the amd64 VARIANT layout.
type variant struct {
	vt  uint16
	_   [3]uint16
	val uint64
	_   uint64
}

// setVariant stores v, which is nil, string, int32, bool or float64.
func setVariant(out *variant, v any) {
	*out = variant{}
	switch x := v.(type) {
	case string:
		out.vt, out.val = vtBSTR, uint64(allocBSTR(x))
	case int32:
		out.vt, out.val = vtI4, uint64(uint32(x))
	case bool:
		out.vt = vtBool
		if x {
			out.val = 0xffff // VARIANT_TRUE
		}
	case float64:
		out.vt, out.val = vtR8, math.Float64bits(x)
	}
}

func clearVariant(v *variant) {
	if v.vt == vtBSTR && v.val != 0 {
		pSysFreeString.Call(uintptr(v.val))
	}
	*v = variant{}
}

// allocBSTR keeps embedded NULs, which BSTRs allow since they carry a length.
func allocBSTR(s string) uintptr {
	u := utf16.Encode([]rune(s))
	r, _, _ := pSysAllocStringLen.Call(uintptr(unsafe.Pointer(unsafe.SliceData(u))), uintptr(len(u)))
	return r
}

func int32SafeArray(v []int32) uintptr {
	sa, _, _ := pSafeArrayCreateVector.Call(vtI4, 0, uintptr(len(v)))
	if sa == 0 {
		return 0
	}
	var data uintptr
	if hr, _, _ := pSafeArrayAccessData.Call(sa, uintptr(unsafe.Pointer(&data))); int32(hr) < 0 {
		return 0
	}
	copy(unsafe.Slice((*int32)(com.Ptr(data)), len(v)), v)
	pSafeArrayUnaccessData.Call(sa)
	return sa
}

func utf16At(p uintptr) string {
	if p == 0 {
		return ""
	}
	return windows.UTF16PtrToString((*uint16)(com.Ptr(p)))
}
