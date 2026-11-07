// Package radar parses and lays out Mermaid radar charts (radar-beta).
package radar

import (
	"errors"
	"fmt"
	"image/color"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/lea/mermaid/internal/chart"
	"github.com/lea/mermaid/internal/diagram"
	"github.com/lea/mermaid/internal/guard"
	"github.com/lea/mermaid/internal/scene"
	"github.com/lea/mermaid/internal/source"
	"github.com/lea/mermaid/internal/textgrid"
	"github.com/lea/mermaid/internal/theme"
)

// Axis is a spoke of the chart.
type Axis struct{ ID, Label string }

// Curve is a data series with one value per axis.
type Curve struct {
	ID, Label string
	Values    []float64
	line      int // source line, for errors
}

// Diagram is a parsed radar chart.
type Diagram struct {
	Title      string
	Axes       []Axis
	Curves     []Curve
	Min, Max   float64
	hasMax     bool
	Circle     bool // circular graticule instead of polygons
	Ticks      int
	ShowLegend bool
}

var (
	itemRe  = regexp.MustCompile(`^([\p{L}\p{N}_\-]+)\s*(?:\[\s*("[^"]*"|[^\]]*)\s*\])?$`)
	curveRe = regexp.MustCompile(`^([\p{L}\p{N}_\-]+)\s*(?:\[\s*("[^"]*"|[^\]]*)\s*\])?\s*\{(.*)\}$`)
)

// maxTicks bounds the graticule rings.
const maxTicks = 20

// Parse parses a radar chart.
func Parse(g *guard.Guard, lines []source.Line) (*Diagram, error) {
	d := &Diagram{Ticks: 5, ShowLegend: true}
	axisIdx := map[string]int{}
	for _, l := range lines[1:] {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		t := strings.TrimSpace(l.Text)
		kw, rest, _ := strings.Cut(t, " ")
		rest = strings.TrimSpace(rest)
		switch kw {
		case "title":
			d.Title = source.Label(rest)
		case "axis":
			for _, part := range splitTop(rest) {
				m := itemRe.FindStringSubmatch(part)
				if m == nil {
					return nil, source.Errorf(l.No, "expected id[\"label\"], got %q", part)
				}
				label := m[2]
				if label == "" {
					label = m[1]
				}
				axisIdx[m[1]] = len(d.Axes)
				d.Axes = append(d.Axes, Axis{ID: m[1], Label: source.Label(label)})
			}
		case "curve":
			for _, part := range splitTop(rest) {
				m := curveRe.FindStringSubmatch(part)
				if m == nil {
					return nil, source.Errorf(l.No, "expected id[\"label\"]{values}, got %q", part)
				}
				label := m[2]
				if label == "" {
					label = m[1]
				}
				c := Curve{ID: m[1], Label: source.Label(label)}
				named := strings.Contains(m[3], ":")
				if named {
					c.Values = make([]float64, len(d.Axes))
				}
				for _, v := range strings.Split(m[3], ",") {
					v = strings.TrimSpace(v)
					if v == "" {
						continue
					}
					k := len(c.Values)
					if named {
						a, val, _ := strings.Cut(v, ":")
						i, ok := axisIdx[strings.TrimSpace(a)]
						if !ok {
							return nil, source.Errorf(l.No, "unknown axis %q", strings.TrimSpace(a))
						}
						k, v = i, strings.TrimSpace(val)
					}
					f, err := strconv.ParseFloat(v, 64)
					if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
						return nil, source.Errorf(l.No, "invalid value %q", v)
					}
					if named {
						c.Values[k] = f
					} else {
						c.Values = append(c.Values, f)
					}
				}
				c.line = l.No
				d.Curves = append(d.Curves, c)
			}
		case "max", "min":
			f, err := strconv.ParseFloat(rest, 64)
			if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
				return nil, source.Errorf(l.No, "invalid %s %q", kw, rest)
			}
			if kw == "max" {
				d.Max, d.hasMax = f, true
			} else {
				d.Min = f
			}
		case "graticule":
			switch rest {
			case "circle":
				d.Circle = true
			case "polygon":
				d.Circle = false
			default:
				return nil, source.Errorf(l.No, "unknown graticule %q", rest)
			}
		case "ticks":
			n, err := strconv.Atoi(rest)
			if errors.Is(err, strconv.ErrRange) && !strings.HasPrefix(strings.TrimSpace(rest), "-") || err == nil && n > maxTicks {
				// A limit of this package, not a mistake (as block columns).
				return nil, source.TooLarge(l.No, "more than %d ticks", maxTicks)
			}
			if err != nil || n < 1 {
				return nil, source.Errorf(l.No, "invalid ticks %q", rest)
			}
			d.Ticks = n
		case "showLegend":
			d.ShowLegend = rest != "false"
		case "accTitle", "accDescr", "accTitle:", "accDescr:":
			// NOTE(mermaid-compat): accessibility texts do not affect the image.
		default:
			return nil, source.Errorf(l.No, "unrecognized statement %q", t)
		}
		if err := g.Count("axes and curves", len(d.Axes)+len(d.Curves)); err != nil {
			return nil, err
		}
	}
	if len(d.Axes) < 3 {
		return nil, source.Errorf(lines[len(lines)-1].No, "a radar chart needs at least three axes")
	}
	for _, c := range d.Curves {
		if len(c.Values) != len(d.Axes) {
			return nil, source.Errorf(c.line, "curve %q has %d values for %d axes", c.ID, len(c.Values), len(d.Axes))
		}
	}
	if !d.hasMax {
		for _, c := range d.Curves {
			for _, v := range c.Values {
				d.Max = math.Max(d.Max, v)
			}
		}
		_, _, d.Max = chart.Ticks(d.Min, d.Max, d.Ticks)
	}
	if !(d.Max > d.Min) {
		return nil, source.Errorf(lines[len(lines)-1].No, "max must be greater than min")
	}
	// As in xychart: positions are (v-min)/(max-min), which is NaN when the
	// range overflows and meaningless when it is subnormal.
	if span := d.Max - d.Min; math.IsInf(span, 0) || span < chart.MinSpan {
		return nil, source.Errorf(lines[len(lines)-1].No, "min and max span too large or too small a range")
	}
	return d, nil
}

// splitTop splits at commas outside brackets, braces and quotes.
func splitTop(s string) []string {
	var out []string
	depth := 0
	quoted := false
	start := 0
	for i, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
		case quoted:
		case r == '[' || r == '{':
			depth++
		case r == ']' || r == '}':
			depth--
		case r == ',' && depth == 0:
			out = append(out, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	if p := strings.TrimSpace(s[start:]); p != "" {
		out = append(out, p)
	}
	return out
}

// DiagramTitle returns the title given with the title statement.
func (d *Diagram) DiagramTitle() string { return d.Title }

// Visual constants in layout units.
const (
	radius   = 150.0
	labelGap = 10.0
	lineW    = 1.3
	curveA   = 0x50 // alpha of the curve fills
	legendSq = 10.0
)

// Layout implements diagram.Layouter.
func (d *Diagram) Layout(env *diagram.Env) (*scene.Scene, error) {
	m := env.M
	t := env.T
	lh := m.LineHeight()
	r := float64(radius * math.Max(env.Spacing, 0.6))
	n := len(d.Axes)
	angle := func(i int) float64 { return -math.Pi/2 + float64(2*math.Pi*float64(i)/float64(n)) }
	at := func(i int, f float64) scene.Point {
		a := angle(i)
		return scene.Point{X: float64(f * r * math.Cos(a)), Y: float64(f * r * math.Sin(a))}
	}
	sc := &scene.Scene{}
	grid := theme.Mix(t.Base, t.Muted, 0.5)
	for k := 1; k <= d.Ticks; k++ {
		f := float64(float64(k) / float64(d.Ticks))
		if d.Circle {
			sc.Line(scene.Ellipse(scene.Point{}, float64(f*r), float64(f*r)), grid, 1, nil)
			continue
		}
		pts := make([]scene.Point, n)
		for i := range n {
			pts[i] = at(i, f)
		}
		sc.Line(scene.Polygon(pts), grid, 1, nil)
	}
	for i, a := range d.Axes {
		end := at(i, 1)
		sc.Line(scene.Polyline([]scene.Point{{}, end}), grid, 1, nil)
		lp := at(i, 1+float64(labelGap/r))
		align := scene.Center
		switch c := math.Cos(angle(i)); {
		case c > 0.2:
			align = scene.Left
		case c < -0.2:
			align = scene.Right
		}
		lines := env.Label(a.Label)
		_, h := m.Size(lines)
		dy := float64(math.Sin(angle(i)) * h / 2)
		sc.Label(lp.X, lp.Y+dy, lines, align, t.Text)
	}
	for k, c := range d.Curves {
		if err := env.G.Tick(); err != nil {
			return nil, err
		}
		pts := make([]scene.Point, n)
		for i, v := range c.Values {
			f := math.Max(0, math.Min(1, float64((v-d.Min)/(d.Max-d.Min))))
			pts[i] = at(i, f)
		}
		col := t.Strong(k)
		sc.Add(&scene.Shape{Path: scene.Polygon(pts), Fill: color.NRGBA{col.R, col.G, col.B, curveA},
			Stroke: scene.Stroke{Color: col, Width: lineW * 1.5}})
	}
	if d.ShowLegend && len(d.Curves) > 0 {
		x := r + float64(labelGap*4)
		for _, a := range d.Axes {
			x = math.Max(x, r+labelGap+m.Width(a.Label)+float64(labelGap*2))
		}
		for k, c := range d.Curves {
			y := -r + float64(float64(k)*(lh+4))
			col := t.Strong(k)
			sc.Fill(scene.RectPath(scene.Rect{X: x, Y: y - legendSq/2, W: legendSq, H: legendSq}), color.NRGBA{col.R, col.G, col.B, 0xa0}, col, 1)
			sc.Label(x+legendSq+6, y, []string{c.Label}, scene.Left, t.Text)
		}
	}
	return diagram.Finish(sc, env, 0, 0), nil
}

// Text renders a table: one row per axis, one column per curve.
func (d *Diagram) Text(g *guard.Guard, maxCols int) ([]string, error) {
	rows := [][]string{{""}}
	for _, c := range d.Curves {
		rows[0] = append(rows[0], strings.ReplaceAll(c.Label, "\n", " "))
	}
	for i, a := range d.Axes {
		row := []string{strings.ReplaceAll(a.Label, "\n", " ")}
		for _, c := range d.Curves {
			row = append(row, chart.Format(c.Values[i]))
		}
		rows = append(rows, row)
	}
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for k, cell := range row {
			widths[k] = max(widths[k], textgrid.Width(cell))
		}
	}
	var out []string
	for r, row := range rows {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		var b strings.Builder
		for k, cell := range row {
			if k > 0 {
				b.WriteString(" │ ")
			}
			pad := strings.Repeat(" ", widths[k]-textgrid.Width(cell))
			if k == 0 {
				b.WriteString(cell + pad)
			} else {
				b.WriteString(pad + cell)
			}
		}
		out = append(out, strings.TrimRight(b.String(), " "))
		if r == 0 {
			var sep strings.Builder
			for k, w := range widths {
				if k > 0 {
					sep.WriteString("─┼─")
				}
				sep.WriteString(strings.Repeat("─", w))
			}
			out = append(out, sep.String())
		}
	}
	out = append(out, fmt.Sprintf("(scale %s to %s)", chart.Format(d.Min), chart.Format(d.Max)))
	if w := textgrid.MaxWidth(out); w > maxCols {
		return nil, guard.Limit("columns", w, maxCols)
	}
	return out, nil
}
