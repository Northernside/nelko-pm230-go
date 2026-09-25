package render

import (
	"errors"
	"image"
	"strings"
	"unicode/utf8"

	"github.com/Northernside/nelko-pm230-go/internal/enum"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

type Align uint8

const (
	Left Align = iota
	Center
	Right
)

var aligns = enum.Names[Align]{"left", "center", "right"}

func AlignNames() []string                         { return aligns.List() }
func ParseAlign(s string, def Align) (Align, bool) { return aligns.Parse(s, def) }
func (a Align) String() string                     { return aligns.String(a) }

// sizes in dots
type TextOptions struct {
	Font        Font
	Size        int
	Align       Align
	Width       int // ignored with NoWrap, the canvas is then as wide as the longest line
	Margin      int
	LineSpacing int
	NoWrap      bool
	MaxWidth    int // NoWrap only, 0 = no limit
}

var ErrTooWide = errors.New("text is too wide")

// antialiased, threshold it before packing
func Text(s string, o TextOptions) (*image.Gray, error) {
	face, err := o.Font.Face(o.Size)
	if err != nil {
		return nil, err
	}
	defer face.Close()

	var lines []string
	width := o.Width
	if o.NoWrap {
		lines = strings.Split(s, "\n")
		widest := 0
		for _, l := range lines {
			widest = max(widest, font.MeasureString(face, l).Ceil())
		}

		width = widest + 2*o.Margin
		if o.MaxWidth > 0 && width > o.MaxWidth {
			return nil, ErrTooWide
		}
	} else {
		lines = wrap(s, face, o.Width-2*o.Margin)
	}

	m := face.Metrics()
	ascent, lineHeight := m.Ascent.Ceil(), m.Ascent.Ceil()+m.Descent.Ceil()
	height := 2*o.Margin + len(lines)*lineHeight + (len(lines)-1)*o.LineSpacing

	img := White(max(width, 1), max(height, 1))
	d := font.Drawer{Dst: img, Src: image.Black, Face: face}
	for i, l := range lines {
		w := font.MeasureString(face, l).Ceil()
		x := o.Margin
		switch o.Align {
		case Center:
			x = (width - w) / 2
		case Right:
			x = width - o.Margin - w
		}

		d.Dot = fixed.P(x, o.Margin+i*(lineHeight+o.LineSpacing)+ascent)
		d.DrawString(l)
	}

	return img, nil
}

func wrap(s string, face font.Face, maxWidth int) []string {
	fits := func(t string) bool { return font.MeasureString(face, t).Ceil() <= maxWidth }
	var lines []string
	for para := range strings.SplitSeq(s, "\n") {
		var words []string
		for w := range strings.FieldsSeq(para) {
			if fits(w) {
				words = append(words, w)
				continue
			}

			words = append(words, breakToken(w, fits)...)
		}

		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}

		cur := words[0]
		for _, w := range words[1:] {
			if trial := cur + " " + w; fits(trial) {
				cur = trial
			} else {
				lines = append(lines, cur)
				cur = w
			}
		}

		lines = append(lines, cur)
	}

	return lines
}

func breakToken(w string, fits func(string) bool) []string {
	var parts []string
	start := 0
	for i, r := range w {
		if i > start && !fits(w[start:i+utf8.RuneLen(r)]) {
			parts = append(parts, w[start:i])
			start = i
		}
	}

	return append(parts, w[start:])
}
