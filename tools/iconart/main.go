// Command iconart draws Hotwatch's app icon, resources/icon.png (1024 px):
// a rose squircle with a white gauge — an arc of 240° lit two thirds of
// the way, a needle at that reading and a hub. Everything is computed here
// (signed distances, 4×4 supersampling); no artwork is copied.
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
	size   = 1024
	half   = 412.0 // the squircle's half side (Apple's 824 px icon grid)
	cx, cy = 512.0, 500.0
	ringR  = 250.0 // gauge radius
	ringW  = 70.0  // gauge stroke
	start  = 150.0 // arc start, degrees clockwise from +x (screen coordinates)
	sweep  = 240.0
	level  = 0.66 // lit share of the arc
)

func main() {
	out := "resources/icon.png"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	const ss = 4
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
			n := float64(ss * ss)
			if a == 0 {
				continue
			}
			img.SetNRGBA(px, py, color.NRGBA{R: u8(r / a), G: u8(g / a), B: u8(b / a), A: u8(a / n)})
		}
	}
	f, err := os.Create(out)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
}

// sample is the colour (0–1, straight alpha) at x, y.
func sample(x, y float64) (r, g, b, a float64) {
	// Soft shadow under the squircle.
	if !inSquircle(x, y) {
		d := squircleDist(x, y-14)
		if d > 0 {
			return 0, 0, 0, 0.28 * math.Exp(-d/18)
		}
		return 0, 0, 0, 0.28
	}
	// Ground: rose at the top left to crimson at the bottom right, with a
	// faint light from the top.
	t := ((x-100)/824 + (y-100)/824) / 2
	r, g, b = mix(rgb(0xfb, 0x71, 0x85), rgb(0xbe, 0x12, 0x3c), clamp(t))
	lift := 0.10 * clamp(1-(y-100)/500)
	r, g, b = r+(1-r)*lift, g+(1-g)*lift, b+(1-b)*lift
	a = 1

	dx, dy := x-cx, y-cy
	d := math.Hypot(dx, dy)
	ang := math.Mod(math.Atan2(dy, dx)*180/math.Pi+360, 360)
	along := math.Mod(ang-start+360, 360) / sweep // 0…1 along the arc, >1 outside it
	white := func(alpha float64) {
		r, g, b = r+(1-r)*alpha, g+(1-g)*alpha, b+(1-b)*alpha
	}
	// The track, then the lit part, with round caps.
	if onArc(d, along, 0, 1, dx, dy) {
		white(0.30)
	}
	if onArc(d, along, 0, level, dx, dy) {
		white(1)
	}
	// The needle: a capsule from the hub to just short of the arc.
	na := (start + sweep*level) * math.Pi / 180
	ex, ey := cx+math.Cos(na)*(ringR-80), cy+math.Sin(na)*(ringR-80)
	if segDist(x, y, cx, cy, ex, ey) <= 22 {
		white(1)
	}
	if d <= 54 {
		white(1)
	}
	if d <= 24 {
		r, g, b = mix(rgb(0xfb, 0x71, 0x85), rgb(0xbe, 0x12, 0x3c), 0.6)
	}
	return r, g, b, a
}

// onArc reports whether a point at distance d and arc position along lies
// on the stroke between from and to (shares of the arc), caps included.
func onArc(d, along, from, to, dx, dy float64) bool {
	if math.Abs(d-ringR) <= ringW/2 && along >= from && along <= to {
		return true
	}
	for _, s := range []float64{from, to} {
		a := (start + sweep*s) * math.Pi / 180
		if math.Hypot(dx-math.Cos(a)*ringR, dy-math.Sin(a)*ringR) <= ringW/2 {
			return true
		}
	}
	return false
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

func segDist(px, py, ax, ay, bx, by float64) float64 {
	vx, vy := bx-ax, by-ay
	t := clamp(((px-ax)*vx + (py-ay)*vy) / (vx*vx + vy*vy))
	return math.Hypot(px-(ax+t*vx), py-(ay+t*vy))
}

type col struct{ r, g, b float64 }

func rgb(r, g, b uint8) col { return col{float64(r) / 255, float64(g) / 255, float64(b) / 255} }

func mix(a, b col, t float64) (float64, float64, float64) {
	return a.r + (b.r-a.r)*t, a.g + (b.g-a.g)*t, a.b + (b.b-a.b)*t
}

func clamp(v float64) float64 { return math.Min(math.Max(v, 0), 1) }

func u8(v float64) uint8 { return uint8(math.Round(clamp(v) * 255)) }
