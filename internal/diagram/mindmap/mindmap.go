// Package mindmap parses and lays out Mermaid mind maps.
package mindmap

import (
	"image/color"
	"math"
	"strings"

	"github.com/lea-151107/dam/internal/diagram"
	"github.com/lea-151107/dam/internal/guard"
	"github.com/lea-151107/dam/internal/layout/tree"
	"github.com/lea-151107/dam/internal/scene"
	"github.com/lea-151107/dam/internal/source"
	"github.com/lea-151107/dam/internal/theme"
)

// Shape is a node shape.
type Shape uint8

// Shapes.
const (
	Default Shape = iota
	Square
	Rounded
	Circle
	Bang
	Cloud
	Hexagon
)

// Node is a mind map node.
type Node struct {
	Label    string
	Shape    Shape
	Parent   int
	Children []int
	Depth    int
	indent   int
}

// Diagram is a parsed mind map.
type Diagram struct {
	Nodes []*Node
}

// maxDepth bounds tree depth.
const maxDepth = 64

var shapes = []struct {
	open, close string
	shape       Shape
}{
	{"((", "))", Circle},
	{"))", "((", Bang},
	{"{{", "}}", Hexagon},
	{"(", ")", Rounded},
	{")", "(", Cloud},
	{"[", "]", Square},
}

// Parse parses a mind map.
func Parse(g *guard.Guard, lines []source.Line) (*Diagram, error) {
	d := &Diagram{}
	var stack []int
	for _, l := range lines[1:] {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		t := strings.TrimSpace(l.Text)
		if strings.HasPrefix(t, "::icon(") || strings.HasPrefix(t, ":::") {
			// NOTE(mermaid-compat): icons and CSS classes are not drawn.
			continue
		}
		indent := source.Indent(l.Text)
		n := &Node{indent: indent, Parent: -1}
		n.Label, n.Shape = parseNode(t)
		for len(stack) > 0 && d.Nodes[stack[len(stack)-1]].indent >= indent {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 && len(d.Nodes) > 0 {
			return nil, source.Errorf(l.No, "a mind map can only have one root")
		}
		if len(stack) > 0 {
			p := stack[len(stack)-1]
			n.Parent = p
			n.Depth = d.Nodes[p].Depth + 1
			if n.Depth > maxDepth {
				return nil, source.TooLarge(l.No, "mind map is nested too deeply")
			}
			d.Nodes[p].Children = append(d.Nodes[p].Children, len(d.Nodes))
		}
		d.Nodes = append(d.Nodes, n)
		stack = append(stack, len(d.Nodes)-1)
		if err := g.Count("nodes", len(d.Nodes)); err != nil {
			return nil, err
		}
	}
	if len(d.Nodes) == 0 {
		return nil, source.Errorf(lines[0].No, "mind map has no root")
	}
	return d, nil
}

func parseNode(t string) (string, Shape) {
	// Strip a trailing ":::class".
	if i := strings.Index(t, ":::"); i > 0 {
		t = strings.TrimSpace(t[:i])
	}
	for _, s := range shapes {
		i := strings.Index(t, s.open)
		if i < 0 || !strings.HasSuffix(t, s.close) || len(t) < i+len(s.open)+len(s.close) {
			continue
		}
		// The id must not contain bracket characters.
		if strings.ContainsAny(t[:i], "([{)") {
			continue
		}
		return source.Label(t[i+len(s.open) : len(t)-len(s.close)]), s.shape
	}
	return source.Label(t), Default
}

// Visual constants in layout units.
const (
	padX     = 14.0
	padY     = 8.0
	hgap     = 50.0
	vgap     = 14.0
	rootPad  = 10.0
	lineBase = 4.5
)

// Layout implements diagram.Layouter.
func (d *Diagram) Layout(env *diagram.Env) (*scene.Scene, error) {
	m := env.M
	t := env.T
	sp := env.Spacing
	n := len(d.Nodes)
	lines := make([][]string, n)
	tn := make([]tree.Node, n)
	for i, nd := range d.Nodes {
		lines[i] = env.Label(nd.Label)
		tw, th := m.Size(lines[i])
		w, h := tw+float64(2*padX), th+float64(2*padY)
		switch nd.Shape {
		case Circle:
			dd := math.Max(tw, th) + 2*padY
			w, h = dd, dd
		case Bang, Cloud:
			w, h = w+2*padX, h+padY*2
		case Hexagon:
			w += float64(h / 2)
		}
		if i == 0 {
			w, h = w+2*rootPad, h+rootPad
		}
		tn[i] = tree.Node{W: w, H: h, Children: nd.Children}
	}
	// Split root children between right and left by subtree size.
	size := make([]int, n)
	for i := n - 1; i >= 0; i-- {
		size[i]++
		if p := d.Nodes[i].Parent; p >= 0 {
			size[p] += size[i]
		}
	}
	var right, left []int
	total := size[0] - 1
	acc := 0
	for _, c := range d.Nodes[0].Children {
		if acc*2 < total || len(right) == 0 {
			right = append(right, c)
			acc += size[c]
		} else {
			left = append(left, c)
		}
	}
	pos := make([]scene.Point, n)
	hg := float64(hgap * sp)
	root := tn[0]
	rh := tree.Layout(tn, right, hg, float64(vgap*sp), float64(root.W/2)+hg, pos)
	lpos := make([]scene.Point, n)
	lh := tree.Layout(tn, left, hg, float64(vgap*sp), float64(root.W/2)+hg, lpos)
	for i := range pos {
		if inSubtrees(d, i, left) {
			pos[i] = scene.Point{X: -lpos[i].X, Y: lpos[i].Y - float64(lh/2)}
		} else if i != 0 {
			pos[i].Y -= float64(rh / 2)
		}
	}
	pos[0] = scene.Point{}

	branch := make([]int, n)
	for i := 1; i < n; i++ {
		if d.Nodes[i].Parent == 0 {
			branch[i] = i
		} else {
			branch[i] = branch[d.Nodes[i].Parent]
		}
	}
	colorIdx := map[int]int{}
	for k, c := range d.Nodes[0].Children {
		colorIdx[c] = k
	}

	sc := &scene.Scene{}
	for i := 1; i < n; i++ {
		p := d.Nodes[i].Parent
		a, b := pos[p], pos[i]
		dir := 1.0
		if b.X < a.X {
			dir = -1
		}
		start := scene.Point{X: a.X + float64(dir*tn[p].W/2*0.9), Y: a.Y}
		if p == 0 {
			start = a
		}
		end := scene.Point{X: b.X - float64(dir*tn[i].W/2), Y: b.Y}
		mx := float64((start.X + end.X) / 2)
		path := scene.NewPath().MoveTo(start.X, start.Y).CubeTo(mx, start.Y, mx, end.Y, end.X, end.Y)
		w := math.Max(1.2, lineBase-float64(float64(d.Nodes[i].Depth-1)*1.2))
		sc.Line(path, t.Strong(colorIdx[branch[i]]), w, nil)
	}
	for i, nd := range d.Nodes {
		fill := t.Color(colorIdx[branch[i]])
		stroke := t.Strong(colorIdx[branch[i]])
		if i == 0 {
			fill, stroke = theme.Mix(t.Base, t.Accent, 0.55), t.NodeStroke
		}
		drawNode(sc, nd.Shape, pos[i], tn[i].W, tn[i].H, fill, stroke, i == 0)
		sc.Label(pos[i].X, pos[i].Y, lines[i], scene.Center, t.Text)
	}
	return diagram.Finish(sc, env, 0, 0), nil
}

func inSubtrees(d *Diagram, i int, roots []int) bool {
	for k := i; k > 0; k = d.Nodes[k].Parent {
		for _, r := range roots {
			if k == r {
				return true
			}
		}
	}
	return false
}

func drawNode(sc *scene.Scene, s Shape, c scene.Point, w, h float64, fill, stroke color.NRGBA, root bool) {
	r := scene.Rect{X: c.X - float64(w/2), Y: c.Y - float64(h/2), W: w, H: h}
	switch s {
	case Square:
		sc.Fill(scene.RectPath(r), fill, stroke, 1.2)
	case Rounded:
		sc.Fill(scene.RoundRect(r, 8), fill, stroke, 1.2)
	case Circle:
		sc.Fill(scene.Ellipse(c, float64(w/2), float64(w/2)), fill, stroke, 1.2)
	case Hexagon:
		i := float64(h / 4)
		sc.Fill(scene.Polygon([]scene.Point{{X: r.X + i, Y: r.Y}, {X: r.X + w - i, Y: r.Y}, {X: r.X + w, Y: c.Y},
			{X: r.X + w - i, Y: r.Y + h}, {X: r.X + i, Y: r.Y + h}, {X: r.X, Y: c.Y}}), fill, stroke, 1.2)
	case Bang:
		const spikes = 14
		var pts []scene.Point
		for k := 0; k < 2*spikes; k++ {
			a := float64(math.Pi * float64(k) / spikes)
			f := 1.0
			if k%2 == 1 {
				f = 0.8
			}
			pts = append(pts, scene.Point{X: c.X + float64(w/2*f*math.Cos(a)), Y: c.Y + float64(h/2*f*math.Sin(a))})
		}
		sc.Fill(scene.Polygon(pts), fill, stroke, 1.2)
	case Cloud:
		p := scene.NewPath()
		const bumps = 10
		for k := 0; k < bumps; k++ {
			a0 := float64(2 * math.Pi * float64(k) / bumps)
			a1 := float64(2 * math.Pi * float64(k+1) / bumps)
			p0 := scene.Point{X: c.X + float64(w/2*0.85*math.Cos(a0)), Y: c.Y + float64(h/2*0.85*math.Sin(a0))}
			p1 := scene.Point{X: c.X + float64(w/2*0.85*math.Cos(a1)), Y: c.Y + float64(h/2*0.85*math.Sin(a1))}
			am := float64((a0 + a1) / 2)
			ctl := scene.Point{X: c.X + float64(w/2*1.15*math.Cos(am)), Y: c.Y + float64(h/2*1.15*math.Sin(am))}
			if k == 0 {
				p.MoveTo(p0.X, p0.Y)
			}
			p.QuadTo(ctl.X, ctl.Y, p1.X, p1.Y)
		}
		sc.Fill(p.Close(), fill, stroke, 1.2)
	default:
		rad := 6.0
		if root {
			rad = float64(h / 2)
		}
		sc.Fill(scene.RoundRect(r, rad), fill, stroke, 1.2)
	}
}
