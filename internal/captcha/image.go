package captcha

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
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
	glyphScale  = 8 // nominal size of one glyph dot in pixels
	glyphWidth  = 5
	glyphHeight = 7
	imgPadding  = 10
)

// RenderNumberPNG renders n as a PNG of distorted dot-matrix digits. The
// number is only ever delivered as an image, never as text, so scrapers
// cannot read it from the page markup.
//
// Every rendering differs so a fixed glyph template cannot be matched: each
// digit gets its own dot size, shear, offset and ink colour, every dot is
// jittered and drawn as a blob of random shape, and ink-coloured clutter
// (strokes crossing the digits and stray dots) is painted over the glyphs.
// This raises the cost of automated reading; it is not a guarantee, which is
// why the optional Turnstile check and per-IP action limits exist as well.
func RenderNumberPNG(n int) ([]byte, error) {
	if n < 0 {
		n = 0
	}
	s := strconv.Itoa(n)

	// Each digit cell leaves room for the largest dot size plus its shear.
	cellW := glyphWidth*(glyphScale+1) + 2*glyphScale
	gap := glyphScale / 2
	w := imgPadding*2 + len(s)*cellW + (len(s)-1)*gap
	h := imgPadding*2 + glyphHeight*(glyphScale+1) + glyphScale

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	bg := color.RGBA{R: 233, G: 238, B: 248, A: 255}
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.SetRGBA(x, y, bg)
		}
	}

	// Background speckle.
	for i := 0; i < w*h/40; i++ {
		g := uint8(120 + mathrand.Intn(100)) // #nosec G115
		img.SetRGBA(mathrand.Intn(w), mathrand.Intn(h), color.RGBA{R: g, G: g, B: g, A: 255})
	}

	for di := 0; di < len(s); di++ {
		glyph, ok := digitGlyphs[s[di]]
		if !ok {
			continue
		}
		scale := glyphScale - 2 + mathrand.Intn(4) // 6..9 px per dot
		shear := mathrand.Float64()*0.3 - 0.15     // horizontal px per vertical px
		ink := randomInk()
		ox := imgPadding + di*(cellW+gap) + mathrand.Intn(cellW-glyphWidth*scale-2*glyphScale+1)
		oy := imgPadding + mathrand.Intn(h-2*imgPadding-glyphHeight*scale+1)
		for row := 0; row < glyphHeight; row++ {
			for col := 0; col < glyphWidth; col++ {
				if glyph[row][col] != '1' {
					continue
				}
				y := oy + row*scale + mathrand.Intn(3) - 1
				x := ox + col*scale + int(shear*float64(row*scale)) + glyphScale + mathrand.Intn(3) - 1
				drawBlob(img, x, y, scale, ink)
			}
		}
	}

	// Clutter drawn over the digits in the same palette as the ink, so it
	// cannot be filtered out by colour.
	for i := 0; i < 2+len(s); i++ {
		drawStroke(img, randomInk(), w, h)
	}
	for i := 0; i < len(s)*6; i++ {
		drawBlob(img, mathrand.Intn(w), mathrand.Intn(h), 2+mathrand.Intn(3), randomInk())
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// randomInk returns a dark, slightly varied ink colour.
func randomInk() color.RGBA {
	return color.RGBA{
		R: uint8(20 + mathrand.Intn(50)), // #nosec G115
		G: uint8(25 + mathrand.Intn(45)), // #nosec G115
		B: uint8(50 + mathrand.Intn(50)), // #nosec G115
		A: 255,
	}
}

// drawBlob fills a size x size cell at (x, y) as either a square or a disc.
func drawBlob(img *image.RGBA, x, y, size int, c color.RGBA) {
	round := mathrand.Intn(2) == 0
	r := float64(size) / 2
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			if round {
				dx, dy := float64(px)+0.5-r, float64(py)+0.5-r
				if dx*dx+dy*dy > r*r {
					continue
				}
			}
			if image.Pt(x+px, y+py).In(img.Rect) {
				img.SetRGBA(x+px, y+py, c)
			}
		}
	}
}

// drawStroke paints a 2px wavy line across the full image width.
func drawStroke(img *image.RGBA, c color.RGBA, w, h int) {
	y0 := float64(imgPadding + mathrand.Intn(max(1, h-2*imgPadding)))
	slope := (mathrand.Float64() - 0.5) * float64(h) / float64(w)
	amp := 2 + mathrand.Float64()*4
	freq := 0.02 + mathrand.Float64()*0.06
	phase := mathrand.Float64() * 2 * math.Pi
	for x := 0; x < w; x++ {
		y := int(y0 + slope*float64(x) + amp*math.Sin(freq*float64(x)+phase))
		for t := 0; t < 2; t++ {
			if image.Pt(x, y+t).In(img.Rect) {
				img.SetRGBA(x, y+t, c)
			}
		}
	}
}
