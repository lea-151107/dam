// Package boxgraph lays out and draws diagrams of boxes joined by edges:
// entity relationship, requirement, C4 and architecture diagrams. A box
// has a centered header and optional sections below rules, whose rows are
// aligned in columns (entity attributes). The nodes are placed with
// internal/layout/sugiyama. The same Graph converts to the text renderer's
// model with TextGraph.
package boxgraph

import (
	"image/color"
	"strings"

	"github.com/lea-151107/dam/internal/diagram"
	"github.com/lea-151107/dam/internal/layout/sugiyama"
	"github.com/lea-151107/dam/internal/scene"
	"github.com/lea-151107/dam/internal/textgraph"
	"github.com/lea-151107/dam/internal/textgrid"
)

// Visual constants in layout units.
const (
	padX       = 12.0
	padY       = 6.0
	colGap     = 12.0
	nodeSep    = 50.0
	rankSep    = 60.0
	clusterPad = 16.0
	lineW      = 1.3
	markerSize = 12.0
	cardOffset = 14.0
	radius     = 6.0
)

// Graph is a diagram of boxes and edges.
type Graph struct {
	Dir      sugiyama.Dir
	Nodes    []Node
	Edges    []Edge
	Clusters []Cluster
}

// Shape is the outline of a box.
type Shape uint8

// Shapes.
const (
	Rect Shape = iota
	Round
	Note
)

// Node is a box.
type Node struct {
	// Header lines are centered; each is a label (markup allowed).
	Header []string
	// Sections follow below rules. Each row is a list of cells aligned in
	// columns across the section.
	Sections [][][]string
	Shape    Shape
	// Fill, Stroke and Text override the theme's node colors.
	Fill, Stroke, Text *color.NRGBA
	// Dashed draws the outline dashed.
	Dashed bool
	// Cluster is the innermost cluster containing the box, or -1.
	Cluster int
}

// Edge joins two boxes.
type Edge struct {
	From, To   int
	Label      string
	Start, End scene.Marker
	Dashed     bool
	// FromCard and ToCard are written next to the ends (multiplicities).
	FromCard, ToCard string
}

// Cluster is a framed group of boxes.
type Cluster struct {
	Title  string
	Parent int
	Dashed bool
}

type box struct {
	header   []string
	sections []section
	w, h     float64
	headH    float64
}

type section struct {
	rows  [][]string // cells, each a single line
	cols  []float64  // column widths
	lineH float64
	h     float64
}

// Layout lays out and draws g.
func Layout(env *diagram.Env, g *Graph) (*scene.Scene, error) {
	m := env.M
	t := env.T
	lh := m.LineHeight()
	boxes := make([]box, len(g.Nodes))
	nodes := make([]sugiyama.Node, len(g.Nodes))
	for i, n := range g.Nodes {
		var b box
		for _, h := range n.Header {
			b.header = append(b.header, env.Label(h)...)
		}
		hw, hh := m.Size(b.header)
		b.headH = hh + float64(2*padY)
		b.w = hw + 2*padX
		b.h = b.headH
		for _, rows := range n.Sections {
			s := section{lineH: lh}
			for _, row := range rows {
				var cells []string
				for k, c := range row {
					// Cells are single lines: line breaks become spaces.
					c = strings.ReplaceAll(c, "\n", " ")
					cells = append(cells, c)
					for len(s.cols) <= k {
						s.cols = append(s.cols, 0)
					}
					s.cols[k] = max(s.cols[k], m.Width(c))
				}
				s.rows = append(s.rows, cells)
			}
			s.h = float64(float64(len(s.rows))*lh) + float64(2*padY)
			rowW := 0.0
			for k, w := range s.cols {
				if k > 0 && w > 0 {
					rowW += colGap
				}
				rowW += w
			}
			b.w = max(b.w, rowW+2*padX)
			b.h += s.h
			b.sections = append(b.sections, s)
		}
		boxes[i] = b
		nodes[i] = sugiyama.Node{W: b.w, H: b.h, Cluster: n.Cluster}
	}
	var clusters []sugiyama.Cluster
	titles := make([][]string, len(g.Clusters))
	for i, c := range g.Clusters {
		titles[i] = env.Label(c.Title)
		tw, th := m.Size(titles[i])
		clusters = append(clusters, sugiyama.Cluster{Parent: c.Parent, TitleW: tw, TitleH: th + 4})
	}
	edges := make([]sugiyama.Edge, len(g.Edges))
	labels := make([][]string, len(g.Edges))
	for i, e := range g.Edges {
		se := sugiyama.Edge{From: e.From, To: e.To, MinLen: 1}
		if e.Label != "" {
			labels[i] = env.Label(e.Label)
			tw, th := m.Size(labels[i])
			se.LabelW, se.LabelH, se.HasLabel = tw+8, th+4, true
		}
		edges[i] = se
	}
	sp := env.Spacing
	res, err := sugiyama.Layout(env.G, nodes, edges, clusters, sugiyama.Config{
		Dir: g.Dir, NodeSep: float64(nodeSep * sp), RankSep: float64(rankSep * sp), ClusterPad: float64(clusterPad * sp),
	})
	if err != nil {
		return nil, err
	}
	sc := &scene.Scene{}
	for i, r := range res.Clusters {
		if r.W <= 0 {
			continue
		}
		var dash []float64
		fill := t.ClusterFill
		if g.Clusters[i].Dashed {
			dash = []float64{6, 4}
			fill = color.NRGBA{}
		}
		sc.Add(&scene.Shape{Path: scene.RoundRect(r, radius/2), Fill: fill, Stroke: scene.Stroke{Color: t.ClusterStroke, Width: lineW, Dash: dash}})
		_, th := m.Size(titles[i])
		sc.Label(r.X+float64(r.W/2), r.Y+float64(clusterPad*sp/2)+float64(th/2)+2, titles[i], scene.Center, t.Text)
	}
	rect := func(i int) scene.Rect {
		c := res.Nodes[i]
		return scene.Rect{X: c.X - float64(boxes[i].w/2), Y: c.Y - float64(boxes[i].h/2), W: boxes[i].w, H: boxes[i].h}
	}
	for i, e := range g.Edges {
		var dash []float64
		if e.Dashed {
			dash = []float64{5, 4}
		}
		if e.From == e.To {
			b := rect(e.From)
			x, y := b.X+b.W, b.Y+float64(b.H/2)
			p := scene.NewPath().MoveTo(x, y-8).CubeTo(x+30, y-24, x+30, y+24, x, y+8)
			sc.Line(p, t.Line, lineW, dash)
			if len(labels[i]) > 0 {
				sc.Label(x+32, y, labels[i], scene.Left, t.Text)
			}
			continue
		}
		pts := res.Edges[i]
		if len(pts) < 2 {
			continue
		}
		pts = scene.Dedupe(scene.ClipEndRect(scene.ClipStartRect(pts, rect(e.From)), rect(e.To)))
		if len(pts) < 2 {
			continue
		}
		line := scene.ShortenStart(scene.Shorten(pts, scene.MarkerLen(e.End, markerSize)), scene.MarkerLen(e.Start, markerSize))
		sc.Line(scene.Smooth(line), t.Line, lineW, dash)
		n := len(pts)
		if e.End != scene.NoMarker {
			sc.DrawMarker(e.End, pts[n-1], pts[n-2], markerSize, t.Line, t.Base, lineW)
		}
		if e.Start != scene.NoMarker {
			sc.DrawMarker(e.Start, pts[0], pts[1], markerSize, t.Line, t.Base, lineW)
		}
		if e.FromCard != "" {
			cardinality(sc, env, pts[0], pts[1], e.FromCard)
		}
		if e.ToCard != "" {
			cardinality(sc, env, pts[n-1], pts[n-2], e.ToCard)
		}
		if len(labels[i]) > 0 {
			p := res.Labels[i]
			tw, th := m.Size(labels[i])
			sc.Fill(scene.RectPath(scene.Rect{X: p.X - float64(tw/2) - 3, Y: p.Y - float64(th/2) - 1, W: tw + 6, H: th + 2}), t.LabelBg, color.NRGBA{}, 0)
			sc.Label(p.X, p.Y, labels[i], scene.Center, t.Text)
		}
	}
	for i, n := range g.Nodes {
		b := boxes[i]
		r := rect(i)
		fill, stroke, text := t.NodeFill, t.NodeStroke, t.Text
		if n.Shape == Note {
			fill, stroke = t.NoteFill, t.NoteStroke
		}
		if n.Fill != nil {
			fill = *n.Fill
		}
		if n.Stroke != nil {
			stroke = *n.Stroke
		}
		if n.Text != nil {
			text = *n.Text
		} else {
			text = diagram.Readable(text, fill)
		}
		path := scene.RectPath(r)
		if n.Shape == Round {
			path = scene.RoundRect(r, radius)
		}
		var dash []float64
		if n.Dashed {
			dash = []float64{5, 4}
		}
		sc.Add(&scene.Shape{Path: path, Fill: fill, Stroke: scene.Stroke{Color: stroke, Width: lineW, Dash: dash}})
		sc.Label(r.X+float64(r.W/2), r.Y+float64(b.headH/2), b.header, scene.Center, text)
		y := r.Y + b.headH
		for _, s := range b.sections {
			sc.Line(scene.Polyline([]scene.Point{{X: r.X, Y: y}, {X: r.X + r.W, Y: y}}), stroke, lineW, nil)
			for k, row := range s.rows {
				x := r.X + padX
				cy := y + padY + float64((float64(k)+0.5)*s.lineH)
				for c, cell := range row {
					if cell != "" {
						sc.Label(x, cy, []string{cell}, scene.Left, text)
					}
					if s.cols[c] > 0 {
						x += s.cols[c] + colGap
					}
				}
			}
			y += s.h
		}
	}
	return diagram.Finish(sc, env, res.Width, res.Height), nil
}

// cardinality writes a multiplicity label near the end of an edge.
func cardinality(sc *scene.Scene, env *diagram.Env, end, next scene.Point, text string) {
	dir := next.Sub(end).Norm()
	nrm := scene.Point{X: -dir.Y, Y: dir.X}
	p := end.Add(dir.Mul(cardOffset + 4)).Add(nrm.Mul(cardOffset))
	sc.Label(p.X, p.Y, []string{text}, scene.Center, env.T.Text)
}

// TextGraph converts g to the text renderer's model. Section rows become
// lines with their columns aligned; crow's foot markers have no text head,
// so the edge label carries the multiplicities instead.
func (g *Graph) TextGraph() *textgraph.Graph {
	tg := &textgraph.Graph{Dir: g.Dir}
	for _, n := range g.Nodes {
		tn := textgraph.Node{Label: strings.Join(n.Header, "\n"), Cluster: n.Cluster}
		if n.Shape == Round {
			tn.Corners = textgrid.Rounded
		}
		for _, rows := range n.Sections {
			tn.Sections = append(tn.Sections, alignRows(rows))
		}
		tg.Nodes = append(tg.Nodes, tn)
	}
	for _, c := range g.Clusters {
		tg.Clusters = append(tg.Clusters, textgraph.Cluster{Title: c.Title, Parent: c.Parent})
	}
	for _, e := range g.Edges {
		te := textgraph.NewEdge(e.From, e.To)
		te.Start, te.End = textHead(e.Start), textHead(e.End)
		from, to := e.FromCard, e.ToCard
		if from == "" {
			from = crowCard(e.Start)
		}
		if to == "" {
			to = crowCard(e.End)
		}
		te.Label = strings.Join(nonEmpty(from, e.Label, to), " ")
		if e.Dashed {
			te.Style = textgraph.Dashed
		}
		tg.Edges = append(tg.Edges, te)
	}
	return tg
}

// crowCard returns the multiplicity a crow's foot marker stands for.
func crowCard(m scene.Marker) string {
	switch m {
	case scene.OneOnly:
		return "1"
	case scene.ZeroOrOne:
		return "0..1"
	case scene.OneOrMore:
		return "1..*"
	case scene.ZeroOrMore:
		return "0..*"
	}
	return ""
}

func textHead(m scene.Marker) textgraph.Head {
	switch m {
	case scene.OneOnly, scene.ZeroOrOne, scene.OneOrMore, scene.ZeroOrMore:
		return textgraph.Head{}
	}
	return textgraph.HeadFor(m)
}

// alignRows joins the cells of each row with their columns aligned.
func alignRows(rows [][]string) string {
	var widths []int
	for _, row := range rows {
		for k, c := range row {
			for len(widths) <= k {
				widths = append(widths, 0)
			}
			widths[k] = max(widths[k], textgrid.Width(strings.ReplaceAll(c, "\n", " ")))
		}
	}
	lines := make([]string, len(rows))
	for i, row := range rows {
		var b strings.Builder
		for k, c := range row {
			c = strings.ReplaceAll(c, "\n", " ")
			if k > 0 && widths[k] > 0 {
				b.WriteString("  ")
			}
			b.WriteString(c)
			if k < len(row)-1 {
				b.WriteString(strings.Repeat(" ", widths[k]-textgrid.Width(c)))
			}
		}
		lines[i] = strings.TrimRight(b.String(), " ")
	}
	return strings.Join(lines, "\n")
}

func nonEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
