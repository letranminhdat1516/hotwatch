// Command iconart draws Hotwatch's app icon, resources/icon.png (1024 px):
// the "flame watch" — a hand-cut white stopwatch with a crimson flame for a
// dial on a black squircle, in the style of the owner's CICCA mark (heavy
// lopsided strokes, cut nicks, crimson fire) without using that mark.
//
// The shapes are the ones on the design canvas, in its 100-unit viewBox,
// mapped onto the 824 px squircle of Apple's 1024 px icon grid. Everything
// is computed here: 4×4 supersampling, curves flattened to polygons.
//
//	go run ./tools/iconart resources/icon.png
package main

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

const (
	size = 1024
	half = 412.0 // the squircle's half side
	unit = 2 * half / 100
	ss   = 4 // samples per pixel side
)

var (
	white   = col{1, 1, 1}
	crimson = rgb(0xAE, 0x1C, 0x3F)
)

func main() {
	out := "resources/icon.png"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	f, err := os.Create(out)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := png.Encode(f, draw()); err != nil {
		panic(err)
	}
}

// shape is one filled region in viewBox units, painted in order.
type shape struct {
	in   func(x, y float64) bool // x, y in viewBox units
	fill *col                    // nil: the ground at that point (a cut)
}

var shapes = buildShapes()

func buildShapes() []shape {
	ring := func(x, y float64) bool {
		return math.Hypot(x-50, y-56.5) <= 32 && math.Hypot(x-52.5, y-55.5) > 20
	}
	crown := func(x, y float64) bool { return roundRect(x, y, 44.5, 12, 11, 8, 1.6) }
	button := func(x, y float64) bool {
		rx, ry := rotate(x, y, 73.5, 22, -42)
		return roundRect(rx, ry, 70, 19, 7, 6, 1.2)
	}
	nick1 := poly([]pt{{21, 44.5}, {30.5, 49.2}, {30, 50.2}, {20.4, 46.2}})
	nick2 := poly([]pt{{70.5, 75}, {76.6, 70.6}, {77.3, 71.4}, {71.4, 76.1}})
	flame := poly(path(pt{52, 72},
		cubic{pt{41.5, 72}, pt{39, 62.5}, pt{43.5, 56}},
		cubic{pt{44.6, 60.2}, pt{47.4, 60.6}, pt{47.4, 60.6}},
		cubic{pt{45.2, 54}, pt{48.8, 46.4}, pt{55.6, 42}},
		cubic{pt{54.6, 48.3}, pt{58.6, 50.6}, pt{60.8, 55}},
		cubic{pt{64, 60.8}, pt{61.6, 72}, pt{52, 72}},
	))
	core := poly(path(pt{52.3, 69.6},
		cubic{pt{48.2, 69.6}, pt{46.8, 65.4}, pt{48.9, 62.4}},
		cubic{pt{49.9, 64.4}, pt{51.8, 64.6}, pt{51.8, 64.6}},
		cubic{pt{51.6, 61.4}, pt{53, 59}, pt{55.4, 57.6}},
		cubic{pt{56.4, 61.6}, pt{58.5, 62.8}, pt{57.9, 65.9}},
		cubic{pt{57.4, 68.2}, pt{55.2, 69.6}, pt{52.3, 69.6}},
	))
	spark := func(x, y float64) bool { return math.Hypot(x-53.6, y-66.2) <= 1.5 }
	return []shape{
		{crown, &white}, {button, &white}, {ring, &white},
		{nick1, nil}, {nick2, nil},
		{flame, &crimson}, {core, nil}, {spark, &white},
	}
}

func draw() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var r, g, b, a float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					x := float64(px) + (float64(sx)+0.5)/ss
					y := float64(py) + (float64(sy)+0.5)/ss
					cr, cg, cb, ca := sample(x, y)
					r, g, b, a = r+cr*ca, g+cg*ca, b+cb*ca, a+ca
				}
			}
			if a == 0 {
				continue
			}
			img.SetNRGBA(px, py, color.NRGBA{R: u8(r / a), G: u8(g / a), B: u8(b / a), A: u8(a / (ss * ss))})
		}
	}
	return img
}

// ground is the squircle's black, lifted a little at the top.
func ground(y float64) col {
	t := clamp((y - (512 - half)) / (2 * half))
	return col{0.11 - 0.07*t, 0.10 - 0.065*t, 0.12 - 0.075*t}
}

// sample is the colour (0–1, straight alpha) at pixel x, y.
func sample(x, y float64) (r, g, b, a float64) {
	if !inSquircle(x, y) { // soft shadow under the squircle
		if d := squircleDist(x, y-14); d > 0 {
			return 0, 0, 0, 0.28 * math.Exp(-d/18)
		}
		return 0, 0, 0, 0.28
	}
	c := ground(y)
	ux, uy := (x-(512-half))/unit, (y-(512-half))/unit
	for _, s := range shapes {
		if s.in(ux, uy) {
			if s.fill == nil {
				c = ground(y)
			} else {
				c = *s.fill
			}
		}
	}
	return c.r, c.g, c.b, 1
}

type pt struct{ x, y float64 }

type cubic struct{ c1, c2, end pt }

// path flattens a closed run of cubic curves from start into a polygon.
func path(start pt, cs ...cubic) []pt {
	out := []pt{start}
	p := start
	for _, c := range cs {
		for i := 1; i <= 24; i++ {
			t := float64(i) / 24
			u := 1 - t
			out = append(out, pt{
				u*u*u*p.x + 3*u*u*t*c.c1.x + 3*u*t*t*c.c2.x + t*t*t*c.end.x,
				u*u*u*p.y + 3*u*u*t*c.c1.y + 3*u*t*t*c.c2.y + t*t*t*c.end.y,
			})
		}
		p = c.end
	}
	return out
}

// poly is an even-odd point-in-polygon test, with a bounding-box shortcut.
func poly(ps []pt) func(x, y float64) bool {
	minX, minY, maxX, maxY := ps[0].x, ps[0].y, ps[0].x, ps[0].y
	for _, p := range ps {
		minX, minY, maxX, maxY = math.Min(minX, p.x), math.Min(minY, p.y), math.Max(maxX, p.x), math.Max(maxY, p.y)
	}
	return func(x, y float64) bool {
		if x < minX || x > maxX || y < minY || y > maxY {
			return false
		}
		in := false
		for i, j := 0, len(ps)-1; i < len(ps); j, i = i, i+1 {
			a, b := ps[i], ps[j]
			if (a.y > y) != (b.y > y) && x < (b.x-a.x)*(y-a.y)/(b.y-a.y)+a.x {
				in = !in
			}
		}
		return in
	}
}

// roundRect reports whether x, y is inside the rectangle at rx, ry of
// w × h with corner radius rad.
func roundRect(x, y, rx, ry, w, h, rad float64) bool {
	cx, cy := rx+w/2, ry+h/2
	qx, qy := math.Abs(x-cx)-(w/2-rad), math.Abs(y-cy)-(h/2-rad)
	return math.Hypot(math.Max(qx, 0), math.Max(qy, 0))+math.Min(math.Max(qx, qy), 0) <= rad
}

// rotate turns x, y by deg degrees about cx, cy.
func rotate(x, y, cx, cy, deg float64) (float64, float64) {
	s, c := math.Sincos(deg * math.Pi / 180)
	dx, dy := x-cx, y-cy
	return cx + dx*c - dy*s, cy + dx*s + dy*c
}

func inSquircle(x, y float64) bool {
	u, v := math.Abs(x-512)/half, math.Abs(y-512)/half
	return math.Pow(u, 5)+math.Pow(v, 5) <= 1
}

// squircleDist approximates the distance outside the squircle, in px.
func squircleDist(x, y float64) float64 {
	u, v := math.Abs(x-512)/half, math.Abs(y-512)/half
	k := math.Pow(math.Pow(u, 5)+math.Pow(v, 5), 0.2)
	return (k - 1) * half
}

type col struct{ r, g, b float64 }

func rgb(r, g, b uint8) col { return col{float64(r) / 255, float64(g) / 255, float64(b) / 255} }

func clamp(v float64) float64 { return math.Min(math.Max(v, 0), 1) }

func u8(v float64) uint8 { return uint8(math.Round(clamp(v) * 255)) }
