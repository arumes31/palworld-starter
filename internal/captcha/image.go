package captcha

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	mathrand "math/rand"
	"strconv"
)

// 5x7 dot-matrix glyphs for the digits 0-9.
var digitGlyphs = map[byte][7]string{
	'0': {"01110", "10001", "10011", "10101", "11001", "10001", "01110"},
	'1': {"00100", "01100", "00100", "00100", "00100", "00100", "01110"},
	'2': {"01110", "10001", "00001", "00010", "00100", "01000", "11111"},
	'3': {"11111", "00010", "00100", "00010", "00001", "10001", "01110"},
	'4': {"00010", "00110", "01010", "10010", "11111", "00010", "00010"},
	'5': {"11111", "10000", "11110", "00001", "00001", "10001", "01110"},
	'6': {"00110", "01000", "10000", "11110", "10001", "10001", "01110"},
	'7': {"11111", "00001", "00010", "00100", "01000", "01000", "01000"},
	'8': {"01110", "10001", "10001", "01110", "10001", "10001", "01110"},
	'9': {"01110", "10001", "10001", "01111", "00001", "00010", "01100"},
}

const (
	glyphScale  = 8
	glyphWidth  = 5
	glyphHeight = 7
	imgPadding  = 10
)

// RenderNumberPNG renders n as a PNG of dot-matrix digits with per-digit
// vertical jitter and pixel noise. The number is only ever delivered as an
// image, never as text, so scrapers cannot read it from the page markup.
func RenderNumberPNG(n int) ([]byte, error) {
	if n < 0 {
		n = 0
	}
	s := strconv.Itoa(n)

	digitW := glyphWidth * glyphScale
	gap := glyphScale
	w := imgPadding*2 + len(s)*digitW + (len(s)-1)*gap
	h := imgPadding*2 + glyphHeight*glyphScale + glyphScale // extra row for jitter

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	bg := color.RGBA{R: 233, G: 238, B: 248, A: 255}
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.SetRGBA(x, y, bg)
		}
	}

	// Sprinkle noise so the image is not trivially OCR-able.
	for i := 0; i < w*h/40; i++ {
		g := uint8(120 + mathrand.Intn(100)) // #nosec G115
		img.SetRGBA(mathrand.Intn(w), mathrand.Intn(h), color.RGBA{R: g, G: g, B: g, A: 255})
	}

	ink := color.RGBA{R: 35, G: 42, B: 66, A: 255}
	for di := 0; di < len(s); di++ {
		glyph, ok := digitGlyphs[s[di]]
		if !ok {
			continue
		}
		ox := imgPadding + di*(digitW+gap)
		oy := imgPadding + mathrand.Intn(glyphScale)
		for row := 0; row < glyphHeight; row++ {
			for col := 0; col < glyphWidth; col++ {
				if glyph[row][col] != '1' {
					continue
				}
				for py := 0; py < glyphScale; py++ {
					for px := 0; px < glyphScale; px++ {
						img.SetRGBA(ox+col*glyphScale+px, oy+row*glyphScale+py, ink)
					}
				}
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
