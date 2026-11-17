package geom

// Fit is how an image fills a box. The values match fairy.Fit.
type Fit uint8

const (
	FitCover Fit = iota
	FitContain
	FitFill
)

// FitImage places an iw x ih image in box: the destination rectangle and the
// UV rectangle (x, y, w, h) to sample.
func FitImage(box Rect, iw, ih float32, fit Fit) (Rect, [4]float32) {
	dst := box
	uv := [4]float32{0, 0, 1, 1}
	switch fit {
	case FitCover:
		boxAR, imgAR := box.W/box.H, iw/ih
		if imgAR > boxAR {
			f := boxAR / imgAR
			uv = [4]float32{(1 - f) / 2, 0, f, 1}
		} else if imgAR < boxAR {
			f := imgAR / boxAR
			uv = [4]float32{0, (1 - f) / 2, 1, f}
		}
	case FitContain:
		sc := min(box.W/iw, box.H/ih)
		dw, dh := iw*sc, ih*sc
		dst = Rect{X: box.X + (box.W-dw)/2, Y: box.Y + (box.H-dh)/2, W: dw, H: dh}
	}
	return dst, uv
}
