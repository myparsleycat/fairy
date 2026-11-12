package fairy

// MenuItem is an entry of a native popup menu.
type MenuItem struct {
	Label     string
	OnClick   func()
	Disabled  bool
	Checked   bool
	Separator bool       // draws a line; other fields are ignored
	Items     []MenuItem // makes the item a submenu
}
