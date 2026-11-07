// Package scene is a device-independent list of drawing commands. Units are
// layout pixels at Scale = 1.
package scene

import (
	"image/color"
	"math"
)

// Point is a 2D point.
type Point struct{ X, Y float64 }

// Add returns p+q.
func (p Point) Add(q Point) Point { return Point{p.X + q.X, p.Y + q.Y} }

// Sub returns p-q.
func (p Point) Sub(q Point) Point { return Point{p.X - q.X, p.Y - q.Y} }

// Mul returns p*k.
func (p Point) Mul(k float64) Point { return Point{float64(p.X * k), float64(p.Y * k)} }

// Len returns |p|.
func (p Point) Len() float64 { return math.Hypot(p.X, p.Y) }

// Norm returns p scaled to unit length (zero stays zero).
func (p Point) Norm() Point {
	l := p.Len()
	if l < 1e-9 {
		return Point{}
	}
	return Point{float64(p.X / l), float64(p.Y / l)}
}

// Rect is an axis-aligned rectangle.
type Rect struct{ X, Y, W, H float64 }

// Center returns the center of r.
func (r Rect) Center() Point { return Point{r.X + float64(r.W/2), r.Y + float64(r.H/2)} }

// Union returns the smallest rectangle containing r and o. A zero r is
// treated as empty.
func (r Rect) Union(o Rect) Rect {
	if r.W == 0 && r.H == 0 && r.X == 0 && r.Y == 0 {
		return o
	}
	x0, y0 := math.Min(r.X, o.X), math.Min(r.Y, o.Y)
	x1, y1 := math.Max(r.X+r.W, o.X+o.W), math.Max(r.Y+r.H, o.Y+o.H)
	return Rect{x0, y0, x1 - x0, y1 - y0}
}

// Inset grows r by d on each side.
func (r Rect) Inset(d float64) Rect {
	return Rect{r.X - d, r.Y - d, r.W + float64(2*d), r.H + float64(2*d)}
}

// Contains reports whether p is inside r.
func (r Rect) Contains(p Point) bool {
	return p.X >= r.X && p.X <= r.X+r.W && p.Y >= r.Y && p.Y <= r.Y+r.H
}

// Stroke describes an outline. Width == 0 means no outline.
type Stroke struct {
	Color color.NRGBA
	Width float64
	Dash  []float64
}

// Shape is a filled and/or stroked path.
type Shape struct {
	Path   *Path
	Fill   color.NRGBA // A == 0: no fill
	Stroke Stroke
}

// Align is horizontal text alignment relative to Text.X.
type Align uint8

// Alignments.
const (
	Center Align = iota
	Left
	Right
)

// Text is a block of lines whose vertical center is at Y.
type Text struct {
	X, Y  float64
	Lines []string
	Align Align
	Color color.NRGBA
}

// Item is a drawable element: *Shape or *Text.
type Item interface{ isItem() }

func (*Shape) isItem() {}
func (*Text) isItem()  {}

// Scene is an ordered list of items on a canvas of Width×Height.
type Scene struct {
	Width, Height float64
	Items         []Item
}

// Add appends items.
func (s *Scene) Add(items ...Item) { s.Items = append(s.Items, items...) }

// Fill appends a filled path with an optional outline.
func (s *Scene) Fill(p *Path, fill color.NRGBA, stroke color.NRGBA, width float64) {
	sh := &Shape{Path: p, Fill: fill}
	if width > 0 && stroke.A > 0 {
		sh.Stroke = Stroke{Color: stroke, Width: width}
	}
	s.Add(sh)
}

// Line appends a stroked path.
func (s *Scene) Line(p *Path, c color.NRGBA, width float64, dash []float64) {
	s.Add(&Shape{Path: p, Stroke: Stroke{Color: c, Width: width, Dash: dash}})
}

// Label appends a text block.
func (s *Scene) Label(x, y float64, lines []string, align Align, c color.NRGBA) {
	if len(lines) == 0 {
		return
	}
	s.Add(&Text{X: x, Y: y, Lines: lines, Align: align, Color: c})
}

// Translate moves every item by d and grows nothing; used to add margins.
func (s *Scene) Translate(d Point) {
	for _, it := range s.Items {
		switch v := it.(type) {
		case *Shape:
			for i := range v.Path.Pts {
				v.Path.Pts[i] = v.Path.Pts[i].Add(d)
			}
		case *Text:
			v.X += d.X
			v.Y += d.Y
		}
	}
}
