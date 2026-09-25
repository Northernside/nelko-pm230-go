package raster

import (
	"bytes"
	"image"
	"image/color"
	"math/bits"
)

// packed rows, MSB first, bit 0 = black -> a blank row is 0xFF
// https://github.com/an21p/nelko-pm230-driver/blob/main/research/FINDINGS.md#polarity--0--black
type Bitmap struct {
	Width, Height int
	Stride        int
	Pix           []byte
}

func New(width, height int) *Bitmap {
	stride := (width + 7) / 8
	return &Bitmap{
		Width:  width,
		Height: height,
		Stride: stride,
		Pix:    bytes.Repeat([]byte{0xFF}, stride*height),
	}
}

func (b *Bitmap) Black(x, y int) bool {
	return b.Pix[y*b.Stride+x>>3]&(0x80>>(x&7)) == 0
}

// g below 128 = black, clipped to b
func (b *Bitmap) Draw(g *image.Gray, x0, y0 int) {
	r := g.Bounds()
	for y := range r.Dy() {
		by := y0 + y
		if by < 0 || by >= b.Height {
			continue
		}

		row := g.Pix[y*g.Stride : y*g.Stride+r.Dx()]
		dst := b.Pix[by*b.Stride : (by+1)*b.Stride]
		for x, v := range row {
			bx := x0 + x
			if v < 128 && bx >= 0 && bx < b.Width {
				dst[bx>>3] &^= 0x80 >> (bx & 7)
			}
		}
	}
}

// black dots, row padding bits stay set so they never count
func (b *Bitmap) Ink() int {
	white := 0
	for _, v := range b.Pix {
		white += bits.OnesCount8(v)
	}

	return len(b.Pix)*8 - white
}

var palette = color.Palette{color.Gray{Y: 0}, color.Gray{Y: 255}}

func (b *Bitmap) Image() *image.Paletted {
	img := image.NewPaletted(image.Rect(0, 0, b.Width, b.Height), palette)
	for y := range b.Height {
		row := img.Pix[y*img.Stride : y*img.Stride+b.Width]
		src := b.Pix[y*b.Stride : (y+1)*b.Stride]
		for x := range row {
			row[x] = src[x>>3] >> (7 - x&7) & 1
		}
	}

	return img
}
