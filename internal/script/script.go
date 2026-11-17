// Package script parses the commands of a FAIRY_SCRIPT file.
package script

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/myparsleycat/fairy/keyboard"
)

// Command is one line of a script.
type Command struct {
	Name string
	Args []string // the fields after the name, with "@KEY" replaced by "X Y" once resolved
	// Key, DX and DY are set when the command names a point as "@KEY" or
	// "@KEY+DX,DY": the center of the topmost element with that Key, plus an
	// offset. The caller resolves it with At.
	Key    string
	DX, DY float32
	Line   string // the whole line
	nums   [3]float32
}

// args gives the least number of arguments of each command and how many of
// the first ones are numbers.
var args = map[string]struct{ min, nums int }{
	"move": {2, 2}, "down": {2, 2}, "up": {2, 2}, "click": {2, 2}, "rclick": {2, 2}, "dblclick": {2, 2},
	"wheel": {3, 3}, "hwheel": {3, 3}, "tdown": {2, 2}, "tmove": {2, 2}, "tup": {2, 2}, "key": {1, 0}, "dragfiles": {2, 2}, "dropfiles": {2, 2},
	"resize": {2, 2}, "snap": {1, 0}, "tree": {1, 0}, "step": {1, 0},
}

var reOffset = regexp.MustCompile(`\+(-?[0-9.]+),(-?[0-9.]+)$`)

// Parse splits line, which holds at least one field, into a command. A point
// named by "@KEY" is left for At.
func Parse(line string) Command {
	f := strings.Fields(line)
	c := Command{Name: f[0], Args: f[1:], Line: line}
	if len(f) > 1 && strings.HasPrefix(f[1], "@") {
		s := f[1][1:]
		if m := reOffset.FindStringSubmatchIndex(s); m != nil {
			dx, _ := strconv.ParseFloat(s[m[2]:m[3]], 32)
			dy, _ := strconv.ParseFloat(s[m[4]:m[5]], 32)
			c.DX, c.DY = float32(dx), float32(dy)
			s = s[:m[0]]
		}
		c.Key = s
		c.Args = append([]string{"", ""}, f[2:]...)
	}
	return c
}

// At sets the point a command named by Key to (x, y), as if the script had
// given it with two decimals.
func (c *Command) At(x, y float32) {
	c.Args[0] = strconv.FormatFloat(float64(x), 'f', 2, 32)
	c.Args[1] = strconv.FormatFloat(float64(y), 'f', 2, 32)
}

// Check reads the numbers a command takes and reports a missing argument.
// The Key of the command must be resolved first.
func (c *Command) Check() error {
	a := args[c.Name]
	if len(c.Args) < a.min {
		return fmt.Errorf("%s needs %d arguments", c.Name, a.min)
	}
	for i := range a.nums {
		v, err := strconv.ParseFloat(c.Args[i], 32)
		if err != nil {
			return err
		}
		c.nums[i] = float32(v)
	}
	return nil
}

// Num returns the i-th argument, counted from 1, read as a number by Check.
func (c *Command) Num(i int) float32 { return c.nums[i-1] }

// From returns the arguments from the i-th on, counted from 1.
func (c *Command) From(i int) []string {
	if i-1 >= len(c.Args) {
		return nil
	}
	return c.Args[i-1:]
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

// Key returns the key a key command names: a name such as "enter" or "f5",
// or a single letter or digit.
func Key(name string) (keyboard.Key, bool) {
	if k, ok := keyNames[strings.ToLower(name)]; ok {
		return k, true
	}
	if len(name) == 1 {
		return keyboard.Key(strings.ToUpper(name)[0]), true
	}
	return 0, false
}

// Button reads an optional button name and returns the rest.
func Button(f []string) (int, []string) {
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

// Mods returns the modifier keys named in names.
func Mods(names []string) keyboard.Mods {
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
