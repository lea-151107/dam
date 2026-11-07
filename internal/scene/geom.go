package scene

import "math"

// Smooth returns a path through pts using Catmull-Rom splines converted to
// cubic Béziers. Two points give a straight line.
func Smooth(pts []Point) *Path {
	p := NewPath()
	if len(pts) == 0 {
		return p
	}
	p.MoveTo(pts[0].X, pts[0].Y)
	if len(pts) == 2 {
		return p.LineTo(pts[1].X, pts[1].Y)
	}
	const tension = 1.0 / 6
	for i := 0; i < len(pts)-1; i++ {
		p0 := pts[max(i-1, 0)]
		p1 := pts[i]
		p2 := pts[i+1]
		p3 := pts[min(i+2, len(pts)-1)]
		c1 := p1.Add(p2.Sub(p0).Mul(tension))
		c2 := p2.Sub(p3.Sub(p1).Mul(tension))
		p.CubeTo(c1.X, c1.Y, c2.X, c2.Y, p2.X, p2.Y)
	}
	return p
}

// ClipStart trims the start of polyline pts where it is inside outline
// (a closed polygon) and returns the new polyline. When no intersection is
// found the input is returned unchanged.
func ClipStart(pts []Point, outline []Point) []Point {
	for i := 0; i+1 < len(pts); i++ {
		if q, ok := segPolygon(pts[i], pts[i+1], outline, true); ok {
			out := append([]Point{q}, pts[i+1:]...)
			return out
		}
		if !pointInPolygon(pts[i+1], outline) {
			break
		}
	}
	return pts
}

// ClipEnd trims the end of polyline pts where it is inside outline.
func ClipEnd(pts []Point, outline []Point) []Point {
	rev := reverse(pts)
	rev = ClipStart(rev, outline)
	return reverse(rev)
}

// ClipStartRect trims the start of pts to the boundary of r.
func ClipStartRect(pts []Point, r Rect) []Point { return ClipStart(pts, RectPoints(r)) }

// ClipEndRect trims the end of pts to the boundary of r.
func ClipEndRect(pts []Point, r Rect) []Point { return ClipEnd(pts, RectPoints(r)) }

// RectPoints returns the corners of r.
func RectPoints(r Rect) []Point {
	return []Point{{r.X, r.Y}, {r.X + r.W, r.Y}, {r.X + r.W, r.Y + r.H}, {r.X, r.Y + r.H}}
}

// EllipsePoints approximates an ellipse with a polygon.
func EllipsePoints(c Point, rx, ry float64) []Point {
	const n = 32
	pts := make([]Point, n)
	for i := range pts {
		a := float64(2 * math.Pi * float64(i) / n)
		pts[i] = Point{c.X + float64(rx*math.Cos(a)), c.Y + float64(ry*math.Sin(a))}
	}
	return pts
}

func reverse(pts []Point) []Point {
	out := make([]Point, len(pts))
	for i, p := range pts {
		out[len(pts)-1-i] = p
	}
	return out
}

// segPolygon returns the intersection of segment a→b with the polygon that
// is farthest along the segment from a (the exit point when a is inside).
func segPolygon(a, b Point, poly []Point, farthest bool) (Point, bool) {
	best := -1.0
	var hit Point
	for i := range poly {
		c, d := poly[i], poly[(i+1)%len(poly)]
		t, ok := segIntersect(a, b, c, d)
		if !ok {
			continue
		}
		if best < 0 || (farthest && t > best) || (!farthest && t < best) {
			best = t
			hit = a.Add(b.Sub(a).Mul(t))
		}
	}
	return hit, best >= 0
}

func segIntersect(a, b, c, d Point) (float64, bool) {
	r := b.Sub(a)
	s := d.Sub(c)
	// float64(...) keeps the cross products from being fused into FMAs,
	// which arm64 did and amd64 did not: t and u near 0 or 1 could land on
	// different sides and change the path.
	den := float64(r.X*s.Y) - float64(r.Y*s.X)
	if math.Abs(den) < 1e-12 {
		return 0, false
	}
	ca := c.Sub(a)
	t := float64((float64(ca.X*s.Y) - float64(ca.Y*s.X)) / den)
	u := float64((float64(ca.X*r.Y) - float64(ca.Y*r.X)) / den)
	if t < 0 || t > 1 || u < 0 || u > 1 {
		return 0, false
	}
	return t, true
}

func pointInPolygon(p Point, poly []Point) bool {
	in := false
	for i, j := 0, len(poly)-1; i < len(poly); j, i = i, i+1 {
		a, b := poly[i], poly[j]
		if (a.Y > p.Y) != (b.Y > p.Y) && p.X < float64((b.X-a.X)*(p.Y-a.Y)/(b.Y-a.Y))+a.X {
			in = !in
		}
	}
	return in
}

// Shorten moves the last point of pts back by d along the last segment,
// so that a marker can be drawn in the freed space.
func Shorten(pts []Point, d float64) []Point {
	if len(pts) < 2 || d <= 0 {
		return pts
	}
	out := append([]Point(nil), pts...)
	n := len(out)
	seg := out[n-1].Sub(out[n-2])
	l := seg.Len()
	if l <= d {
		if n > 2 {
			return Shorten(out[:n-1], d-l)
		}
		out[n-1] = out[n-2].Add(seg.Mul(0.01))
		return out
	}
	out[n-1] = out[n-1].Sub(seg.Norm().Mul(d))
	return out
}

// ShortenStart is Shorten applied to the first point.
func ShortenStart(pts []Point, d float64) []Point { return reverse(Shorten(reverse(pts), d)) }

// MidPoint returns the point halfway along the polyline.
func MidPoint(pts []Point) Point {
	if len(pts) == 0 {
		return Point{}
	}
	total := 0.0
	for i := 1; i < len(pts); i++ {
		total += pts[i].Sub(pts[i-1]).Len()
	}
	half := float64(total / 2)
	for i := 1; i < len(pts); i++ {
		l := pts[i].Sub(pts[i-1]).Len()
		if half <= l && l > 0 {
			return pts[i-1].Add(pts[i].Sub(pts[i-1]).Mul(float64(half / l)))
		}
		half -= l
	}
	return pts[len(pts)-1]
}

// dedupeEps is the distance below which consecutive points are merged.
const dedupeEps = 0.5

// Dedupe removes points that (nearly) repeat the previous one. Clipping a
// polyline at a border it already ends on leaves such a pair, and a
// zero-length last segment gives an arrow head no direction.
func Dedupe(pts []Point) []Point {
	if len(pts) < 2 {
		return pts
	}
	out := []Point{pts[0]}
	for _, p := range pts[1:] {
		if math.Hypot(p.X-out[len(out)-1].X, p.Y-out[len(out)-1].Y) < dedupeEps {
			// Keep the later point so that the end stays exact.
			out[len(out)-1] = p
			continue
		}
		out = append(out, p)
	}
	return out
}
