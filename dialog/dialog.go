// Package dialog shows native file dialogs and message boxes.
package dialog

import "errors"

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
