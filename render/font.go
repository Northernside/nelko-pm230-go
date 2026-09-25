package render

import (
	"fmt"
	"sync"

	"github.com/Northernside/nelko-pm230-go/internal/enum"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/gofont/gosmallcaps"
	"golang.org/x/image/font/opentype"
)

type Font uint8

const (
	SansBold Font = iota
	Sans
	SansMedium
	SansItalic
	SansBoldItalic
	Mono
	MonoBold
	SmallCaps
)

type fontFile struct {
	name string
	ttf  []byte
}

var fonts = [...]fontFile{
	SansBold:       {"sans-bold", gobold.TTF},
	Sans:           {"sans", goregular.TTF},
	SansMedium:     {"sans-medium", gomedium.TTF},
	SansItalic:     {"sans-italic", goitalic.TTF},
	SansBoldItalic: {"sans-bold-italic", gobolditalic.TTF},
	Mono:           {"mono", gomono.TTF},
	MonoBold:       {"mono-bold", gomonobold.TTF},
	SmallCaps:      {"smallcaps", gosmallcaps.TTF},
}

var (
	fontNames = enum.Names[Font](enum.Collect(fonts[:], func(f fontFile) string { return f.name }))

	parsed = func() (out [len(fonts)]func() (*opentype.Font, error)) {
		for i := range fonts {
			out[i] = sync.OnceValues(func() (*opentype.Font, error) { return opentype.Parse(fonts[i].ttf) })
		}

		return out
	}()
)

func FontNames() []string             { return fontNames.List() }
func ParseFont(s string) (Font, bool) { return fontNames.Parse(s, SansBold) }
func (f Font) String() string         { return fontNames.String(f) }

// size in dots, same meaning as pillow's truetype(path, size)
func (f Font) Face(size int) (font.Face, error) {
	if int(f) >= len(fonts) {
		return nil, fmt.Errorf("unknown font %d", f)
	}

	ft, err := parsed[f]()
	if err != nil {
		return nil, err
	}

	return opentype.NewFace(ft, &opentype.FaceOptions{Size: float64(size), DPI: 72, Hinting: font.HintingFull})
}
