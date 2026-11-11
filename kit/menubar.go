package kit

import (
	"strconv"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/motion"
	"github.com/myparsleycat/fairy/svg"
)

// MenubarMenu is one menu of a Menubar. Items runs only while the menu is open.
type MenubarMenu struct {
	Label string
	Icon  *svg.Vector
	Items func() []MenuItem
}

type menubarState struct {
	popupState
	menu    int  // the open menu, while open is set
	instant bool // the open menu replaced another one, so it appears without motion
	fromEnd bool // focus starts on the menu's last row
}

// Menubar is a row of triggers that open menus, as an application's menu
// bar. Clicking a trigger opens its menu and clicking it again closes it.
// While a menu is open, moving the pointer onto another trigger opens that
// one instead. The triggers are one focus group: Left and Right move between
// them, Down, Return and Space open the focused one, and Up opens it on its
// last row. In an open menu, Left and Right open the adjacent menu, except
// that Right on a row with a submenu opens the submenu and Left in a submenu
// closes it. Escape closes the menu and returns focus to its trigger. Check
// and radio items leave the menu open when chosen. id must be unique in the
// window.
func Menubar(id string, menus []MenubarMenu) *fairy.El {
	t := &current
	st := fairy.Local[menubarState]("kit.menubar:" + id)
	n := len(menus)
	if st.menu >= n {
		st.open = false
	}
	close := func() { st.open, st.sub = false, st.sub[:0] }
	open := func(i int, fromEnd bool) {
		st.instant = st.open && st.menu != i
		st.open, st.menu, st.sub, st.fromEnd = true, i, st.sub[:0], fromEnd
	}
	step := func(d int) { open((st.menu+d+n)%n, false) }
	bar := fairy.Row().H(32).Pad(3).Gap(2).Align(fairy.Center).Radius(t.RadiusLg()).Border(1, t.Border).
		Role(fairy.RoleMenuBar).FocusGroup()
	for i, m := range menus {
		expanded := st.open && st.menu == i
		trigger := fairy.Row().H(24).PadXY(6, 2).Gap(4).Align(fairy.Center).Radius(t.RadiusSm()).Medium().
			HoverFill(t.Muted).Role(fairy.RoleMenuItem).Key("kit.menubar:" + id + ":" + strconv.Itoa(i))
		if m.Icon != nil {
			trigger.Child(fairy.Icon(m.Icon).Size(14, 14))
		}
		trigger.Child(fairy.Text(m.Label).NoWrap())
		focusRing(trigger).Focusable().Cursor(fairy.CursorPointer).Expanded(expanded).
			OnClick(func() {
				if expanded {
					close()
				} else {
					open(i, false)
				}
			}).
			OnHover(func(in bool) {
				if in && st.open && st.menu != i {
					open(i, false)
				}
			}).
			OnKey(func(k keyboard.KeyEvent) bool {
				switch {
				case k.Is(keyboard.KeyDown, 0), k.Is(keyboard.KeyEnter, 0), k.Is(keyboard.KeySpace, 0):
					open(i, false)
				case k.Is(keyboard.KeyUp, 0):
					open(i, true)
				default:
					return false
				}
				return true
			})
		if expanded {
			trigger.Fill(t.Muted)
			o := menuOpts{bar: true, fromEnd: st.fromEnd, step: step}
			// One MatchID for all the menus: the menu being replaced goes at
			// once instead of running its exit, as Base UI switches menus.
			panel := popupPanel(false, menuItems(&st.popupState, 0, m.Items(), close, o)...).MinW(144).
				Role(fairy.RoleMenu).FocusGroup().MatchID("kit.menubar:" + id).AnimateLayout(motion.Instant)
			if st.instant {
				panel.Enter(fairy.Transition{Opacity: 1, Motion: motion.Instant})
			}
			trigger.Popover(panel, fairy.PopoverOptions{Gap: 8, AlignOffset: -4, OnDismiss: close})
		}
		bar.Child(trigger)
	}
	return bar
}
