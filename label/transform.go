package label

import (
	"image"

	"github.com/disintegration/imaging"
)

// imaging turns counter-clockwise, Rotate is clockwise
var clockwise = map[int]func(image.Image) *image.NRGBA{
	90:  imaging.Rotate270,
	180: imaging.Rotate180,
	270: imaging.Rotate90,
}

func rotate(img image.Image, degrees int) image.Image {
	if turn, ok := clockwise[degrees]; ok {
		return turn(img)
	}

	return img
}

func invert(g *image.Gray) {
	for i, v := range g.Pix {
		g.Pix[i] = 255 - v
	}
}
