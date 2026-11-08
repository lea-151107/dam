// Package timeline parses and lays out Mermaid timeline diagrams.
package timeline

import (
	"math"
	"strings"

	"github.com/lea-151107/dam/internal/diagram"
	"github.com/lea-151107/dam/internal/guard"
	"github.com/lea-151107/dam/internal/scene"
	"github.com/lea-151107/dam/internal/source"
	"github.com/lea-151107/dam/internal/textgrid"
	"github.com/lea-151107/dam/internal/theme"
)

// Period is one point on the time line with its events.
type Period struct {
	Title   string
	Events  []string
	Section int // -1 when the diagram has no sections
}

// Diagram is a parsed timeline.
type Diagram struct {
	Title    string
	Sections []string
	Periods  []Period
}

// Parse parses a timeline.
func Parse(g *guard.Guard, lines []source.Line) (*Diagram, error) {
	d := &Diagram{}
	section := -1
	for _, l := range lines[1:] {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		t := strings.TrimSpace(l.Text)
		if r, ok := source.CutKeyword(t, "title"); ok {
			d.Title = source.Label(r)
			continue
		}
		if r, ok := source.CutKeyword(t, "section"); ok {
			d.Sections = append(d.Sections, source.Label(r))
			section = len(d.Sections) - 1
			continue
		}
		if strings.HasPrefix(t, "accTitle") || strings.HasPrefix(t, "accDescr") {
			// NOTE(mermaid-compat): accessibility texts do not affect the image.
			continue
		}
		parts := strings.Split(t, ":")
		if strings.HasPrefix(t, ":") {
			// A continuation adds events to the previous period.
			if len(d.Periods) == 0 {
				return nil, source.Errorf(l.No, "event without a period")
			}
			p := &d.Periods[len(d.Periods)-1]
			for _, e := range parts[1:] {
				if e = strings.TrimSpace(e); e != "" {
					p.Events = append(p.Events, source.Label(e))
				}
			}
		} else {
			p := Period{Title: source.Label(parts[0]), Section: section}
			if p.Title == "" {
				return nil, source.Errorf(l.No, "period without a name")
			}
			for _, e := range parts[1:] {
				if e = strings.TrimSpace(e); e != "" {
					p.Events = append(p.Events, source.Label(e))
				}
			}
			d.Periods = append(d.Periods, p)
		}
		n := 0
		for _, p := range d.Periods {
			n += 1 + len(p.Events)
		}
		if err := g.Count("periods and events", n); err != nil {
			return nil, err
		}
	}
	if len(d.Periods) == 0 {
		return nil, source.Errorf(lines[len(lines)-1].No, "timeline has no periods")
	}
	return d, nil
}

// DiagramTitle returns the title given with the title statement.
func (d *Diagram) DiagramTitle() string { return d.Title }

// Visual constants in layout units.
const (
	padX       = 12.0
	padY       = 8.0
	colGap     = 16.0
	rowGap     = 10.0
	axisGap    = 18.0
	maxLabelW  = 160.0
	lineW      = 1.3
	markerSize = 10.0
	radius     = 5.0
)

// Layout implements diagram.Layouter: periods side by side, each with its
// events stacked below the axis.
func (d *Diagram) Layout(env *diagram.Env) (*scene.Scene, error) {
	m := env.M
	t := env.T
	type col struct {
		head   []string
		events [][]string
		w      float64
	}
	wrap := maxLabelW
	if env.Wrap > 0 {
		wrap = math.Min(wrap, env.Wrap)
	}
	cols := make([]col, len(d.Periods))
	headH := 0.0
	for i, p := range d.Periods {
		c := col{head: m.Lines(p.Title, wrap)}
		w, h := m.Size(c.head)
		headH = max(headH, h)
		for _, e := range p.Events {
			lines := m.Lines(e, wrap)
			ew, _ := m.Size(lines)
			w = max(w, ew)
			c.events = append(c.events, lines)
		}
		c.w = w + 2*padX
		cols[i] = c
	}
	gap := float64(colGap * env.Spacing)
	x := 0.0
	xs := make([]float64, len(cols))
	for i, c := range cols {
		xs[i] = x
		x += c.w + gap
	}
	width := x - gap
	sc := &scene.Scene{}
	colorOf := func(i int) int {
		if d.Periods[i].Section >= 0 {
			return d.Periods[i].Section
		}
		return i
	}
	y := 0.0
	// Section bands over their periods.
	if len(d.Sections) > 0 {
		_, sh := m.Size([]string{"X"})
		bandH := sh + 2*padY
		for s, name := range d.Sections {
			lo, hi := math.Inf(1), math.Inf(-1)
			for i, p := range d.Periods {
				if p.Section == s {
					lo, hi = math.Min(lo, xs[i]), math.Max(hi, xs[i]+cols[i].w)
				}
			}
			if lo > hi {
				continue
			}
			r := scene.Rect{X: lo, Y: 0, W: hi - lo, H: bandH}
			sc.Fill(scene.RoundRect(r, radius), theme.Mix(t.Base, t.Color(s), 0.85), t.Strong(s), lineW)
			sc.Label(r.X+float64(r.W/2), r.Y+float64(r.H/2), m.Lines(name, r.W-float64(2*padX)), scene.Center, t.Text)
		}
		y = bandH + rowGap
	}
	boxH := headH + 2*padY
	axisY := y + boxH + axisGap
	sc.Line(scene.Polyline([]scene.Point{{X: float64(-gap / 2), Y: axisY}, {X: width + float64(gap/2) - markerSize, Y: axisY}}), t.Line, lineW*1.5, nil)
	sc.DrawMarker(scene.Arrow, scene.Point{X: width + float64(gap/2), Y: axisY}, scene.Point{X: 0, Y: axisY}, markerSize, t.Line, t.Base, lineW)
	bottom := axisY
	for i, c := range cols {
		k := colorOf(i)
		fill, stroke := theme.Mix(t.Base, t.Color(k), 0.85), t.Strong(k)
		r := scene.Rect{X: xs[i], Y: y, W: c.w, H: boxH}
		cx := r.X + float64(r.W/2)
		ey := axisY + axisGap
		var events []scene.Rect
		for _, e := range c.events {
			_, eh := m.Size(e)
			er := scene.Rect{X: xs[i], Y: ey, W: c.w, H: eh + float64(2*padY)}
			events = append(events, er)
			ey += er.H + rowGap
		}
		end := r.Y + r.H
		if len(events) > 0 {
			end = events[len(events)-1].Y
		}
		sc.Line(scene.Polyline([]scene.Point{{X: cx, Y: r.Y + r.H}, {X: cx, Y: end}}), stroke, lineW, []float64{4, 3})
		sc.Fill(scene.RoundRect(r, radius), fill, stroke, lineW)
		sc.Label(cx, r.Y+float64(r.H/2), c.head, scene.Center, t.Text)
		for k, er := range events {
			sc.Fill(scene.RoundRect(er, radius), theme.Mix(t.Base, t.Color(colorOf(i)), 0.92), stroke, lineW)
			sc.Label(cx, er.Y+float64(er.H/2), c.events[k], scene.Center, t.Text)
			bottom = math.Max(bottom, er.Y+er.H)
		}
	}
	return diagram.Finish(sc, env, width, bottom), nil
}

// Text renders the timeline as a list: each period, then its events after
// a bar, under section headings.
func (d *Diagram) Text(g *guard.Guard, maxCols int) ([]string, error) {
	headW := 0
	for _, p := range d.Periods {
		headW = max(headW, textgrid.MaxWidth(textgrid.Wrap(p.Title, 0)))
	}
	headW = min(headW, max(maxCols/3, minCols))
	room := maxCols - headW - 3
	if room < minCols {
		return nil, guard.Limit("columns", headW+3+minCols, maxCols)
	}
	var out []string
	section := -1
	for _, p := range d.Periods {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		if p.Section != section && p.Section >= 0 {
			section = p.Section
			if len(out) > 0 {
				out = append(out, "")
			}
			out = append(out, textgrid.Wrap("■ "+d.Sections[section], maxCols)...)
		}
		head := textgrid.Wrap(strings.ReplaceAll(p.Title, "\n", " "), headW)
		var events []string
		for _, e := range p.Events {
			events = append(events, textgrid.Wrap(strings.ReplaceAll(e, "\n", " "), room)...)
		}
		n := max(len(head), len(events), 1)
		for k := 0; k < n; k++ {
			h, e := "", ""
			if k < len(head) {
				h = head[k]
			}
			if k < len(events) {
				e = events[k]
			}
			out = append(out, h+strings.Repeat(" ", headW-textgrid.Width(h))+" │ "+e)
		}
	}
	return out, nil
}

// minCols is the narrowest column for period names and events.
const minCols = 6
