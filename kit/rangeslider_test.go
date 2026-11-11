package kit

import "testing"

func TestRangeSetKeepsThumbsOrdered(t *testing.T) {
	value := [2]float64{0.2, 0.6}
	tests := []struct {
		name  string
		thumb int
		v     float64
		want  [2]float64
	}{
		{"lower stops at upper", 0, 0.9, [2]float64{0.6, 0.6}},
		{"upper stops at lower", 1, 0.1, [2]float64{0.2, 0.2}},
		{"lower clamps to min", 0, -1, [2]float64{0, 0.6}},
		{"upper clamps to max", 1, 2, [2]float64{0.2, 1}},
		{"rounds to step", 1, 0.737, [2]float64{0.2, 0.75}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rangeSet(value, tt.thumb, tt.v, 0, 1, 0.25); got != tt.want {
				t.Fatalf("rangeSet(%v, %d, %v) = %v, want %v", value, tt.thumb, tt.v, got, tt.want)
			}
		})
	}
}

func TestRangeThumbPicksNearer(t *testing.T) {
	tests := []struct {
		value [2]float64
		v     float64
		want  int
	}{
		{[2]float64{0.2, 0.6}, 0.1, 0},
		{[2]float64{0.2, 0.6}, 0.5, 1},
		{[2]float64{0.25, 0.75}, 0.5, 0},
		{[2]float64{0.5, 0.5}, 0.3, 0},
		{[2]float64{0.5, 0.5}, 0.7, 1},
		// Both thumbs at the press: the drag direction decides.
		{[2]float64{0.5, 0.5}, 0.5, -1},
		{[2]float64{1, 1}, 1, -1},
	}
	for _, tt := range tests {
		if got := rangeThumb(tt.value, tt.v); got != tt.want {
			t.Errorf("rangeThumb(%v, %v) = %d, want %d", tt.value, tt.v, got, tt.want)
		}
	}
}
