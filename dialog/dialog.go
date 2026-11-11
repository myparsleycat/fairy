// Package dialog shows native file dialogs and message boxes.
package dialog

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/internal/apphost"
	"github.com/myparsleycat/fairy/internal/com"
	"github.com/myparsleycat/fairy/internal/win"
)

// ErrCanceled is returned when the user dismisses a dialog.
var ErrCanceled = errors.New("dialog: canceled")

// Filter is one entry of a file dialog's type list.
type Filter struct {
	Name     string   // "Images"
	Patterns []string // "*.png", "*.jpg"
}

// FileOptions configures OpenFiles, OpenFolders and SaveFile.
type FileOptions struct {
	Title string
	// Folder is the folder the dialog opens in. A file path opens its folder.
	Folder string
	// FileName is the suggested name in a save dialog.
	FileName string
	Filters  []Filter
	// DefaultExt is appended in a save dialog when the user types a name
	// without an extension, as in "png".
	DefaultExt string
	// Role gives dialogs of one kind their own remembered last folder.
	// Dialogs without a role share one.
	Role string
	// Multiple allows selecting several items in an open dialog.
	Multiple bool
}

var (
	clsidFileOpenDialog = com.MustGUID("dc1c5a9c-e88a-4dde-a5a1-60f82a20aef7")
	clsidFileSaveDialog = com.MustGUID("c0b4e2f3-ba21-4773-8dba-335ec946eb8b")
	iidIFileOpenDialog  = com.MustGUID("d57c7288-d4ad-4768-be02-9d969532d960")
	iidIFileSaveDialog  = com.MustGUID("84bccd23-5fde-4cdb-aea4-af64b83d78ab")
	iidIShellItem       = com.MustGUID("43826d1e-e718-42ee-bc55-a1e261c37bfe")
)

const (
	fosOverwritePrompt = 0x2
	fosNoChangeDir     = 0x8
	fosPickFolders     = 0x20
	fosForceFileSystem = 0x40
	fosAllowMulti      = 0x200
	fosPathMustExist   = 0x800
	fosFileMustExist   = 0x1000

	sigdnFileSysPath = 0x80058000
	hrCanceled       = com.HRESULT(-2147023673) // HRESULT_FROM_WIN32(ERROR_CANCELLED)
)

// OpenFiles asks for one file, or several with Multiple. It blocks until the
// dialog closes and may be called from any goroutine. owner may be nil.
func OpenFiles(owner *fairy.Window, d FileOptions) ([]string, error) {
	return runFileDialog(owner, d, false, false)
}

// OpenFolders asks for one folder, or several with Multiple.
func OpenFolders(owner *fairy.Window, d FileOptions) ([]string, error) {
	return runFileDialog(owner, d, false, true)
}

// SaveFile asks for a path to save to, confirming before overwriting.
func SaveFile(owner *fairy.Window, d FileOptions) (string, error) {
	paths, err := runFileDialog(owner, d, true, false)
	if err != nil {
		return "", err
	}
	return paths[0], nil
}

func runFileDialog(owner *fairy.Window, d FileOptions, save, folders bool) (paths []string, err error) {
	if !apphost.Do(func() { paths, err = showFileDialog(ownerHWND(owner), d, save, folders) }) {
		return nil, apphost.ErrUIUnavailable
	}
	if err == nil && len(paths) == 0 {
		err = ErrCanceled
	}
	return paths, err
}

func ownerHWND(w *fairy.Window) win.HWND {
	if w == nil {
		return 0
	}
	return win.HWND(w.HWND())
}

func showFileDialog(owner win.HWND, d FileOptions, save, folders bool) ([]string, error) {
	clsid, iid := clsidFileOpenDialog, iidIFileOpenDialog
	if save {
		clsid, iid = clsidFileSaveDialog, iidIFileSaveDialog
	}
	dlg, hr := win.CoCreateInstance(unsafe.Pointer(&clsid), unsafe.Pointer(&iid))
	if hr < 0 {
		return nil, fmt.Errorf("dialog: file dialog: %w", com.HRESULT(hr))
	}
	defer com.Release(dlg)

	var opts uint32
	com.HR(dlg, 10, uintptr(unsafe.Pointer(&opts)))
	opts |= fosForceFileSystem | fosNoChangeDir | fosPathMustExist
	switch {
	case save:
		opts |= fosOverwritePrompt
	case folders:
		opts |= fosPickFolders
	default:
		opts |= fosFileMustExist
	}
	if d.Multiple && !save {
		opts |= fosAllowMulti
	}
	com.HR(dlg, 9, uintptr(opts))

	var keep []any // UTF-16 strings the dialog reads until Show returns
	utf16 := func(s string) uintptr {
		p, _ := syscall.UTF16PtrFromString(s)
		keep = append(keep, p)
		return uintptr(unsafe.Pointer(p))
	}
	if d.Role != "" {
		sum := sha256.Sum256([]byte("nahida.fairy.dialog:" + d.Role))
		com.HR(dlg, 24, uintptr(unsafe.Pointer(&sum[0])))
	}
	if d.Title != "" {
		com.HR(dlg, 17, utf16(d.Title))
	}
	if d.FileName != "" {
		com.HR(dlg, 15, utf16(d.FileName))
	}
	if d.DefaultExt != "" {
		com.HR(dlg, 22, utf16(strings.TrimPrefix(d.DefaultExt, ".")))
	}
	if !folders && len(d.Filters) > 0 {
		specs := make([][2]uintptr, len(d.Filters))
		for i, f := range d.Filters {
			specs[i] = [2]uintptr{utf16(f.Name), utf16(strings.Join(f.Patterns, ";"))}
		}
		keep = append(keep, specs)
		com.HR(dlg, 4, uintptr(len(specs)), uintptr(unsafe.Pointer(&specs[0])))
	}
	if folder := dialogFolder(d.Folder); folder != "" {
		if item, hr := win.SHCreateItemFromParsingName(folder, unsafe.Pointer(&iidIShellItem)); hr >= 0 {
			com.HR(dlg, 12, uintptr(item))
			com.Release(item)
		}
	}

	hr2 := com.HR(dlg, 3, uintptr(owner))
	runtime.KeepAlive(keep)
	if hr2 == hrCanceled {
		return nil, ErrCanceled
	}
	if err := hr2.Err("IFileDialog.Show"); err != nil {
		return nil, err
	}
	if !save && d.Multiple {
		var arr unsafe.Pointer
		if err := com.HR(dlg, 27, uintptr(unsafe.Pointer(&arr))).Err("GetResults"); err != nil {
			return nil, err
		}
		defer com.Release(arr)
		var n uint32
		com.HR(arr, 7, uintptr(unsafe.Pointer(&n)))
		paths := make([]string, 0, n)
		for i := uint32(0); i < n; i++ {
			var item unsafe.Pointer
			if com.HR(arr, 8, uintptr(i), uintptr(unsafe.Pointer(&item))).Failed() {
				continue
			}
			if p := itemPath(item); p != "" {
				paths = append(paths, p)
			}
			com.Release(item)
		}
		return paths, nil
	}
	var item unsafe.Pointer
	if err := com.HR(dlg, 20, uintptr(unsafe.Pointer(&item))).Err("GetResult"); err != nil {
		return nil, err
	}
	defer com.Release(item)
	if p := itemPath(item); p != "" {
		return []string{p}, nil
	}
	return nil, errors.New("dialog: the selected item has no file system path")
}

func itemPath(item unsafe.Pointer) string {
	var p uintptr
	if com.HR(item, 5, sigdnFileSysPath, uintptr(unsafe.Pointer(&p))).Failed() {
		return ""
	}
	return win.CoTaskString(p)
}

// dialogFolder returns path if it is a folder, else its parent if that is one.
func dialogFolder(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		return path
	}
	if parent := filepath.Dir(path); parent != path {
		if fi, err := os.Stat(parent); err == nil && fi.IsDir() {
			return parent
		}
	}
	return ""
}

// MessageKind selects a message box icon.
type MessageKind uint8

const (
	MessageInfo MessageKind = iota
	MessageWarning
	MessageError
	MessageQuestion
)

// Buttons selects the buttons of a message box.
type Buttons uint8

const (
	ButtonsOK Buttons = iota
	ButtonsOKCancel
	ButtonsYesNo
	ButtonsYesNoCancel
)

// Button is the button that closed a message box.
type Button uint8

const (
	ButtonOK Button = iota + 1
	ButtonCancel
	ButtonYes
	ButtonNo
)

// Message shows a native message box and returns the button pressed.
// Closing the box with Escape returns ButtonCancel (ButtonOK for ButtonsOK,
// ButtonNo for ButtonsYesNo). It may be called from any goroutine.
func Message(owner *fairy.Window, title, message string, kind MessageKind, buttons Buttons) Button {
	flags := [...]uint32{win.MB_ICONINFORMATION, win.MB_ICONWARNING, win.MB_ICONERROR, win.MB_ICONQUESTION}[kind] |
		[...]uint32{win.MB_OK, win.MB_OKCANCEL, win.MB_YESNO, win.MB_YESNOCANCEL}[buttons]
	if owner == nil {
		flags |= win.MB_TASKMODAL | win.MB_SETFOREGROUND
	}
	var r int32
	show := func() { r = win.MessageBox(ownerHWND(owner), message, title, flags) }
	if apphost.Running() {
		apphost.Do(show)
	} else {
		show()
	}
	switch r {
	case 1:
		return ButtonOK
	case 6:
		return ButtonYes
	case 7:
		return ButtonNo
	}
	switch buttons {
	case ButtonsOK:
		return ButtonOK
	case ButtonsYesNo:
		return ButtonNo
	}
	return ButtonCancel
}
