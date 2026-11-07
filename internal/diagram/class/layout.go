package class

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
	padX       = 12.0
	padY       = 6.0
	nodeSep    = 50.0
	rankSep    = 60.0
	clusterPad = 16.0
	lineW      = 1.3
	markerSize = 12.0
	cardOffset = 14.0
)

type box struct {
	header, attrs, methods []string
	w, h                   float64
	headH, attrH           float64
}

// Layout implements diagram.Layouter.
func (d *Diagram) Layout(env *diagram.Env) (*scene.Scene, error) {
	m := env.M
	t := env.T
	lh := m.LineHeight()
	boxes := make([]box, len(d.Classes))
	nodes := make([]sugiyama.Node, len(d.Classes))
	for i, c := range d.Classes {
		var b box
		if c.Note {
			b.header = env.Label(c.Label)
			tw, th := m.Size(b.header)
			b.w, b.h = tw+float64(2*padX), th+float64(2*padY)
			b.headH = b.h
		} else {
			for _, a := range c.Annotations {
				b.header = append(b.header, "«"+a+"»")
			}
			b.header = append(b.header, env.Label(c.Label)...)
			for _, a := range c.Attributes {
				b.attrs = append(b.attrs, env.Label(a)...)
			}
			for _, a := range c.Methods {
				b.methods = append(b.methods, env.Label(a)...)
			}
			w1, h1 := m.Size(b.header)
			w2, h2 := m.Size(b.attrs)
			w3, h3 := m.Size(b.methods)
			b.headH = h1 + float64(2*padY)
			b.attrH = math.Max(h2, float64(lh/2)) + 2*padY
			b.w = max(w1, w2, w3) + 2*padX
			b.h = b.headH + b.attrH + math.Max(h3, float64(lh/2)) + 2*padY
		}
		boxes[i] = b
		nodes[i] = sugiyama.Node{W: b.w, H: b.h, Cluster: c.Namespace}
	}
	var clusters []sugiyama.Cluster
	titles := make([][]string, len(d.Namespaces))
	for i, n := range d.Namespaces {
		titles[i] = env.Label(n)
		tw, th := m.Size(titles[i])
		clusters = append(clusters, sugiyama.Cluster{Parent: -1, TitleW: tw, TitleH: th + 4})
	}
	edges := make([]sugiyama.Edge, len(d.Relations))
	labels := make([][]string, len(d.Relations))
	for i, r := range d.Relations {
		e := sugiyama.Edge{From: r.From, To: r.To, MinLen: 1}
		if r.Label != "" {
			labels[i] = env.Label(r.Label)
			tw, th := m.Size(labels[i])
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
	for i, r := range res.Clusters {
		if r.W <= 0 {
			continue
		}
		sc.Fill(scene.RectPath(r), t.ClusterFill, t.ClusterStroke, lineW)
		_, th := m.Size(titles[i])
		sc.Label(r.X+float64(r.W/2), r.Y+float64(clusterPad*sp/2)+float64(th/2)+2, titles[i], scene.Center, t.Text)
	}
	rect := func(i int) scene.Rect {
		c := res.Nodes[i]
		return scene.Rect{X: c.X - float64(boxes[i].w/2), Y: c.Y - float64(boxes[i].h/2), W: boxes[i].w, H: boxes[i].h}
	}
	for i, r := range d.Relations {
		if r.From == r.To {
			// NOTE(mermaid-compat): self relations are drawn as a small loop.
			b := rect(r.From)
			x, y := b.X+b.W, b.Y+float64(b.H/2)
			p := scene.NewPath().MoveTo(x, y-8).CubeTo(x+30, y-24, x+30, y+24, x, y+8)
			sc.Line(p, t.Line, lineW, dashOf(r.Dashed))
			continue
		}
		pts := res.Edges[i]
		if len(pts) < 2 {
			continue
		}
		pts = scene.ClipStartRect(pts, rect(r.From))
		pts = scene.ClipEndRect(pts, rect(r.To))
		line := scene.ShortenStart(scene.Shorten(pts, scene.MarkerLen(r.EndM, markerSize)), scene.MarkerLen(r.StartM, markerSize))
		sc.Line(scene.Smooth(line), t.Line, lineW, dashOf(r.Dashed))
		n := len(pts)
		if r.EndM != scene.NoMarker {
			sc.DrawMarker(r.EndM, pts[n-1], pts[n-2], markerSize, t.Line, t.Base, lineW)
		}
		if r.StartM != scene.NoMarker {
			sc.DrawMarker(r.StartM, pts[0], pts[1], markerSize, t.Line, t.Base, lineW)
		}
		if r.FromCard != "" {
			cardinality(sc, env, pts[0], pts[1], r.FromCard)
		}
		if r.ToCard != "" {
			cardinality(sc, env, pts[n-1], pts[n-2], r.ToCard)
		}
		if len(labels[i]) > 0 {
			p := res.Labels[i]
			tw, th := m.Size(labels[i])
			sc.Fill(scene.RectPath(scene.Rect{X: p.X - float64(tw/2) - 3, Y: p.Y - float64(th/2) - 1, W: tw + 6, H: th + 2}), t.LabelBg, color.NRGBA{}, 0)
			sc.Label(p.X, p.Y, labels[i], scene.Center, t.Text)
		}
	}
	for i, c := range d.Classes {
		b := boxes[i]
		r := rect(i)
		fill, stroke, text := t.NodeFill, t.NodeStroke, t.Text
		if c.Style.Fill != nil {
			fill = *c.Style.Fill
		}
		if c.Style.Stroke != nil {
			stroke = *c.Style.Stroke
		}
		if c.Style.Color != nil {
			text = *c.Style.Color
		} else {
			text = theme.Readable(text, fill)
		}
		if c.Note {
			sc.Fill(scene.RectPath(r), t.NoteFill, t.NoteStroke, lineW)
			sc.Label(r.X+float64(r.W/2), r.Y+float64(r.H/2), b.header, scene.Center, t.Text)
			continue
		}
		sc.Fill(scene.RectPath(r), fill, stroke, lineW)
		y1 := r.Y + b.headH
		y2 := y1 + b.attrH
		sc.Line(scene.Polyline([]scene.Point{{X: r.X, Y: y1}, {X: r.X + r.W, Y: y1}}), stroke, lineW, nil)
		sc.Line(scene.Polyline([]scene.Point{{X: r.X, Y: y2}, {X: r.X + r.W, Y: y2}}), stroke, lineW, nil)
		sc.Label(r.X+float64(r.W/2), r.Y+float64(b.headH/2), b.header, scene.Center, text)
		_, ha := m.Size(b.attrs)
		sc.Label(r.X+padX, y1+padY+float64(ha/2), b.attrs, scene.Left, text)
		_, hm := m.Size(b.methods)
		sc.Label(r.X+padX, y2+padY+float64(hm/2), b.methods, scene.Left, text)
	}
	return diagram.Finish(sc, env, res.Width, res.Height), nil
}

func dashOf(d bool) []float64 {
	if d {
		return []float64{4, 3}
	}
	return nil
}

// cardinality writes a multiplicity label near the end of an edge.
func cardinality(sc *scene.Scene, env *diagram.Env, end, next scene.Point, text string) {
	dir := next.Sub(end).Norm()
	nrm := scene.Point{X: -dir.Y, Y: dir.X}
	p := end.Add(dir.Mul(cardOffset + 4)).Add(nrm.Mul(cardOffset))
	sc.Label(p.X, p.Y, []string{text}, scene.Center, env.T.Text)
}
