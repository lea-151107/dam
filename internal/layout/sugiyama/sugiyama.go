// Package sugiyama implements layered graph layout: cycle removal, ranking,
// crossing reduction and coordinate assignment, with support for nested
// clusters and edge labels.
package sugiyama

import (
	"math"
	"slices"

	"github.com/lea-151107/dam/internal/guard"
	"github.com/lea-151107/dam/internal/scene"
)

// Dir is the rank direction.
type Dir uint8

// Directions.
const (
	TB Dir = iota
	BT
	LR
	RL
)

// Horizontal reports whether ranks advance along the x axis.
func (d Dir) Horizontal() bool { return d == LR || d == RL }

// Node is a graph vertex with its real size.
type Node struct {
	W, H    float64
	Cluster int // index into clusters, or -1
}

// Edge connects two nodes. Self loops are ignored by the layout.
type Edge struct {
	From, To       int
	MinLen         int // >= 1
	LabelW, LabelH float64
	HasLabel       bool
}

// Cluster is a group of nodes drawn inside a box.
type Cluster struct {
	Parent int // -1 for top level
	TitleH float64
	TitleW float64
	// Dir, when HasDir is set, is the direction of the cluster's own
	// contents instead of Config.Dir.
	Dir    Dir
	HasDir bool
}

// Config controls spacing (layout units).
type Config struct {
	Dir        Dir
	NodeSep    float64
	RankSep    float64
	ClusterPad float64
}

// Result is the computed layout. Coordinates start at (0,0).
type Result struct {
	Nodes    []scene.Point   // node centers
	Edges    [][]scene.Point // From→To polyline through centers; nil for self loops
	Labels   []scene.Point   // label centers (valid when Edge.HasLabel)
	Clusters []scene.Rect
	Width    float64
	Height   float64
}

const (
	maxSweeps     = 24
	maxPositionIt = 10
	maxVertices   = 20000
	dummyWeight   = 4.0
)

type vertex struct {
	w, h    float64 // extent along the layer, thickness along the rank axis
	rank    int
	pos     int
	x       float64
	dummy   bool
	path    []int // cluster chain from top level to innermost
	in, out []int // neighbour vertices (in the DAG)
}

type chain struct {
	verts    []int
	reversed bool
	label    int // vertex holding the label, or -1
}

// layoutFlat lays out a graph without clusters.
func layoutFlat(g *guard.Guard, nodes []Node, edges []Edge, cfg Config) (*Result, error) {
	var clusters []Cluster
	n := len(nodes)
	paths := clusterPaths(nodes, clusters)
	vs := make([]vertex, n, n+len(edges))
	for i, nd := range nodes {
		w, h := nd.W, nd.H
		if cfg.Dir.Horizontal() {
			w, h = h, w
		}
		vs[i] = vertex{w: w, h: h, path: paths[i]}
	}

	anyLabel := false
	for _, e := range edges {
		if e.HasLabel && e.From != e.To {
			anyLabel = true
		}
	}
	// Labels occupy a rank of their own between nodes, so ranks are doubled
	// when any edge has a label.
	mult := 1
	if anyLabel {
		mult = 2
	}

	// 1. Cycle removal.
	rev, err := backEdges(g, n, edges)
	if err != nil {
		return nil, err
	}
	type dagEdge struct{ u, v, minlen int }
	dag := make([]dagEdge, len(edges))
	for i, e := range edges {
		u, v := e.From, e.To
		if rev[i] {
			u, v = v, u
		}
		ml := max(e.MinLen, 1) * mult
		dag[i] = dagEdge{u, v, ml}
	}

	// 2. Ranking: longest path, then pull sources down toward successors.
	succ := make([][]int, n)
	pred := make([][]int, n)
	for i, e := range dag {
		if e.u == e.v {
			continue
		}
		succ[e.u] = append(succ[e.u], i)
		pred[e.v] = append(pred[e.v], i)
	}
	order, err := topo(g, n, succ, func(ei int) int { return dag[ei].v })
	if err != nil {
		return nil, err
	}
	rank := make([]int, n)
	for _, u := range order {
		for _, ei := range pred[u] {
			rank[u] = max(rank[u], rank[dag[ei].u]+dag[ei].minlen)
		}
	}
	for i := len(order) - 1; i >= 0; i-- {
		u := order[i]
		if len(pred[u]) > 0 || len(succ[u]) == 0 {
			continue
		}
		best := math.MaxInt
		for _, ei := range succ[u] {
			best = min(best, rank[dag[ei].v]-dag[ei].minlen)
		}
		rank[u] = max(rank[u], best)
	}
	for i := range vs {
		vs[i].rank = rank[i]
	}

	// 3. Dummy vertices for long edges.
	chains := make([]chain, len(edges))
	for i, e := range dag {
		ch := chain{reversed: rev[i], label: -1}
		if e.u == e.v {
			chains[i] = ch
			continue
		}
		ch.verts = append(ch.verts, e.u)
		ru, rv := rank[e.u], rank[e.v]
		lcp := commonPath(vs[e.u].path, vs[e.v].path)
		mid := ru + (rv-ru)/2
		for r := ru + 1; r < rv; r++ {
			if err := guard.Limit("layout vertices", len(vs), maxVertices); err != nil {
				return nil, err
			}
			d := vertex{rank: r, dummy: true, path: lcp}
			if edges[i].HasLabel && r == mid {
				d.w, d.h = edges[i].LabelW, edges[i].LabelH
				if cfg.Dir.Horizontal() {
					d.w, d.h = d.h, d.w
				}
				ch.label = len(vs)
			}
			vs = append(vs, d)
			ch.verts = append(ch.verts, len(vs)-1)
		}
		ch.verts = append(ch.verts, e.v)
		for k := 0; k+1 < len(ch.verts); k++ {
			a, b := ch.verts[k], ch.verts[k+1]
			vs[a].out = append(vs[a].out, b)
			vs[b].in = append(vs[b].in, a)
		}
		chains[i] = ch
	}

	// Normalize ranks to start at 0 and group into layers.
	minRank := math.MaxInt
	maxRank := 0
	for _, v := range vs {
		minRank = min(minRank, v.rank)
	}
	if len(vs) == 0 {
		minRank = 0
	}
	for i := range vs {
		vs[i].rank -= minRank
		maxRank = max(maxRank, vs[i].rank)
	}
	layers := make([][]int, maxRank+1)

	// 4. Initial order: DFS from real nodes in input order.
	seen := make([]bool, len(vs))
	var visit func(v int)
	stack := []int{}
	visit = func(v int) {
		stack = append(stack[:0], v)
		for len(stack) > 0 {
			u := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if seen[u] {
				continue
			}
			seen[u] = true
			layers[vs[u].rank] = append(layers[vs[u].rank], u)
			for k := len(vs[u].out) - 1; k >= 0; k-- {
				if !seen[vs[u].out[k]] {
					stack = append(stack, vs[u].out[k])
				}
			}
		}
	}
	byRank := make([]int, n)
	for i := range byRank {
		byRank[i] = i
	}
	slices.SortStableFunc(byRank, func(a, b int) int { return vs[a].rank - vs[b].rank })
	for _, v := range byRank {
		if !seen[v] {
			visit(v)
		}
	}
	for v := range vs {
		if !seen[v] {
			visit(v)
		}
	}
	for _, l := range layers {
		for i, v := range l {
			vs[v].pos = i
		}
		groupSort(vs, l, func(v int) float64 { return float64(vs[v].pos) }, true)
		for i, v := range l {
			vs[v].pos = i
		}
	}

	// 5. Crossing reduction.
	best := cloneLayers(layers)
	bestC := crossings(vs, layers)
	for it := 0; it < maxSweeps && bestC > 0; it++ {
		if err := g.Err(); err != nil {
			return nil, err
		}
		down := it%2 == 0
		if down {
			for r := 1; r < len(layers); r++ {
				reorder(vs, layers[r], true)
			}
		} else {
			for r := len(layers) - 2; r >= 0; r-- {
				reorder(vs, layers[r], false)
			}
		}
		if c := crossings(vs, layers); c < bestC {
			bestC = c
			best = cloneLayers(layers)
		}
	}
	layers = best
	for _, l := range layers {
		for i, v := range l {
			vs[v].pos = i
		}
	}

	// 6. Coordinates within layers.
	sep := func(a, b int) float64 {
		s := float64((vs[a].w + vs[b].w) / 2)
		gap := cfg.NodeSep
		if vs[a].dummy && vs[b].dummy {
			gap /= 2
		}
		s += gap
		c := len(commonPath(vs[a].path, vs[b].path))
		boundaries := (len(vs[a].path) - c) + (len(vs[b].path) - c)
		s += float64(float64(boundaries) * cfg.ClusterPad)
		return s
	}
	for _, l := range layers {
		x := 0.0
		for i, v := range l {
			if i > 0 {
				x += sep(l[i-1], v)
			}
			vs[v].x = x
		}
		for _, v := range l {
			vs[v].x -= float64(x / 2)
		}
	}
	for it := 0; it < maxPositionIt; it++ {
		if err := g.Err(); err != nil {
			return nil, err
		}
		mode := it % 3 // 0: from above, 1: from below, 2: both
		for r := range layers {
			if mode == 1 {
				r = len(layers) - 1 - r
			}
			place(vs, layers[r], mode, sep)
		}
	}

	// 7. Rank coordinates.
	thick := make([]float64, len(layers))
	for _, v := range vs {
		thick[v.rank] = max(thick[v.rank], v.h)
	}
	rankSep := cfg.RankSep
	if len(clusters) > 0 {
		title := 0.0
		for _, c := range clusters {
			title = max(title, c.TitleH)
		}
		rankSep += cfg.ClusterPad + title
	}
	if mult == 2 {
		rankSep /= 2
	}
	rankY := make([]float64, len(layers))
	y := 0.0
	for r := range layers {
		if r > 0 {
			y += float64(thick[r-1]/2) + rankSep + float64(thick[r]/2)
		} else {
			y = float64(thick[0] / 2)
		}
		rankY[r] = y
	}

	// 8. Transform to the requested direction.
	minX := math.Inf(1)
	for _, v := range vs {
		minX = math.Min(minX, v.x-float64(v.w/2))
	}
	if math.IsInf(minX, 1) {
		minX = 0
	}
	pt := make([]scene.Point, len(vs))
	for i, v := range vs {
		x, y := v.x-minX, rankY[v.rank]
		if cfg.Dir == BT || cfg.Dir == RL {
			y = -y
		}
		if cfg.Dir.Horizontal() {
			x, y = y, x
		}
		pt[i] = scene.Point{X: x, Y: y}
	}
	res := &Result{
		Nodes:  pt[:n],
		Edges:  make([][]scene.Point, len(edges)),
		Labels: make([]scene.Point, len(edges)),
	}
	for i, ch := range chains {
		if len(ch.verts) == 0 {
			continue
		}
		pts := make([]scene.Point, len(ch.verts))
		for k, v := range ch.verts {
			pts[k] = pt[v]
		}
		if ch.reversed {
			slices.Reverse(pts)
		}
		res.Edges[i] = pts
		if ch.label >= 0 {
			res.Labels[i] = pt[ch.label]
		} else {
			res.Labels[i] = scene.MidPoint(pts)
		}
	}
	res.Clusters = clusterBoxes(nodes, clusters, res.Nodes, cfg)

	var bb scene.Rect
	first := true
	addR := func(r scene.Rect) {
		if first {
			bb, first = r, false
		} else {
			bb = bb.Union(r)
		}
	}
	for i, nd := range nodes {
		addR(scene.Rect{X: res.Nodes[i].X - float64(nd.W/2), Y: res.Nodes[i].Y - float64(nd.H/2), W: nd.W, H: nd.H})
	}
	for i, e := range edges {
		if res.Edges[i] == nil {
			continue
		}
		for _, p := range res.Edges[i] {
			addR(scene.Rect{X: p.X, Y: p.Y})
		}
		if e.HasLabel {
			p := res.Labels[i]
			addR(scene.Rect{X: p.X - float64(e.LabelW/2), Y: p.Y - float64(e.LabelH/2), W: e.LabelW, H: e.LabelH})
		}
	}
	for _, r := range res.Clusters {
		if r.W > 0 {
			addR(r)
		}
	}
	if first {
		return res, nil
	}
	o := scene.Point{X: bb.X, Y: bb.Y}
	for i := range res.Nodes {
		res.Nodes[i] = res.Nodes[i].Sub(o)
	}
	for i := range res.Edges {
		for k := range res.Edges[i] {
			res.Edges[i][k] = res.Edges[i][k].Sub(o)
		}
		res.Labels[i] = res.Labels[i].Sub(o)
	}
	for i := range res.Clusters {
		res.Clusters[i].X -= o.X
		res.Clusters[i].Y -= o.Y
	}
	res.Width, res.Height = bb.W, bb.H
	return res, nil
}

// backEdges marks edges that close a cycle in DFS order.
func backEdges(g *guard.Guard, n int, edges []Edge) ([]bool, error) {
	adj := make([][]int, n)
	for i, e := range edges {
		if e.From != e.To {
			adj[e.From] = append(adj[e.From], i)
		}
	}
	rev := make([]bool, len(edges))
	state := make([]uint8, n) // 0 new, 1 on stack, 2 done
	type frame struct{ v, k int }
	for s := 0; s < n; s++ {
		if state[s] != 0 {
			continue
		}
		st := []frame{{s, 0}}
		state[s] = 1
		for len(st) > 0 {
			if err := g.Tick(); err != nil {
				return nil, err
			}
			f := &st[len(st)-1]
			if f.k == len(adj[f.v]) {
				state[f.v] = 2
				st = st[:len(st)-1]
				continue
			}
			ei := adj[f.v][f.k]
			f.k++
			w := edges[ei].To
			switch state[w] {
			case 0:
				state[w] = 1
				st = append(st, frame{w, 0})
			case 1:
				rev[ei] = true
			}
		}
	}
	return rev, nil
}

// topo returns a deterministic topological order (Kahn, smallest index
// first). succ lists outgoing edge indices; head maps an edge to its target.
func topo(g *guard.Guard, n int, succ [][]int, head func(int) int) ([]int, error) {
	indeg := make([]int, n)
	for u := range succ {
		for _, ei := range succ[u] {
			indeg[head(ei)]++
		}
	}
	var q []int
	for v := 0; v < n; v++ {
		if indeg[v] == 0 {
			q = append(q, v)
		}
	}
	order := make([]int, 0, n)
	for len(q) > 0 {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		u := q[0]
		q = q[1:]
		order = append(order, u)
		for _, ei := range succ[u] {
			v := head(ei)
			indeg[v]--
			if indeg[v] == 0 {
				q = append(q, v)
			}
		}
	}
	return order, nil
}
