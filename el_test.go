package fairy

import "testing"

// Builders that show nothing return nil; passing one to a container constructor must not leave a nil child
// for the frame to dereference.
func TestContainerConstructorsSkipNilChildren(t *testing.T) {
	for name, e := range map[string]*El{
		"Box": Box(nil, Text("a")),
		"Col": Col(Text("a"), nil),
		"Row": Row(nil, Text("a"), nil),
	} {
		if len(e.children) != 1 || e.children[0] == nil {
			t.Fatalf("%s kept %d children, want the one non-nil child", name, len(e.children))
		}
	}
}
