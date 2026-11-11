package imaging

import (
	"image"
	"image/draw"
	"math"
)

// resampler scales whole images with a Catmull-Rom filter that widens with
// the reduction, as x/image/draw's CatmullRom does. It filters rows and then
// columns in 16-bit premultiplied fixed point, and keeps only the filtered
// rows that the column filter still needs instead of a full-height
// intermediate.
type resampler struct {
	sw, sh, dw, dh int
	x, y           filterTaps

	row     []uint16    // one source row, premultiplied as color.RGBA64
	row8    *image.RGBA // one source row, for the formats image/draw converts quickly
	palette [256][4]uint16
	ring    []int32 // filtered rows, y.most of them
	acc     []int64 // one output row, scaled by 1<<tapBits
}

type filterTaps struct {
	first []int   // first source index of each output
	at    []int   // each output's weights are w[at[i]:at[i+1]]
	w     []int32 // scaled by 1<<tapBits; each output's weights sum to exactly that
	most  int     // largest number of weights of one output
}

const tapBits = 14

func newResampler(sw, sh, dw, dh int) *resampler {
	return &resampler{sw: sw, sh: sh, dw: dw, dh: dh, x: newFilterTaps(sw, dw), y: newFilterTaps(sh, dh)}
}

// ResampleWork is the memory a resampler from sw x sh to dw x dh holds
// besides its output.
func ResampleWork(sw, sh, dw, dh int) int64 {
	if sw == dw && sh == dh {
		return 0
	}
	taps := func(s, d int) int64 { return int64(2*math.Ceil(2*max(1, float64(s)/float64(d))) + 1) }
	tx, ty := taps(sw, dw), taps(sh, dh)
	return int64(sw)*12 + ty*int64(dw)*16 + int64(dw)*32 + (tx*int64(dw)+ty*int64(dh))*4 + int64(dw+dh)*16 + 2048
}

func catmullRom(t float64) float64 {
	t = math.Abs(t)
	switch {
	case t < 1:
		return (1.5*t-2.5)*t*t + 1
	case t < 2:
		return ((-0.5*t+2.5)*t-4)*t + 2
	}
	return 0
}

func newFilterTaps(src, dst int) filterTaps {
	scale := float64(src) / float64(dst)
	stretch := max(1, scale)
	support := 2 * stretch
	t := filterTaps{first: make([]int, dst), at: make([]int, dst+1)}
	t.w = make([]int32, 0, dst*(int(math.Ceil(2*support))+1))
	var fw []float64
	for i := range dst {
		c := (float64(i)+0.5)*scale - 0.5
		lo := max(0, int(math.Floor(c-support))+1)
		hi := min(src-1, int(math.Ceil(c+support))-1)
		fw = fw[:0]
		var sum float64
		for j := lo; j <= hi; j++ {
			v := catmullRom((float64(j) - c) / stretch)
			fw = append(fw, v)
			sum += v
		}
		// The rounding error goes to the largest weight, so that a flat
		// area stays exactly flat.
		big, total := 0, int32(0)
		for k, v := range fw {
			q := int32(math.Round(v / sum * (1 << tapBits)))
			t.w = append(t.w, q)
			total += q
			if v > fw[big] {
				big = k
			}
		}
		t.w[t.at[i]+big] += 1<<tapBits - total
		t.first[i] = lo
		t.at[i+1] = len(t.w)
		t.most = max(t.most, len(fw))
	}
	return t
}

// scale returns src, whose size must be sw x sh, scaled to dw x dh, with
// premultiplied alpha.
func (r *resampler) scale(src image.Image) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, r.dw, r.dh))
	r.scaleInto(src, dst)
	return dst
}

// scaleInto is scale writing into dst, a dw x dh image at the origin.
func (r *resampler) scaleInto(src image.Image, dst *image.RGBA) {
	if r.row == nil {
		r.row = make([]uint16, r.sw*4)
		r.row8 = image.NewRGBA(image.Rect(0, 0, r.sw, 1))
		r.ring = make([]int32, r.y.most*r.dw*4)
		r.acc = make([]int64, r.dw*4)
	}
	if p, ok := src.(*image.Paletted); ok {
		r.palette = [256][4]uint16{}
		for i, c := range p.Palette[:min(len(p.Palette), 256)] {
			cr, cg, cb, ca := c.RGBA()
			r.palette[i] = [4]uint16{uint16(cr), uint16(cg), uint16(cb), uint16(ca)}
		}
	}
	n := r.dw * 4
	next := 0 // next source row to filter
	for y := range r.dh {
		first := r.y.first[y]
		ws := r.y.w[r.y.at[y]:r.y.at[y+1]]
		for ; next < first+len(ws); next++ {
			if next >= first {
				r.readRow(src, next)
				r.filterRow(r.ring[next%r.y.most*n:][:n])
			}
		}
		acc := r.acc
		clear(acc)
		for k, w := range ws {
			row := r.ring[(first+k)%r.y.most*n:][:n]
			for i, v := range row {
				acc[i] += int64(w) * int64(v)
			}
		}
		pack(dst.Pix[y*dst.Stride:][:n], acc)
	}
}

// readRow stores row y of src in r.row with the values color.RGBA64 holds.
func (r *resampler) readRow(src image.Image, y int) {
	b := src.Bounds()
	x0, y := b.Min.X, b.Min.Y+y
	out := r.row
	switch s := src.(type) {
	case *image.NRGBA:
		pix := s.Pix[s.PixOffset(x0, y):][:len(out)]
		for i := 0; i < len(pix); i += 4 {
			premultiply(out[i:i+4:i+4], pix[i], pix[i+1], pix[i+2], pix[i+3])
		}
	case *image.NYCbCrA:
		draw.Draw(r.row8, r.row8.Rect, &s.YCbCr, image.Pt(x0, y), draw.Src)
		pix := r.row8.Pix
		for x, a := range s.A[s.AOffset(x0, y):][:r.sw] {
			i := x * 4
			premultiply(out[i:i+4:i+4], pix[i], pix[i+1], pix[i+2], a)
		}
	case *image.Paletted:
		for x, c := range s.Pix[s.PixOffset(x0, y):][:r.sw] {
			copy(out[x*4:x*4+4], r.palette[c][:])
		}
	case *image.RGBA:
		for i, v := range s.Pix[s.PixOffset(x0, y):][:len(out)] {
			out[i] = uint16(v) * 0x101
		}
	case *image.YCbCr, *image.Gray, *image.CMYK:
		draw.Draw(r.row8, r.row8.Rect, src, image.Pt(x0, y), draw.Src)
		for i, v := range r.row8.Pix {
			out[i] = uint16(v) * 0x101
		}
	default:
		for x := range r.sw {
			cr, cg, cb, ca := src.At(x0+x, y).RGBA()
			o := out[x*4 : x*4+4 : x*4+4]
			o[0], o[1], o[2], o[3] = uint16(cr), uint16(cg), uint16(cb), uint16(ca)
		}
	}
}

// premultiply stores what color.NRGBA.RGBA returns.
func premultiply(o []uint16, r, g, b, a uint8) {
	if a == 255 {
		o[0], o[1], o[2], o[3] = uint16(r)*0x101, uint16(g)*0x101, uint16(b)*0x101, 0xffff
		return
	}
	a16 := uint32(a) * 0x101
	o[0] = uint16(uint32(r) * 0x101 * a16 / 0xffff)
	o[1] = uint16(uint32(g) * 0x101 * a16 / 0xffff)
	o[2] = uint16(uint32(b) * 0x101 * a16 / 0xffff)
	o[3] = uint16(a16)
}

func (r *resampler) filterRow(out []int32) {
	row := r.row
	for x := range r.dw {
		p := r.x.first[x] * 4
		var cr, cg, cb, ca int64
		for _, w := range r.x.w[r.x.at[x]:r.x.at[x+1]] {
			px := row[p : p+4 : p+4]
			cr += int64(w) * int64(px[0])
			cg += int64(w) * int64(px[1])
			cb += int64(w) * int64(px[2])
			ca += int64(w) * int64(px[3])
			p += 4
		}
		const half = 1 << (tapBits - 1)
		o := out[x*4 : x*4+4 : x*4+4]
		o[0] = int32((cr + half) >> tapBits)
		o[1] = int32((cg + half) >> tapBits)
		o[2] = int32((cb + half) >> tapBits)
		o[3] = int32((ca + half) >> tapBits)
	}
}

// pack writes the premultiplied sums in acc to out as 8-bit premultiplied
// RGBA. The filter's negative lobes can push a color above its alpha, which
// is clamped to the alpha like any other overshoot.
func pack(out []byte, acc []int64) {
	const half = 1 << (tapBits - 1)
	clamp := func(v int64) int64 { return max(0, min(0xffff, (v+half)>>tapBits)) }
	for i := 0; i < len(out); i += 4 {
		a := clamp(acc[i+3])
		o := out[i : i+4 : i+4]
		for c := range 3 {
			o[c] = byte((min(clamp(acc[i+c]), a)*255 + 0x7fff) / 0xffff)
		}
		o[3] = byte((a*255 + 0x7fff) / 0xffff)
	}
}
