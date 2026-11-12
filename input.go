package fairy

import (
	"time"

	"github.com/myparsleycat/fairy/internal/edit"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/motion"
)

type inputProps struct {
	value       string
	onChange    func(string)
	onSubmit    func(string)
	onKey       func(keyboard.KeyEvent) bool
	onPaste     func() bool
	placeholder string
	password    bool
	multiline   bool
	selColor    Color
	caretColor  Color
	phColor     Color
	spell       *bool
}

// Input is an editable text field. It is controlled: value is the current text
// and onChange receives every edit; store the new value in your state so the
// next view shows it. Without onChange the field is read-only but selectable.
func Input(value string, onChange func(string)) *El {
	e := newEl(kindInput)
	e.input = &inputProps{value: value, onChange: onChange}
	e.cursor = CursorText
	e.focusable = true
	return e
}

// Placeholder shows hint text while an Input is empty.
func (e *El) Placeholder(s string) *El {
	if e.input != nil {
		e.input.placeholder = s
	}
	return e
}

// PlaceholderColor sets the placeholder color.
func (e *El) PlaceholderColor(c Color) *El {
	if e.input != nil {
		e.input.phColor = c
	}
	return e
}

// OnSubmit runs when Enter is pressed in a single-line Input.
func (e *El) OnSubmit(f func(string)) *El {
	if e.input != nil {
		e.input.onSubmit = f
	}
	return e
}

// OnInputKey receives key presses in an Input before the Input edits with
// them; returning true skips the Input's own handling, so that Enter can send
// a message while Shift+Enter still inserts a newline. Keys that an input
// method is composing with never arrive, including the Enter that confirms a
// composition.
func (e *El) OnInputKey(f func(keyboard.KeyEvent) bool) *El {
	if e.input != nil {
		e.input.onKey = f
	}
	return e
}

// OnPaste runs when the user pastes into an editable Input (Ctrl+V or
// Shift+Insert), before the clipboard text is inserted. Returning true skips
// the insertion, for example after taking an image with Window.ClipboardImage.
func (e *El) OnPaste(f func() bool) *El {
	if e.input != nil {
		e.input.onPaste = f
	}
	return e
}

// Password masks the Input's text.
func (e *El) Password() *El {
	if e.input != nil {
		e.input.password = true
	}
	return e
}

// Multiline lets an Input hold several lines; Enter inserts a newline.
func (e *El) Multiline() *El {
	if e.input != nil {
		e.input.multiline = true
	}
	return e
}

// SpellCheck turns spell checking of an Input on or off. It is on by default
// for Multiline inputs and never runs on Password or read-only inputs.
// Misspelled words get a wavy underline, and the context menu offers
// replacements; see Options.SpellLanguage.
func (e *El) SpellCheck(on bool) *El {
	if e.input != nil {
		e.input.spell = &on
	}
	return e
}

// SelectionColor sets the selection highlight of an Input.
func (e *El) SelectionColor(c Color) *El {
	if e.input != nil {
		e.input.selColor = c
	}
	return e
}

// CaretColor sets the caret color of an Input.
func (e *El) CaretColor(c Color) *El {
	if e.input != nil {
		e.input.caretColor = c
	}
	return e
}

// elState is retained across frames for elements with the same identity.
type elState struct {
	frame   int // build generation that last saw the element
	born    int // build generation that created the state
	scrollX float32
	scrollY float32
	// Wheel scrolling animates toward these; other scrolls set both.
	scrollTX, scrollTY float32
	smooth             bool
	endY               float32 // the bottom offset when last placed, for StickToBottom
	reveal             int     // the row ScrollToRow is bringing into view
	revealing          bool
	revealEdge         uint8   // how the row is lined up with the view
	revealOff          float32 // for revealKeep, the row's distance below the top of the view
	revealInstant      bool    // the ScrollToRow request jumps instead of gliding
	toBottom           bool    // a ScrollToBottom request waits to be applied
	glide              bool    // smooth scrolling runs on motionGlide, as for ScrollToRow
	revealGlide        bool    // gliding to s.reveal, retargeted as rows are measured
	bottomGlide        bool    // gliding to the bottom, retargeted as content grows
	fling              bool    // a touch pan let go and the view coasts at flingVX, flingVY
	flingVX, flingVY   float32 // logical pixels per second
	barScroll          float32 // scrollY when the thumb was last placed
	barScrollX         float32 // scrollX when the thumb was last placed
	scrolledAt         time.Time
	layoutW, layoutH   float32 // the size OnLayout last reported
	laidOut            bool
	edit               *editState
	rows               *rowHeights // VirtualListMeasured row heights
	anims              [animCount]*motion.Track
}

const (
	revealUndecided = iota
	revealTop
	revealBottom
	revealKeep
)

func (s *elState) track(p animProp, n int, eps float32) *motion.Track {
	if s.anims[p] == nil {
		s.anims[p] = motion.NewTrack(n, eps)
	}
	return s.anims[p]
}

type editState struct {
	edit.Buffer
	comp       string // IME composition
	compCursor int
	scrollX    float32
	scrollY    float32
	geom       *inputGeom
	upstream   bool // the caret sits at the end of a wrapped line, not the start of the next
	spell      *Latest[spellResult]
}

// keyProcess is VK_PROCESSKEY: the key press an input method is handling.
// Its real key may be Enter confirming a composition.
const keyProcess keyboard.Key = 0xE5

// inputHookSees reports whether an Input's OnInputKey handler receives ev.
func inputHookSees(ev keyboard.KeyEvent, composing bool) bool {
	return !composing && ev.Key != keyProcess
}
