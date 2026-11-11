package fairy

import (
	"testing"

	"github.com/myparsleycat/fairy/internal/render"
	"github.com/myparsleycat/fairy/internal/text"
)

func TestTooltipArrowSharesBackgroundOpacity(t *testing.T) {
	r, err := render.NewOffscreen(96, 96)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	ts, err := text.NewSystem(r.Atlas)
	if err != nil {
		t.Fatal(err)
	}
	defer ts.Release()
	w := Window{
		opts: Options{Font: "Segoe UI"}, ts: ts, scale: 1, width: 96, height: 96,
		tipShown: true, tipText: "X",
	}
	style := TooltipStyle{Fill: Hex(0xffffff).Alpha(0.5), Radius: 4, PadX: 16, PadY: 8, Gap: 4, Arrow: true}
	for _, tc := range []struct {
		name              string
		anchorY           float32
		body, join, arrow int
		outside           int
	}{
		{name: "above", anchorY: 64, body: 50, join: 58, arrow: 62, outside: 67},
		{name: "below", anchorY: 8, body: 30, join: 21, arrow: 17, outside: 12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w.tipAnchor = rectF{40, tc.anchorY, 16, 8}
			w.scene.Reset()
			if !w.paintStyledTooltip(&style) {
				t.Fatal("could not paint tooltip")
			}
			if err := r.Draw(&w.scene, [4]float32{}); err != nil {
				t.Fatal(err)
			}
			img, err := r.ReadTarget()
			if err != nil {
				t.Fatal(err)
			}
			for _, y := range []int{tc.body, tc.join, tc.arrow} {
				if a := img.NRGBAAt(48, y).A; a < 127 || a > 128 {
					t.Errorf("alpha at (48, %d) = %d, want half opacity", y, a)
				}
			}
			if a := img.NRGBAAt(48, tc.outside).A; a != 0 {
				t.Errorf("alpha outside tooltip = %d, want 0", a)
			}
		})
	}
}
