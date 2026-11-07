package curve

import (
	"math"
	"testing"

	"golang.org/x/image/math/fixed"
)

func pt(x, y float64) fixed.Point26_6 {
	return fixed.Point26_6{X: fixed.Int26_6(x * 64), Y: fixed.Int26_6(y * 64)}
}

// TestFlatten checks that the segments end exactly at the end point, stay
// within the tolerance of the true curve, and are bounded in number.
func TestFlatten(t *testing.T) {
	for _, tc := range []struct {
		name string
		quad bool
		p    [4]fixed.Point26_6
	}{
		{name: "quad", quad: true, p: [4]fixed.Point26_6{pt(0, 0), pt(50, 100), pt(100, 0)}},
		{name: "quad straight", quad: true, p: [4]fixed.Point26_6{pt(0, 0), pt(5, 5), pt(10, 10)}},
		{name: "cube", p: [4]fixed.Point26_6{pt(0, 0), pt(0, 300), pt(300, 300), pt(300, 0)}},
		{name: "cube tiny", p: [4]fixed.Point26_6{pt(1, 1), pt(1.2, 1.5), pt(1.5, 1.5), pt(2, 1)}},
		{name: "cube negative", p: [4]fixed.Point26_6{pt(-10, -10), pt(-40, 20), pt(30, -50), pt(10, 10)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []fixed.Point26_6
			emit := func(p fixed.Point26_6) { got = append(got, p) }
			end := tc.p[3]
			if tc.quad {
				Quad(tc.p[0], tc.p[1], tc.p[2], emit)
				end = tc.p[2]
			} else {
				Cube(tc.p[0], tc.p[1], tc.p[2], tc.p[3], emit)
			}
			if len(got) == 0 || len(got) > maxSegments {
				t.Fatalf("%d segments", len(got))
			}
			if got[len(got)-1] != end {
				t.Errorf("last point %v, want %v", got[len(got)-1], end)
			}
			// Every emitted point lies on the curve (within rounding).
			n := float64(len(got))
			for i, p := range got {
				u := float64(i+1) / n
				x, y := eval(tc.quad, tc.p, u)
				if d := math.Hypot(float64(p.X)-x, float64(p.Y)-y); d > 1 {
					t.Errorf("point %d is %.2f/64 px off the curve", i, d)
				}
			}
		})
	}
}

func eval(quad bool, p [4]fixed.Point26_6, u float64) (x, y float64) {
	f := func(i int) (float64, float64) { return float64(p[i].X), float64(p[i].Y) }
	v := 1 - u
	if quad {
		x0, y0 := f(0)
		x1, y1 := f(1)
		x2, y2 := f(2)
		return v*v*x0 + 2*u*v*x1 + u*u*x2, v*v*y0 + 2*u*v*y1 + u*u*y2
	}
	x0, y0 := f(0)
	x1, y1 := f(1)
	x2, y2 := f(2)
	x3, y3 := f(3)
	return v*v*v*x0 + 3*u*v*v*x1 + 3*u*u*v*x2 + u*u*u*x3, v*v*v*y0 + 3*u*v*v*y1 + 3*u*u*v*y2 + u*u*u*y3
}
