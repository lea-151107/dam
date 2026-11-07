package state

import (
	"image/color"

	"github.com/lea/mermaid/internal/diagram"
	"github.com/lea/mermaid/internal/layout/sugiyama"
	"github.com/lea/mermaid/internal/scene"
	"github.com/lea/mermaid/internal/theme"
)

// Visual constants in layout units.
const (
	padX       = 14.0
	padY       = 8.0
	minW       = 50.0
	pseudoD    = 14.0
	choiceD    = 26.0
	barLong    = 70.0
	barShort   = 8.0
	nodeSep    = 50.0
	rankSep    = 50.0
	clusterPad = 14.0
	lineW      = 1.3
	markerSize = 9.0
	radius     = 6.0
	// noCluster is a parent index that no cluster has, so the layout never
	// places the cluster.
	noCluster = -2
)

type geo struct {
	head, desc []string
	w, h       float64
	headH      float64
}

// Layout implements diagram.Layouter.
func (d *Diagram) Layout(env *diagram.Env) (*scene.Scene, error) {
	m := env.M
	t := env.T
	hasMembers := d.hasMembers()
	geos := make([]geo, len(d.States))
	var nodes []sugiyama.Node
	li := make([]int, len(d.States))
	for i, s := range d.States {
		li[i] = -1
		if s.Composite >= 0 && hasMembers[s.Composite] {
			continue
		}
		var g geo
		switch s.Kind {
		case Start, End:
			g.w, g.h = pseudoD, pseudoD
		case Choice:
			g.w, g.h = choiceD, choiceD
		case Fork, Join:
			g.w, g.h = barLong, barShort
			if d.Dir.Horizontal() {
				g.w, g.h = barShort, barLong
			}
		default:
			g.head = env.Label(s.Label)
			for _, ds := range s.Desc {
				g.desc = append(g.desc, env.Label(ds)...)
			}
			w1, h1 := m.Size(g.head)
			w2, h2 := m.Size(g.desc)
			g.headH = h1 + float64(2*padY)
			g.w = max(w1, w2) + 2*padX
			if s.Kind != Note {
				g.w = max(g.w, minW)
			}
			g.h = g.headH
			if len(g.desc) > 0 {
				g.h += h2 + float64(2*padY)
			}
		}
		geos[i] = g
		li[i] = len(nodes)
		nodes = append(nodes, sugiyama.Node{W: g.w, H: g.h, Cluster: s.Parent})
	}
	var clusters []sugiyama.Cluster
	titles := make([][]string, len(d.Composites))
	for i, c := range d.Composites {
		if !c.Region {
			titles[i] = env.Label(d.States[c.State].Label)
		}
		tw, th := m.Size(titles[i])
		parent := c.Parent
		if !hasMembers[i] {
			// Not a cluster: its state is laid out as a node instead.
			parent = noCluster
		}
		cl := sugiyama.Cluster{Parent: parent, TitleW: tw, TitleH: th + padY}
		if c.Region {
			cl.TitleH = 0
		}
		// Regions follow the direction of their composite.
		dirOf := c
		if c.Region && c.Parent >= 0 {
			dirOf = d.Composites[c.Parent]
		}
		cl.Dir, cl.HasDir = dirOf.Dir, dirOf.HasDir
		clusters = append(clusters, cl)
	}
	first := func(c int) int {
		for i, s := range d.States {
			if li[i] < 0 {
				continue
			}
			for p := s.Parent; p >= 0; p = d.Composites[p].Parent {
				if p == c {
					return i
				}
			}
		}
		return -1
	}
	endpoint := func(s int) (int, int) {
		if li[s] >= 0 {
			return li[s], -1
		}
		c := d.States[s].Composite
		return li[first(c)], c
	}
	type eg struct {
		from, to, fc, tc int
		lines            []string
	}
	egs := make([]eg, len(d.Transitions))
	edges := make([]sugiyama.Edge, len(d.Transitions))
	for i, tr := range d.Transitions {
		f, fc := endpoint(tr.From)
		to, tc := endpoint(tr.To)
		egs[i] = eg{from: f, to: to, fc: fc, tc: tc}
		e := sugiyama.Edge{From: f, To: to, MinLen: 1}
		if tr.Label != "" {
			egs[i].lines = env.Label(tr.Label)
			tw, th := m.Size(egs[i].lines)
			e.LabelW, e.LabelH, e.HasLabel = tw+8, th+4, true
		}
		edges[i] = e
	}
	sp := env.Spacing
	res, err := sugiyama.Layout(env.G, nodes, edges, clusters, sugiyama.Config{
		Dir: d.Dir, NodeSep: float64(nodeSep * sp), RankSep: float64(rankSep * sp), ClusterPad: float64(clusterPad * sp),
	})
	if err != nil {
		return nil, err
	}
	sc := &scene.Scene{}
	// Composite states, outermost first.
	for depth := 0; depth <= maxDepth; depth++ {
		for i, c := range d.Composites {
			dd := 0
			for p := c.Parent; p >= 0; p = d.Composites[p].Parent {
				dd++
			}
			r := res.Clusters[i]
			if dd != depth || r.W <= 0 || !hasMembers[i] {
				continue
			}
			if c.Region {
				// NOTE(mermaid-compat): concurrent regions get a dashed
				// frame each instead of dashed dividers between them.
				sc.Line(scene.RoundRect(r, radius), t.Muted, lineW, []float64{4, 3})
				continue
			}
			sc.Fill(scene.RoundRect(r, radius), t.ClusterFill, t.NodeStroke, lineW)
			_, th := m.Size(titles[i])
			ty := r.Y + float64(clusterPad*sp/2) + float64(th/2) + 2
			sc.Label(r.X+float64(r.W/2), ty, titles[i], scene.Center, t.Text)
			y := r.Y + float64(clusterPad*sp/2) + th + padY
			sc.Line(scene.Polyline([]scene.Point{{X: r.X, Y: y}, {X: r.X + r.W, Y: y}}), t.NodeStroke, lineW, nil)
		}
	}
	outline := func(s, l int) []scene.Point {
		c := res.Nodes[l]
		g := geos[s]
		switch d.States[s].Kind {
		case Start, End:
			return scene.EllipsePoints(c, float64(g.w/2)+1, float64(g.h/2)+1)
		case Choice:
			return []scene.Point{{X: c.X, Y: c.Y - float64(g.h/2)}, {X: c.X + float64(g.w/2), Y: c.Y}, {X: c.X, Y: c.Y + float64(g.h/2)}, {X: c.X - float64(g.w/2), Y: c.Y}}
		}
		return scene.RectPoints(scene.Rect{X: c.X - float64(g.w/2), Y: c.Y - float64(g.h/2), W: g.w, H: g.h})
	}
	stateOf := make([]int, len(nodes))
	for s, l := range li {
		if l >= 0 {
			stateOf[l] = s
		}
	}
	for i, tr := range d.Transitions {
		e := egs[i]
		if e.from == e.to && e.fc < 0 && e.tc < 0 {
			c := res.Nodes[e.from]
			g := geos[tr.From]
			x, y := c.X+float64(g.w/2), c.Y
			p := scene.NewPath().MoveTo(x, y-6).CubeTo(x+28, y-22, x+28, y+22, x+markerSize*0.8, y+6)
			sc.Line(p, t.Line, lineW, nil)
			sc.DrawMarker(scene.Arrow, scene.Point{X: x, Y: y + 6}, scene.Point{X: x + markerSize, Y: y + 8}, markerSize, t.Line, t.Base, lineW)
			if len(e.lines) > 0 {
				sc.Label(x+30, y, e.lines, scene.Left, t.Text)
			}
			continue
		}
		pts := res.Edges[i]
		if len(pts) < 2 {
			continue
		}
		if e.fc >= 0 {
			pts = scene.ClipStartRect(pts, res.Clusters[e.fc])
		} else {
			pts = scene.ClipStart(pts, outline(stateOf[e.from], e.from))
		}
		if e.tc >= 0 {
			pts = scene.ClipEndRect(pts, res.Clusters[e.tc])
		} else {
			pts = scene.ClipEnd(pts, outline(stateOf[e.to], e.to))
		}
		if tr.Note {
			sc.Line(scene.Polyline(pts), t.Muted, lineW, []float64{4, 3})
			continue
		}
		sc.Line(scene.Smooth(scene.Shorten(pts, markerSize*0.9)), t.Line, lineW, nil)
		n := len(pts)
		sc.DrawMarker(scene.Arrow, pts[n-1], pts[n-2], markerSize, t.Line, t.Base, lineW)
		if len(e.lines) > 0 {
			p := res.Labels[i]
			tw, th := m.Size(e.lines)
			sc.Fill(scene.RectPath(scene.Rect{X: p.X - float64(tw/2) - 3, Y: p.Y - float64(th/2) - 1, W: tw + 6, H: th + 2}), t.LabelBg, color.NRGBA{}, 0)
			sc.Label(p.X, p.Y, e.lines, scene.Center, t.Text)
		}
	}
	for s, st := range d.States {
		l := li[s]
		if l < 0 {
			continue
		}
		c := res.Nodes[l]
		g := geos[s]
		r := scene.Rect{X: c.X - float64(g.w/2), Y: c.Y - float64(g.h/2), W: g.w, H: g.h}
		fill, stroke, text := t.NodeFill, t.NodeStroke, t.Text
		if st.Style.Fill != nil {
			fill = *st.Style.Fill
		}
		if st.Style.Stroke != nil {
			stroke = *st.Style.Stroke
		}
		if st.Style.Color != nil {
			text = *st.Style.Color
		} else {
			text = theme.Readable(text, fill)
		}
		switch st.Kind {
		case Start:
			sc.Fill(scene.Ellipse(c, float64(g.w/2), float64(g.w/2)), t.Line, color.NRGBA{}, 0)
		case End:
			sc.Fill(scene.Ellipse(c, float64(g.w/2), float64(g.w/2)), t.Base, t.Line, lineW)
			sc.Fill(scene.Ellipse(c, float64(g.w/2)-3.5, float64(g.w/2)-3.5), t.Line, color.NRGBA{}, 0)
		case Choice:
			sc.Fill(scene.Polygon(outline(s, l)), fill, stroke, lineW)
		case Fork, Join:
			sc.Fill(scene.RoundRect(r, 2), t.Line, color.NRGBA{}, 0)
		case Note:
			sc.Fill(scene.RectPath(r), t.NoteFill, t.NoteStroke, lineW)
			sc.Label(c.X, c.Y, g.head, scene.Center, t.Text)
		default:
			sc.Fill(scene.RoundRect(r, radius), fill, stroke, lineW)
			sc.Label(c.X, r.Y+float64(g.headH/2), g.head, scene.Center, text)
			if len(g.desc) > 0 {
				y := r.Y + g.headH
				sc.Line(scene.Polyline([]scene.Point{{X: r.X, Y: y}, {X: r.X + r.W, Y: y}}), stroke, lineW, nil)
				_, h2 := m.Size(g.desc)
				sc.Label(r.X+padX, y+padY+float64(h2/2), g.desc, scene.Left, text)
			}
		}
	}
	return diagram.Finish(sc, env, res.Width, res.Height), nil
}

// hasMembers reports for each composite whether any state laid out as a
// node is nested inside it. A composite without members is drawn as a
// plain state, and such a state is itself a member of its parent, so this
// repeats until stable (bounded by the nesting depth).
func (d *Diagram) hasMembers() []bool {
	has := make([]bool, len(d.Composites))
	for changed, round := true, 0; changed && round <= maxDepth+1; round++ {
		changed = false
		for _, s := range d.States {
			isNode := s.Composite < 0 || !has[s.Composite]
			if !isNode || s.Parent < 0 {
				continue
			}
			for c := s.Parent; c >= 0; c = d.Composites[c].Parent {
				if !has[c] {
					has[c] = true
					changed = true
				}
			}
		}
	}
	return has
}
