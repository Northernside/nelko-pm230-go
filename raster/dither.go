package raster

import (
	"image"

	"github.com/Northernside/nelko-pm230-go/internal/enum"
)

type Dither uint8

const (
	FloydSteinberg Dither = iota // pillow's convert("1"), what the python driver did
	Atkinson
	Ordered
	Threshold
)

var dithers = enum.Names[Dither]{"floyd-steinberg", "atkinson", "ordered", "threshold"}

func DitherNames() []string               { return dithers.List() }
func ParseDither(s string) (Dither, bool) { return dithers.Parse(s, FloydSteinberg) }
func (d Dither) String() string           { return dithers.String(d) }

// in place, to pure 0/255, v >= threshold is white
func Apply(g *image.Gray, d Dither, threshold uint8) {
	w, h := g.Bounds().Dx(), g.Bounds().Dy()
	if w == 0 || h == 0 {
		return
	}

	t := int32(threshold)
	switch d {
	case Threshold:
		for y := range h {
			row := g.Pix[y*g.Stride : y*g.Stride+w]
			for x, v := range row {
				row[x] = pick(int32(v), t)
			}
		}

	case Ordered:
		for y := range h {
			row := g.Pix[y*g.Stride : y*g.Stride+w]
			for x, v := range row {
				m := (int32(bayer8[y&7][x&7])*256+128)/64 + t - 128
				row[x] = pick(int32(v), m)
			}
		}

	case Atkinson:
		rows := [3][]int32{make([]int32, w+4), make([]int32, w+4), make([]int32, w+4)}
		for y := range h {
			cur, next, after := rows[0], rows[1], rows[2]
			row := g.Pix[y*g.Stride : y*g.Stride+w]
			for x, v := range row {
				l := clamp(int32(v) + cur[x+2])
				out := pick(l, t)
				row[x] = out
				e := (l - int32(out)) / 8
				cur[x+3] += e
				cur[x+4] += e
				next[x+1] += e
				next[x+2] += e
				next[x+3] += e
				after[x+2] += e
			}

			clear(cur)
			rows = [3][]int32{next, after, cur}
		}

	default:
		cur, next := make([]int32, w+2), make([]int32, w+2)
		for y := range h {
			row := g.Pix[y*g.Stride : y*g.Stride+w]
			for x, v := range row {
				l := clamp(int32(v) + cur[x+1])
				out := pick(l, t)
				row[x] = out
				e := l - int32(out)
				cur[x+2] += e * 7 / 16
				next[x] += e * 3 / 16
				next[x+1] += e * 5 / 16
				next[x+2] += e / 16
			}

			clear(cur)
			cur, next = next, cur
		}
	}
}

func pick(v, t int32) uint8 {
	if v >= t {
		return 255
	}

	return 0
}

func clamp(v int32) int32 {
	return min(max(v, 0), 255)
}

var bayer8 = [8][8]uint8{
	{0, 32, 8, 40, 2, 34, 10, 42},
	{48, 16, 56, 24, 50, 18, 58, 26},
	{12, 44, 4, 36, 14, 46, 6, 38},
	{60, 28, 52, 20, 62, 30, 54, 22},
	{3, 35, 11, 43, 1, 33, 9, 41},
	{51, 19, 59, 27, 49, 17, 57, 25},
	{15, 47, 7, 39, 13, 45, 5, 37},
	{63, 31, 55, 23, 61, 29, 53, 21},
}
