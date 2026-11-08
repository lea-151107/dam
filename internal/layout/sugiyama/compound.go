package sugiyama

import (
	"github.com/lea-151107/dam/internal/guard"
	"github.com/lea-151107/dam/internal/scene"
)

// Layout computes a layout. Clusters are laid out recursively: each cluster
// is first arranged on its own and then placed as a single large node in its
// parent, so cluster boxes never overlap other nodes.
func Layout(g *guard.Guard, nodes []Node, edges []Edge, clusters []Cluster, cfg Config) (*Result, error) {
	c := &compound{g: g, nodes: nodes, edges: edges, clusters: clusters, cfg: cfg}
	return c.run()
}

type compound struct {
	g        *guard.Guard
	nodes    []Node
	edges    []Edge
	clusters []Cluster
	cfg      Config

	chains [][]int // cluster chain per node
	lca    []int   // per edge: level (cluster index or -1)
	rep    [][2]item
	levels map[int]*level
	res    *Result
}

// item is a node or a cluster at some level.
type item struct {
	node    int // >= 0 for nodes
	cluster int // >= 0 for clusters
}

type level struct {
	items   []item
	index   map[item]int
	res     *Result
	edgeIdx map[int]int // original edge → edge in res
	sizes   []scene.Point
	w, h    float64
}

func (c *compound) run() (*Result, error) {
	n := len(c.nodes)
	c.chains = make([][]int, n)
	for i, nd := range c.nodes {
		c.chains[i] = clusterChain(nd.Cluster, c.clusters)
	}
	c.lca = make([]int, len(c.edges))
	c.rep = make([][2]item, len(c.edges))
	for i, e := range c.edges {
		a, b := c.chains[e.From], c.chains[e.To]
		common := commonPath(a, b)
		c.lca[i] = -1
		if len(common) > 0 {
			c.lca[i] = common[len(common)-1]
		}
		c.rep[i] = [2]item{c.repAt(e.From, len(common)), c.repAt(e.To, len(common))}
	}
	c.levels = map[int]*level{}
	if _, err := c.lay(-1, 0); err != nil {
		return nil, err
	}
	root := c.levels[-1]
	c.res = &Result{
		Nodes:    make([]scene.Point, n),
		Edges:    make([][]scene.Point, len(c.edges)),
		Labels:   make([]scene.Point, len(c.edges)),
		Clusters: make([]scene.Rect, len(c.clusters)),
		Width:    root.w,
		Height:   root.h,
	}
	c.place(-1, scene.Point{})
	c.routeEdges()
	return c.res, nil
}

// repAt returns the element that represents node v at the level whose depth
// is d (number of enclosing clusters).
func (c *compound) repAt(v, d int) item {
	if len(c.chains[v]) > d {
		return item{node: -1, cluster: c.chains[v][d]}
	}
	return item{node: v, cluster: -1}
}

// lay lays out the contents of cluster cl (or the root for -1) and returns
// its outer size.
func (c *compound) lay(cl, depth int) (scene.Point, error) {
	if depth > maxClusterDepth {
		return scene.Point{}, guard.Limit("cluster depth", depth, maxClusterDepth)
	}
	if err := c.g.Err(); err != nil {
		return scene.Point{}, err
	}
	lv := &level{index: map[item]int{}, edgeIdx: map[int]int{}}
	var fnodes []Node
	add := func(it item, w, h float64) {
		lv.index[it] = len(lv.items)
		lv.items = append(lv.items, it)
		fnodes = append(fnodes, Node{W: w, H: h, Cluster: -1})
		lv.sizes = append(lv.sizes, scene.Point{X: w, Y: h})
	}
	for i, nd := range c.nodes {
		if nd.Cluster == cl || (cl == -1 && len(c.chains[i]) == 0) {
			add(item{node: i, cluster: -1}, nd.W, nd.H)
		}
	}
	for k, sub := range c.clusters {
		if sub.Parent != cl || k == cl {
			continue
		}
		sz, err := c.lay(k, depth+1)
		if err != nil {
			return scene.Point{}, err
		}
		add(item{node: -1, cluster: k}, sz.X, sz.Y)
	}
	var fedges []Edge
	for i, e := range c.edges {
		if c.lca[i] != cl || e.From == e.To {
			continue
		}
		a, b := lv.index[c.rep[i][0]], lv.index[c.rep[i][1]]
		if a == b {
			continue
		}
		fe := e
		fe.From, fe.To = a, b
		lv.edgeIdx[i] = len(fedges)
		fedges = append(fedges, fe)
	}
	cfg := c.cfg
	if cl >= 0 && c.clusters[cl].HasDir {
		cfg.Dir = c.clusters[cl].Dir
	}
	res, err := layoutFlat(c.g, fnodes, fedges, cfg)
	if err != nil {
		return scene.Point{}, err
	}
	lv.res = res
	lv.w, lv.h = res.Width, res.Height
	c.levels[cl] = lv
	if cl < 0 {
		return scene.Point{X: lv.w, Y: lv.h}, nil
	}
	pad := c.cfg.ClusterPad
	info := c.clusters[cl]
	w := max(lv.w+float64(2*pad), info.TitleW+float64(2*pad))
	h := lv.h + float64(2*pad) + info.TitleH
	if len(lv.items) == 0 {
		h = info.TitleH + float64(2*pad)
	}
	return scene.Point{X: w, Y: h}, nil
}

// place converts level-relative positions to absolute ones. origin is the
// top-left corner of the level's content area.
func (c *compound) place(cl int, origin scene.Point) {
	lv := c.levels[cl]
	for idx, it := range lv.items {
		p := lv.res.Nodes[idx].Add(origin)
		if it.node >= 0 {
			c.res.Nodes[it.node] = p
			continue
		}
		sz := lv.sizes[idx]
		box := scene.Rect{X: p.X - float64(sz.X/2), Y: p.Y - float64(sz.Y/2), W: sz.X, H: sz.Y}
		c.res.Clusters[it.cluster] = box
		inner := c.levels[it.cluster]
		pad := c.cfg.ClusterPad
		o := scene.Point{
			X: box.X + float64((box.W-inner.w)/2),
			Y: box.Y + pad + c.clusters[it.cluster].TitleH,
		}
		c.place(it.cluster, o)
	}
	for orig, fe := range lv.edgeIdx {
		pts := make([]scene.Point, len(lv.res.Edges[fe]))
		for k, q := range lv.res.Edges[fe] {
			pts[k] = q.Add(origin)
		}
		c.res.Edges[orig] = pts
		c.res.Labels[orig] = lv.res.Labels[fe].Add(origin)
	}
}

// routeEdges connects level polylines to the real endpoints inside
// clusters.
func (c *compound) routeEdges() {
	for i, e := range c.edges {
		pts := c.res.Edges[i]
		if pts == nil {
			continue
		}
		if r := c.rep[i][0]; r.cluster >= 0 {
			pts = append([]scene.Point{c.res.Nodes[e.From]}, scene.ClipStartRect(pts, c.res.Clusters[r.cluster])...)
		}
		if r := c.rep[i][1]; r.cluster >= 0 {
			pts = append(scene.ClipEndRect(pts, c.res.Clusters[r.cluster]), c.res.Nodes[e.To])
		}
		c.res.Edges[i] = pts
	}
}
