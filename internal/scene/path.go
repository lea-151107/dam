package scene

import "math"

// Op is a path command.
type Op uint8

// Path commands. Each consumes 1 (Move, Line), 2 (Quad) or 3 (Cube) points.
const (
	MoveTo Op = iota
	LineTo
	QuadTo
	CubeTo
	Close
)

// Path is a sequence of subpaths.
type Path struct {
	Ops []Op
	Pts []Point
}

// NewPath returns an empty path.
func NewPath() *Path { return &Path{} }

// MoveTo starts a subpath.
func (p *Path) MoveTo(x, y float64) *Path {
	p.Ops = append(p.Ops, MoveTo)
	p.Pts = append(p.Pts, Point{x, y})
	return p
}

// LineTo adds a line segment.
func (p *Path) LineTo(x, y float64) *Path {
	p.Ops = append(p.Ops, LineTo)
	p.Pts = append(p.Pts, Point{x, y})
	return p
}

// QuadTo adds a quadratic Bézier segment.
func (p *Path) QuadTo(cx, cy, x, y float64) *Path {
	p.Ops = append(p.Ops, QuadTo)
	p.Pts = append(p.Pts, Point{cx, cy}, Point{x, y})
	return p
}

// CubeTo adds a cubic Bézier segment.
func (p *Path) CubeTo(c1x, c1y, c2x, c2y, x, y float64) *Path {
	p.Ops = append(p.Ops, CubeTo)
	p.Pts = append(p.Pts, Point{c1x, c1y}, Point{c2x, c2y}, Point{x, y})
	return p
}

// Close closes the current subpath.
func (p *Path) Close() *Path {
	p.Ops = append(p.Ops, Close)
	return p
}

// Polyline returns an open path through pts.
func Polyline(pts []Point) *Path {
	p := NewPath()
	for i, q := range pts {
		if i == 0 {
			p.MoveTo(q.X, q.Y)
		} else {
			p.LineTo(q.X, q.Y)
		}
	}
	return p
}

// Polygon returns a closed path through pts.
func Polygon(pts []Point) *Path { return Polyline(pts).Close() }

// RectPath returns a rectangle.
func RectPath(r Rect) *Path {
	return Polygon([]Point{{r.X, r.Y}, {r.X + r.W, r.Y}, {r.X + r.W, r.Y + r.H}, {r.X, r.Y + r.H}})
}

// kappa is the cubic Bézier control distance for a quarter circle.
const kappa = 0.5522847498

// RoundRect returns a rectangle with corner radius rad.
func RoundRect(r Rect, rad float64) *Path {
	rad = math.Min(rad, float64(math.Min(r.W, r.H)/2))
	if rad <= 0 {
		return RectPath(r)
	}
	k := float64(rad * kappa)
	x0, y0, x1, y1 := r.X, r.Y, r.X+r.W, r.Y+r.H
	p := NewPath().MoveTo(x0+rad, y0).LineTo(x1-rad, y0)
	p.CubeTo(x1-rad+k, y0, x1, y0+rad-k, x1, y0+rad)
	p.LineTo(x1, y1-rad)
	p.CubeTo(x1, y1-rad+k, x1-rad+k, y1, x1-rad, y1)
	p.LineTo(x0+rad, y1)
	p.CubeTo(x0+rad-k, y1, x0, y1-rad+k, x0, y1-rad)
	p.LineTo(x0, y0+rad)
	p.CubeTo(x0, y0+rad-k, x0+rad-k, y0, x0+rad, y0)
	return p.Close()
}

// Ellipse returns an ellipse centered at c.
func Ellipse(c Point, rx, ry float64) *Path {
	kx, ky := float64(rx*kappa), float64(ry*kappa)
	p := NewPath().MoveTo(c.X+rx, c.Y)
	p.CubeTo(c.X+rx, c.Y+ky, c.X+kx, c.Y+ry, c.X, c.Y+ry)
	p.CubeTo(c.X-kx, c.Y+ry, c.X-rx, c.Y+ky, c.X-rx, c.Y)
	p.CubeTo(c.X-rx, c.Y-ky, c.X-kx, c.Y-ry, c.X, c.Y-ry)
	p.CubeTo(c.X+kx, c.Y-ry, c.X+rx, c.Y-ky, c.X+rx, c.Y)
	return p.Close()
}

// Arc appends a circular arc from angle a0 to a1 (radians, y down) to p.
// If p has no current point the arc starts with MoveTo, otherwise LineTo.
func (p *Path) Arc(c Point, r, a0, a1 float64) *Path {
	start := Point{c.X + float64(r*math.Cos(a0)), c.Y + float64(r*math.Sin(a0))}
	if len(p.Ops) == 0 || p.Ops[len(p.Ops)-1] == Close {
		p.MoveTo(start.X, start.Y)
	} else {
		p.LineTo(start.X, start.Y)
	}
	n := int(math.Ceil(float64(math.Abs(a1-a0) / (math.Pi / 2))))
	if n < 1 {
		n = 1
	}
	step := float64((a1 - a0) / float64(n))
	k := float64(4.0 / 3 * math.Tan(float64(step/4)))
	for i := 0; i < n; i++ {
		t0 := a0 + float64(float64(i)*step)
		t1 := t0 + step
		c0, s0 := math.Cos(t0), math.Sin(t0)
		c1, s1 := math.Cos(t1), math.Sin(t1)
		p.CubeTo(c.X+float64(r*(c0-float64(k*s0))), c.Y+float64(r*(s0+float64(k*c0))),
			c.X+float64(r*(c1+float64(k*s1))), c.Y+float64(r*(s1-float64(k*c1))),
			c.X+float64(r*c1), c.Y+float64(r*s1))
	}
	return p
}

// Subpath is a flattened subpath.
type Subpath struct {
	Pts    []Point
	Closed bool
}

// Flatten converts the path to polylines with maximum error tol.
func (p *Path) Flatten(tol float64) []Subpath {
	var out []Subpath
	var cur *Subpath
	var start, last Point
	pi := 0
	flush := func() {
		if cur != nil && len(cur.Pts) > 0 {
			out = append(out, *cur)
		}
		cur = nil
	}
	ensure := func() {
		if cur == nil {
			cur = &Subpath{Pts: []Point{last}}
			start = last
		}
	}
	for _, op := range p.Ops {
		switch op {
		case MoveTo:
			flush()
			last = p.Pts[pi]
			pi++
			cur = &Subpath{Pts: []Point{last}}
			start = last
		case LineTo:
			ensure()
			last = p.Pts[pi]
			pi++
			cur.Pts = append(cur.Pts, last)
		case QuadTo:
			ensure()
			c, e := p.Pts[pi], p.Pts[pi+1]
			pi += 2
			// Elevate to cubic for a single flattening routine.
			c1 := last.Add(c.Sub(last).Mul(2.0 / 3))
			c2 := e.Add(c.Sub(e).Mul(2.0 / 3))
			cur.Pts = flattenCubic(cur.Pts, last, c1, c2, e, tol)
			last = e
		case CubeTo:
			ensure()
			c1, c2, e := p.Pts[pi], p.Pts[pi+1], p.Pts[pi+2]
			pi += 3
			cur.Pts = flattenCubic(cur.Pts, last, c1, c2, e, tol)
			last = e
		case Close:
			if cur != nil {
				cur.Closed = true
				flush()
			}
			last = start
		}
	}
	flush()
	return out
}

func flattenCubic(dst []Point, p0, p1, p2, p3 Point, tol float64) []Point {
	// Segment count from the control polygon's deviation.
	d := math.Max(p1.Sub(p0.Add(p3.Sub(p0).Mul(1.0/3))).Len(), p2.Sub(p0.Add(p3.Sub(p0).Mul(2.0/3))).Len())
	n := int(math.Ceil(math.Sqrt(float64(d / math.Max(tol, 0.01)))))
	if n < 1 {
		n = 1
	}
	if n > 100 {
		n = 100
	}
	for i := 1; i <= n; i++ {
		t := float64(float64(i) / float64(n))
		u := 1 - t
		a, b, c, e := float64(u*u*u), float64(3*u*u*t), float64(3*u*t*t), float64(t*t*t)
		dst = append(dst, Point{float64(a*p0.X) + float64(b*p1.X) + float64(c*p2.X) + float64(e*p3.X), float64(a*p0.Y) + float64(b*p1.Y) + float64(c*p2.Y) + float64(e*p3.Y)})
	}
	return dst
}

// Bounds returns the bounding box of the control points.
func (p *Path) Bounds() Rect {
	if len(p.Pts) == 0 {
		return Rect{}
	}
	x0, y0, x1, y1 := p.Pts[0].X, p.Pts[0].Y, p.Pts[0].X, p.Pts[0].Y
	for _, q := range p.Pts[1:] {
		x0, y0 = math.Min(x0, q.X), math.Min(y0, q.Y)
		x1, y1 = math.Max(x1, q.X), math.Max(y1, q.Y)
	}
	return Rect{x0, y0, x1 - x0, y1 - y0}
}
