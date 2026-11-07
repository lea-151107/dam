package flowchart

import (
	"image/color"
	"math"

	"github.com/lea/mermaid/internal/diagram"
	"github.com/lea/mermaid/internal/layout/sugiyama"
	"github.com/lea/mermaid/internal/scene"
	"github.com/lea/mermaid/internal/theme"
)

// Visual constants in layout units.
const (
	padX         = 15.0
	padY         = 10.0
	nodeSep      = 50.0
	rankSep      = 50.0
	clusterPad   = 14.0
	strokeW      = 1.3
	edgeW        = 1.5
	thickW       = 3.0
	markerSize   = 9.0
	labelPad     = 4.0
	selfLoopSize = 26.0
	cornerRadius = 5.0
)

var dottedDash = []float64{3, 3}

// Layout implements diagram.Layouter.
func (g *Graph) Layout(env *diagram.Env) (*scene.Scene, error) {
	m := env.M
	type nodeGeo struct {
		lines []string
		w, h  float64
	}
	geo := make([]nodeGeo, len(g.Nodes))
	var snodes []sugiyama.Node
	sIndex := make([]int, len(g.Nodes)) // graph node → layout node, or -1
	for i, n := range g.Nodes {
		sIndex[i] = -1
		if g.ClusterRef[i] >= 0 && g.FirstMember[g.ClusterRef[i]] >= 0 {
			continue
		}
		lines := env.Label(n.Label)
		if !showsLabel(n.Shape) {
			lines = nil
		}
		tw, th := m.Size(lines)
		th = max(th, m.LineHeight())
		w, h := nodeSize(n.Shape, tw, th)
		geo[i] = nodeGeo{lines, w, h}
		sIndex[i] = len(snodes)
		snodes = append(snodes, sugiyama.Node{W: w, H: h, Cluster: n.Cluster})
	}
	var sclusters []sugiyama.Cluster
	titles := make([][]string, len(g.Subgraphs))
	for i, sg := range g.Subgraphs {
		titles[i] = env.Label(sg.Title)
		tw, th := m.Size(titles[i])
		sclusters = append(sclusters, sugiyama.Cluster{Parent: sg.Parent, TitleW: tw, TitleH: th + 4,
			Dir: sg.Dir, HasDir: sg.HasDir && !g.linkedOutside(i)})
	}
	// endpoint maps a graph node to a layout node; cluster references use a
	// representative member and are clipped at the cluster box later.
	endpoint := func(i int) (int, int) {
		if sIndex[i] >= 0 {
			return sIndex[i], -1
		}
		c := g.ClusterRef[i]
		return sIndex[g.FirstMember[c]], c
	}
	type edgeGeo struct {
		lines      []string
		lw, lh     float64
		from, to   int
		fromC, toC int
		selfLoop   bool
		// own is set for an edge between a node and a subgraph that
		// contains it; it is drawn from the node to the subgraph border.
		own bool
	}
	eg := make([]edgeGeo, len(g.Edges))
	var sedges []sugiyama.Edge
	edgeIdx := make([]int, len(g.Edges))
	for i, e := range g.Edges {
		from, fc := endpoint(e.From)
		to, tc := endpoint(e.To)
		x := edgeGeo{from: from, to: to, fromC: fc, toC: tc}
		if e.Label != "" {
			x.lines = env.Label(e.Label)
			tw, th := m.Size(x.lines)
			x.lw, x.lh = tw+float64(2*labelPad), th+labelPad
		}
		x.selfLoop = from == to && fc < 0 && tc < 0
		x.own = from == to && (fc >= 0 || tc >= 0)
		eg[i] = x
		edgeIdx[i] = len(sedges)
		sedges = append(sedges, sugiyama.Edge{From: from, To: to, MinLen: e.MinLen,
			LabelW: x.lw, LabelH: x.lh, HasLabel: e.Label != "" && !x.selfLoop})
	}
	sp := env.Spacing
	res, err := sugiyama.Layout(env.G, snodes, sedges, sclusters, sugiyama.Config{
		Dir: g.Dir, NodeSep: float64(nodeSep * sp), RankSep: float64(rankSep * sp), ClusterPad: float64(clusterPad * sp),
	})
	if err != nil {
		return nil, err
	}

	t := env.T
	sc := &scene.Scene{}
	width, height := res.Width, res.Height

	// Clusters, outermost first.
	order := clusterOrder(g)
	for _, c := range order {
		r := res.Clusters[c]
		if r.W <= 0 {
			continue
		}
		sc.Fill(scene.RectPath(r), t.ClusterFill, t.ClusterStroke, strokeW)
		_, th := m.Size(titles[c])
		sc.Label(r.X+float64(r.W/2), r.Y+float64(clusterPad*sp/2)+float64(th/2)+2, titles[c], scene.Center, t.Text)
	}

	// Edges.
	outlines := make([][]scene.Point, len(snodes))
	for gi, li := range sIndex {
		if li >= 0 {
			outlines[li] = shapeOutline(g.Nodes[gi].Shape, res.Nodes[li], geo[gi].w, geo[gi].h)
		}
	}
	type pendingLabel struct {
		at    scene.Point
		lines []string
		w, h  float64
		color color.NRGBA
	}
	var labels []pendingLabel
	for i, e := range g.Edges {
		x := eg[i]
		if e.Kind == Invisible {
			continue
		}
		col := t.Line
		if e.Style.Stroke != nil {
			col = *e.Style.Stroke
		}
		w := edgeW
		switch e.Kind {
		case Thick:
			w = thickW
		}
		if e.Style.StrokeWidth > 0 {
			w = e.Style.StrokeWidth
		}
		var dash []float64
		if e.Kind == Dotted || e.Style.Dashed {
			dash = dottedDash
		}
		if x.selfLoop {
			lp := drawSelfLoop(sc, res.Nodes[x.from], snodes[x.from], e, col, t.Base, w, dash)
			width = max(width, lp.X+x.lw+4)
			if len(x.lines) > 0 {
				labels = append(labels, pendingLabel{scene.Point{X: lp.X + float64(x.lw/2) + 2, Y: lp.Y}, x.lines, x.lw, x.lh, col})
			}
			continue
		}
		pts := res.Edges[edgeIdx[i]]
		if x.own {
			// The node lies inside the subgraph: connect it to the border
			// in the flow direction.
			c := x.toC
			if c < 0 {
				c = x.fromC
			}
			pts = ownEdge(res.Nodes[x.from], res.Clusters[c], g.Dir, x.fromC >= 0)
		}
		if len(pts) < 2 {
			continue
		}
		switch {
		case x.own && x.fromC >= 0:
			pts = scene.ClipEnd(pts, outlines[x.to])
		case x.own:
			pts = scene.ClipStart(pts, outlines[x.from])
		default:
			if x.fromC >= 0 {
				pts = scene.ClipStartRect(pts, res.Clusters[x.fromC])
			} else {
				pts = scene.ClipStart(pts, outlines[x.from])
			}
			if x.toC >= 0 {
				pts = scene.ClipEndRect(pts, res.Clusters[x.toC])
			} else {
				pts = scene.ClipEnd(pts, outlines[x.to])
			}
		}
		pts = scene.Dedupe(pts)
		if len(pts) < 2 {
			continue
		}
		tip, tail := pts[len(pts)-1], pts[0]
		ms := float64(markerSize * math.Max(1, float64(w/edgeW*0.7)))
		line := scene.Shorten(pts, float64(scene.MarkerLen(e.End, ms)*0.9))
		line = scene.ShortenStart(line, float64(scene.MarkerLen(e.Start, ms)*0.9))
		if e.Linear {
			sc.Line(scene.Polyline(line), col, w, dash)
		} else {
			sc.Line(scene.Smooth(line), col, w, dash)
		}
		if e.End != scene.NoMarker {
			sc.DrawMarker(e.End, tip, pts[len(pts)-2], ms, col, t.Base, edgeW)
		}
		if e.Start != scene.NoMarker {
			sc.DrawMarker(e.Start, tail, pts[1], ms, col, t.Base, edgeW)
		}
		if len(x.lines) > 0 {
			at := res.Labels[edgeIdx[i]]
			if x.own {
				at = scene.MidPoint(pts)
			}
			labels = append(labels, pendingLabel{at, x.lines, x.lw, x.lh, col})
		}
	}

	// Nodes.
	for gi, n := range g.Nodes {
		li := sIndex[gi]
		if li < 0 {
			continue
		}
		c := res.Nodes[li]
		fill, stroke, textCol := t.NodeFill, t.NodeStroke, t.Text
		if n.Style.Fill != nil {
			fill = *n.Style.Fill
		}
		if n.Style.Stroke != nil {
			stroke = *n.Style.Stroke
		}
		if n.Style.Color != nil {
			textCol = *n.Style.Color
		} else {
			textCol = theme.Readable(textCol, fill)
		}
		sw := strokeW
		if n.Style.StrokeWidth > 0 {
			sw = n.Style.StrokeWidth
		}
		drawShape(sc, n.Shape, c, geo[gi].w, geo[gi].h, fill, stroke, sw, n.Style.Dashed)
		tc := textCenter(n.Shape, c, geo[gi].w, geo[gi].h)
		sc.Label(tc.X, tc.Y, geo[gi].lines, scene.Center, textCol)
	}

	// Edge labels on top.
	for _, l := range labels {
		r := scene.Rect{X: l.at.X - float64(l.w/2), Y: l.at.Y - float64(l.h/2), W: l.w, H: l.h}
		sc.Fill(scene.RoundRect(r, 3), t.LabelBg, color.NRGBA{}, 0)
		sc.Label(l.at.X, l.at.Y, l.lines, scene.Center, t.Text)
	}
	return diagram.Finish(sc, env, width, height), nil
}

// ownEdge returns a polyline between node center c and the border of the
// subgraph r that contains it, following the flow direction. fromBorder
// reverses it (subgraph → node).
func ownEdge(c scene.Point, r scene.Rect, dir sugiyama.Dir, fromBorder bool) []scene.Point {
	var b scene.Point
	switch dir {
	case sugiyama.TB:
		b = scene.Point{X: c.X, Y: r.Y + r.H}
	case sugiyama.BT:
		b = scene.Point{X: c.X, Y: r.Y}
	case sugiyama.LR:
		b = scene.Point{X: r.X + r.W, Y: c.Y}
	default:
		b = scene.Point{X: r.X, Y: c.Y}
	}
	if fromBorder {
		// Enter from the opposite side.
		b = scene.Point{X: float64(2*c.X) - b.X, Y: float64(2*c.Y) - b.Y}
		b.X = min(max(b.X, r.X), r.X+r.W)
		b.Y = min(max(b.Y, r.Y), r.Y+r.H)
		return []scene.Point{b, c}
	}
	return []scene.Point{c, b}
}

func clusterOrder(g *Graph) []int {
	depth := make([]int, len(g.Subgraphs))
	for i := range g.Subgraphs {
		for k, d := g.Subgraphs[i].Parent, 0; k >= 0 && d <= maxSubgraphDepth; k, d = g.Subgraphs[k].Parent, d+1 {
			depth[i]++
		}
	}
	var out []int
	for d := 0; d <= maxSubgraphDepth; d++ {
		for i := range g.Subgraphs {
			if depth[i] == d {
				out = append(out, i)
			}
		}
	}
	return out
}

func drawSelfLoop(sc *scene.Scene, c scene.Point, n sugiyama.Node, e Edge, col, bg color.NRGBA, w float64, dash []float64) scene.Point {
	right := c.X + float64(n.W/2)
	y0, y1 := c.Y-float64(n.H/4), c.Y+float64(n.H/4)
	ext := right + selfLoopSize
	p := scene.NewPath().MoveTo(right, y0).CubeTo(ext, y0-selfLoopSize/2, ext, y1+selfLoopSize/2, right+markerSize*0.8, y1)
	sc.Line(p, col, w, dash)
	if e.End != scene.NoMarker {
		sc.DrawMarker(e.End, scene.Point{X: right, Y: y1}, scene.Point{X: right + markerSize, Y: y1 + 2}, markerSize, col, bg, edgeW)
	}
	return scene.Point{X: ext, Y: c.Y}
}

// linkedOutside reports whether an edge connects a node inside subgraph c
// (at any depth) with one outside. Mermaid then ignores the subgraph's own
// direction, and so does this layout. Edges to the subgraph itself
// ("A --> TOP") do not count.
func (g *Graph) linkedOutside(c int) bool {
	within := func(k int) bool {
		for depth := 0; k >= 0 && depth <= maxSubgraphDepth; k, depth = g.Subgraphs[k].Parent, depth+1 {
			if k == c {
				return true
			}
		}
		return false
	}
	inside := func(n int) bool {
		if k := g.ClusterRef[n]; k >= 0 {
			return within(k)
		}
		return within(g.Nodes[n].Cluster)
	}
	for _, e := range g.Edges {
		if g.ClusterRef[e.From] == c || g.ClusterRef[e.To] == c {
			continue
		}
		if inside(e.From) != inside(e.To) {
			return true
		}
	}
	return false
}
