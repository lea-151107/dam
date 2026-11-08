package flowchart

import (
	"image/color"
	"math"

	"github.com/lea-151107/dam/internal/scene"
)

// nodeSize returns the outer size of a shape that holds text of tw×th.
func nodeSize(s Shape, tw, th float64) (w, h float64) {
	w, h = tw+2*padX, th+2*padY
	switch s {
	case Circle, DoubleCircle:
		d := math.Max(tw, th) + 2*padY
		return d, d
	case Rhombus:
		d := tw + th + 2*padY
		return d, d
	case Hexagon:
		return w + float64(h/2), h
	case Parallelogram, ParallelogramAlt, Trapezoid, TrapezoidAlt:
		return w + h, h
	case Asymmetric:
		return w + float64(h/4), h
	case Cylinder:
		return w, h + float64(2*cylinderRy(w))
	case Subroutine:
		return w + 16, h
	case Document, LinedDocument, TaggedDocument:
		return w, h + float64(waveAmp(h)*2)
	case Documents:
		return w + 2*stackOff, h + float64(waveAmp(h)*2) + 2*stackOff
	case StackedRect:
		return w + 2*stackOff, h + 2*stackOff
	case Delay:
		return w + float64(h/2), h
	case HCylinder:
		return w + float64(h/2), h
	case LinedCylinder:
		return w, h + float64(2*cylinderRy(w)) + 4
	case CurvedTrapezoid:
		return w + h, h
	case DividedRect:
		return w, h + dividerH
	case Triangle, FlippedTriangle:
		return float64(2*tw) + 2*padX + th, float64(2*th) + 3*padY
	case WindowPane:
		return w + paneW, h + paneW
	case FilledCircle, SmallCircle, FramedCircle, CrossedCircle:
		return markerD, markerD
	case NotchedPentagon:
		return w + float64(h/2), h
	case SlopedRect:
		return w, h + float64(h/3)
	case BowTieRect:
		return w + float64(h/2), h
	case TextBlock:
		return tw + 4, th + 4
	case Hourglass:
		// The glass on top, the label below it.
		return max(tw, glassW), glassW + th + 4
	case BraceLeft, BraceRight:
		return w + braceW, h
	case Braces:
		return w + 2*braceW, h
	case Bolt:
		return 20, 36
	case Flag:
		return w, h + float64(waveAmp(h)*2)
	}
	return w, h
}

// Sizes of the Mermaid 11 shapes, in layout units.
const (
	stackOff = 4.0  // offset of the copies behind stacked shapes
	dividerH = 10.0 // band above the divider of a divided rectangle
	paneW    = 8.0  // width of the bands of a window pane
	markerD  = 14.0 // diameter of the small circle shapes
	braceW   = 10.0 // width of a brace
	glassW   = 30.0 // width and height of an hourglass
)

// waveAmp is the amplitude of the wavy edge of documents and flags.
func waveAmp(h float64) float64 { return math.Min(float64(h*0.1), 6) }

// wave returns points along a sine wave from (x0, y) to (x1, y), one period.
func wave(x0, x1, y, amp float64) []scene.Point {
	const n = 16
	pts := make([]scene.Point, 0, n+1)
	for i := 0; i <= n; i++ {
		t := float64(float64(i) / n)
		pts = append(pts, scene.Point{X: x0 + float64((x1-x0)*t), Y: y + float64(amp*math.Sin(float64(2*math.Pi*t)))})
	}
	return pts
}

// arc returns points on the ellipse around c from angle a0 to a1 (radians,
// y down).
func arc(c scene.Point, rx, ry, a0, a1 float64) []scene.Point {
	const n = 12
	pts := make([]scene.Point, 0, n+1)
	for i := 0; i <= n; i++ {
		a := a0 + float64((a1-a0)*float64(i)/n)
		pts = append(pts, scene.Point{X: c.X + float64(rx*math.Cos(a)), Y: c.Y + float64(ry*math.Sin(a))})
	}
	return pts
}

// documentPoints is the outline of a document: straight top and sides and
// a wavy bottom.
func documentPoints(x, y, w, h float64) []scene.Point {
	a := waveAmp(h)
	pts := []scene.Point{{X: x, Y: y}, {X: x + w, Y: y}}
	return append(pts, wave(x+w, x, y+h-a, a)...)
}

func cylinderRy(w float64) float64 { return math.Min(4+float64(w*0.04), 12) }

// shapeOutline returns a polygon approximating the shape, for edge clipping.
func shapeOutline(s Shape, c scene.Point, w, h float64) []scene.Point {
	x, y := c.X-float64(w/2), c.Y-float64(h/2)
	switch s {
	case Circle, DoubleCircle:
		r := float64(w / 2)
		if s == DoubleCircle {
			r += 4
		}
		return scene.EllipsePoints(c, r, r)
	case Rhombus:
		return []scene.Point{{X: c.X, Y: y}, {X: x + w, Y: c.Y}, {X: c.X, Y: y + h}, {X: x, Y: c.Y}}
	case Hexagon:
		i := float64(h / 4)
		return []scene.Point{{X: x + i, Y: y}, {X: x + w - i, Y: y}, {X: x + w, Y: c.Y}, {X: x + w - i, Y: y + h}, {X: x + i, Y: y + h}, {X: x, Y: c.Y}}
	case Parallelogram:
		k := float64(h / 2)
		return []scene.Point{{X: x + k, Y: y}, {X: x + w, Y: y}, {X: x + w - k, Y: y + h}, {X: x, Y: y + h}}
	case ParallelogramAlt:
		k := float64(h / 2)
		return []scene.Point{{X: x, Y: y}, {X: x + w - k, Y: y}, {X: x + w, Y: y + h}, {X: x + k, Y: y + h}}
	case Trapezoid:
		k := float64(h / 2)
		return []scene.Point{{X: x + k, Y: y}, {X: x + w - k, Y: y}, {X: x + w, Y: y + h}, {X: x, Y: y + h}}
	case TrapezoidAlt:
		k := float64(h / 2)
		return []scene.Point{{X: x, Y: y}, {X: x + w, Y: y}, {X: x + w - k, Y: y + h}, {X: x + k, Y: y + h}}
	case Asymmetric:
		return []scene.Point{{X: x, Y: y}, {X: x + w, Y: y}, {X: x + w, Y: y + h}, {X: x, Y: y + h}, {X: x + float64(h/4), Y: c.Y}}
	case Stadium:
		return roundedPoints(scene.Rect{X: x, Y: y, W: w, H: h}, float64(h/2))
	case Document, LinedDocument, TaggedDocument:
		return documentPoints(x, y, w, h)
	case Documents:
		o := 2 * stackOff
		return documentPoints(x, y+o, w-o, h-o)
	case StackedRect:
		o := 2 * stackOff
		return scene.RectPoints(scene.Rect{X: x, Y: y + o, W: w - o, H: h - o})
	case Delay:
		pts := []scene.Point{{X: x, Y: y}}
		return append(append(pts, arc(scene.Point{X: x + w - float64(h/2), Y: c.Y}, float64(h/2), float64(h/2), -math.Pi/2, math.Pi/2)...), scene.Point{X: x, Y: y + h})
	case HCylinder:
		rx := float64(h / 4)
		pts := arc(scene.Point{X: x + rx, Y: c.Y}, rx, float64(h/2), math.Pi/2, 3*math.Pi/2)
		return append(pts, arc(scene.Point{X: x + w - rx, Y: c.Y}, rx, float64(h/2), -math.Pi/2, math.Pi/2)...)
	case CurvedTrapezoid:
		pts := []scene.Point{{X: x, Y: c.Y}, {X: x + float64(h/2), Y: y}}
		pts = append(pts, arc(scene.Point{X: x + w - float64(h/2), Y: c.Y}, float64(h/2), float64(h/2), -math.Pi/2, math.Pi/2)...)
		return append(pts, scene.Point{X: x + float64(h/2), Y: y + h})
	case Triangle:
		return []scene.Point{{X: c.X, Y: y}, {X: x + w, Y: y + h}, {X: x, Y: y + h}}
	case FlippedTriangle:
		return []scene.Point{{X: x, Y: y}, {X: x + w, Y: y}, {X: c.X, Y: y + h}}
	case FilledCircle, SmallCircle, FramedCircle, CrossedCircle:
		return scene.EllipsePoints(c, float64(w/2), float64(w/2))
	case NotchedPentagon:
		k := float64(h / 3)
		return []scene.Point{{X: x + k, Y: y}, {X: x + w - k, Y: y}, {X: x + w, Y: y + k}, {X: x + w, Y: y + h}, {X: x, Y: y + h}, {X: x, Y: y + k}}
	case SlopedRect:
		return []scene.Point{{X: x, Y: y + float64(h/4)}, {X: x + w, Y: y}, {X: x + w, Y: y + h}, {X: x, Y: y + h}}
	case BowTieRect:
		k := float64(h / 4)
		pts := arc(scene.Point{X: x + k, Y: c.Y}, k, float64(h/2), math.Pi/2, 3*math.Pi/2)
		return append(pts, arc(scene.Point{X: x + w, Y: c.Y}, k, float64(h/2), -math.Pi/2, -3*math.Pi/2)...)
	case Bolt:
		return []scene.Point{{X: x + float64(w*0.6), Y: y}, {X: x, Y: y + float64(h*0.55)}, {X: x + float64(w*0.45), Y: y + float64(h*0.55)}, {X: x + float64(w*0.3), Y: y + h}, {X: x + w, Y: y + float64(h*0.42)}, {X: x + float64(w*0.55), Y: y + float64(h*0.42)}}
	case NotchedRect:
		k := math.Min(12, float64(h/2))
		return []scene.Point{{X: x + k, Y: y}, {X: x + w, Y: y}, {X: x + w, Y: y + h}, {X: x, Y: y + h}, {X: x, Y: y + k}}
	case Flag:
		a := waveAmp(h)
		pts := wave(x, x+w, y+a, a)
		return append(pts, wave(x+w, x, y+h-a, a)...)
	}
	return scene.RectPoints(scene.Rect{X: x, Y: y, W: w, H: h})
}

func roundedPoints(r scene.Rect, rad float64) []scene.Point {
	var out []scene.Point
	for _, sp := range scene.RoundRect(r, rad).Flatten(1) {
		out = append(out, sp.Pts...)
	}
	return out
}

// drawShape draws the node outline and fill.
func drawShape(sc *scene.Scene, s Shape, c scene.Point, w, h float64, fill, stroke color.NRGBA, sw float64, dashed bool) {
	r := scene.Rect{X: c.X - float64(w/2), Y: c.Y - float64(h/2), W: w, H: h}
	var dash []float64
	if dashed {
		dash = []float64{4, 3}
	}
	add := func(p *scene.Path) {
		sc.Add(&scene.Shape{Path: p, Fill: fill, Stroke: scene.Stroke{Color: stroke, Width: sw, Dash: dash}})
	}
	switch s {
	case Rect:
		add(scene.RectPath(r))
	case Round:
		add(scene.RoundRect(r, cornerRadius))
	case Stadium:
		add(scene.RoundRect(r, float64(h/2)))
	case Subroutine:
		add(scene.RectPath(r))
		sc.Line(scene.Polyline([]scene.Point{{X: r.X + 8, Y: r.Y}, {X: r.X + 8, Y: r.Y + h}}), stroke, sw, nil)
		sc.Line(scene.Polyline([]scene.Point{{X: r.X + w - 8, Y: r.Y}, {X: r.X + w - 8, Y: r.Y + h}}), stroke, sw, nil)
	case Circle:
		add(scene.Ellipse(c, float64(w/2), float64(w/2)))
	case DoubleCircle:
		sc.Add(&scene.Shape{Path: scene.Ellipse(c, float64(w/2)+4, float64(w/2)+4), Stroke: scene.Stroke{Color: stroke, Width: sw}})
		add(scene.Ellipse(c, float64(w/2), float64(w/2)))
	case Cylinder:
		ry := cylinderRy(w)
		rx := float64(w / 2)
		x0, x1 := r.X, r.X+w
		top, bot := r.Y+ry, r.Y+h-ry
		k := 0.5522847498
		// float64(...) rounds the products before they are added, so that
		// no CPU fuses them into an FMA (arm64 fused top-ry*k, amd64 v3
		// did not, and the points came out different).
		ryk, rxk := float64(ry*k), float64(rx*k)
		p := scene.NewPath().MoveTo(x0, top).LineTo(x0, bot)
		p.CubeTo(x0, bot+ryk, c.X-rxk, bot+ry, c.X, bot+ry)
		p.CubeTo(c.X+rxk, bot+ry, x1, bot+ryk, x1, bot)
		p.LineTo(x1, top)
		p.CubeTo(x1, top-ryk, c.X+rxk, top-ry, c.X, top-ry)
		p.CubeTo(c.X-rxk, top-ry, x0, top-ryk, x0, top)
		add(p.Close())
		front := scene.NewPath().MoveTo(x0, top)
		front.CubeTo(x0, top+ryk, c.X-rxk, top+ry, c.X, top+ry)
		front.CubeTo(c.X+rxk, top+ry, x1, top+ryk, x1, top)
		sc.Line(front, stroke, sw, nil)
	case Documents, StackedRect:
		// Two copies behind, each stackOff up and to the right.
		for k := 2.0; k >= 1; k-- {
			off := float64(k * stackOff)
			add(scene.Polygon(shapeOutline(s, scene.Point{X: c.X + off, Y: c.Y - off}, w, h)))
		}
		add(scene.Polygon(shapeOutline(s, c, w, h)))
	case LinedDocument:
		add(scene.Polygon(shapeOutline(s, c, w, h)))
		sc.Line(scene.Polyline([]scene.Point{{X: r.X + 8, Y: r.Y}, {X: r.X + 8, Y: r.Y + h - waveAmp(h)}}), stroke, sw, nil)
	case TaggedDocument, TaggedRect:
		add(scene.Polygon(shapeOutline(s, c, w, h)))
		k := math.Min(10, float64(h/3))
		bx, by := r.X+w, r.Y+h
		if s == TaggedDocument {
			by -= float64(waveAmp(h) * 2)
		}
		sc.Fill(scene.Polygon([]scene.Point{{X: bx, Y: by - k}, {X: bx, Y: by}, {X: bx - k, Y: by}}), stroke, color.NRGBA{}, 0)
	case LinedCylinder:
		drawShape(sc, Cylinder, c, w, h, fill, stroke, sw, dashed)
		ry := cylinderRy(w)
		top := r.Y + ry + 4
		k := 0.5522847498
		front := scene.NewPath().MoveTo(r.X, top)
		front.CubeTo(r.X, top+float64(ry*k), c.X-float64(w/2*k), top+ry, c.X, top+ry)
		front.CubeTo(c.X+float64(w/2*k), top+ry, r.X+w, top+float64(ry*k), r.X+w, top)
		sc.Line(front, stroke, sw, nil)
	case HCylinder:
		add(scene.Polygon(shapeOutline(s, c, w, h)))
		rx := float64(h / 4)
		sc.Line(scene.Polyline(arc(scene.Point{X: r.X + w - rx, Y: c.Y}, rx, float64(h/2), math.Pi/2, 3*math.Pi/2)), stroke, sw, nil)
	case DividedRect:
		add(scene.RectPath(r))
		sc.Line(scene.Polyline([]scene.Point{{X: r.X, Y: r.Y + dividerH}, {X: r.X + w, Y: r.Y + dividerH}}), stroke, sw, nil)
	case WindowPane:
		add(scene.RectPath(r))
		sc.Line(scene.Polyline([]scene.Point{{X: r.X + paneW, Y: r.Y}, {X: r.X + paneW, Y: r.Y + h}}), stroke, sw, nil)
		sc.Line(scene.Polyline([]scene.Point{{X: r.X, Y: r.Y + paneW}, {X: r.X + w, Y: r.Y + paneW}}), stroke, sw, nil)
	case LinedRect:
		add(scene.RectPath(r))
		sc.Line(scene.Polyline([]scene.Point{{X: r.X + 8, Y: r.Y}, {X: r.X + 8, Y: r.Y + h}}), stroke, sw, nil)
	case FilledCircle:
		sc.Add(&scene.Shape{Path: scene.Ellipse(c, float64(w/2), float64(w/2)), Fill: stroke, Stroke: scene.Stroke{Color: stroke, Width: sw}})
	case SmallCircle:
		add(scene.Ellipse(c, float64(w/2), float64(w/2)))
	case FramedCircle:
		sc.Add(&scene.Shape{Path: scene.Ellipse(c, float64(w/2), float64(w/2)), Fill: fill, Stroke: scene.Stroke{Color: stroke, Width: sw}})
		sc.Add(&scene.Shape{Path: scene.Ellipse(c, float64(w/2)-3, float64(w/2)-3), Fill: stroke})
	case CrossedCircle:
		add(scene.Ellipse(c, float64(w/2), float64(w/2)))
		d := float64(w / 2 * math.Sqrt2 / 2)
		sc.Line(scene.Polyline([]scene.Point{{X: c.X - d, Y: c.Y - d}, {X: c.X + d, Y: c.Y + d}}), stroke, sw, nil)
		sc.Line(scene.Polyline([]scene.Point{{X: c.X - d, Y: c.Y + d}, {X: c.X + d, Y: c.Y - d}}), stroke, sw, nil)
	case TextBlock:
		// Only the label.
	case Hourglass:
		// The outline used for clipping is the bounding box; the glass is
		// two triangles meeting at a point.
		gx, gy, m := c.X-glassW/2, r.Y, c.X
		add(scene.Polygon([]scene.Point{{X: gx, Y: gy}, {X: gx + glassW, Y: gy}, {X: m, Y: gy + glassW/2}}))
		add(scene.Polygon([]scene.Point{{X: m, Y: gy + glassW/2}, {X: gx + glassW, Y: gy + glassW}, {X: gx, Y: gy + glassW}}))
	case BraceLeft, BraceRight, Braces:
		if s != BraceRight {
			sc.Line(bracePath(r.X+braceW, r.Y, h, 1), stroke, sw, dash)
		}
		if s != BraceLeft {
			sc.Line(bracePath(r.X+w-braceW, r.Y, h, -1), stroke, sw, dash)
		}
	case Bolt:
		sc.Add(&scene.Shape{Path: scene.Polygon(shapeOutline(s, c, w, h)), Fill: stroke, Stroke: scene.Stroke{Color: stroke, Width: sw}})
	default:
		add(scene.Polygon(shapeOutline(s, c, w, h)))
	}
}

// bracePath draws a curly brace of height h whose ends are at x and whose
// tip is braceW away: to the left for dir = +1 ("{"), to the right for
// dir = -1 ("}").
func bracePath(x, y, h, dir float64) *scene.Path {
	k := float64(braceW * dir)
	m := y + float64(h/2)
	p := scene.NewPath().MoveTo(x-float64(k*0.2), y)
	p.CubeTo(x-float64(k*0.7), y, x-float64(k*0.5), y+float64(h*0.1), x-float64(k*0.5), y+float64(h*0.25))
	p.CubeTo(x-float64(k*0.5), m-float64(h*0.1), x-float64(k*0.6), m, x-k, m)
	p.CubeTo(x-float64(k*0.6), m, x-float64(k*0.5), m+float64(h*0.1), x-float64(k*0.5), y+float64(h*0.75))
	p.CubeTo(x-float64(k*0.5), y+float64(h*0.9), x-float64(k*0.7), y+h, x-float64(k*0.2), y+h)
	return p
}

// textCenter returns where the label of a shape is centered.
func textCenter(s Shape, c scene.Point, w, h float64) scene.Point {
	switch s {
	case Cylinder:
		return scene.Point{X: c.X, Y: c.Y + float64(cylinderRy(w)/2)}
	case Asymmetric:
		return scene.Point{X: c.X + float64(h/8), Y: c.Y}
	case Document, LinedDocument, TaggedDocument, Flag:
		return scene.Point{X: c.X, Y: c.Y - float64(waveAmp(h)/2)}
	case Documents:
		return scene.Point{X: c.X - stackOff, Y: c.Y + stackOff - float64(waveAmp(h)/2)}
	case StackedRect:
		return scene.Point{X: c.X - stackOff, Y: c.Y + stackOff}
	case Delay, NotchedPentagon, BowTieRect:
		return scene.Point{X: c.X - float64(h/8), Y: c.Y}
	case HCylinder:
		return scene.Point{X: c.X - float64(h/8), Y: c.Y}
	case LinedCylinder:
		return scene.Point{X: c.X, Y: c.Y + float64(cylinderRy(w)/2) + 2}
	case CurvedTrapezoid:
		return scene.Point{X: c.X + float64(h/8), Y: c.Y}
	case DividedRect:
		return scene.Point{X: c.X, Y: c.Y + dividerH/2}
	case WindowPane:
		return scene.Point{X: c.X + paneW/2, Y: c.Y + paneW/2}
	case Triangle:
		return scene.Point{X: c.X, Y: c.Y + float64(h/6)}
	case FlippedTriangle:
		return scene.Point{X: c.X, Y: c.Y - float64(h/6)}
	case SlopedRect:
		return scene.Point{X: c.X, Y: c.Y + float64(h/8)}
	case Hourglass:
		return scene.Point{X: c.X, Y: c.Y + glassW/2 + 2}
	}
	return c
}
