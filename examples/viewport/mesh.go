package main

import "math"

// vertex is a corner of a cube face as the shaders read it.
type vertex struct{ pos, color [3]float32 }

// cameraRows is the camera matrix row by row, as the shaders read it.
type cameraRows struct{ rows [4][4]float32 }

// cameraMatrix returns the rows of the map from model space to clip space
// that viewer.project applies, with depth from near 1 to far 20. Direct3D
// and Metal share the clip space: y up, depth from 0 to 1.
func cameraMatrix(c camera, w, h float64) cameraRows {
	cy, sy := math.Cos(c.yaw), math.Sin(c.yaw)
	cp, sp := math.Cos(c.pitch), math.Sin(c.pitch)
	s := c.scale
	rx := [3]float64{s * cy, 0, s * sy}
	ry := [3]float64{s * sp * sy, s * cp, -s * sp * cy}
	rz := [3]float64{-s * cp * sy, s * sp, s * cp * cy}
	hw, hh := w/2, h/2
	focal := math.Min(w, h) * 0.9 * float64(c.zoom)
	px, py := float64(c.panX)/hw, float64(c.panY)/hh
	const near, far = 1.0, 20.0
	a, b := far/(far-near), -near*far/(far-near)
	row := func(k float64, r [3]float64, p float64) [4]float32 {
		// k*r·x + p*(rz·x + 5)
		return [4]float32{
			float32(k*r[0] + p*rz[0]), float32(k*r[1] + p*rz[1]), float32(k*r[2] + p*rz[2]), float32(p * 5),
		}
	}
	var m cameraRows
	m.rows[0] = row(focal/hw, rx, px)
	r1 := row(focal/hh, ry, py)
	m.rows[1] = [4]float32{-r1[0], -r1[1], -r1[2], -r1[3]}
	m.rows[2] = [4]float32{float32(a * rz[0]), float32(a * rz[1]), float32(a * rz[2]), float32(a*5 + b)}
	m.rows[3] = [4]float32{float32(rz[0]), float32(rz[1]), float32(rz[2]), 5}
	return m
}

func cubeMesh() ([]vertex, []uint16) {
	faces := []struct {
		n, u, v [3]float32
		color   [3]float32
	}{
		{[3]float32{0, 0, -1}, [3]float32{1, 0, 0}, [3]float32{0, 1, 0}, [3]float32{0.43, 0.36, 0.99}},
		{[3]float32{0, 0, 1}, [3]float32{-1, 0, 0}, [3]float32{0, 1, 0}, [3]float32{0.29, 0.24, 0.66}},
		{[3]float32{1, 0, 0}, [3]float32{0, 0, 1}, [3]float32{0, 1, 0}, [3]float32{0.95, 0.62, 0.20}},
		{[3]float32{-1, 0, 0}, [3]float32{0, 0, -1}, [3]float32{0, 1, 0}, [3]float32{0.62, 0.40, 0.13}},
		{[3]float32{0, -1, 0}, [3]float32{1, 0, 0}, [3]float32{0, 0, -1}, [3]float32{0.30, 0.78, 0.55}},
		{[3]float32{0, 1, 0}, [3]float32{1, 0, 0}, [3]float32{0, 0, 1}, [3]float32{0.18, 0.47, 0.33}},
	}
	var verts []vertex
	var idx []uint16
	for _, f := range faces {
		base := uint16(len(verts))
		for _, c := range [4][2]float32{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}} {
			var p [3]float32
			for i := range p {
				p[i] = f.n[i] + c[0]*f.u[i] + c[1]*f.v[i]
			}
			verts = append(verts, vertex{p, f.color})
		}
		// The view maps model x right and y down on screen, so for the face
		// toward the viewer at the start (-z: u right, v down) corners 0, 1,
		// 2 run clockwise on screen, and every face does when it is toward the
		// viewer: the front face of Direct3D and Metal, whose back-face
		// culling then drops the far faces.
		idx = append(idx, base, base+1, base+2, base, base+2, base+3)
	}
	return verts, idx
}
