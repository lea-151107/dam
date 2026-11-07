// Package pie parses and lays out Mermaid pie charts.
package pie

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/lea/mermaid/internal/diagram"
	"github.com/lea/mermaid/internal/guard"
	"github.com/lea/mermaid/internal/scene"
	"github.com/lea/mermaid/internal/source"
)

// Slice is one data point.
type Slice struct {
	Label string
	Value float64
}

// Diagram is a parsed pie chart.
type Diagram struct {
	Title    string
	ShowData bool
	Slices   []Slice
	total    float64
}

var sliceRe = regexp.MustCompile(`^"([^"]*)"\s*:\s*([-+0-9.eE]+)$`)

// Parse parses a pie chart.
func Parse(g *guard.Guard, lines []source.Line) (*Diagram, error) {
	d := &Diagram{}
	_, rest := source.Header(lines)
	if r, ok := source.CutKeyword(rest, "showData"); ok || rest == "showData" {
		d.ShowData = true
		rest = r
	}
	if r, ok := source.CutKeyword(rest, "title"); ok {
		d.Title = source.Label(r)
	} else if rest != "" {
		return nil, source.Errorf(lines[0].No, "unexpected %q", rest)
	}
	for _, l := range lines[1:] {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		t := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(l.Text), ";"))
		if r, ok := source.CutKeyword(t, "title"); ok {
			d.Title = source.Label(r)
			continue
		}
		if t == "showData" {
			d.ShowData = true
			continue
		}
		if strings.HasPrefix(t, "accTitle") || strings.HasPrefix(t, "accDescr") {
			continue
		}
		m := sliceRe.FindStringSubmatch(t)
		if m == nil {
			return nil, source.Errorf(l.No, `expected "label" : value`)
		}
		v, err := strconv.ParseFloat(m[2], 64)
		if err != nil || v < 0 || math.IsInf(v, 0) || math.IsNaN(v) {
			return nil, source.Errorf(l.No, "invalid value %q", m[2])
		}
		d.Slices = append(d.Slices, Slice{Label: source.Label(m[1]), Value: v})
		d.total += v
		if math.IsInf(d.total, 0) {
			// As in treemap and sankey: every slice would be 0%.
			return nil, source.Errorf(l.No, "the values add up to too large a number")
		}
		if err := g.Count("slices", len(d.Slices)); err != nil {
			return nil, err
		}
	}
	if len(d.Slices) == 0 {
		return nil, source.Errorf(lines[len(lines)-1].No, "pie chart has no data")
	}
	if d.total <= 0 {
		return nil, source.Errorf(lines[len(lines)-1].No, "pie chart values sum to zero")
	}
	return d, nil
}

// Visual constants in layout units.
const (
	radius      = 120.0
	legendBox   = 14.0
	legendGap   = 6.0
	legendSep   = 30.0
	titleGap    = 14.0
	minPctAngle = 0.25 // radians; smaller slices get no percentage label
)

// Layout implements diagram.Layouter.
func (d *Diagram) Layout(env *diagram.Env) (*scene.Scene, error) {
	m := env.M
	t := env.T
	sc := &scene.Scene{}
	r := radius

	legend := make([][]string, len(d.Slices))
	legendW, legendH := 0.0, 0.0
	for i, s := range d.Slices {
		text := s.Label
		if d.ShowData {
			text += " [" + formatValue(s.Value) + "]"
		}
		legend[i] = env.Label(text)
		w, h := m.Size(legend[i])
		legendW = max(legendW, w+legendBox+legendGap)
		legendH += max(h, legendBox) + legendGap
	}
	below := env.MaxWidth > 0 && float64(2*r)+legendSep+legendW > env.MaxWidth
	if below && env.MaxWidth > 0 {
		r = math.Max(60, math.Min(r, float64(env.ContentWidth()/2)))
	}

	y := 0.0
	if d.Title != "" {
		title := env.Label(d.Title)
		_, th := m.Size(title)
		sc.Label(r, float64(th/2), title, scene.Center, t.Text)
		y = th + titleGap
	}
	c := scene.Point{X: r, Y: y + r}
	a := -math.Pi / 2
	for i, s := range d.Slices {
		frac := float64(s.Value / d.total)
		if frac <= 0 {
			continue
		}
		span := float64(frac * 2 * math.Pi)
		col := t.Color(i)
		var p *scene.Path
		if frac >= 0.9999 {
			p = scene.Ellipse(c, r, r)
		} else {
			p = scene.NewPath().MoveTo(c.X, c.Y).Arc(c, r, a, a+span).Close()
		}
		sc.Fill(p, col, t.Base, 2)
		if span >= minPctAngle {
			mid := a + float64(span/2)
			q := scene.Point{X: c.X + float64(0.68*r*math.Cos(mid)), Y: c.Y + float64(0.68*r*math.Sin(mid))}
			sc.Label(q.X, q.Y, []string{fmt.Sprintf("%.0f%%", float64(frac*100))}, scene.Center, t.Text)
		}
		a += span
	}
	sc.Line(scene.Ellipse(c, r, r), t.Muted, 1, nil)

	lx, ly := float64(2*r)+legendSep, c.Y-float64(legendH/2)
	if below {
		lx, ly = 0, c.Y+r+titleGap
	}
	for i := range d.Slices {
		_, h := m.Size(legend[i])
		rowH := max(h, legendBox)
		box := scene.Rect{X: lx, Y: ly + float64((rowH-legendBox)/2), W: legendBox, H: legendBox}
		sc.Fill(scene.RectPath(box), t.Color(i), t.Muted, 1)
		sc.Label(lx+legendBox+legendGap, ly+float64(rowH/2), legend[i], scene.Left, t.Text)
		ly += rowH + legendGap
	}
	return diagram.Finish(sc, env, float64(2*r), c.Y+r), nil
}

func formatValue(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// SetDefaultTitle sets the title from front matter unless the diagram has
// its own.
func (d *Diagram) SetDefaultTitle(t string) {
	if d.Title == "" {
		d.Title = t
	}
}
