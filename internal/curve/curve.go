// Package curve flattens Bézier curves into line segments using only
// integer arithmetic on 26.6 fixed-point coordinates.
//
// The rasterizer in golang.org/x/image/vector flattens curves with float32
// arithmetic, which the Go compiler may fuse into multiply-adds on arm64 and
// with GOAMD64=v3. Fused and unfused builds then produce different
// coverage, so the same input gives different pixels on different machines.
// Flattening here and passing only straight lines to the rasterizer keeps
// the output identical everywhere.
package curve

import "golang.org/x/image/math/fixed"

// maxSegments bounds the segments one curve is split into.
const maxSegments = 64

// tolUnits is the flattening tolerance in 1/64 pixel (1/8 pixel).
const tolUnits = 8

// Quad calls lineTo for the end points of the segments that approximate
// the quadratic curve from p0 through control point p1 to p2. The last
// point is exactly p2.
func Quad(p0, p1, p2 fixed.Point26_6, lineTo func(fixed.Point26_6)) {
	// The distance from the chord is at most |p0 - 2p1 + p2| / (4 n²).
	d := max(abs(int64(p0.X)-2*int64(p1.X)+int64(p2.X)), abs(int64(p0.Y)-2*int64(p1.Y)+int64(p2.Y)))
	n := segments(d, 4*tolUnits)
	nn := n * n
	for i := int64(1); i < n; i++ {
		j := n - i
		a, b, c := j*j, 2*i*j, i*i
		lineTo(fixed.Point26_6{
			X: fixed.Int26_6(divRound(a*int64(p0.X)+b*int64(p1.X)+c*int64(p2.X), nn)),
			Y: fixed.Int26_6(divRound(a*int64(p0.Y)+b*int64(p1.Y)+c*int64(p2.Y), nn)),
		})
	}
	lineTo(p2)
}

// Cube is Quad for the cubic curve from p0 through p1 and p2 to p3.
func Cube(p0, p1, p2, p3 fixed.Point26_6, lineTo func(fixed.Point26_6)) {
	// The distance from the chord is at most 3/4 · max second difference / n².
	d := max(
		abs(int64(p0.X)-2*int64(p1.X)+int64(p2.X)), abs(int64(p0.Y)-2*int64(p1.Y)+int64(p2.Y)),
		abs(int64(p1.X)-2*int64(p2.X)+int64(p3.X)), abs(int64(p1.Y)-2*int64(p2.Y)+int64(p3.Y)))
	n := segments(3*d, 4*tolUnits)
	nnn := n * n * n
	for i := int64(1); i < n; i++ {
		j := n - i
		a, b, c, e := j*j*j, 3*i*j*j, 3*i*i*j, i*i*i
		lineTo(fixed.Point26_6{
			X: fixed.Int26_6(divRound(a*int64(p0.X)+b*int64(p1.X)+c*int64(p2.X)+e*int64(p3.X), nnn)),
			Y: fixed.Int26_6(divRound(a*int64(p0.Y)+b*int64(p1.Y)+c*int64(p2.Y)+e*int64(p3.Y), nnn)),
		})
	}
	lineTo(p3)
}

// segments returns the smallest n in [1, maxSegments] with n² ≥ d / div.
func segments(d, div int64) int64 {
	n := int64(1)
	for n < maxSegments && n*n*div < d {
		n++
	}
	return n
}

// divRound divides with rounding half away from zero (b > 0).
func divRound(a, b int64) int64 {
	if a >= 0 {
		return (a + b/2) / b
	}
	return -((-a + b/2) / b)
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
