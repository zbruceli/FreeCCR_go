// Command genicon draws the FreeCCR-go app icon (a film frame converting a
// negative to a color positive) to a 1024×1024 PNG. Distinct from the original
// FreeCCR logo. Run: go run ./tools/genicon > bin/icon_1024.png
package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
)

const S = 1024

func main() {
	img := image.NewNRGBA(image.Rect(0, 0, S, S))

	// Rounded-square background with a subtle vertical gray gradient.
	const bgR = 228.0
	for y := 0; y < S; y++ {
		t := float64(y) / S
		br := lerp(0x3a, 0x1c, t)
		for x := 0; x < S; x++ {
			if !roundRect(float64(x), float64(y), 0, 0, S, S, bgR) {
				continue
			}
			img.Set(x, y, color.NRGBA{uint8(br), uint8(br), uint8(br), 255})
		}
	}

	// Film strip (near-black panel) across the middle.
	stripX0, stripX1 := 96.0, 928.0
	stripY0, stripY1 := 300.0, 724.0
	fillRound(img, stripX0, stripY0, stripX1, stripY1, 44, color.NRGBA{14, 14, 16, 255})

	// Sprocket holes, top and bottom rows.
	const holeW, holeH = 50.0, 64.0
	for _, cy := range []float64{338, 686} {
		for cx := 168.0; cx <= 860; cx += 116 {
			fillRound(img, cx-holeW/2, cy-holeH/2, cx+holeW/2, cy+holeH/2, 14,
				color.NRGBA{60, 60, 66, 255})
		}
	}

	// Image window: negative (orange) on the left → color positive on the right.
	winX0, winY0, winX1, winY1 := 150.0, 402.0, 874.0, 622.0
	for y := int(winY0); y < int(winY1); y++ {
		for x := int(winX0); x < int(winX1); x++ {
			if !roundRect(float64(x), float64(y), winX0, winY0, winX1, winY1, 18) {
				continue
			}
			t := (float64(x) - winX0) / (winX1 - winX0)
			img.Set(x, y, convGradient(t))
		}
	}

	png.Encode(os.Stdout, img)
}

// convGradient: film-base orange → teal → sky blue across t∈[0,1].
func convGradient(t float64) color.NRGBA {
	stops := []struct {
		p       float64
		r, g, b uint8
	}{
		{0.0, 0xC4, 0x6A, 0x2F}, // negative orange
		{0.5, 0x2E, 0xA8, 0x8C}, // teal
		{1.0, 0x4C, 0x86, 0xD8}, // sky blue (positive)
	}
	for i := 0; i < len(stops)-1; i++ {
		a, b := stops[i], stops[i+1]
		if t >= a.p && t <= b.p {
			f := (t - a.p) / (b.p - a.p)
			return color.NRGBA{
				uint8(lerp(float64(a.r), float64(b.r), f)),
				uint8(lerp(float64(a.g), float64(b.g), f)),
				uint8(lerp(float64(a.b), float64(b.b), f)), 255}
		}
	}
	return color.NRGBA{stops[len(stops)-1].r, stops[len(stops)-1].g, stops[len(stops)-1].b, 255}
}

func fillRound(img *image.NRGBA, x0, y0, x1, y1, r float64, c color.NRGBA) {
	for y := int(y0); y < int(y1); y++ {
		for x := int(x0); x < int(x1); x++ {
			if roundRect(float64(x), float64(y), x0, y0, x1, y1, r) {
				img.Set(x, y, c)
			}
		}
	}
}

// roundRect reports whether (x,y) lies inside the rounded rectangle.
func roundRect(x, y, x0, y0, x1, y1, r float64) bool {
	if x < x0 || y < y0 || x >= x1 || y >= y1 {
		return false
	}
	cx := clampf(x, x0+r, x1-r)
	cy := clampf(y, y0+r, y1-r)
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= r*r
}

func lerp(a, b, t float64) float64 { return a + (b-a)*t }
func clampf(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
