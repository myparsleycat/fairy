//go:build fairydebug

package fairy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/myparsleycat/fairy/keyboard"
)

// The automation hooks in this file read FAIRY_SCRIPT, FAIRY_CLOCK, FAIRY_SNAPSHOT
// and FAIRY_SNAPSHOT_DELAY. They are built only with -tags fairydebug, so a
// released app cannot be driven through its environment.

// runScriptFromEnv drives the window from FAIRY_SCRIPT, a file of commands, one per line:
//
//	move X Y | click X Y [shift|ctrl|alt...] | dblclick X Y | rclick X Y [mods]
//	down X Y [right|middle] [mods] | up X Y [right|middle]
//	wheel X Y NOTCHES [mods] | hwheel X Y NOTCHES | type TEXT | key NAME [shift|ctrl|alt...]
//	tdown X Y | tmove X Y | tup X Y
//	resize W H | maximize | wait DURATION | snap FILE.png | quit
//	dragfiles X Y PATH... | dropfiles X Y | dragleave
//	tree FILE.json | clock DURATION|off | step N
//
// The modifier keys given to down stay held until up. Wheel notches are
// positive away from the user (up) and, for hwheel, to the right. tdown,
// tmove and tup are one touch contact.
// Coordinates are logical pixels. In place of "X Y", "@KEY" names the center
// of the topmost element with that Key, and "@KEY+DX,DY" a point offset from
// it. Input goes through the same handlers as real input, so scripts can check
// behavior without a person at the keyboard.
//
// tree writes the element tree as JSON: each element's id, key, kind, role,
// accessible name, text, rectangle and state. clock switches to a virtual
// clock that advances by DURATION per frame, and only in frames run by step:
// animations, pulses, spinners, animated images and scroll bar fades then
// look the same on every run. FAIRY_CLOCK=fixed (1/60 s) or a duration turns
// it on from the start. Timers of the app keep wall-clock time.
func (w *Window) runScriptFromEnv() {
	path := os.Getenv("FAIRY_SCRIPT")
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("fairy: script: %v", err)
		return
	}
	switch v := os.Getenv("FAIRY_CLOCK"); v {
	case "":
	case "fixed":
		w.setClock(time.Second / 60)
	default:
		d, err := time.ParseDuration(v)
		if err != nil {
			log.Printf("fairy: FAIRY_CLOCK: %v", err)
		}
		w.setClock(d)
	}
	w.scripted = true
	w.ignoreMouseLeave()
	go func() {
		sc := bufio.NewScanner(strings.NewReader(string(data)))
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			f := strings.Fields(line)
			if f[0] == "wait" {
				if len(f) < 2 {
					log.Printf("fairy: script %q: wait needs a duration", line)
					continue
				}
				d, err := time.ParseDuration(f[1])
				if err != nil {
					log.Printf("fairy: script %q: %v", line, err)
					continue
				}
				time.Sleep(d)
				continue
			}
			done := make(chan struct{})
			w.Post(func() {
				defer close(done)
				if err := w.scriptStep(f, line); err != nil {
					log.Printf("fairy: script %q: %v", line, err)
				}
			})
			<-done
			if f[0] == "snap" || f[0] == "quit" {
				// Let the snapshot frame render before continuing.
				time.Sleep(50 * time.Millisecond)
			} else {
				time.Sleep(16 * time.Millisecond)
			}
		}
	}()
}

const scriptTouchID = 1

func (w *Window) scriptStep(f []string, line string) error {
	if len(f) > 1 && strings.HasPrefix(f[1], "@") {
		x, y, err := w.scriptPoint(f[1][1:])
		if err != nil {
			return err
		}
		xs, ys := strconv.FormatFloat(float64(x), 'f', 2, 32), strconv.FormatFloat(float64(y), 'f', 2, 32)
		f = append([]string{f[0], xs, ys}, f[2:]...)
	}
	args := scriptArgs[f[0]]
	if len(f)-1 < args.min {
		return fmt.Errorf("%s needs %d arguments", f[0], args.min)
	}
	var a [3]float32
	for i := range args.nums {
		v, err := strconv.ParseFloat(f[i+1], 32)
		if err != nil {
			return err
		}
		a[i] = float32(v)
	}
	num := func(i int) float32 { return a[i-1] }
	// Modifier keys that down holds stay held through other commands until up.
	held := w.scriptMods
	var heldMods keyboard.Mods
	if held != nil {
		heldMods = *held
	}
	switch f[0] {
	case "move":
		w.onMouseMove(num(1), num(2))
	case "down":
		b, rest := scriptButton(f[3:])
		if len(rest) > 0 {
			m := scriptMods(rest)
			w.scriptMods = &m
		}
		w.onMouseMove(num(1), num(2))
		w.onButtonDown(num(1), num(2), b)
	case "up":
		b, _ := scriptButton(f[3:])
		w.onMouseMove(num(1), num(2))
		w.onButtonUp(num(1), num(2), b)
		w.scriptMods = nil
	case "click", "rclick":
		b := 0
		if f[0] == "rclick" {
			b = 1
		}
		m := scriptMods(f[3:]) | heldMods
		w.scriptMods = &m
		w.onMouseMove(num(1), num(2))
		w.onButtonDown(num(1), num(2), b)
		w.onButtonUp(num(1), num(2), b)
		w.scriptMods = held
	case "dblclick":
		for range 2 {
			w.onMouseMove(num(1), num(2))
			w.onButtonDown(num(1), num(2), 0)
			w.onButtonUp(num(1), num(2), 0)
		}
	case "wheel", "hwheel":
		m := scriptMods(f[4:]) | heldMods
		w.scriptMods = &m
		// A fraction of a notch is a touchpad step.
		n := num(3)
		precise := n != float32(int(n))
		if f[0] == "hwheel" {
			w.onWheel(num(1), num(2), -n, true, precise)
		} else {
			w.onWheel(num(1), num(2), n, false, precise)
		}
		w.scriptMods = held
	case "tdown":
		w.onTouchDown(scriptTouchID, num(1), num(2), w.now())
	case "tmove":
		w.onTouchMove(scriptTouchID, num(1), num(2), w.now())
	case "tup":
		w.onTouchUp(scriptTouchID, num(1), num(2), w.now())
	case "type":
		text := strings.TrimPrefix(line[len("type"):], " ")
		for _, r := range text {
			w.onTypedRune(r)
		}
	case "key":
		k, ok := keyNames[strings.ToLower(f[1])]
		if !ok && len(f[1]) == 1 {
			k, ok = keyboard.Key(strings.ToUpper(f[1])[0]), true
		}
		if !ok {
			return fmt.Errorf("unknown key %s", f[1])
		}
		m := scriptMods(f[2:]) | heldMods
		text := ""
		if len(f[1]) == 1 {
			text = f[1]
			if m&keyboard.ModShift != 0 {
				text = strings.ToUpper(text)
			}
		} else if k == keyboard.KeySpace {
			text = " "
		}
		w.scriptMods = &m
		w.onKeyDown(k, false, text)
		w.scriptMods = held
	case "dragfiles":
		if w.drag == nil {
			w.fileDragEnter(f[3:], num(1), num(2))
		} else {
			w.fileDragOver(num(1), num(2))
		}
	case "dropfiles":
		w.fileDrop(nil, num(1), num(2))
	case "dragleave":
		w.fileDragLeave()
	case "resize":
		w.resizeClient(num(1), num(2))
	case "maximize":
		w.ToggleMaximize()
	case "snap":
		w.SaveSnapshot(f[1], nil)
	case "quit":
		w.Close()
	case "tree":
		return w.writeTree(f[1])
	case "clock":
		if len(f) < 2 || f[1] == "off" {
			w.setClock(0)
			return nil
		}
		d, err := time.ParseDuration(f[1])
		if err != nil {
			return err
		}
		w.setClock(d)
	case "step":
		n, err := strconv.Atoi(f[1])
		if err != nil {
			return err
		}
		w.step(n)
	default:
		return fmt.Errorf("unknown command")
	}
	return nil
}

// scriptArgs gives the least number of arguments of each command and how
// many of the first ones are numbers.
var scriptArgs = map[string]struct{ min, nums int }{
	"move": {2, 2}, "down": {2, 2}, "up": {2, 2}, "click": {2, 2}, "rclick": {2, 2}, "dblclick": {2, 2},
	"wheel": {3, 3}, "hwheel": {3, 3}, "tdown": {2, 2}, "tmove": {2, 2}, "tup": {2, 2}, "key": {1, 0}, "dragfiles": {2, 2}, "dropfiles": {2, 2},
	"resize": {2, 2}, "snap": {1, 0}, "tree": {1, 0}, "step": {1, 0},
}

var reOffset = regexp.MustCompile(`\+(-?[0-9.]+),(-?[0-9.]+)$`)

// scriptPoint resolves "KEY" or "KEY+DX,DY" to a point in the window.
func (w *Window) scriptPoint(s string) (float32, float32, error) {
	var dx, dy float64
	if m := reOffset.FindStringSubmatchIndex(s); m != nil {
		dx, _ = strconv.ParseFloat(s[m[2]:m[3]], 32)
		dy, _ = strconv.ParseFloat(s[m[4]:m[5]], 32)
		s = s[:m[0]]
	}
	return w.keyPoint(s, float32(dx), float32(dy))
}

var keyNames = map[string]keyboard.Key{
	"backspace": keyboard.KeyBackspace, "tab": keyboard.KeyTab, "enter": keyboard.KeyEnter, "escape": keyboard.KeyEscape,
	"space": keyboard.KeySpace, "pageup": keyboard.KeyPageUp, "pagedown": keyboard.KeyPageDown, "end": keyboard.KeyEnd,
	"home": keyboard.KeyHome, "left": keyboard.KeyLeft, "up": keyboard.KeyUp, "right": keyboard.KeyRight, "down": keyboard.KeyDown,
	"delete": keyboard.KeyDelete, "insert": keyboard.KeyInsert,
}

func init() {
	for i := range 24 {
		keyNames["f"+strconv.Itoa(i+1)] = keyboard.KeyF1 + keyboard.Key(i)
	}
}

// scriptButton reads an optional button name and returns the rest.
func scriptButton(f []string) (int, []string) {
	if len(f) > 0 {
		switch f[0] {
		case "left":
			return 0, f[1:]
		case "right":
			return 1, f[1:]
		case "middle":
			return 2, f[1:]
		}
	}
	return 0, f
}

func scriptMods(names []string) keyboard.Mods {
	var m keyboard.Mods
	for _, name := range names {
		switch name {
		case "shift":
			m |= keyboard.ModShift
		case "ctrl":
			m |= keyboard.ModCtrl
		case "alt":
			m |= keyboard.ModAlt
		}
	}
	return m
}

// snapshotFromEnv supports FAIRY_SNAPSHOT=out.png: render, save the frame, then close.
// FAIRY_SNAPSHOT_DELAY (a Go duration) waits before capturing.
func (w *Window) snapshotFromEnv() {
	path := os.Getenv("FAIRY_SNAPSHOT")
	if path == "" {
		return
	}
	delay, _ := time.ParseDuration(os.Getenv("FAIRY_SNAPSHOT_DELAY"))
	go func() {
		time.Sleep(delay)
		w.Post(func() {
			w.SaveSnapshot(path, func(error) { w.Close() })
		})
	}()
}

func (w *Window) setClock(dt time.Duration) {
	if dt <= 0 {
		w.clock = nil
		return
	}
	// Paint-time clocks measure from pulseEpoch, so starting there makes
	// their phases the same on every run.
	w.clock = &scriptClock{dt: dt, now: pulseEpoch}
	w.caretOn = true
}

// step runs n frames on the virtual clock.
func (w *Window) step(n int) {
	if w.clock == nil {
		w.setClock(time.Second / 60)
	}
	w.clock.stepping = true
	for range n {
		w.renderFrame()
	}
	w.clock.stepping = false
}

// treeNode is an element in the JSON tree that the "tree" script command writes.
type treeNode struct {
	ID       string      `json:"id"`
	Key      string      `json:"key,omitempty"`
	Kind     string      `json:"kind"`
	Role     string      `json:"role,omitempty"`
	Label    string      `json:"label,omitempty"`
	Text     string      `json:"text,omitempty"`
	Rect     [4]float32  `json:"rect"`
	Culled   bool        `json:"culled,omitempty"`
	Focused  bool        `json:"focused,omitempty"`
	Hovered  bool        `json:"hovered,omitempty"`
	Disabled bool        `json:"disabled,omitempty"`
	Leaving  bool        `json:"leaving,omitempty"`
	Children []*treeNode `json:"children,omitempty"`
}

var kindNames = [...]string{"box", "text", "image", "input", "icon", "spinner", "surface", "video", "webview", "arc"}

var roleNames = map[Role]string{
	RoleNone: "none", RoleHidden: "hidden", RoleGroup: "group", RoleButton: "button", RoleLink: "link",
	RoleCheckBox: "checkbox", RoleSwitch: "switch", RoleRadio: "radio", RoleText: "text", RoleImage: "image",
	RoleEdit: "edit", RoleComboBox: "combobox", RoleSlider: "slider", RoleProgressBar: "progressbar",
	RoleList: "list", RoleListItem: "listitem", RoleTree: "tree", RoleTreeItem: "treeitem", RoleMenu: "menu",
	RoleMenuItem: "menuitem", RoleMenuBar: "menubar", RoleTabList: "tablist", RoleTab: "tab", RoleToolBar: "toolbar",
	RoleStatusBar: "statusbar", RoleDialog: "dialog", RoleSeparator: "separator", RoleDocument: "document",
	RoleGrid: "grid", RoleRow: "row", RoleGridCell: "gridcell", RoleColumnHeader: "columnheader",
}

func (w *Window) treeOf(e *El, leaving bool) *treeNode {
	n := &treeNode{
		ID:       strconv.FormatUint(e.id, 16),
		Key:      e.key,
		Kind:     kindNames[e.kind],
		Rect:     [4]float32{e.absX, e.absY, e.w, e.h},
		Culled:   e.culled,
		Focused:  e.id == w.focused,
		Hovered:  w.hoverIDs[e.id],
		Disabled: e.disabled(),
		Leaving:  leaving,
	}
	if r := e.a11yRole(); r != RoleNone {
		n.Role = roleNames[r]
		n.Label = e.a11yName(r)
	}
	if e.kind == kindText || e.kind == kindInput {
		t := e.text
		if e.input != nil {
			t = e.input.value
			if e.input.password {
				t = ""
			}
		}
		if r := []rune(t); len(r) > 80 {
			t = string(r[:80]) + "…"
		}
		n.Text = t
	}
	for _, c := range e.children {
		n.Children = append(n.Children, w.treeOf(c, leaving))
	}
	return n
}

// writeTree writes the element tree as placed in the last frame: the root,
// then each overlay and popover, then elements still running their exit.
func (w *Window) writeTree(path string) error {
	var roots []*treeNode
	if w.root != nil {
		roots = append(roots, w.treeOf(w.root, false))
	}
	for _, l := range w.layers {
		roots = append(roots, w.treeOf(l.root, false))
	}
	for _, g := range w.ghosts {
		roots = append(roots, w.treeOf(g.el, true))
	}
	data, err := json.MarshalIndent(roots, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// keyPoint returns the center of the topmost element with key, plus an
// offset, for scripts that name elements instead of coordinates.
func (w *Window) keyPoint(key string, dx, dy float32) (float32, float32, error) {
	for i := len(w.hits) - 1; i >= 0; i-- {
		if e := w.hits[i].el; e.key == key {
			return e.absX + e.w/2 + dx, e.absY + e.h/2 + dy, nil
		}
	}
	var found *El
	var walk func(*El)
	walk = func(e *El) {
		if e.key == key && !e.culled {
			found = e
		}
		for _, c := range e.children {
			walk(c)
		}
	}
	if w.root != nil {
		walk(w.root)
	}
	for _, l := range w.layers {
		walk(l.root)
	}
	if found == nil {
		return 0, 0, fmt.Errorf("no element with key %q", key)
	}
	return found.absX + found.w/2 + dx, found.absY + found.h/2 + dy, nil
}
