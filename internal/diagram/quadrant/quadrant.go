// Package quadrant parses and lays out Mermaid quadrant charts.
package quadrant

import (
	"fmt"
	"image/color"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/lea/mermaid/internal/diagram"
	"github.com/lea/mermaid/internal/guard"
	"github.com/lea/mermaid/internal/scene"
	"github.com/lea/mermaid/internal/source"
	"github.com/lea/mermaid/internal/textgrid"
	"github.com/lea/mermaid/internal/theme"
)

// Point is a data point with optional style.
type Point struct {
	Label        string
	X, Y         float64 // 0 to 1
	Radius       float64 // 0 = default
	Fill, Stroke *color.NRGBA
	StrokeWidth  float64
	Class        string
}

// Diagram is a parsed quadrant chart.
type Diagram struct {
	Title       string
	XLow, XHigh string
	YLow, YHigh string
	Quadrants   [4]string // 1 top right, 2 top left, 3 bottom left, 4 bottom right
	Points      []Point
	classDefs   map[string]Point
}

var (
	pointRe    = regexp.MustCompile(`^(.+?)(?::::([\w-]+))?\s*:\s*\[\s*([-+0-9.eE]+)\s*,\s*([-+0-9.eE]+)\s*\]\s*(.*)$`)
	classDefRe = regexp.MustCompile(`^classDef\s+([\w-]+)\s+(.*)$`)
)

// Parse parses a quadrant chart.
func Parse(g *guard.Guard, lines []source.Line) (*Diagram, error) {
	d := &Diagram{classDefs: map[string]Point{}}
	for _, l := range lines[1:] {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		t := strings.TrimSpace(l.Text)
		if r, ok := source.CutKeyword(t, "title"); ok {
			d.Title = source.Label(r)
			continue
		}
		if r, ok := source.CutKeyword(t, "x-axis"); ok {
			d.XLow, d.XHigh = axis(r)
			continue
		}
		if r, ok := source.CutKeyword(t, "y-axis"); ok {
			d.YLow, d.YHigh = axis(r)
			continue
		}
		if strings.HasPrefix(t, "quadrant-") && len(t) > len("quadrant-1") {
			k := t[len("quadrant-")] - '1'
			if k > 3 || (len(t) > len("quadrant-1") && t[len("quadrant-1")] != ' ') {
				return nil, source.Errorf(l.No, "unknown quadrant %q", strings.Fields(t)[0])
			}
			d.Quadrants[k] = source.Label(strings.TrimSpace(t[len("quadrant-1"):]))
			continue
		}
		if strings.HasPrefix(t, "accTitle") || strings.HasPrefix(t, "accDescr") {
			// NOTE(mermaid-compat): accessibility texts do not affect the image.
			continue
		}
		if m := classDefRe.FindStringSubmatch(t); m != nil {
			var p Point
			if err := style(&p, m[2]); err != nil {
				return nil, source.Errorf(l.No, "%v", err)
			}
			d.classDefs[m[1]] = p
			continue
		}
		m := pointRe.FindStringSubmatch(t)
		if m == nil {
			return nil, source.Errorf(l.No, "unrecognized statement %q", t)
		}
		x, err1 := strconv.ParseFloat(m[3], 64)
		y, err2 := strconv.ParseFloat(m[4], 64)
		if err1 != nil || err2 != nil || x < 0 || x > 1 || y < 0 || y > 1 {
			return nil, source.Errorf(l.No, "point coordinates must be between 0 and 1")
		}
		p := Point{Label: source.Label(m[1]), X: x, Y: y, Class: m[2]}
		if err := style(&p, m[5]); err != nil {
			return nil, source.Errorf(l.No, "%v", err)
		}
		d.Points = append(d.Points, p)
		if err := g.Count("points", len(d.Points)); err != nil {
			return nil, err
		}
	}
	// Class styles apply where the point sets nothing itself.
	for i := range d.Points {
		p := &d.Points[i]
		c, ok := d.classDefs[p.Class]
		if p.Class != "" && !ok {
			return nil, source.Errorf(lines[len(lines)-1].No, "unknown class %q", p.Class)
		}
		if p.Radius == 0 {
			p.Radius = c.Radius
		}
		if p.Fill == nil {
			p.Fill = c.Fill
		}
		if p.Stroke == nil {
			p.Stroke = c.Stroke
		}
		if p.StrokeWidth == 0 {
			p.StrokeWidth = c.StrokeWidth
		}
	}
	return d, nil
}

// axis splits "Low --> High" (the high end is optional).
func axis(s string) (lo, hi string) {
	a, b, _ := strings.Cut(s, "-->")
	return source.Label(a), source.Label(b)
}

// style applies "radius: 10, color: #f00, stroke-color: #00f,
// stroke-width: 2px".
func style(p *Point, s string) error {
	for _, part := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(part, ":")
		if !ok {
			if strings.TrimSpace(part) == "" {
				continue
			}
			return fmt.Errorf("expected key: value in %q", strings.TrimSpace(part))
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "radius":
			r, err := strconv.ParseFloat(v, 64)
			if err != nil || r <= 0 || r > maxRadius {
				return fmt.Errorf("invalid radius %q", v)
			}
			p.Radius = r
		case "color", "stroke-color":
			c, ok := theme.ParseColor(v)
			if !ok {
				return fmt.Errorf("invalid color %q", v)
			}
			c.A = 255
			if k == "color" {
				p.Fill = &c
			} else {
				p.Stroke = &c
			}
		case "stroke-width":
			w, err := strconv.ParseFloat(strings.TrimSuffix(v, "px"), 64)
			if err != nil || w < 0 || w > maxRadius {
				return fmt.Errorf("invalid stroke-width %q", v)
			}
			p.StrokeWidth = w
		default:
			return fmt.Errorf("unknown point style %q", k)
		}
	}
	return nil
}

// DiagramTitle returns the title given with the title statement.
func (d *Diagram) DiagramTitle() string { return d.Title }

// Visual constants in layout units.
const (
	plotSize  = 400.0
	pointR    = 5.0
	maxRadius = 50.0
	labelGap  = 6.0
	axisGap   = 8.0
	lineW     = 1.3
)

// quadrantOf returns the quadrant (0 for quadrant-1) a point falls in.
func quadrantOf(p Point) int {
	switch {
	case p.X >= 0.5 && p.Y >= 0.5:
		return 0
	case p.Y >= 0.5:
		return 1
	case p.X < 0.5:
		return 2
	}
	return 3
}

// Layout implements diagram.Layouter: a square split into four colored
// quadrants with the axis labels outside and the points inside.
//
// NOTE(mermaid-compat): the y-axis labels are written horizontally (text
// is not rotated).
func (d *Diagram) Layout(env *diagram.Env) (*scene.Scene, error) {
	m := env.M
	t := env.T
	lh := m.LineHeight()
	size := float64(plotSize * math.Max(env.Spacing, 0.6))
	if env.MaxWidth > 0 {
		size = math.Min(size, math.Max(env.ContentWidth()-120, 160))
	}
	half := float64(size / 2)
	// The y-axis labels sit left of the plot.
	yw := max(m.Width(d.YLow), m.Width(d.YHigh))
	x0 := 0.0
	if yw > 0 {
		x0 = yw + axisGap
	}
	sc := &scene.Scene{}
	// Quadrants: 1 top right, 2 top left, 3 bottom left, 4 bottom right.
	origins := [4]scene.Point{{X: x0 + half, Y: 0}, {X: x0, Y: 0}, {X: x0, Y: half}, {X: x0 + half, Y: half}}
	shades := [4]float64{0.30, 0.22, 0.14, 0.22}
	for k, o := range origins {
		r := scene.Rect{X: o.X, Y: o.Y, W: half, H: half}
		sc.Fill(scene.RectPath(r), theme.Mix(t.Base, t.Accent, shades[k]), t.Base, 1)
		if d.Quadrants[k] != "" {
			lines := m.Lines(d.Quadrants[k], half-float64(2*labelGap))
			_, h := m.Size(lines)
			sc.Label(r.X+float64(half/2), r.Y+labelGap+float64(h/2), lines, scene.Center, t.Text)
		}
	}
	sc.Line(scene.RectPath(scene.Rect{X: x0, Y: 0, W: size, H: size}), t.Muted, lineW, nil)
	// Axis labels.
	by := size + axisGap + float64(lh/2)
	if d.XLow != "" {
		sc.Label(x0, by, []string{d.XLow}, scene.Left, t.Text)
	}
	if d.XHigh != "" {
		sc.Label(x0+size, by, []string{d.XHigh}, scene.Right, t.Text)
	}
	if d.YHigh != "" {
		sc.Label(x0-axisGap, float64(lh/2), []string{d.YHigh}, scene.Right, t.Text)
	}
	if d.YLow != "" {
		sc.Label(x0-axisGap, size-float64(lh/2), []string{d.YLow}, scene.Right, t.Text)
	}
	// Points.
	for _, p := range d.Points {
		c := scene.Point{X: x0 + float64(p.X*size), Y: float64((1 - p.Y) * size)}
		r := pointR
		if p.Radius > 0 {
			r = p.Radius
		}
		fill, stroke, sw := t.Accent, t.NodeStroke, 0.0
		if p.Fill != nil {
			fill = *p.Fill
		}
		if p.Stroke != nil {
			stroke = *p.Stroke
		}
		if p.StrokeWidth > 0 {
			sw = p.StrokeWidth
		}
		sc.Fill(scene.Ellipse(c, r, r), fill, stroke, sw)
		sc.Label(c.X, c.Y+r+labelGap+float64(lh/2), []string{p.Label}, scene.Center, t.Text)
	}
	height := size
	if d.XLow != "" || d.XHigh != "" {
		height = by + float64(lh/2)
	}
	return diagram.Finish(sc, env, x0+size, height), nil
}

// Text lists the points by quadrant, with the axes first.
func (d *Diagram) Text(g *guard.Guard, maxCols int) ([]string, error) {
	var out []string
	if d.XLow != "" || d.XHigh != "" {
		out = append(out, textgrid.Wrap("x: "+d.XLow+" → "+d.XHigh, maxCols)...)
	}
	if d.YLow != "" || d.YHigh != "" {
		out = append(out, textgrid.Wrap("y: "+d.YLow+" → "+d.YHigh, maxCols)...)
	}
	where := [4]string{"top right", "top left", "bottom left", "bottom right"}
	for k := range 4 {
		name := d.Quadrants[k]
		if name == "" {
			name = fmt.Sprintf("Quadrant %d", k+1)
		}
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, textgrid.Wrap("■ "+name+" ("+where[k]+")", maxCols)...)
		for _, p := range d.Points {
			if err := g.Tick(); err != nil {
				return nil, err
			}
			if quadrantOf(p) == k {
				out = append(out, textgrid.Wrap(fmt.Sprintf("  %s (%.2f, %.2f)", strings.ReplaceAll(p.Label, "\n", " "), p.X, p.Y), maxCols)...)
			}
		}
	}
	return out, nil
}
