package main

import (
	"image"
	"image/color"
	"math"

	"github.com/lxn/walk"
)

// The palette follows Limbus Company's interface language: a near-black
// canvas, deep crimson rules, muted gold/amber typography and warning-orange
// accents. Values are drawn from the in-game/wiki colour reference.
var (
	colBG        = walk.RGB(0x0D, 0x0C, 0x0B)
	colBGDeep    = walk.RGB(0x07, 0x07, 0x06)
	colHeaderHi  = walk.RGB(0x16, 0x13, 0x0E)
	colHeaderLo  = walk.RGB(0x0A, 0x09, 0x08)
	colPanel     = walk.RGB(0x13, 0x12, 0x10)
	colPanelHi   = walk.RGB(0x1E, 0x1A, 0x13)
	colLine      = walk.RGB(0x2C, 0x28, 0x22)
	colLineDim   = walk.RGB(0x16, 0x14, 0x11)
	colGold      = walk.RGB(0xED, 0xC3, 0x82)
	colGoldSoft  = walk.RGB(0xC9, 0xA2, 0x67)
	colGoldDim   = walk.RGB(0x8A, 0x6B, 0x41)
	colGoldDeep  = walk.RGB(0x4A, 0x38, 0x1E)
	colRed       = walk.RGB(0x81, 0x00, 0x00)
	colRedHi     = walk.RGB(0xC4, 0x1E, 0x1E)
	colOrange    = walk.RGB(0xCF, 0x72, 0x1C)
	colText      = walk.RGB(0xED, 0xE9, 0xE0)
	colTextDim   = walk.RGB(0x8F, 0x89, 0x7D)
	colTextFaint = walk.RGB(0x5E, 0x59, 0x50)
	colMono      = walk.RGB(0xD6, 0xD0, 0xC2)
	colErr       = walk.RGB(0xC4, 0x52, 0x44)
)

// fonts keeps the small set of typefaces the interface uses. Bahnschrift is
// the closest system stand-in for the condensed sans used by the game.
type fonts struct {
	display *walk.Font
	heading *walk.Font
	label   *walk.Font
	body    *walk.Font
	small   *walk.Font
	mono    *walk.Font
	monoSm  *walk.Font
	log     *walk.Font
}

func loadFonts() *fonts {
	must := func(family string, size int, style walk.FontStyle) *walk.Font {
		f, err := walk.NewFont(family, size, style)
		if err != nil {
			panic(err)
		}
		return f
	}
	return &fonts{
		display: must("Bahnschrift SemiCondensed", 28, walk.FontBold),
		heading: must("Microsoft YaHei UI", 11, walk.FontBold),
		label:   must("Microsoft YaHei UI", 9, walk.FontBold),
		body:    must("Microsoft YaHei UI", 9, 0),
		small:   must("Microsoft YaHei UI", 9, 0),
		mono:    must("Consolas", 9, 0),
		monoSm:  must("Consolas", 8, 0),
		log:     must("Microsoft YaHei UI", 9, 0),
	}
}

// makeAppIcon paints the terminal sigil: a gold compass ring with a warning
// needle on a black plate.
func makeAppIcon() image.Image {
	const (
		size = 64
		ss   = 4
		n    = size * ss
	)
	img := image.NewRGBA(image.Rect(0, 0, n, n))

	plate := color.RGBA{0x0B, 0x0A, 0x09, 0xFF}
	edge := color.RGBA{0x6E, 0x55, 0x33, 0xFF}
	gold := color.RGBA{0xED, 0xC3, 0x82, 0xFF}
	red := color.RGBA{0x81, 0x00, 0x00, 0xFF}
	orange := color.RGBA{0xCF, 0x72, 0x1C, 0xFF}

	cx, cy := 32.0, 32.0
	for y := 0; y < n; y++ {
		fy := (float64(y) + 0.5) / ss
		for x := 0; x < n; x++ {
			fx := (float64(x) + 0.5) / ss
			var c color.RGBA
			if insideRoundRect(fx, fy, 1.5, 1.5, 61, 61, 12) {
				c = plate
				if !insideRoundRect(fx, fy, 2.8, 2.8, 58.4, 58.4, 10.6) {
					c = edge
				}
				d := math.Hypot(fx-cx, fy-cy)
				switch {
				case d >= 20.4 && d <= 22.2:
					c = gold
				case d >= 14.6 && d <= 15.4:
					c = red
				}
				if distToSegment(fx, fy, cx, cy, cx+12, cy-12) <= 1.5 {
					c = orange
				}
				if distToSegment(fx, fy, cx, cy, cx-6.5, cy+6.5) <= 1.5 {
					c = red
				}
				if d <= 2.4 {
					c = gold
				}
			}
			img.SetRGBA(x, y, c)
		}
	}

	return downsample(img, size, ss)
}

func insideRoundRect(x, y, rx, ry, w, h, r float64) bool {
	if x < rx || x > rx+w || y < ry || y > ry+h {
		return false
	}
	ix := math.Min(math.Max(x, rx+r), rx+w-r)
	iy := math.Min(math.Max(y, ry+r), ry+h-r)
	return math.Hypot(x-ix, y-iy) <= r
}

func distToSegment(px, py, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	if dx == 0 && dy == 0 {
		return math.Hypot(px-ax, py-ay)
	}
	t := ((px-ax)*dx + (py-ay)*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(px-(ax+t*dx), py-(ay+t*dy))
}

func downsample(src *image.RGBA, size, ss int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var sr, sg, sb, sa uint64
			for dy := 0; dy < ss; dy++ {
				for dx := 0; dx < ss; dx++ {
					r, g, b, a := src.At(x*ss+dx, y*ss+dy).RGBA()
					sr += uint64(r)
					sg += uint64(g)
					sb += uint64(b)
					sa += uint64(a)
				}
			}
			n := uint64(ss * ss)
			sa /= n
			var r, g, b uint8
			if sa > 0 {
				r = uint8((sr / n) * 0xFFFF / sa >> 8)
				g = uint8((sg / n) * 0xFFFF / sa >> 8)
				b = uint8((sb / n) * 0xFFFF / sa >> 8)
			}
			dst.SetRGBA(x, y, color.RGBA{R: r, G: g, B: b, A: uint8(sa >> 8)})
		}
	}
	return dst
}
