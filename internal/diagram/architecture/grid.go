package architecture

import (
	"image/color"
	"math"

	"github.com/lea-151107/dam/internal/diagram"
	"github.com/lea-151107/dam/internal/scene"
	"github.com/lea-151107/dam/internal/theme"
)

// Visual constants in layout units.
const (
	cellGap    = 70.0 // between neighbouring cells
	padX       = 14.0
	padY       = 10.0
	junctionR  = 5.0
	groupPad   = 18.0
	lineW      = 1.4
	markerSize = 10.0
	radius     = 6.0
)

// sideDir maps a side to the grid step toward a neighbour on that side.
func sideDir(s byte) (dx, dy int) {
	switch s {
	case 'L':
		return -1, 0
	case 'R':
		return 1, 0
	case 'T':
		return 0, -1
	}
	return 0, 1
}

type gpos struct{ x, y int }

// link joins units a and b; aSide is the side of a it leaves from.
type link struct {
	a, b         int
	aSide, bSide byte
}

// placeUnits puts n units on grid cells: a link "a:R -- L:b" puts b in
// the cell right of a. An occupied cell pushes the newcomer further in the
// same direction. Units that are not connected are placed side by side.
func placeUnits(n int, links []link) []gpos {
	pos := make([]gpos, n)
	placed := make([]bool, n)
	taken := map[gpos]bool{}
	put := func(i int, p gpos, dx, dy int) {
		for taken[p] {
			if dx == 0 && dy == 0 {
				dx = 1
			}
			p = gpos{p.x + dx, p.y + dy}
		}
		pos[i], placed[i] = p, true
		taken[p] = true
	}
	nextX := 0
	for start := range n {
		if placed[start] {
			continue
		}
		put(start, gpos{nextX, 0}, 1, 0)
		// Spread along the links until nothing changes; each pass places
		// at least one unit or stops.
		for changed := true; changed; {
			changed = false
			for _, l := range links {
				switch {
				case placed[l.a] && !placed[l.b]:
					dx, dy := sideDir(l.aSide)
					put(l.b, gpos{pos[l.a].x + dx, pos[l.a].y + dy}, dx, dy)
					changed = true
				case placed[l.b] && !placed[l.a]:
					dx, dy := sideDir(l.bSide)
					put(l.a, gpos{pos[l.b].x + dx, pos[l.b].y + dy}, dx, dy)
					changed = true
				}
			}
		}
		for i := range n {
			if placed[i] {
				nextX = max(nextX, pos[i].x+1)
			}
		}
	}
	return pos
}

// place puts the services of a diagram without groups on the grid.
func (d *Diagram) place() []gpos {
	links := make([]link, len(d.Edges))
	for i, e := range d.Edges {
		links[i] = link{e.From, e.To, e.FromSide, e.ToSide}
	}
	return placeUnits(len(d.Nodes), links)
}

// unit is a direct child of a group (or of the diagram): a service or a
// nested group.
type unit struct{ service, group int }

// children lists the direct children of group g (-1: the diagram), groups
// first, each in declaration order.
func (d *Diagram) children(g int) []unit {
	var out []unit
	for k, grp := range d.Groups {
		if grp.Parent == g {
			out = append(out, unit{-1, k})
		}
	}
	for i, n := range d.Nodes {
		if n.Group == g {
			out = append(out, unit{i, -1})
		}
	}
	return out
}

// within returns the index in kids of the child of group g that contains
// service s, or -1 when s is not inside g.
func (d *Diagram) within(g int, s int, kids []unit) int {
	k := d.Nodes[s].Group
	if k == g {
		for i, u := range kids {
			if u.service == s {
				return i
			}
		}
		return -1
	}
	for depth := 0; k >= 0 && depth <= len(d.Groups); depth++ {
		if d.Groups[k].Parent == g {
			for i, u := range kids {
				if u.group == k {
					return i
				}
			}
		}
		k = d.Groups[k].Parent
	}
	return -1
}

// placed is the layout of a group's contents, relative to its top-left.
type placed struct {
	w, h     float64
	services map[int]scene.Rect
	groups   map[int]scene.Rect
}

// gridLayout draws the diagram: every group is laid out on its own grid
// (its services and inner groups placed by the sides of the edges between
// them) and then placed as one unit in its parent, so that a group's frame
// holds only its own members. Edges leave and enter by the sides they
// name.
func (d *Diagram) gridLayout(env *diagram.Env) (*scene.Scene, error) {
	m := env.M
	t := env.T
	lh := m.LineHeight()
	gap := float64(cellGap * env.Spacing)
	labels := make([][]string, len(d.Nodes))
	size := func(i int) (float64, float64) {
		n := d.Nodes[i]
		if n.Junction {
			return 2 * junctionR, 2 * junctionR
		}
		lines := env.Label(n.Label)
		if n.Icon != "" {
			lines = append([]string{"«" + n.Icon + "»"}, lines...)
		}
		labels[i] = lines
		w, h := m.Size(lines)
		return w + float64(2*padX), h + float64(2*padY)
	}
	// A group's parent is declared before it, so nesting cannot loop and
	// is at most len(d.Groups) deep.
	var lay func(g, depth int) (*placed, error)
	lay = func(g, depth int) (*placed, error) {
		if err := env.G.Tick(); err != nil {
			return nil, err
		}
		kids := d.children(g)
		var links []link
		for _, e := range d.Edges {
			a, b := d.within(g, e.From, kids), d.within(g, e.To, kids)
			if a >= 0 && b >= 0 && a != b {
				links = append(links, link{a, b, e.FromSide, e.ToSide})
			}
		}
		pos := placeUnits(len(kids), links)
		inner := make([]*placed, len(kids))
		ws := make([]float64, len(kids))
		hs := make([]float64, len(kids))
		for i, u := range kids {
			if u.group >= 0 {
				p, err := lay(u.group, depth+1)
				if err != nil {
					return nil, err
				}
				inner[i], ws[i], hs[i] = p, p.w, p.h
			} else {
				ws[i], hs[i] = size(u.service)
			}
		}
		colW, rowH := map[int]float64{}, map[int]float64{}
		minX, minY, maxX, maxY := math.MaxInt, math.MaxInt, math.MinInt, math.MinInt
		for i, p := range pos {
			colW[p.x] = math.Max(colW[p.x], ws[i])
			rowH[p.y] = math.Max(rowH[p.y], hs[i])
			minX, minY, maxX, maxY = min(minX, p.x), min(minY, p.y), max(maxX, p.x), max(maxY, p.y)
		}
		top := 0.0
		pad := 0.0
		if g >= 0 {
			pad, top = groupPad, groupPad+lh
		}
		colX, rowY := map[int]float64{}, map[int]float64{}
		x := pad
		for c := minX; c <= maxX; c++ {
			colX[c] = x
			if _, ok := colW[c]; ok {
				x += colW[c] + gap
			}
		}
		y := top
		for r := minY; r <= maxY; r++ {
			rowY[r] = y
			if _, ok := rowH[r]; ok {
				y += rowH[r] + gap
			}
		}
		out := &placed{w: x - gap + pad, h: y - gap + pad, services: map[int]scene.Rect{}, groups: map[int]scene.Rect{}}
		if len(kids) == 0 {
			out.w, out.h = float64(2*pad)+m.Width(d.Groups[g].Label), top+pad
		}
		for i, u := range kids {
			p := pos[i]
			cx, cy := colX[p.x]+float64(colW[p.x]/2), rowY[p.y]+float64(rowH[p.y]/2)
			o := scene.Point{X: cx - float64(ws[i]/2), Y: cy - float64(hs[i]/2)}
			if u.service >= 0 {
				out.services[u.service] = scene.Rect{X: o.X, Y: o.Y, W: ws[i], H: hs[i]}
				continue
			}
			out.groups[u.group] = scene.Rect{X: o.X, Y: o.Y, W: ws[i], H: hs[i]}
			for s, r := range inner[i].services {
				out.services[s] = scene.Rect{X: r.X + o.X, Y: r.Y + o.Y, W: r.W, H: r.H}
			}
			for k, r := range inner[i].groups {
				out.groups[k] = scene.Rect{X: r.X + o.X, Y: r.Y + o.Y, W: r.W, H: r.H}
			}
		}
		return out, nil
	}
	root, err := lay(-1, 0)
	if err != nil {
		return nil, err
	}
	sc := &scene.Scene{}
	// Group frames, outer before inner.
	depthOf := func(g int) int {
		k := 0
		for p := d.Groups[g].Parent; p >= 0 && k <= len(d.Groups); p = d.Groups[p].Parent {
			k++
		}
		return k
	}
	for depth := 0; depth <= len(d.Groups); depth++ {
		for g := range d.Groups {
			if depthOf(g) != depth {
				continue
			}
			f := root.groups[g]
			sc.Add(&scene.Shape{Path: scene.RoundRect(f, radius), Fill: theme.Mix(t.Base, t.ClusterFill, 0.6),
				Stroke: scene.Stroke{Color: t.ClusterStroke, Width: lineW, Dash: []float64{6, 4}}})
		}
	}
	// Edges: out of the named side, an orthogonal path, into the other.
	side := func(r scene.Rect, s byte) (scene.Point, scene.Point) {
		c := r.Center()
		dx, dy := sideDir(s)
		p := scene.Point{X: c.X + float64(float64(dx)*r.W/2), Y: c.Y + float64(float64(dy)*r.H/2)}
		return p, scene.Point{X: float64(dx), Y: float64(dy)}
	}
	used := newOccupancy()
	rt := &router{env: env}
	obstacles := make([]scene.Rect, 0, len(d.Nodes)+len(d.Groups))
	for i := range d.Nodes {
		obstacles = append(obstacles, root.services[i])
	}
	// Group titles: edges cross group frames but not the title text.
	for g, grp := range d.Groups {
		f := root.groups[g]
		title := grp.Label
		if grp.Icon != "" {
			title = "«" + grp.Icon + "» " + title
		}
		at := scene.Point{X: f.X + float64(groupPad/2), Y: f.Y + float64(groupPad/2) + float64(lh/2)}
		obstacles = append(obstacles, scene.Rect{X: at.X - 2, Y: at.Y - float64(lh/2), W: m.Width(title) + 4, H: lh})
	}
	for _, e := range d.Edges {
		if err := env.G.Tick(); err != nil {
			return nil, err
		}
		a, da := side(root.services[e.From], e.FromSide)
		b, db := side(root.services[e.To], e.ToSide)
		out := float64(gap / 3)
		a1, b1 := a.Add(da.Mul(out)), b.Add(db.Mul(out))
		var pts []scene.Point
		// A path around the services and group titles; an L shape only
		// when every way is blocked.
		path, err := rt.routeOrth(a1, b1, da, db, obstacles, out, used)
		if err != nil {
			return nil, err
		}
		if path != nil {
			pts = straighten(append(append([]scene.Point{a}, path...), b))
			used.add(pts)
		} else {
			corner := scene.Point{X: a1.X, Y: b1.Y}
			if da.Y == 0 {
				corner = scene.Point{X: b1.X, Y: a1.Y}
			}
			pts = scene.Dedupe([]scene.Point{a, a1, corner, b1, b})
		}
		line := pts
		if e.ToArrow {
			line = scene.Shorten(line, scene.MarkerLen(scene.Arrow, markerSize))
		}
		if e.FromArrow {
			line = scene.ShortenStart(line, scene.MarkerLen(scene.Arrow, markerSize))
		}
		sc.Line(scene.Polyline(line), t.Line, lineW, nil)
		if n := len(pts); e.ToArrow && n >= 2 {
			sc.DrawMarker(scene.Arrow, pts[n-1], pts[n-2], markerSize, t.Line, t.Base, lineW)
		}
		if e.FromArrow && len(pts) >= 2 {
			sc.DrawMarker(scene.Arrow, pts[0], pts[1], markerSize, t.Line, t.Base, lineW)
		}
		if e.Label != "" {
			mid := scene.MidPoint(pts)
			lines := env.Label(e.Label)
			tw, th := m.Size(lines)
			sc.Fill(scene.RectPath(scene.Rect{X: mid.X - float64(tw/2) - 3, Y: mid.Y - float64(th/2) - 1, W: tw + 6, H: th + 2}), t.LabelBg, color.NRGBA{}, 0)
			sc.Label(mid.X, mid.Y, lines, scene.Center, t.Text)
		}
	}
	// Group titles on top of the edges, on a background, so that an edge
	// leaving the group does not run through the text.
	for g, grp := range d.Groups {
		f := root.groups[g]
		title := grp.Label
		if grp.Icon != "" {
			title = "«" + grp.Icon + "» " + title
		}
		at := scene.Point{X: f.X + float64(groupPad/2), Y: f.Y + float64(groupPad/2) + float64(lh/2)}
		sc.Fill(scene.RectPath(scene.Rect{X: at.X - 2, Y: at.Y - float64(lh/2), W: m.Width(title) + 4, H: lh}), theme.Mix(t.Base, t.ClusterFill, 0.6), color.NRGBA{}, 0)
		sc.Label(at.X, at.Y, []string{title}, scene.Left, t.Text)
	}
	for i, n := range d.Nodes {
		r := root.services[i]
		if n.Junction {
			sc.Fill(scene.Ellipse(r.Center(), junctionR, junctionR), t.Line, t.Line, 0)
			continue
		}
		sc.Fill(scene.RoundRect(r, radius), t.NodeFill, t.NodeStroke, lineW)
		sc.Label(r.X+float64(r.W/2), r.Y+float64(r.H/2), labels[i], scene.Center, t.Text)
	}
	return diagram.Finish(sc, env, root.w, root.h), nil
}
