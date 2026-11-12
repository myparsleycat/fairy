package dialog

import (
	"errors"

	"github.com/myparsleycat/fairy"
)

// errNoBackend is returned until the macOS dialogs exist.
var errNoBackend = errors.New("dialog: not implemented on macOS yet")

// OpenFiles asks for one file, or several with Multiple.
func OpenFiles(owner *fairy.Window, d FileOptions) ([]string, error) { return nil, errNoBackend }

// OpenFolders asks for one folder, or several with Multiple.
func OpenFolders(owner *fairy.Window, d FileOptions) ([]string, error) { return nil, errNoBackend }

// SaveFile asks for a file name to save to.
func SaveFile(owner *fairy.Window, d FileOptions) (string, error) { return "", errNoBackend }

// Message shows a message box and returns the button pressed. Until macOS
// has message boxes it returns the button Escape would.
func Message(owner *fairy.Window, title, message string, kind MessageKind, buttons Buttons) Button {
	return [...]Button{ButtonOK, ButtonCancel, ButtonNo, ButtonCancel}[buttons]
}
