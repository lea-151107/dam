// Package xychart parses and lays out Mermaid XY charts (xychart-beta):
// bar and line series over a category or numeric x axis.
package xychart

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/lea-151107/dam/internal/chart"
	"github.com/lea-151107/dam/internal/diagram"
	"github.com/lea-151107/dam/internal/guard"
	"github.com/lea-151107/dam/internal/scene"
	"github.com/lea-151107/dam/internal/source"
	"github.com/lea-151107/dam/internal/textgrid"
	"github.com/lea-151107/dam/internal/theme"
)

// Series is one bar or line data set.
type Series struct {
	Name   string
	Line   bool
	Values []float64
	line   int
}

// Diagram is a parsed XY chart.
type Diagram struct {
	Title      string
	Horizontal bool
	XTitle     string
	// Categories label the x axis; when empty, the x axis is numeric
	// from XMin to XMax and the values are spread evenly over it.
	Categories []string
	XMin, XMax float64
	YTitle     string
	YMin, YMax float64
	hasY       bool
	Series     []Series
}

var (
	listRe  = regexp.MustCompile(`^(?:("[^"]*"|[^\[]+?)\s*)?\[(.*)\]$`)
	rangeRe = regexp.MustCompile(`^(?:("[^"]*"|.+?)\s+)?([-+0-9.eE]+)\s*-->\s*([-+0-9.eE]+)$`)
)

// Parse parses an XY chart.
func Parse(g *guard.Guard, lines []source.Line) (*Diagram, error) {
	d := &Diagram{}
	_, rest := source.Header(lines)
	switch rest {
	case "":
	case "horizontal":
		d.Horizontal = true
	case "vertical":
	default:
		return nil, source.Errorf(lines[0].No, "unexpected %q", rest)
	}
	for _, l := range lines[1:] {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		t := strings.TrimSpace(l.Text)
		switch {
		case strings.HasPrefix(t, "accTitle") || strings.HasPrefix(t, "accDescr"):
			// NOTE(mermaid-compat): accessibility texts do not affect the image.
			continue
		}
		if r, ok := source.CutKeyword(t, "title"); ok {
			d.Title = source.Label(r)
			continue
		}
		if r, ok := source.CutKeyword(t, "x-axis"); ok {
			if m := listRe.FindStringSubmatch(r); m != nil {
				d.XTitle = source.Label(m[1])
				d.Categories = nil
				for _, c := range splitList(m[2]) {
					d.Categories = append(d.Categories, source.Label(c))
				}
				if err := g.Count("categories", len(d.Categories)); err != nil {
					return nil, err
				}
				continue
			}
			if m := rangeRe.FindStringSubmatch(r); m != nil {
				lo, hi, err := parseRange(m[2], m[3])
				if err != nil {
					return nil, source.Errorf(l.No, "%v", err)
				}
				d.XTitle, d.XMin, d.XMax = source.Label(m[1]), lo, hi
				continue
			}
			d.XTitle = source.Label(r)
			continue
		}
		if r, ok := source.CutKeyword(t, "y-axis"); ok {
			if m := rangeRe.FindStringSubmatch(r); m != nil {
				lo, hi, err := parseRange(m[2], m[3])
				if err != nil {
					return nil, source.Errorf(l.No, "%v", err)
				}
				d.YTitle, d.YMin, d.YMax, d.hasY = source.Label(m[1]), lo, hi, true
				continue
			}
			d.YTitle = source.Label(r)
			continue
		}
		kw, r, _ := strings.Cut(t, " ")
		if kw != "bar" && kw != "line" {
			return nil, source.Errorf(l.No, "unrecognized statement %q", t)
		}
		m := listRe.FindStringSubmatch(strings.TrimSpace(r))
		if m == nil {
			return nil, source.Errorf(l.No, "expected %s [values]", kw)
		}
		s := Series{Name: source.Label(m[1]), Line: kw == "line", line: l.No}
		for _, v := range splitList(m[2]) {
			f, err := strconv.ParseFloat(v, 64)
			if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
				return nil, source.Errorf(l.No, "invalid value %q", v)
			}
			s.Values = append(s.Values, f)
		}
		if len(s.Values) == 0 {
			return nil, source.Errorf(l.No, "%s has no values", kw)
		}
		d.Series = append(d.Series, s)
		n := 0
		for _, s := range d.Series {
			n += len(s.Values)
		}
		if err := g.Count("values", n); err != nil {
			return nil, err
		}
	}
	if len(d.Series) == 0 {
		return nil, source.Errorf(lines[len(lines)-1].No, "chart has no bar or line data")
	}
	errLine := lines[len(lines)-1].No
	if !d.hasY {
		d.YMin, d.YMax = math.Inf(1), math.Inf(-1)
		for _, s := range d.Series {
			for _, v := range s.Values {
				d.YMin, d.YMax = math.Min(d.YMin, v), math.Max(d.YMax, v)
			}
			if math.IsInf(math.Max(d.YMax, 0)-math.Min(d.YMin, 0), 0) {
				// The first series with which the range overflows.
				return nil, source.Errorf(s.line, "the values span too large a range")
			}
		}
		// Bars grow from zero when all values have the same sign.
		d.YMin = math.Min(d.YMin, 0)
		d.YMax = math.Max(d.YMax, 0)
		if d.YMax <= d.YMin {
			// All values are zero: keep a range to draw them in.
			d.YMax = d.YMin + 1
		}
	}
	if math.IsInf(d.YMax-d.YMin, 0) {
		return nil, source.Errorf(errLine, "the values span too large a range")
	}
	if d.YMax-d.YMin < chart.MinSpan {
		return nil, source.Errorf(lines[len(lines)-1].No, "the values span too small a range")
	}
	return d, nil
}

func parseRange(a, b string) (lo, hi float64, err error) {
	lo, err1 := strconv.ParseFloat(a, 64)
	hi, err2 := strconv.ParseFloat(b, 64)
	if err1 != nil || err2 != nil || !(hi-lo >= chart.MinSpan) || math.IsInf(hi-lo, 0) {
		return 0, 0, fmt.Errorf("invalid range %s --> %s", a, b)
	}
	return lo, hi, nil
}

// splitList splits "a, "b, c", d" at commas outside quotes.
func splitList(s string) []string {
	var out []string
	var cur strings.Builder
	quoted := false
	for _, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
			cur.WriteRune(r)
		case r == ',' && !quoted:
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	if t := strings.TrimSpace(cur.String()); t != "" || len(out) > 0 {
		out = append(out, t)
	}
	return out
}

// DiagramTitle returns the title given with the title statement.
func (d *Diagram) DiagramTitle() string { return d.Title }

// count is the number of x positions.
func (d *Diagram) count() int {
	n := len(d.Categories)
	for _, s := range d.Series {
		n = max(n, len(s.Values))
	}
	return n
}

// xLabel is the label of x position i.
func (d *Diagram) xLabel(i int) string {
	if i < len(d.Categories) {
		return d.Categories[i]
	}
	if len(d.Categories) == 0 && d.XMax > d.XMin {
		n := d.count()
		if n == 1 {
			return chart.Format(d.XMin)
		}
		return chart.Format(d.XMin + float64((d.XMax-d.XMin)*float64(i)/float64(n-1)))
	}
	return strconv.Itoa(i + 1)
}

// Visual constants in layout units.
const (
	plotW      = 480.0
	plotH      = 280.0
	tickLen    = 5.0
	tickGap    = 6.0
	titleGap   = 8.0
	barShare   = 0.7 // part of a category's band taken by its bars
	pointR     = 3.0
	lineW      = 1.3
	seriesLine = 2.0
	yTicks     = 6
)

// Layout implements diagram.Layouter. In a horizontal chart the category
// axis runs down the left side and the values to the right.
func (d *Diagram) Layout(env *diagram.Env) (*scene.Scene, error) {
	m := env.M
	t := env.T
	lh := m.LineHeight()
	n := d.count()
	ticks, lo, hi := chart.Ticks(d.YMin, d.YMax, yTicks)
	if d.hasY {
		lo, hi = d.YMin, d.YMax
		ticks = ticks[:0]
		all, _, _ := chart.Ticks(lo, hi, yTicks)
		for _, v := range all {
			if v >= lo-1e-9 && v <= hi+1e-9 {
				ticks = append(ticks, v)
			}
		}
	}
	valueW := 0.0
	for _, v := range ticks {
		valueW = max(valueW, m.Width(chart.Format(v)))
	}
	catW := 0.0
	for i := range n {
		catW = max(catW, m.Width(d.xLabel(i)))
	}
	pw, ph := float64(plotW*math.Max(env.Spacing, 0.6)), plotH
	if d.Horizontal {
		pw, ph = ph+60, math.Max(plotH, float64(float64(n)*(lh+4)))
	}
	if env.MaxWidth > 0 {
		pw = math.Min(pw, math.Max(env.ContentWidth()-max(valueW, catW)-lh-3*tickGap, 120))
	}
	sc := &scene.Scene{}
	// Plot origin (top-left) leaves room for the value or category labels
	// and the axis title on the left.
	leftW := valueW
	if d.Horizontal {
		leftW = catW
	}
	left := leftW + tickLen + tickGap
	leftTitle := d.YTitle
	if d.Horizontal {
		leftTitle = d.XTitle
	}
	if leftTitle != "" {
		// NOTE(mermaid-compat): axis titles are written horizontally above
		// the axis instead of rotated beside it.
		sc.Label(0, -titleGap-float64(lh/2), []string{leftTitle}, scene.Left, t.Text)
	}
	// value maps a data value to a position along the value axis.
	//
	// NOTE(mermaid-compat): values outside the y-axis range are drawn at
	// the end of the axis, as the text output does. Mermaid lets them run
	// out of the plot (and the SVG clips them); here the drawing would grow
	// instead, to an image of 528×14044 px for a 50 on a 0 --> 1 axis.
	value := func(v float64) float64 {
		f := math.Max(0, math.Min(1, float64((v-lo)/(hi-lo))))
		if d.Horizontal {
			return left + float64(f*pw)
		}
		return ph - float64(f*ph)
	}
	band := float64(pw / float64(max(n, 1)))
	if d.Horizontal {
		band = float64(ph / float64(max(n, 1)))
	}
	center := func(i int) float64 {
		if d.Horizontal {
			return float64((float64(i) + 0.5) * band)
		}
		return left + float64((float64(i)+0.5)*band)
	}
	plot := scene.Rect{X: left, Y: 0, W: pw, H: ph}
	// Grid and value ticks.
	for _, v := range ticks {
		p := value(v)
		label := chart.Format(v)
		if d.Horizontal {
			sc.Line(scene.Polyline([]scene.Point{{X: p, Y: 0}, {X: p, Y: ph}}), theme.Mix(t.Base, t.Muted, 0.4), 1, nil)
			sc.Label(p, ph+tickLen+tickGap+float64(lh/2), []string{label}, scene.Center, t.Text)
		} else {
			sc.Line(scene.Polyline([]scene.Point{{X: left, Y: p}, {X: left + pw, Y: p}}), theme.Mix(t.Base, t.Muted, 0.4), 1, nil)
			sc.Label(left-tickLen-tickGap, p, []string{label}, scene.Right, t.Text)
		}
	}
	// Category labels.
	for i := range n {
		c := center(i)
		if d.Horizontal {
			sc.Label(left-tickLen-tickGap, c, []string{d.xLabel(i)}, scene.Right, t.Text)
		} else {
			sc.Label(c, ph+tickLen+tickGap+float64(lh/2), m.Lines(d.xLabel(i), band), scene.Center, t.Text)
		}
	}
	// Bars share each band; lines run through the band centers.
	var bars []int
	for k, s := range d.Series {
		if !s.Line {
			bars = append(bars, k)
		}
	}
	zero := value(math.Max(lo, math.Min(0, hi)))
	for bi, k := range bars {
		s := d.Series[k]
		w := float64(band * barShare / float64(len(bars)))
		col := t.Strong(k)
		for i, v := range s.Values {
			off := center(i) - float64(band*barShare/2) + float64(float64(bi)*w)
			p := value(v)
			var r scene.Rect
			if d.Horizontal {
				r = scene.Rect{X: math.Min(zero, p), Y: off, W: math.Abs(p - zero), H: w}
			} else {
				r = scene.Rect{X: off, Y: math.Min(zero, p), W: w, H: math.Abs(p - zero)}
			}
			sc.Fill(scene.RectPath(r), theme.Mix(t.Base, t.Color(k), 0.9), col, 1)
		}
	}
	for k, s := range d.Series {
		if !s.Line {
			continue
		}
		col := t.Strong(k)
		var pts []scene.Point
		for i, v := range s.Values {
			if d.Horizontal {
				pts = append(pts, scene.Point{X: value(v), Y: center(i)})
			} else {
				pts = append(pts, scene.Point{X: center(i), Y: value(v)})
			}
		}
		sc.Line(scene.Polyline(pts), col, seriesLine, nil)
		for _, p := range pts {
			sc.Fill(scene.Ellipse(p, pointR, pointR), col, col, 0)
		}
	}
	// Axes.
	sc.Line(scene.Polyline([]scene.Point{{X: left, Y: 0}, {X: left, Y: ph}, {X: left + pw, Y: ph}}), t.Line, lineW, nil)
	bottom := ph + tickLen + tickGap + lh
	bottomTitle := d.XTitle
	if d.Horizontal {
		bottomTitle = d.YTitle
	}
	if bottomTitle != "" {
		bottom += titleGap
		sc.Label(plot.X+float64(plot.W/2), bottom+float64(lh/2), []string{bottomTitle}, scene.Center, t.Text)
		bottom += lh
	}
	// Legend for several series.
	if len(d.Series) > 1 {
		x := left
		y := bottom + titleGap + float64(lh/2)
		for k, s := range d.Series {
			name := s.Name
			if name == "" {
				name = fmt.Sprintf("Series %d", k+1)
			}
			sc.Fill(scene.RectPath(scene.Rect{X: x, Y: y - 5, W: 10, H: 10}), theme.Mix(t.Base, t.Color(k), 0.9), t.Strong(k), 1)
			sc.Label(x+14, y, []string{name}, scene.Left, t.Text)
			x += 14 + m.Width(name) + 16
		}
		bottom = y + float64(lh/2)
	}
	return diagram.Finish(sc, env, left+pw, bottom), nil
}

// Text renders each series as horizontal bars (one row per x position)
// with the values.
func (d *Diagram) Text(g *guard.Guard, maxCols int) ([]string, error) {
	n := d.count()
	labelW := 0
	for i := range n {
		labelW = max(labelW, textgrid.Width(d.xLabel(i)))
	}
	valW := 0
	for _, s := range d.Series {
		for _, v := range s.Values {
			valW = max(valW, len(chart.Format(v)))
		}
	}
	labelW = min(labelW, max(maxCols/3, 4))
	barW := min(maxCols-labelW-valW-4, maxBar)
	if barW < minBar {
		return nil, guard.Limit("columns", labelW+valW+4+minBar, maxCols)
	}
	lo, hi := d.YMin, d.YMax
	var out []string
	var axes []string
	if d.XTitle != "" {
		axes = append(axes, "x: "+d.XTitle)
	}
	if d.YTitle != "" {
		axes = append(axes, "y: "+d.YTitle)
	}
	if len(axes) > 0 {
		out = append(out, textgrid.Wrap(strings.Join(axes, "  "), maxCols)...)
	}
	for k, s := range d.Series {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		kind := "bar"
		if s.Line {
			kind = "line"
		}
		name := s.Name
		if name == "" {
			name = fmt.Sprintf("Series %d", k+1)
		}
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, textgrid.Wrap("■ "+name+" ("+kind+")", maxCols)...)
		// Bars grow from zero (or the axis end nearest to it), as in the
		// image: with negative values they extend to the left of zero.
		col := func(v float64) int {
			return int(math.Round(float64(math.Max(0, math.Min(1, float64((v-lo)/(hi-lo)))) * float64(barW))))
		}
		zero := col(math.Max(lo, math.Min(hi, 0)))
		for i, v := range s.Values {
			label := truncate(d.xLabel(i), labelW)
			cells := col(v)
			a, b := min(zero, cells), max(zero, cells)
			bar := strings.Repeat(" ", a) + strings.Repeat("█", b-a) + strings.Repeat(" ", barW-b)
			if s.Line {
				bar = strings.Repeat(" ", max(cells-1, 0)) + "●" + strings.Repeat(" ", barW-max(cells, 1))
			}
			out = append(out, strings.TrimRight(label+strings.Repeat(" ", labelW-textgrid.Width(label))+" │"+bar+" "+chart.Format(v), " "))
		}
	}
	return out, nil
}

// Bar widths in text output.
const (
	minBar = 4
	maxBar = 40
)

// truncate shortens s to w columns, ending with … when cut.
func truncate(s string, w int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if textgrid.Width(s) <= w {
		return s
	}
	var b strings.Builder
	n := 0
	for _, r := range s {
		rw := textgrid.RuneWidth(r)
		if n+rw > w-1 {
			break
		}
		b.WriteRune(r)
		n += rw
	}
	return b.String() + "…"
}
