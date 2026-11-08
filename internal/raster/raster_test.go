package raster

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"testing"

	"golang.org/x/image/vector"

	"github.com/lea-151107/dam/internal/scene"
)

var black = color.NRGBA{0, 0, 0, 255}

// fillOnce fills polys with the tiled renderer into a w×h image and
// returns the alpha of each pixel.
func fillOnce(w, h int, polys [][]scene.Point) *image.RGBA {
	r := &renderer{dst: image.NewRGBA(image.Rect(0, 0, w, h)), scale: 1}
	r.fill(polys, black)
	return r.dst
}

// reference rasterizes polys in one pass over the whole image, as fill did
// before tiling. Its float32 accumulation is not the same on every CPU, so
// it is only compared within a tolerance.
func reference(w, h int, polys [][]scene.Point) *image.RGBA {
	var z vector.Rasterizer
	z.Reset(w, h)
	z.DrawOp = draw.Over
	for _, p := range polys {
		z.MoveTo(float32(p[0].X), float32(p[0].Y))
		for _, q := range p[1:] {
			z.LineTo(float32(q.X), float32(q.Y))
		}
		z.ClosePath()
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	z.Draw(dst, dst.Bounds(), image.NewUniform(black), image.Point{})
	return dst
}

func alpha(img *image.RGBA, x, y int) int { return int(img.RGBAAt(x, y).A) }

// band returns a parallelogram of the given width (in x) along the line
// from (x0, y0) to (x1, y1).
func band(x0, y0, x1, y1, width float64) []scene.Point {
	return []scene.Point{{X: x0 - width/2, Y: y0}, {X: x1 - width/2, Y: y1}, {X: x1 + width/2, Y: y1}, {X: x0 + width/2, Y: y0}}
}

// rowCenter returns the coverage-weighted center of row y.
func rowCenter(img *image.RGBA, y int) float64 {
	var sum, wsum float64
	for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
		a := float64(alpha(img, x, y))
		sum += a * (float64(x) + 0.5)
		wsum += a
	}
	return sum / wsum
}

// TestLongDiagonal checks that a long slanted edge stays where it should in
// every tile. Before edges were clipped to the rows of each tile, the
// integer rasterizer walked them from their top through all the rows above
// a tile and drifted: 6.6 px off after 6000 rows.
func TestLongDiagonal(t *testing.T) {
	const w, h = 600, 6000
	x0, y0, x1, y1 := 1.3, 0.4, 500.0, 5999.6
	img := fillOnce(w, h, [][]scene.Point{band(x0, y0, x1, y1, 2)})
	for _, y := range []int{10, 511, 512, 1500, 3000, 4607, 5990} {
		want := x0 + (float64(y)+0.5-y0)/(y1-y0)*(x1-x0)
		if got := rowCenter(img, y); math.Abs(got-want) > 0.1 {
			t.Errorf("row %d: center %.3f, want %.3f", y, got, want)
		}
	}
}

// TestMatchesSinglePass compares tiled fills with a single pass over the
// whole image, for shapes that cross tile boundaries in both directions.
func TestMatchesSinglePass(t *testing.T) {
	const w, h = 1300, 1700
	var circle []scene.Point
	for i := 0; i < 200; i++ {
		a := 2 * math.Pi * float64(i) / 200
		circle = append(circle, scene.Point{X: 650.3 + 600*math.Cos(a), Y: 850.7 + 800*math.Sin(a)})
	}
	tests := map[string][][]scene.Point{
		"ellipse":  {circle},
		"triangle": {{{X: 3.2, Y: 1690.1}, {X: 1296.4, Y: 1500.3}, {X: 700.7, Y: 2.9}}},
		"frame": { // a stroked rectangle: outer and inner path in opposite directions
			{{X: 10.5, Y: 10.5}, {X: 1290.5, Y: 10.5}, {X: 1290.5, Y: 1690.5}, {X: 10.5, Y: 1690.5}},
			{{X: 12.5, Y: 12.5}, {X: 12.5, Y: 1688.5}, {X: 1288.5, Y: 1688.5}, {X: 1288.5, Y: 12.5}},
		},
		"diagonal": {band(1.3, 0.4, 1290, 1699.6, 2)},
	}
	for name, polys := range tests {
		t.Run(name, func(t *testing.T) {
			got, want := fillOnce(w, h, polys), reference(w, h, polys)
			worst, n := 0, 0
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					if d := abs(alpha(got, x, y) - alpha(want, x, y)); d > 0 {
						n++
						worst = max(worst, d)
					}
				}
			}
			// Integer and float accumulation round differently on the
			// antialiased edge pixels of steep edges, by up to about 5%.
			// Seams, lost coverage or drift differ by far more (the
			// unclipped tiles of 0.6.12 differed by 173 here).
			if worst > 16 {
				t.Errorf("%d pixels differ, by up to %d", n, worst)
			}
		})
	}
}

// TestTileSeams checks that a filled rectangle with fractional edges is
// fully covered inside, including on both sides of tile boundaries.
func TestTileSeams(t *testing.T) {
	const w, h = 1100, 1100
	img := fillOnce(w, h, [][]scene.Point{{{X: 3.25, Y: 2.75}, {X: 1096.5, Y: 2.75}, {X: 1096.5, Y: 1097.25}, {X: 3.25, Y: 1097.25}}})
	for _, v := range []int{4, 511, 512, 513, 1023, 1024, 1095} {
		for _, u := range []int{4, 300, 511, 512, 1024, 1095} {
			if a := alpha(img, v, u); a != 255 {
				t.Errorf("pixel (%d, %d) alpha %d, want 255", v, u, a)
			}
			if a := alpha(img, u, v); a != 255 {
				t.Errorf("pixel (%d, %d) alpha %d, want 255", u, v, a)
			}
		}
	}
}

// TestWinding checks that the direction of a polygon does not matter:
// edges are clipped pointing down and added in their own direction.
func TestWinding(t *testing.T) {
	const w, h = 900, 1400
	cw := []scene.Point{{X: 5.5, Y: 3.1}, {X: 880.2, Y: 700.6}, {X: 120.9, Y: 1390.4}}
	ccw := []scene.Point{cw[2], cw[1], cw[0]}
	a, b := fillOnce(w, h, [][]scene.Point{cw}), fillOnce(w, h, [][]scene.Point{ccw})
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if alpha(a, x, y) != alpha(b, x, y) {
				t.Fatalf("pixel (%d, %d): %d clockwise, %d counter-clockwise", x, y, alpha(a, x, y), alpha(b, x, y))
			}
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
