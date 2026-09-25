package label

import (
	"cmp"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"slices"
	"strconv"
	"strings"

	"github.com/Northernside/nelko-pm230-go/internal/enum"
	"github.com/Northernside/nelko-pm230-go/raster"
	"github.com/Northernside/nelko-pm230-go/render"
	"github.com/Northernside/nelko-pm230-go/tspl"
)

const (
	Width         = tspl.PrintWidthDots
	Margin        = 8
	CodeWidth     = Width - 2*Margin
	MaxHeightDots = 16000 // 2m of paper
	MaxBlocks     = 32
	MaxBannerDots = 2400 // 300mm, text or barcodes turned to run along the paper
	MaxPixels     = 50_000_000
)

const (
	KindText    = "text"
	KindImage   = "image"
	KindQR      = "qr"
	KindBarcode = "barcode"
	KindSpacer  = "spacer"
	KindLine    = "line"
)

type Block struct {
	Kind string `json:"kind"`

	Text  string `json:"text,omitzero"`
	Data  string `json:"data,omitzero"`
	Image []byte `json:"image,omitzero"` // png, jpeg, gif, bmp, webp or tiff

	Font   string `json:"font,omitzero"`
	Size   int    `json:"size,omitzero"` // dots, text em size / qr side / spacer height / line thickness
	Align  string `json:"align,omitzero"`
	Rotate int    `json:"rotate,omitzero"` // clockwise
	Invert bool   `json:"invert,omitzero"`

	Fit        string `json:"fit,omitzero"` // "width" also enlarges narrow images
	Dither     string `json:"dither,omitzero"`
	Threshold  int    `json:"threshold,omitzero"`
	Brightness int    `json:"brightness,omitzero"`
	Contrast   int    `json:"contrast,omitzero"`

	Level     string `json:"level,omitzero"`
	Symbology string `json:"symbology,omitzero"`
	Height    int    `json:"height,omitzero"`
	HideText  bool   `json:"hide_text,omitzero"`
	Checksum  bool   `json:"checksum,omitzero"`
}

type Label struct {
	Blocks      []Block `json:"blocks"`
	Gap         int     `json:"gap,omitzero"` // dots
	MinLengthMM float64 `json:"min_length_mm,omitzero"`
}

// draw returns grayscale in the block's own orientation, render turns, frames and makes it 1 bit
type kind struct {
	name     string
	draw     func(b *Block, a render.Align, budget int) (*image.Gray, error)
	align    render.Align
	margin   int  // beside a block narrower than the head
	across   int  // widest it may be once turned
	frame    int  // above and below once turned
	dithered bool // Dither and Threshold apply, the rest is cut at 128
}

var kinds = []kind{
	{KindText, (*Block).text, render.Left, Margin, Width, 0, false},
	{KindImage, (*Block).image, render.Left, 0, Width, 0, true},
	{KindQR, (*Block).qr, render.Center, Margin, CodeWidth, Margin, false}, // framed like the python driver's render_qr
	{KindBarcode, (*Block).barcode, render.Center, Margin, CodeWidth, Margin, false},
	{KindSpacer, (*Block).spacer, render.Left, 0, Width, 0, false},
	{KindLine, (*Block).line, render.Left, 0, Width, 0, false},
}

func Kinds() []string { return enum.Collect(kinds, func(k kind) string { return k.name }) }

var (
	ErrEmpty   = errors.New("the label has nothing on it")
	ErrTooTall = errors.New("label too tall")
)

type BlockError struct {
	Index int
	Err   error
}

func (e *BlockError) Error() string { return "block " + strconv.Itoa(e.Index+1) + ": " + e.Err.Error() }
func (e *BlockError) Unwrap() error { return e.Err }

func Render(l *Label) (*raster.Bitmap, error) {
	if len(l.Blocks) == 0 {
		return nil, ErrEmpty
	}

	if len(l.Blocks) > MaxBlocks {
		return nil, fmt.Errorf("%d blocks, the limit is %d", len(l.Blocks), MaxBlocks)
	}

	gap := min(max(l.Gap, 0), 400)

	type placed struct {
		g    *image.Gray
		x, y int
	}

	parts := make([]placed, 0, len(l.Blocks))
	y := 0
	for i := range l.Blocks {
		if i > 0 {
			y += gap
		}

		g, x, err := l.Blocks[i].render(MaxHeightDots - y)
		if err != nil {
			return nil, &BlockError{Index: i, Err: err}
		}

		parts = append(parts, placed{g, x, y})
		if y += g.Rect.Dy(); y > MaxHeightDots {
			return nil, tooTall(y)
		}
	}

	if l.MinLengthMM > 0 {
		y = max(y, tspl.HeightDots(min(l.MinLengthMM, 2000), tspl.DefaultY))
		if y > MaxHeightDots {
			return nil, tooTall(y)
		}
	}

	b := raster.New(Width, max(y, 1))
	for _, p := range parts {
		b.Draw(p.g, p.x, p.y)
	}

	return b, nil
}

func tooTall(dots int) error {
	return fmt.Errorf("%w, %d dots, the limit is %d (%d mm)", ErrTooTall, dots, MaxHeightDots, MaxHeightDots/tspl.DotsPerMM)
}

func oneOf(what, got string, names []string) error {
	return fmt.Errorf("%s must be one of %s, got %q", what, strings.Join(names, ", "), got)
}

// pure 0/255, and the column it goes at
func (b *Block) render(budget int) (*image.Gray, int, error) {
	i := slices.IndexFunc(kinds, func(k kind) bool { return k.name == b.Kind })
	if i < 0 {
		return nil, 0, oneOf("kind", b.Kind, Kinds())
	}

	k := kinds[i]
	if !slices.Contains([]int{0, 90, 180, 270}, b.Rotate) {
		return nil, 0, fmt.Errorf("rotate must be 0, 90, 180 or 270, got %d", b.Rotate)
	}

	a, ok := render.ParseAlign(b.Align, k.align)
	if !ok {
		return nil, 0, oneOf("align", b.Align, render.AlignNames())
	}

	d, threshold := raster.Threshold, 128
	if k.dithered {
		var err error
		if d, threshold, err = b.dither(); err != nil {
			return nil, 0, err
		}
	}

	g, err := k.draw(b, a, budget)
	if err != nil {
		return nil, 0, err
	}

	g = render.Gray(rotate(g, b.Rotate))
	if w := g.Rect.Dx(); w > k.across {
		return nil, 0, fmt.Errorf("the block is %d dots across, the head fits %d, make it smaller or turn it", w, k.across)
	}

	if k.frame > 0 {
		g = render.Pad(g, 0, k.frame, 0, k.frame)
	}

	if g.Rect.Dy() > budget {
		return nil, 0, tooTall(MaxHeightDots - budget + g.Rect.Dy())
	}

	if b.Invert {
		invert(g)
	}

	raster.Apply(g, d, uint8(threshold))
	return g, column(a, g.Rect.Dx(), k.margin), nil
}

func column(a render.Align, w, margin int) int {
	switch a {
	case render.Center:
		return (Width - w) / 2
	case render.Right:
		return Width - margin - w
	}

	return min(margin, Width-w)
}

func (b *Block) sideways() bool { return b.Rotate == 90 || b.Rotate == 270 }

// upside down text keeps the side it was aligned to
var mirrored = [...]render.Align{render.Left: render.Right, render.Center: render.Center, render.Right: render.Left}

func (b *Block) text(a render.Align, _ int) (*image.Gray, error) {
	if strings.TrimSpace(b.Text) == "" {
		return nil, errors.New("the text is empty")
	}

	size := cmp.Or(b.Size, 40)
	if size < 6 || size > 400 {
		return nil, fmt.Errorf("text size must be 6 to 400 dots, got %d", size)
	}

	f, ok := render.ParseFont(b.Font)
	if !ok {
		return nil, oneOf("font", b.Font, render.FontNames())
	}

	o := render.TextOptions{Font: f, Size: size, Align: a, Width: Width, Margin: Margin, LineSpacing: 4}
	if b.sideways() {
		o.NoWrap, o.MaxWidth = true, MaxBannerDots
	}

	if b.Rotate == 180 {
		o.Align = mirrored[a]
	}

	g, err := render.Text(b.Text, o)
	if errors.Is(err, render.ErrTooWide) {
		return nil, fmt.Errorf("the longest line runs past %d mm along the label, add line breaks", MaxBannerDots/tspl.DotsPerMM)
	}
	return g, err
}

func (b *Block) data() error {
	if b.Data == "" {
		return errors.New("there is no data to encode")
	}

	if len(b.Data) > 4096 {
		return errors.New("more data than any code on this label can hold")
	}

	return nil
}

// Size caps the side, 0 = as wide as the head allows
func (b *Block) side() int {
	side := CodeWidth
	if b.Size > 0 {
		side = min(b.Size, side)
	}

	return side
}

func (b *Block) qr(render.Align, int) (*image.Gray, error) {
	if err := b.data(); err != nil {
		return nil, err
	}

	level, ok := render.ParseLevel(b.Level)
	if !ok {
		return nil, oneOf("error correction", b.Level, render.LevelNames())
	}

	return render.QR(b.Data, level, b.side())
}

func (b *Block) barcode(render.Align, int) (*image.Gray, error) {
	if err := b.data(); err != nil {
		return nil, err
	}

	sym, ok := render.ParseSymbology(b.Symbology)
	if !ok {
		return nil, oneOf("symbology", b.Symbology, render.SymbologyNames())
	}

	h := cmp.Or(b.Height, 80)
	if h < 8 || h > 1000 {
		return nil, fmt.Errorf("bar height must be 8 to 1000 dots, got %d", h)
	}

	maxW := CodeWidth
	switch {
	case sym.TwoD():
		maxW = b.side()
	case b.sideways():
		maxW = MaxBannerDots
	}

	return render.Barcode(b.Data, sym, render.BarcodeOptions{MaxWidth: maxW, Height: h, Text: !b.HideText, Checksum: b.Checksum})
}

func (b *Block) spacer(render.Align, int) (*image.Gray, error) {
	if b.Size <= 0 || b.Size > MaxHeightDots {
		return nil, fmt.Errorf("a spacer needs a height from 1 to %d dots", MaxHeightDots)
	}

	return render.White(Width, b.Size), nil
}

func (b *Block) line(render.Align, int) (*image.Gray, error) {
	t := cmp.Or(b.Size, 2)
	if t < 1 || t > 200 {
		return nil, errors.New("line thickness must be 1 to 200 dots")
	}

	pad := Margin / 2
	g := render.White(Width, t+2*pad)
	draw.Draw(g, image.Rect(Margin, pad, Width-Margin, pad+t), image.Black, image.Point{}, draw.Src)
	return g, nil
}
