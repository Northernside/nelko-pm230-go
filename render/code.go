package render

import (
	"errors"
	"fmt"
	"image"
	"strings"

	"github.com/Northernside/nelko-pm230-go/internal/enum"
	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/aztec"
	"github.com/boombuler/barcode/codabar"
	"github.com/boombuler/barcode/code128"
	"github.com/boombuler/barcode/code39"
	"github.com/boombuler/barcode/code93"
	"github.com/boombuler/barcode/datamatrix"
	"github.com/boombuler/barcode/ean"
	"github.com/boombuler/barcode/pdf417"
	"github.com/boombuler/barcode/qr"
	"github.com/boombuler/barcode/twooffive"
	"github.com/disintegration/imaging"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

var ErrTooMuchData = errors.New("too much data to fit the label width")

// QR error correction, ISO/IEC 18004 recovers ~7, 15, 25 and 30% of the symbol
type Level uint8

const (
	LevelL Level = iota
	LevelM
	LevelQ
	LevelH
)

var levels = enum.Names[Level]{"L", "M", "Q", "H"}

func LevelNames() []string                  { return levels.List() }
func ParseLevel(s string) (Level, bool)     { return levels.Parse(s, LevelM) }
func (l Level) String() string              { return levels.String(l) }
func (l Level) qr() qr.ErrorCorrectionLevel { return qr.ErrorCorrectionLevel(l) }

// 2 modules of quiet zone like the python driver, ISO/IEC 18004 asks for 4 (the paper margin makes up the rest)
func QR(data string, level Level, maxSide int) (*image.Gray, error) {
	code, err := qr.Encode(data, level.qr(), qr.Auto)
	if err != nil {
		return nil, err
	}

	return scaleCode(code, 2, maxSide, maxSide, 1)
}

type Symbology uint8

const (
	Code128 Symbology = iota
	Code39
	Code93
	EAN13
	EAN8
	UPCA
	Codabar
	ITF
	DataMatrix
	PDF417
	Aztec
)

type symbology struct {
	name     string
	twoD     bool
	rowScale int
	encode   func(data string, checksum bool) (barcode.Barcode, error)
}

var symbologies = [...]symbology{
	Code128:    {"code128", false, 1, func(d string, _ bool) (barcode.Barcode, error) { return code128.Encode(d) }},
	Code39:     {"code39", false, 1, func(d string, cs bool) (barcode.Barcode, error) { return code39.Encode(strings.ToUpper(d), cs, false) }},
	Code93:     {"code93", false, 1, func(d string, cs bool) (barcode.Barcode, error) { return code93.Encode(strings.ToUpper(d), cs, false) }},
	EAN13:      {"ean13", false, 1, encodeEAN},
	EAN8:       {"ean8", false, 1, encodeEAN},
	UPCA:       {"upca", false, 1, encodeUPCA},
	Codabar:    {"codabar", false, 1, encodeCodabar},
	ITF:        {"itf", false, 1, encodeITF},
	DataMatrix: {"datamatrix", true, 1, func(d string, _ bool) (barcode.Barcode, error) { return datamatrix.Encode(d) }},
	PDF417:     {"pdf417", true, 2, func(d string, _ bool) (barcode.Barcode, error) { return pdf417.Encode(d, 2) }},           // ISO/IEC 15438 wants rows >= 3x the module width
	Aztec:      {"aztec", true, 1, func(d string, _ bool) (barcode.Barcode, error) { return aztec.Encode([]byte(d), 23, 0) }}, // 23% = ISO/IEC 24778 recommended minimum
}

var symbologyNames = enum.Names[Symbology](enum.Collect(symbologies[:], func(s symbology) string { return s.name }))

func SymbologyNames() []string                  { return symbologyNames.List() }
func ParseSymbology(s string) (Symbology, bool) { return symbologyNames.Parse(s, Code128) }
func (s Symbology) String() string              { return symbologyNames.String(s) }
func (s Symbology) TwoD() bool                  { return symbologies[s].twoD }

func encodeEAN(d string, _ bool) (barcode.Barcode, error) { return ean.Encode(d) }

// UPC-A is EAN-13 with a leading 0 (GS1 General Specifications)
func encodeUPCA(d string, _ bool) (barcode.Barcode, error) {
	if len(d) != 11 && len(d) != 12 {
		return nil, errors.New("UPC-A needs 11 digits (12 with the check digit)")
	}

	return ean.Encode("0" + d)
}

// start and stop characters A-D are part of the symbol (ANSI/AIM BC3)
func encodeCodabar(d string, _ bool) (barcode.Barcode, error) {
	d = strings.ToUpper(d)
	if d != "" && !strings.ContainsAny(d[:1], "ABCD") {
		d = "A" + d + "A"
	}

	return codabar.Encode(d)
}

// ITF encodes digit pairs, an odd count gets a leading 0 (ISO/IEC 16390)
func encodeITF(d string, _ bool) (barcode.Barcode, error) {
	if len(d)%2 == 1 {
		d = "0" + d
	}

	return twooffive.Encode(d, true)
}

type BarcodeOptions struct {
	MaxWidth int // dots
	Height   int // bar height in dots, 1D only
	Text     bool
	Checksum bool // code39 and code93, the others always carry one
}

func Barcode(data string, sym Symbology, o BarcodeOptions) (*image.Gray, error) {
	if int(sym) >= len(symbologies) {
		return nil, fmt.Errorf("unknown symbology %d", sym)
	}

	s := symbologies[sym]
	code, err := s.encode(data, o.Checksum)
	if err != nil {
		return nil, err
	}
	if s.twoD {
		return scaleCode(code, 1, o.MaxWidth, o.MaxWidth*4, s.rowScale)
	}

	return linear(code, o)
}

func linear(code barcode.Barcode, o BarcodeOptions) (*image.Gray, error) {
	modules := code.Bounds().Dx()
	quiet := 10 // modules, ISO/IEC 15417
	factor := o.MaxWidth / (modules + 2*quiet)
	if factor == 0 {
		quiet = 0 // the 3mm of paper beside the head stand in for it
		factor = o.MaxWidth / modules
	}

	if factor == 0 {
		return nil, fmt.Errorf("%w (%d modules, the head has %d dots)", ErrTooMuchData, modules, o.MaxWidth)
	}

	bars, err := expand(code, modules*factor, max(o.Height, 1))
	if err != nil {
		return nil, err
	}
	if !o.Text {
		return Pad(bars, quiet*factor, 0, quiet*factor, 0), nil
	}

	face, err := Sans.Face(max(18, min(factor*10, 36)))
	if err != nil {
		return nil, err
	}
	defer face.Close()

	m := face.Metrics()
	img := Pad(bars, quiet*factor, 0, quiet*factor, 4+m.Ascent.Ceil()+m.Descent.Ceil())
	label := code.Content()
	d := font.Drawer{Dst: img, Src: image.Black, Face: face}
	d.Dot = fixed.P((img.Rect.Dx()-font.MeasureString(face, label).Ceil())/2, bars.Rect.Dy()+4+m.Ascent.Ceil())
	d.DrawString(label)

	return img, nil
}

// modules scaled by a whole number of dots, a resampled code does not scan
func scaleCode(code barcode.Barcode, quiet, maxW, maxH, rowScale int) (*image.Gray, error) {
	b := code.Bounds()
	factor := min(maxW/(b.Dx()+2*quiet), maxH/((b.Dy()+2*quiet)*rowScale))
	if factor == 0 {
		return nil, fmt.Errorf("%w (%d modules wide, the head has %d dots)", ErrTooMuchData, b.Dx()+2*quiet, maxW)
	}

	g, err := expand(code, b.Dx()*factor, b.Dy()*factor)
	if err != nil {
		return nil, err
	}

	if rowScale > 1 {
		g = Gray(imaging.Resize(g, g.Rect.Dx(), g.Rect.Dy()*rowScale, imaging.NearestNeighbor))
	}

	q, qy := quiet*factor, quiet*factor*rowScale
	return Pad(g, q, qy, q, qy), nil
}

// barcode.Scale takes a whole factor, w and h are exact multiples so nothing is left to center
func expand(code barcode.Barcode, w, h int) (*image.Gray, error) {
	scaled, err := barcode.Scale(code, w, h)
	if err != nil {
		return nil, fmt.Errorf("%w (%v)", ErrTooMuchData, err)
	}

	return Gray(scaled), nil
}
