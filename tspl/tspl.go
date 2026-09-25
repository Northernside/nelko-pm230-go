// builds the byte stream a Nelko PM230 prints
// Layout from https://github.com/an21p/nelko-pm230-driver/blob/main/research/FINDINGS.md#wire-format
// checked byte for byte against the captured vendor job
package tspl

import (
	"math"
	"strconv"

	"github.com/Northernside/nelko-pm230-go/raster"
)

const (
	PaperWidthMM   = 54.0
	PrintWidthDots = 384
	DotsPerMM      = 8 // 203 dpi, https://github.com/an21p/nelko-pm230-driver/blob/main/research/FINDINGS.md#resolution-measured-not-assumed
	DefaultDensity = 10
	MaxDensity     = 15
	DefaultY       = 8
)

// replies in https://github.com/an21p/nelko-pm230-driver/blob/main/research/FINDINGS.md#transport-behaviour
var (
	QueryStatus  = []byte("\x1b!?\r\n")
	QueryConfig  = []byte("CONFIG?\r\n")
	QueryBattery = []byte("BATTERY?\r\n")
)

// the y offset counts above and below, fits all three captured jobs
// https://github.com/an21p/nelko-pm230-driver/blob/main/research/FINDINGS.md#resolution-measured-not-assumed
func LabelHeightMM(heightDots, y int) float64 {
	return float64(heightDots+2*y) / DotsPerMM
}

// inverse of LabelHeightMM, rounded up
func HeightDots(mm float64, y int) int {
	return int(math.Ceil(mm*DotsPerMM)) - 2*y
}

// x is always 0, y is DefaultY as in the captured jobs
func Encode(b *raster.Bitmap, density int) []byte {
	dst := make([]byte, 0, len(b.Pix)+160)
	dst = append(dst, "\x1b!?\r\nSIZE "...)
	dst = strconv.AppendFloat(dst, PaperWidthMM, 'f', 1, 64)
	dst = append(dst, " mm,"...)
	dst = strconv.AppendFloat(dst, LabelHeightMM(b.Height, DefaultY), 'f', 1, 64)
	dst = append(dst, " mm\r\nDIRECTION 0,0\r\nDENSITY "...)
	dst = strconv.AppendInt(dst, int64(density), 10)
	// PRINT before BITMAP, as in both captured jobs
	dst = append(dst, "\r\nCLS\r\nPRINT 1\r\nBITMAP 0,"...)
	dst = strconv.AppendInt(dst, DefaultY, 10)
	dst = append(dst, ',')
	dst = strconv.AppendInt(dst, int64(b.Stride), 10)
	dst = append(dst, ',')
	dst = strconv.AppendInt(dst, int64(b.Height), 10)
	dst = append(dst, ",1,"...) // mode 1, payload follows without a separator
	dst = append(dst, b.Pix...)
	return append(dst, "ALLEND\r\n\r\n"...)
}
