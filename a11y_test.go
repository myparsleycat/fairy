package fairy

import (
	"fmt"
	"strings"
	"testing"

	"github.com/myparsleycat/fairy/svg"
)

// prepareIDs gives a tree the identities and parents that prepare assigns.
func prepareIDs(e, parent *El, next *uint64) {
	*next++
	e.id, e.parent = *next, parent
	e.applyDisabled(parent)
	for _, c := range e.children {
		prepareIDs(c, e, next)
	}
}

// a11yDump renders what assistive technology sees below id.
func a11yDump(t *a11yTree, id uint64) string {
	var parts []string
	for _, k := range t.kids[id] {
		e, r := t.el[k], t.role[k]
		s := fmt.Sprintf("%d:%s", r, e.a11yName(r))
		if e.disabled() {
			s += "(disabled)"
		}
		if sub := a11yDump(t, k); sub != "" {
			s += "[" + sub + "]"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}

func TestA11yTree(t *testing.T) {
	nop := func() {}
	root := Col(
		// A clickable row is one button named by its text; the icon is not exposed.
		Row(Icon(&svg.Vector{}), Text("Save")).OnClick(nop),
		// An icon-only button is named by its tooltip.
		Box(Icon(&svg.Vector{})).OnClick(nop).Tooltip("Settings"),
		// A clickable card holding its own control stays a group.
		Col(Text("Mod"), Text("Open").OnClick(nop)).OnClick(nop),
		// RoleNone drops the row but keeps its children; RoleHidden drops the subtree.
		Row(Box().Role(RoleCheckBox).Checked(true).Label("Sync").OnClick(nop), Text("Sync").Role(RoleHidden)).
			Role(RoleNone).OnClick(nop),
		// Disabled reaches descendants and removes their handlers.
		Col(Text("Delete").OnClick(nop)).Disabled(true),
		Text("").OnClick(nil),
	)
	var next uint64
	prepareIDs(root, nil, &next)
	tree := buildA11yTree([]*El{root})

	want := fmt.Sprintf("%d:Save %d:Settings %d:[%d:Mod %d:Open] %d:Sync %d:Delete(disabled)",
		RoleButton, RoleButton, RoleGroup, RoleText, RoleButton, RoleCheckBox, RoleButton)
	if got := a11yDump(tree, 0); got != want {
		t.Errorf("tree:\n got %s\nwant %s", got, want)
	}
	del := root.children[4].children[0]
	if del.onClick != nil || del.a11yRole() != RoleButton {
		t.Errorf("disabled button: onClick kept=%v role=%d", del.onClick != nil, del.a11yRole())
	}
}
