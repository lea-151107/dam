package scene

import (
	"image/color"
	"math"
)

// Marker is an edge end decoration.
type Marker uint8

// Markers.
const (
	NoMarker      Marker = iota
	Arrow                // filled triangle
	OpenArrow            // two strokes ("V")
	Triangle             // hollow triangle (inheritance)
	Diamond              // filled diamond (composition)
	HollowDiamond        // hollow diamond (aggregation)
	Circle               // hollow circle
	Cross                // "x"
	HalfArrow            // async arrow: one stroke
	// Crow's foot cardinalities (entity relationship diagrams).
	OneOnly    // ||
	ZeroOrOne  // o|
	OneOrMore  // }|
	ZeroOrMore // }o
)

// MarkerLen returns how much a line should be shortened before drawing m at
// its end with size s.
func MarkerLen(m Marker, s float64) float64 {
	switch m {
	case Arrow, Triangle:
		return s
	case Diamond, HollowDiamond:
		return float64(s * 1.6)
	case Circle:
		return float64(s * 0.9)
	}
	// The crow's foot markers are drawn over the line.
	return 0
}

// DrawMarker draws m at tip, pointing from 'from' toward tip.
func (s *Scene) DrawMarker(m Marker, tip, from Point, size float64, c, bg color.NRGBA, width float64) {
	dir := tip.Sub(from).Norm()
	if dir == (Point{}) {
		dir = Point{0, 1}
	}
	nrm := Point{-dir.Y, dir.X}
	at := func(along, side float64) Point { return tip.Sub(dir.Mul(along)).Add(nrm.Mul(side)) }
	w := float64(size * 0.5)
	switch m {
	case Arrow:
		s.Fill(Polygon([]Point{tip, at(size, w), at(float64(size*0.8), 0), at(size, -w)}), c, c, float64(width*0.5))
	case Triangle:
		s.Fill(Polygon([]Point{tip, at(size, float64(w*1.1)), at(size, float64(-w*1.1))}), bg, c, width)
	case OpenArrow:
		s.Line(Polyline([]Point{at(float64(size*0.8), w), tip, at(float64(size*0.8), -w)}), c, width, nil)
	case HalfArrow:
		s.Line(Polyline([]Point{at(float64(size*0.8), w), tip}), c, width, nil)
	case Diamond, HollowDiamond:
		l := float64(size * 1.6)
		pts := []Point{tip, at(float64(l/2), float64(w*0.9)), at(l, 0), at(float64(l/2), float64(-w*0.9))}
		fill := c
		if m == HollowDiamond {
			fill = bg
		}
		s.Fill(Polygon(pts), fill, c, width)
	case Circle:
		r := float64(size * 0.45)
		s.Fill(Ellipse(at(r, 0), r, r), bg, c, width)
	case OneOnly, ZeroOrOne, OneOrMore, ZeroOrMore:
		// The many fork touches the box; bars and the circle sit behind it.
		bar := func(along float64) {
			s.Line(Polyline([]Point{at(along, w), at(along, -w)}), c, width, nil)
		}
		inner := float64(size * 0.4)
		if m == OneOrMore || m == ZeroOrMore {
			fork := float64(size * 0.9)
			s.Line(Polyline([]Point{at(0, w), at(fork, 0), at(0, -w)}), c, width, nil)
			inner = fork + float64(size*0.3)
		} else {
			bar(inner)
			inner += float64(size * 0.4)
		}
		if m == ZeroOrOne || m == ZeroOrMore {
			r := float64(size * 0.35)
			s.Fill(Ellipse(at(inner+r, 0), r, r), bg, c, width)
		} else {
			bar(inner)
		}
	case Cross:
		d := float64(size * 0.45)
		ctr := at(d, 0)
		a := Point{dir.X + nrm.X, dir.Y + nrm.Y}.Norm().Mul(d)
		b := Point{dir.X - nrm.X, dir.Y - nrm.Y}.Norm().Mul(d)
		s.Line(Polyline([]Point{ctr.Sub(a), ctr.Add(a)}), c, float64(width*1.3), nil)
		s.Line(Polyline([]Point{ctr.Sub(b), ctr.Add(b)}), c, float64(width*1.3), nil)
	}
}

// Angle returns the angle of p in radians.
func Angle(p Point) float64 { return math.Atan2(p.Y, p.X) }
