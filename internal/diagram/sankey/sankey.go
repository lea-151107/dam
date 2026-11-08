// Package sankey parses and lays out Mermaid sankey diagrams
// (sankey-beta): flows between nodes given as CSV lines.
package sankey

import (
	"fmt"
	"image/color"
	"math"
	"strconv"
	"strings"

	"github.com/lea-151107/dam/internal/chart"
	"github.com/lea-151107/dam/internal/diagram"
	"github.com/lea-151107/dam/internal/guard"
	"github.com/lea-151107/dam/internal/scene"
	"github.com/lea-151107/dam/internal/source"
	"github.com/lea-151107/dam/internal/textgrid"
)

// Link is a flow from one node to another.
type Link struct {
	From, To int
	Value    float64
	line     int
}

// Diagram is a parsed sankey diagram.
type Diagram struct {
	Nodes []string
	Links []Link
	index map[string]int
}

// Parse parses a sankey diagram.
func Parse(g *guard.Guard, lines []source.Line) (*Diagram, error) {
	d := &Diagram{index: map[string]int{}}
	node := func(name string) int {
		if i, ok := d.index[name]; ok {
			return i
		}
		d.Nodes = append(d.Nodes, name)
		d.index[name] = len(d.Nodes) - 1
		return len(d.Nodes) - 1
	}
	for _, l := range lines[1:] {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		fields, err := csv(strings.TrimSpace(l.Text))
		if err != nil {
			return nil, source.Errorf(l.No, "%v", err)
		}
		if len(fields) != 3 {
			return nil, source.Errorf(l.No, "expected source,target,value")
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(fields[2]), 64)
		if err != nil || v < 0 || math.IsInf(v, 0) || math.IsNaN(v) {
			return nil, source.Errorf(l.No, "invalid value %q", fields[2])
		}
		from, to := node(source.Label(fields[0])), node(source.Label(fields[1]))
		if from == to {
			return nil, source.Errorf(l.No, "a flow from %q to itself", d.Nodes[from])
		}
		d.Links = append(d.Links, Link{From: from, To: to, Value: v, line: l.No})
		if err := g.Count("nodes and links", len(d.Nodes)+len(d.Links)); err != nil {
			return nil, err
		}
	}
	if len(d.Links) == 0 {
		return nil, source.Errorf(lines[len(lines)-1].No, "sankey has no flows")
	}
	in := make([]float64, len(d.Nodes))
	out := make([]float64, len(d.Nodes))
	for _, l := range d.Links {
		in[l.To] += l.Value
		out[l.From] += l.Value
		if math.IsInf(in[l.To], 0) || math.IsInf(out[l.From], 0) {
			return nil, source.Errorf(l.line, "the flows add up to too large a number")
		}
	}
	if _, err := d.columns(g); err != nil {
		return nil, source.Errorf(lines[len(lines)-1].No, "%v", err)
	}
	return d, nil
}

// csv splits a line into fields; double quotes protect commas and "" is a
// quote.
func csv(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	quoted := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quoted && c == '"' && i+1 < len(s) && s[i+1] == '"':
			cur.WriteByte('"')
			i++
		case c == '"':
			quoted = !quoted
		case c == ',' && !quoted:
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	if quoted {
		return nil, fmt.Errorf("unterminated quote")
	}
	return append(out, strings.TrimSpace(cur.String())), nil
}

// columns places each node in the column of its longest path from a
// source; nodes without outgoing flows go to the last column. It fails on
// cycles.
func (d *Diagram) columns(g *guard.Guard) ([]int, error) {
	n := len(d.Nodes)
	col := make([]int, n)
	out := make([][]int, n)
	indeg := make([]int, n)
	for _, l := range d.Links {
		out[l.From] = append(out[l.From], l.To)
		indeg[l.To]++
	}
	var queue []int
	for i := range n {
		if indeg[i] == 0 {
			queue = append(queue, i)
		}
	}
	seen := 0
	for len(queue) > 0 {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		v := queue[0]
		queue = queue[1:]
		seen++
		for _, w := range out[v] {
			col[w] = max(col[w], col[v]+1)
			if indeg[w]--; indeg[w] == 0 {
				queue = append(queue, w)
			}
		}
	}
	if seen < n {
		return nil, fmt.Errorf("the flows form a cycle")
	}
	last := 0
	for _, c := range col {
		last = max(last, c)
	}
	for i := range n {
		if len(out[i]) == 0 {
			col[i] = last
		}
	}
	return col, nil
}

// Visual constants in layout units.
const (
	plotH    = 400.0
	nodeW    = 12.0
	colGap   = 180.0
	nodePad  = 12.0
	labelGap = 6.0
	flowA    = 0x80 // alpha of the flow bands
	flowRoom = 40.0 // width of a gap left for the flows beside the labels
)

// Layout implements diagram.Layouter.
func (d *Diagram) Layout(env *diagram.Env) (*scene.Scene, error) {
	t := env.T
	cols, err := d.columns(env.G)
	if err != nil {
		return nil, err
	}
	n := len(d.Nodes)
	in := make([]float64, n)
	out := make([]float64, n)
	for _, l := range d.Links {
		out[l.From] += l.Value
		in[l.To] += l.Value
	}
	value := make([]float64, n)
	ncol := 0
	for i := range n {
		value[i] = math.Max(in[i], out[i])
		ncol = max(ncol, cols[i]+1)
	}
	// One scale for all columns, set by the fullest one.
	sum := make([]float64, ncol)
	count := make([]int, ncol)
	for i := range n {
		sum[cols[i]] += value[i]
		count[cols[i]]++
	}
	scale := math.Inf(1)
	for c := range ncol {
		if sum[c] > 0 {
			scale = math.Min(scale, float64((plotH-float64(float64(count[c]-1)*nodePad))/sum[c]))
		}
	}
	if math.IsInf(scale, 1) {
		scale = 1
	}
	// Labels go right of their nodes, so a gap holds the widest label of
	// the columns before the last plus room for the flows to bend.
	labels := make([]string, n)
	gap := float64(colGap * math.Max(env.Spacing, 0.5))
	for i := range n {
		labels[i] = d.Nodes[i] + " (" + chart.Format(round3(value[i])) + ")"
		if cols[i] < ncol-1 {
			gap = math.Max(gap, env.M.Width(labels[i])+2*labelGap+flowRoom)
		}
	}
	x := make([]float64, n)
	y := make([]float64, n)
	h := make([]float64, n)
	next := make([]float64, ncol)
	for i := range n {
		c := cols[i]
		x[i] = float64(float64(c) * (nodeW + gap))
		h[i] = math.Max(float64(value[i]*scale), 1)
		y[i] = next[c]
		next[c] += h[i] + nodePad
	}
	sc := &scene.Scene{}
	// Flows leave and enter in link order, stacked down each side.
	outY := append([]float64(nil), y...)
	inY := append([]float64(nil), y...)
	for _, l := range d.Links {
		if err := env.G.Tick(); err != nil {
			return nil, err
		}
		w := float64(l.Value * scale)
		x0, x1 := x[l.From]+nodeW, x[l.To]
		y0, y1 := outY[l.From], inY[l.To]
		outY[l.From] += w
		inY[l.To] += w
		mx := float64((x0 + x1) / 2)
		p := scene.NewPath().MoveTo(x0, y0).CubeTo(mx, y0, mx, y1, x1, y1).
			LineTo(x1, y1+w).CubeTo(mx, y1+w, mx, y0+w, x0, y0+w).Close()
		c := t.Color(l.From)
		sc.Add(&scene.Shape{Path: p, Fill: color.NRGBA{c.R, c.G, c.B, flowA}})
	}
	right := float64(float64(ncol-1)*(nodeW+gap)) + nodeW
	for i := range n {
		r := scene.Rect{X: x[i], Y: y[i], W: nodeW, H: h[i]}
		sc.Fill(scene.RectPath(r), t.Strong(i), t.Strong(i), 0)
		sc.Label(r.X+r.W+labelGap, r.Y+float64(r.H/2), []string{labels[i]}, scene.Left, t.Text)
	}
	bottom := 0.0
	for c := range ncol {
		bottom = math.Max(bottom, next[c]-nodePad)
	}
	return diagram.Finish(sc, env, right, bottom), nil
}

func round3(v float64) float64 { return float64(math.Round(float64(v*1000)) / 1000) }

// Text lists the flows by source with bars scaled to the largest flow.
func (d *Diagram) Text(g *guard.Guard, maxCols int) ([]string, error) {
	maxV := 0.0
	nameW := 0
	for _, l := range d.Links {
		maxV = math.Max(maxV, l.Value)
		nameW = max(nameW, textgrid.Width(d.Nodes[l.To]))
	}
	valW := 0
	for _, l := range d.Links {
		valW = max(valW, len(chart.Format(round3(l.Value))))
	}
	nameW = min(nameW, max(maxCols/3, 4))
	barW := min(maxCols-4-nameW-2-valW-1, maxBar)
	if barW < minBar {
		return nil, guard.Limit("columns", 4+nameW+2+valW+1+minBar, maxCols)
	}
	var out []string
	for s := range d.Nodes {
		first := true
		for _, l := range d.Links {
			if err := g.Tick(); err != nil {
				return nil, err
			}
			if l.From != s {
				continue
			}
			if first {
				if len(out) > 0 {
					out = append(out, "")
				}
				out = append(out, textgrid.Wrap(d.Nodes[s], maxCols)...)
				first = false
			}
			name := shorten(d.Nodes[l.To], nameW)
			cells := 0
			if maxV > 0 {
				cells = max(int(math.Round(float64(l.Value/maxV*float64(barW)))), 1)
			}
			out = append(out, "  → "+name+strings.Repeat(" ", nameW-textgrid.Width(name))+"  "+
				strings.Repeat("█", cells)+strings.Repeat(" ", barW-cells)+" "+chart.Format(round3(l.Value)))
		}
	}
	return out, nil
}

// Bar widths in text output.
const (
	minBar = 4
	maxBar = 30
)

func shorten(s string, w int) string {
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
