package scroll

import "testing"

func TestStepTarget(t *testing.T) {
	// Rows 28 tall below 4 pixels of padding: content 288, view 100.
	var rows [][2]float32
	for i := range 10 {
		top := 4 + float32(i)*28
		rows = append(rows, [2]float32{top, top + 28})
	}
	// A 24 pixel heading among the rows: content 168.
	mixed := [][2]float32{{4, 28}, {28, 56}, {56, 84}, {84, 108}, {108, 136}, {136, 164}}
	tests := []struct {
		name  string
		spans [][2]float32
		y     float32
		maxY  float32
		up    bool
		want  float32
	}{
		{"down from the top shows the row under the arrow", rows, 0, 188, false, 12},
		{"down a row before the end", rows, 170, 188, false, 180},
		{"down to the end", rows, 180, 188, false, 188},
		{"up from the end shows the row under the arrow", rows, 188, 188, true, 176},
		{"up a row before the top", rows, 20, 188, true, 8},
		{"up to the top", rows, 8, 188, true, 0},
		{"down past rows of other heights", mixed, 0, 68, false, 8},
		{"no children", nil, 5, 0, false, 5},
	}
	for _, tt := range tests {
		if got := StepTarget(tt.spans, tt.y, 100, tt.maxY, 24, tt.up); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}
