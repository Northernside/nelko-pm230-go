package render

import (
	"image"
	"image/draw"
)

func White(w, h int) *image.Gray {
	g := image.NewGray(image.Rect(0, 0, w, h))
	for i := range g.Pix {
		g.Pix[i] = 255
	}

	return g
}

// alpha lands on white paper, luma per ITU-R BT.601 like pillow's "L"
func Gray(img image.Image) *image.Gray {
	switch src := img.(type) {
	case *image.Gray:
		if src.Rect.Min == (image.Point{}) {
			return src
		}
	case *image.NRGBA:
		return nrgbaGray(src)
	}

	b := img.Bounds()
	g := White(b.Dx(), b.Dy())
	draw.Draw(g, g.Rect, img, b.Min, draw.Over)
	return g
}

func nrgbaGray(src *image.NRGBA) *image.Gray {
	w, h := src.Rect.Dx(), src.Rect.Dy()
	g := image.NewGray(image.Rect(0, 0, w, h))
	for y := range h {
		in := src.Pix[y*src.Stride : y*src.Stride+4*w]
		out := g.Pix[y*g.Stride : y*g.Stride+w]
		for x := range out {
			p := in[4*x : 4*x+4]
			l := (uint32(p[0])*299 + uint32(p[1])*587 + uint32(p[2])*114 + 500) / 1000
			a := uint32(p[3])
			out[x] = uint8((l*a + 255*(255-a) + 127) / 255)
		}
	}

	return g
}

func Pad(g *image.Gray, left, top, right, bottom int) *image.Gray {
	out := White(left+g.Rect.Dx()+right, top+g.Rect.Dy()+bottom)
	draw.Draw(out, g.Rect.Sub(g.Rect.Min).Add(image.Pt(left, top)), g, g.Rect.Min, draw.Src)
	return out
}
