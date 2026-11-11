//go:build windows

package win

import (
	"errors"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/myparsleycat/fairy/internal/com"
)

var (
	combase                  = windows.NewLazySystemDLL("combase.dll")
	pRoGetActivationFactory  = combase.NewProc("RoGetActivationFactory")
	pRoActivateInstance      = combase.NewProc("RoActivateInstance")
	pWindowsCreateString     = combase.NewProc("WindowsCreateString")
	pWindowsDeleteString     = combase.NewProc("WindowsDeleteString")
	pGetCurrentProcessAUMID  = shell32.NewProc("GetCurrentProcessExplicitAppUserModelID")
	iidToastManagerStatics   = com.MustGUID("50ac103f-d235-4598-bbef-98fe4d1a3ad4")
	iidToastNotificationFact = com.MustGUID("04124b20-82c6-4229-b109-fd9ed4662b53")
	iidToastNotification2    = com.MustGUID("9dfb9fd1-143a-490e-90bf-b9fba7132de7")
	iidXmlDocument           = com.MustGUID("f7f3a506-1e87-42d6-bcfb-b8c809fa5494")
	iidXmlDocumentIO         = com.MustGUID("6cd0e74e-ee65-4489-9ebf-ca43e87ba637")
	errToastsDisabled        = errors.New("win: toast notifications are disabled for this app")
)

type hstring uintptr

func newHString(s string) (hstring, error) {
	u, err := syscall.UTF16FromString(s)
	if err != nil {
		return 0, err
	}
	var h hstring
	r, _, _ := pWindowsCreateString.Call(uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&h)))
	if err := com.HRESULT(int32(r)).Err("WindowsCreateString"); err != nil {
		return 0, err
	}
	return h, nil
}

func (h hstring) free() {
	if h != 0 {
		pWindowsDeleteString.Call(uintptr(h))
	}
}

func activationFactory(class string, iid *com.GUID) (unsafe.Pointer, error) {
	h, err := newHString(class)
	if err != nil {
		return nil, err
	}
	defer h.free()
	var f unsafe.Pointer
	r, _, _ := pRoGetActivationFactory.Call(uintptr(h), uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&f)))
	if err := com.HRESULT(int32(r)).Err("RoGetActivationFactory " + class); err != nil {
		return nil, err
	}
	return f, nil
}

// AppIDRegistered reports whether appID has an
// HKCU\Software\Classes\AppUserModelId key, which lets an app without a
// Start menu shortcut show toasts.
func AppIDRegistered(appID string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes\AppUserModelId\`+appID, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	k.Close()
	return true
}

// ProcessAppID returns the explicit AppUserModelID set with
// SetCurrentProcessExplicitAppUserModelID, or "".
func ProcessAppID() string {
	if pGetCurrentProcessAUMID.Find() != nil {
		return ""
	}
	var p *uint16
	r, _, _ := pGetCurrentProcessAUMID.Call(uintptr(unsafe.Pointer(&p)))
	if com.HRESULT(int32(r)).Failed() || p == nil {
		return ""
	}
	defer pCoTaskMemFree.Call(uintptr(unsafe.Pointer(p)))
	return windows.UTF16PtrToString(p)
}

func xmlEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ShowToast shows a Windows toast for the app appID. A toast with the same
// tag and group replaces the earlier one in the popup and in the
// notification center. The app must be registered for toasts: a Start menu
// shortcut with the AppUserModelID, or an AppUserModelId registry key.
// COM must be initialized on the calling thread.
func ShowToast(appID, tag, group, title, body string) error {
	if err := pRoGetActivationFactory.Find(); err != nil {
		return err
	}
	xml := `<toast><visual><binding template="ToastGeneric"><text>` + xmlEscape(title) + `</text>`
	if body != "" {
		xml += `<text>` + xmlEscape(body) + `</text>`
	}
	xml += `</binding></visual></toast>`

	cls, err := newHString("Windows.Data.Xml.Dom.XmlDocument")
	if err != nil {
		return err
	}
	defer cls.free()
	var inst unsafe.Pointer
	r, _, _ := pRoActivateInstance.Call(uintptr(cls), uintptr(unsafe.Pointer(&inst)))
	if err := com.HRESULT(int32(r)).Err("RoActivateInstance XmlDocument"); err != nil {
		return err
	}
	defer com.Release(inst)
	io, err := com.QueryInterface(inst, &iidXmlDocumentIO)
	if err != nil {
		return err
	}
	defer com.Release(io)
	hx, err := newHString(xml)
	if err != nil {
		return err
	}
	defer hx.free()
	if err := com.HR(io, 6, uintptr(hx)).Err("IXmlDocumentIO.LoadXml"); err != nil {
		return err
	}
	doc, err := com.QueryInterface(inst, &iidXmlDocument)
	if err != nil {
		return err
	}
	defer com.Release(doc)

	factory, err := activationFactory("Windows.UI.Notifications.ToastNotification", &iidToastNotificationFact)
	if err != nil {
		return err
	}
	defer com.Release(factory)
	var toast unsafe.Pointer
	if err := com.HR(factory, 6, uintptr(doc), uintptr(unsafe.Pointer(&toast))).Err("CreateToastNotification"); err != nil {
		return err
	}
	defer com.Release(toast)
	t2, err := com.QueryInterface(toast, &iidToastNotification2)
	if err != nil {
		return err
	}
	defer com.Release(t2)
	htag, err := newHString(tag)
	if err != nil {
		return err
	}
	defer htag.free()
	hgroup, err := newHString(group)
	if err != nil {
		return err
	}
	defer hgroup.free()
	if err := com.HR(t2, 6, uintptr(htag)).Err("put_Tag"); err != nil {
		return err
	}
	if err := com.HR(t2, 8, uintptr(hgroup)).Err("put_Group"); err != nil {
		return err
	}

	statics, err := activationFactory("Windows.UI.Notifications.ToastNotificationManager", &iidToastManagerStatics)
	if err != nil {
		return err
	}
	defer com.Release(statics)
	happ, err := newHString(appID)
	if err != nil {
		return err
	}
	defer happ.free()
	var notifier unsafe.Pointer
	if err := com.HR(statics, 7, uintptr(happ), uintptr(unsafe.Pointer(&notifier))).Err("CreateToastNotifierWithId"); err != nil {
		return err
	}
	defer com.Release(notifier)
	// get_Setting fails with ERROR_NOT_FOUND for unpackaged apps on some
	// Windows builds even when Show works, so only a reported setting counts.
	var setting int32
	if !com.HR(notifier, 8, uintptr(unsafe.Pointer(&setting))).Failed() && setting != 0 {
		return errToastsDisabled
	}
	return com.HR(notifier, 6, uintptr(toast)).Err("IToastNotifier.Show")
}
