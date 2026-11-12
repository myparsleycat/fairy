package fairy

// Progress is the state of a window's taskbar button progress bar.
type Progress uint8

const (
	ProgressNone          Progress = iota // no progress bar
	ProgressNormal                        // green bar at value
	ProgressPaused                        // yellow bar at value
	ProgressError                         // red bar at value
	ProgressIndeterminate                 // moving bar, value ignored
)
