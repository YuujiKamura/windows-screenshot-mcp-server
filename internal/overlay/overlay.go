package overlay

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
)

// rulerHeight is the height of the top ruler bar in pixels.
const rulerHeight = 20

// rulerWidth is the width of the left ruler bar in pixels.
const rulerWidth = 44

// ToRGBA converts any image.Image to *image.RGBA for direct pixel manipulation.
func ToRGBA(src image.Image) *image.RGBA {
	if rgba, ok := src.(*image.RGBA); ok {
		return rgba
	}
	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)
	draw.Draw(dst, bounds, src, bounds.Min, draw.Src)
	return dst
}

// DrawGrid draws semi-transparent grid lines every spacing pixels.
func DrawGrid(img *image.RGBA, spacing int) {
	gridColor := color.RGBA{128, 128, 128, 80}
	bounds := img.Bounds()

	// Vertical lines
	for x := spacing; x < bounds.Max.X; x += spacing {
		for y := 0; y < bounds.Max.Y; y++ {
			blendPixel(img, x, y, gridColor)
		}
	}
	// Horizontal lines
	for y := spacing; y < bounds.Max.Y; y += spacing {
		for x := 0; x < bounds.Max.X; x++ {
			blendPixel(img, x, y, gridColor)
		}
	}
}

// DrawRulers draws numbered rulers along the top and left edges.
func DrawRulers(img *image.RGBA, spacing int) {
	bounds := img.Bounds()
	barColor := color.RGBA{0, 0, 0, 180}
	textColor := color.RGBA{255, 255, 255, 255}

	// Top bar background
	for y := 0; y < rulerHeight && y < bounds.Max.Y; y++ {
		for x := 0; x < bounds.Max.X; x++ {
			img.Set(x, y, barColor)
		}
	}
	// Left bar background
	for y := 0; y < bounds.Max.Y; y++ {
		for x := 0; x < rulerWidth && x < bounds.Max.X; x++ {
			img.Set(x, y, barColor)
		}
	}

	// Top ruler labels
	for x := 0; x < bounds.Max.X; x += spacing {
		label := fmt.Sprintf("%d", x)
		drawString(img, x+2, 3, label, textColor)
		// Tick mark
		for y := rulerHeight - 3; y < rulerHeight; y++ {
			if x > 0 && x < bounds.Max.X {
				img.Set(x, y, textColor)
			}
		}
	}

	// Left ruler labels
	for y := spacing; y < bounds.Max.Y; y += spacing {
		label := fmt.Sprintf("%d", y)
		drawString(img, 2, y+2, label, textColor)
		// Tick mark
		for x := rulerWidth - 3; x < rulerWidth; x++ {
			if y < bounds.Max.Y {
				img.Set(x, y, textColor)
			}
		}
	}
}

// DrawCrosshair draws a bright red crosshair spanning the full image at (cx, cy),
// with a coordinate label near the intersection.
func DrawCrosshair(img *image.RGBA, cx, cy int) {
	crossColor := color.RGBA{255, 0, 0, 200}
	bounds := img.Bounds()

	// Horizontal line
	for x := bounds.Min.X; x < bounds.Max.X; x++ {
		blendPixel(img, x, cy, crossColor)
		if cy+1 < bounds.Max.Y {
			blendPixel(img, x, cy+1, crossColor)
		}
	}
	// Vertical line
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		blendPixel(img, cx, y, crossColor)
		if cx+1 < bounds.Max.X {
			blendPixel(img, cx+1, y, crossColor)
		}
	}

	// Label
	label := fmt.Sprintf("(%d,%d)", cx, cy)
	labelX := cx + 6
	labelY := cy - 12
	if labelY < 0 {
		labelY = cy + 6
	}
	if labelX+len(label)*6 > bounds.Max.X {
		labelX = cx - len(label)*6 - 4
	}
	// Label background
	bgColor := color.RGBA{0, 0, 0, 180}
	for dy := 0; dy < 11; dy++ {
		for dx := -2; dx < len(label)*6+2; dx++ {
			px, py := labelX+dx, labelY+dy
			if px >= 0 && py >= 0 && px < bounds.Max.X && py < bounds.Max.Y {
				blendPixel(img, px, py, bgColor)
			}
		}
	}
	drawString(img, labelX, labelY+1, label, color.RGBA{255, 255, 0, 255})
}

// blendPixel alpha-blends the given color onto the existing pixel.
func blendPixel(img *image.RGBA, x, y int, c color.RGBA) {
	if x < img.Bounds().Min.X || x >= img.Bounds().Max.X ||
		y < img.Bounds().Min.Y || y >= img.Bounds().Max.Y {
		return
	}
	existing := img.RGBAAt(x, y)
	a := uint32(c.A)
	invA := 255 - a
	r := (uint32(c.R)*a + uint32(existing.R)*invA) / 255
	g := (uint32(c.G)*a + uint32(existing.G)*invA) / 255
	b := (uint32(c.B)*a + uint32(existing.B)*invA) / 255
	outA := a + uint32(existing.A)*invA/255
	img.SetRGBA(x, y, color.RGBA{uint8(r), uint8(g), uint8(b), uint8(outA)})
}

// drawString renders a string using a hardcoded 5x7 bitmap font.
func drawString(img *image.RGBA, x, y int, s string, c color.RGBA) {
	offsetX := 0
	for _, ch := range s {
		glyph, ok := bitmapFont[ch]
		if !ok {
			glyph = bitmapFont['?']
		}
		for row, bits := range glyph {
			for col := 0; col < 5; col++ {
				if bits&(1<<(4-col)) != 0 {
					px := x + offsetX + col
					py := y + row
					if px >= 0 && py >= 0 && px < img.Bounds().Max.X && py < img.Bounds().Max.Y {
						img.SetRGBA(px, py, c)
					}
				}
			}
		}
		offsetX += 6 // 5px glyph + 1px spacing
	}
}

// bitmapFont is a 5x7 pixel font for digits and common punctuation.
// Each entry is 7 rows; each row is a bitmask where bit 4=leftmost, bit 0=rightmost.
var bitmapFont = map[rune][7]byte{
	'0': {0x0E, 0x11, 0x13, 0x15, 0x19, 0x11, 0x0E},
	'1': {0x04, 0x0C, 0x04, 0x04, 0x04, 0x04, 0x0E},
	'2': {0x0E, 0x11, 0x01, 0x02, 0x04, 0x08, 0x1F},
	'3': {0x0E, 0x11, 0x01, 0x06, 0x01, 0x11, 0x0E},
	'4': {0x02, 0x06, 0x0A, 0x12, 0x1F, 0x02, 0x02},
	'5': {0x1F, 0x10, 0x1E, 0x01, 0x01, 0x11, 0x0E},
	'6': {0x06, 0x08, 0x10, 0x1E, 0x11, 0x11, 0x0E},
	'7': {0x1F, 0x01, 0x02, 0x04, 0x08, 0x08, 0x08},
	'8': {0x0E, 0x11, 0x11, 0x0E, 0x11, 0x11, 0x0E},
	'9': {0x0E, 0x11, 0x11, 0x0F, 0x01, 0x02, 0x0C},
	',': {0x00, 0x00, 0x00, 0x00, 0x00, 0x06, 0x04},
	'(': {0x02, 0x04, 0x08, 0x08, 0x08, 0x04, 0x02},
	')': {0x08, 0x04, 0x02, 0x02, 0x02, 0x04, 0x08},
	' ': {0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	'x': {0x00, 0x00, 0x11, 0x0A, 0x04, 0x0A, 0x11},
	'?': {0x0E, 0x11, 0x01, 0x02, 0x04, 0x00, 0x04},
}
