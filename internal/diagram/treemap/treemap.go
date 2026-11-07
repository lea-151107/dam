// Package treemap parses and lays out Mermaid treemap diagrams
// (treemap-beta) with the squarified layout.
package treemap

import (
	"math"
	"regexp"
	"sort"
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

// Node is a section (with children) or a leaf (with a value).
type Node struct {
	Label    string
	Value    float64 // leaves: given; sections: sum of children
	Children []int
	Parent   int
	Class    string
	Style    theme.Style
	indent   int
	line     int
}

// Diagram is a parsed treemap. Node 0 is an implicit root.
type Diagram struct {
	Title     string
	Nodes     []*Node
	classDefs map[string]theme.Style
}

// maxDepth bounds nesting.
const maxDepth = 32

var (
	nodeRe     = regexp.MustCompile(`^"([^"]*)"\s*(?::\s*([-+0-9.eE]+))?\s*(?::::\s*([\w-]+))?$`)
	classDefRe = regexp.MustCompile(`^classDef\s+([\w-]+)\s+(.*)$`)
)

// Parse parses a treemap.
func Parse(g *guard.Guard, lines []source.Line) (*Diagram, error) {
	d := &Diagram{Nodes: []*Node{{Parent: -1, indent: -1}}, classDefs: map[string]theme.Style{}}
	stack := []int{0}
	for _, l := range lines[1:] {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		t := strings.TrimSpace(l.Text)
		if r, ok := source.CutKeyword(t, "title"); ok {
			d.Title = source.Label(r)
			continue
		}
		if strings.HasPrefix(t, "accTitle") || strings.HasPrefix(t, "accDescr") {
			// NOTE(mermaid-compat): accessibility texts do not affect the image.
			continue
		}
		if m := classDefRe.FindStringSubmatch(t); m != nil {
			d.classDefs[m[1]] = d.classDefs[m[1]].Merge(theme.ParseStyle(strings.TrimSuffix(m[2], ";")))
			continue
		}
		m := nodeRe.FindStringSubmatch(t)
		if m == nil {
			return nil, source.Errorf(l.No, `expected "name" or "name": value`)
		}
		ind := source.Indent(l.Text)
		for len(stack) > 1 && d.Nodes[stack[len(stack)-1]].indent >= ind {
			stack = stack[:len(stack)-1]
		}
		parent := stack[len(stack)-1]
		if d.Nodes[parent].Children == nil && parent != 0 && d.Nodes[parent].Value != 0 {
			return nil, source.Errorf(l.No, "%q has a value and cannot have children", d.Nodes[parent].Label)
		}
		if len(stack) > maxDepth {
			return nil, source.TooLarge(l.No, "nested too deeply")
		}
		n := &Node{Label: source.Label(m[1]), Parent: parent, Class: m[3], indent: ind, line: l.No}
		if m[2] != "" {
			v, err := strconv.ParseFloat(m[2], 64)
			if err != nil || v < 0 || math.IsInf(v, 0) || math.IsNaN(v) {
				return nil, source.Errorf(l.No, "invalid value %q", m[2])
			}
			n.Value = v
		}
		d.Nodes = append(d.Nodes, n)
		i := len(d.Nodes) - 1
		d.Nodes[parent].Children = append(d.Nodes[parent].Children, i)
		stack = append(stack, i)
		if err := g.Count("nodes", len(d.Nodes)-1); err != nil {
			return nil, err
		}
	}
	if len(d.Nodes) == 1 {
		return nil, source.Errorf(lines[len(lines)-1].No, "treemap has no nodes")
	}
	// Sections sum their children; nodes are visited children first.
	for i := len(d.Nodes) - 1; i >= 0; i-- {
		n := d.Nodes[i]
		if len(n.Children) > 0 {
			n.Value = 0
			for _, c := range n.Children {
				n.Value += d.Nodes[c].Value
				if math.IsInf(n.Value, 0) {
					// The child whose value overflowed the sum.
					return nil, source.Errorf(d.Nodes[c].line, "the values add up to too large a number")
				}
			}
			// As in xychart: the area per unit of value overflows for a
			// subnormal sum (it gave a "+Inf×NaN pixels" drawing).
			if n.Value > 0 && n.Value < chart.MinSpan {
				ln := n.line
				if i == 0 {
					ln = lines[len(lines)-1].No // the implicit root
				}
				return nil, source.Errorf(ln, "the values add up to too small a number")
			}
		}
		n.Style = d.classDefs[n.Class]
	}
	return d, nil
}

// DiagramTitle returns the title given with the title statement.
func (d *Diagram) DiagramTitle() string { return d.Title }

// Visual constants in layout units.
const (
	mapW    = 600.0
	mapH    = 400.0
	pad     = 4.0
	headPad = 4.0
	lineW   = 1.0
	minSide = 2.0 // rectangles thinner than this get no label
)

// squarify lays out values (sorted descending) in r and returns their
// rectangles in the order of the input indexes.
func squarify(values []float64, r scene.Rect) []scene.Rect {
	out := make([]scene.Rect, len(values))
	order := make([]int, len(values))
	total := 0.0
	for i := range order {
		order[i] = i
		total += values[i]
	}
	if total <= 0 {
		return out
	}
	sort.SliceStable(order, func(a, b int) bool { return values[order[a]] > values[order[b]] })
	scale := float64(r.W * r.H / total)
	area := func(i int) float64 { return float64(values[i] * scale) }
	rest := r
	worst := func(row []int, side float64) float64 {
		s, lo, hi := 0.0, math.Inf(1), 0.0
		for _, i := range row {
			a := area(i)
			s += a
			lo, hi = math.Min(lo, a), math.Max(hi, a)
		}
		if s == 0 || lo == 0 {
			return math.Inf(1)
		}
		return math.Max(float64(side*side*hi/(float64(s*s))), float64(s*s/(float64(side*side*lo))))
	}
	for k := 0; k < len(order); {
		side := math.Min(rest.W, rest.H)
		row := []int{order[k]}
		k++
		for k < len(order) && worst(append(row, order[k]), side) <= worst(row, side) {
			row = append(row, order[k])
			k++
		}
		s := 0.0
		for _, i := range row {
			s += area(i)
		}
		if rest.W >= rest.H {
			// A column on the left.
			w := 0.0
			if rest.H > 0 {
				w = float64(s / rest.H)
			}
			y := rest.Y
			for _, i := range row {
				h := 0.0
				if w > 0 {
					h = float64(area(i) / w)
				}
				out[i] = scene.Rect{X: rest.X, Y: y, W: w, H: h}
				y += h
			}
			rest = scene.Rect{X: rest.X + w, Y: rest.Y, W: math.Max(rest.W-w, 0), H: rest.H}
		} else {
			h := 0.0
			if rest.W > 0 {
				h = float64(s / rest.W)
			}
			x := rest.X
			for _, i := range row {
				w := 0.0
				if h > 0 {
					w = float64(area(i) / h)
				}
				out[i] = scene.Rect{X: x, Y: rest.Y, W: w, H: h}
				x += w
			}
			rest = scene.Rect{X: rest.X, Y: rest.Y + h, W: rest.W, H: math.Max(rest.H-h, 0)}
		}
	}
	return out
}

// Layout implements diagram.Layouter.
func (d *Diagram) Layout(env *diagram.Env) (*scene.Scene, error) {
	m := env.M
	t := env.T
	lh := m.LineHeight()
	w, h := float64(mapW*math.Max(env.Spacing, 0.6)), mapH
	if env.MaxWidth > 0 {
		w = math.Min(w, math.Max(env.ContentWidth(), 200))
	}
	sc := &scene.Scene{}
	var draw func(n int, r scene.Rect, top, depth int) error
	draw = func(n int, r scene.Rect, top, depth int) error {
		if err := env.G.Tick(); err != nil {
			return err
		}
		node := d.Nodes[n]
		inner := r
		if n != 0 {
			fill := theme.Mix(t.Base, t.Color(top), math.Min(0.35+float64(0.2*float64(depth)), 0.9))
			stroke := t.Base
			text := t.Text
			if node.Style.Fill != nil {
				fill = *node.Style.Fill
			}
			if node.Style.Stroke != nil {
				stroke = *node.Style.Stroke
			}
			if node.Style.Color != nil {
				text = *node.Style.Color
			} else {
				text = theme.Readable(text, fill)
			}
			sc.Fill(scene.RectPath(r), fill, stroke, lineW*2)
			label := node.Label
			if len(node.Children) == 0 {
				label += "\n" + chart.Format(node.Value)
			}
			if r.W > minSide*8 && r.H > lh {
				lines := m.Lines(label, r.W-2*pad)
				_, th := m.Size(lines)
				if len(node.Children) == 0 {
					if th <= r.H-2*pad {
						sc.Label(r.X+float64(r.W/2), r.Y+float64(r.H/2), lines, scene.Center, text)
					}
				} else if len(lines) > 0 {
					// A section shows the first line of its name in its
					// header band.
					sc.Label(r.X+pad, r.Y+headPad+float64(lh/2), lines[:1], scene.Left, text)
				}
			}
			if len(node.Children) == 0 {
				return nil
			}
			inner = scene.Rect{X: r.X + pad, Y: r.Y + lh + 2*headPad, W: r.W - 2*pad, H: r.H - lh - 2*headPad - pad}
			if inner.W <= 0 || inner.H <= 0 {
				return nil
			}
		}
		values := make([]float64, len(node.Children))
		for k, c := range node.Children {
			values[k] = d.Nodes[c].Value
		}
		for k, rr := range squarify(values, inner) {
			nt := top
			if n == 0 {
				nt = k
			}
			if err := draw(node.Children[k], rr, nt, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := draw(0, scene.Rect{W: w, H: h}, 0, -1); err != nil {
		return nil, err
	}
	return diagram.Finish(sc, env, w, h), nil
}

// Text renders the tree with values: sections show their totals.
func (d *Diagram) Text(g *guard.Guard, maxCols int) ([]string, error) {
	var out []string
	var walk func(n int, prefix, cont string) error
	walk = func(n int, prefix, cont string) error {
		if err := g.Tick(); err != nil {
			return err
		}
		node := d.Nodes[n]
		line := strings.ReplaceAll(node.Label, "\n", " ") + "  " + chart.Format(node.Value)
		room := maxCols - textgrid.Width(prefix)
		if room < minCols {
			return guard.Limit("columns", textgrid.Width(prefix)+minCols, maxCols)
		}
		lines := textgrid.Wrap(line, room)
		out = append(out, prefix+lines[0])
		for _, l := range lines[1:] {
			out = append(out, cont+l)
		}
		for k, c := range node.Children {
			p, q := cont+"├─ ", cont+"│  "
			if k == len(node.Children)-1 {
				p, q = cont+"└─ ", cont+"   "
			}
			if err := walk(c, p, q); err != nil {
				return err
			}
		}
		return nil
	}
	for _, c := range d.Nodes[0].Children {
		if err := walk(c, "", ""); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// minCols is the narrowest room left for a node.
const minCols = 6
