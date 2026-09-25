package label

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"

	"github.com/Northernside/nelko-pm230-go/raster"
	"github.com/Northernside/nelko-pm230-go/render"
	"github.com/disintegration/imaging"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

func (b *Block) dither() (raster.Dither, int, error) {
	d, ok := raster.ParseDither(b.Dither)
	if !ok {
		return 0, 0, oneOf("dither", b.Dither, raster.DitherNames())
	}

	threshold := cmp.Or(b.Threshold, 128)
	if threshold < 1 || threshold > 255 {
		return 0, 0, fmt.Errorf("threshold must be 1 to 255, got %d", threshold)
	}

	return d, threshold, nil
}

func (b *Block) image(_ render.Align, budget int) (*image.Gray, error) {
	if len(b.Image) == 0 {
		return nil, errors.New("no image chosen")
	}

	if b.Fit != "" && b.Fit != "width" {
		return nil, oneOf("fit", b.Fit, []string{`""`, "width"})
	}

	// the header alone, a decompression bomb stops here
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b.Image))
	if err != nil {
		return nil, errors.New("that file is not an image we can read")
	}

	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > MaxPixels {
		return nil, fmt.Errorf("%dx%d is too many pixels, the limit is %d megapixels", cfg.Width, cfg.Height, MaxPixels/1_000_000)
	}

	src, err := imaging.Decode(bytes.NewReader(b.Image), imaging.AutoOrientation(true)) // Exif 0x0112, all 8 orientations
	if err != nil {
		return nil, fmt.Errorf("decoding the image: %w", err)
	}

	// scaled in the source orientation, render turns the small result
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	if b.sideways() {
		w, h = h, w
	}

	tw := w
	if tw > Width || b.Fit == "width" {
		tw = Width
	}

	th := max(1, int(math.Round(float64(h)*float64(tw)/float64(w))))
	if th > budget {
		return nil, tooTall(MaxHeightDots - budget + th)
	}

	sw, sh := tw, th
	if b.sideways() {
		sw, sh = th, tw
	}

	img := imaging.Resize(src, sw, sh, imaging.Lanczos)
	if b.Brightness != 0 {
		img = imaging.AdjustBrightness(img, float64(min(max(b.Brightness, -100), 100)))
	}

	if b.Contrast != 0 {
		img = imaging.AdjustContrast(img, float64(min(max(b.Contrast, -100), 100)))
	}

	return render.Gray(img), nil
}
