package sugiyama

import (
	"cmp"
	"slices"

	"github.com/lea/mermaid/internal/scene"
)

// maxClusterDepth bounds cluster nesting.
const maxClusterDepth = 64

func clusterPaths(nodes []Node, clusters []Cluster) [][]int {
	out := make([][]int, len(nodes))
	for i, nd := range nodes {
		out[i] = clusterChain(nd.Cluster, clusters)
	}
	return out
}

func clusterChain(c int, clusters []Cluster) []int {
	var p []int
	for c >= 0 && c < len(clusters) && len(p) < maxClusterDepth {
		p = append(p, c)
		c = clusters[c].Parent
	}
	slices.Reverse(p)
	return p
}

func commonPath(a, b []int) []int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return a[:n:n]
}

func cloneLayers(l [][]int) [][]int {
	out := make([][]int, len(l))
	for i := range l {
		out[i] = slices.Clone(l[i])
	}
	return out
}

// groupSort orders a layer by key while keeping members of each cluster
// contiguous. Ties keep the current order.
func groupSort(vs []vertex, l []int, key func(int) float64, _ bool) {
	copy(l, sortLevel(vs, l, 0, key))
}

func sortLevel(vs []vertex, members []int, depth int, key func(int) float64) []int {
	type item struct {
		key   float64
		first int
		verts []int
		sum   float64
		group bool
	}
	var items []*item
	groups := map[int]*item{}
	for _, v := range members {
		if len(vs[v].path) > depth {
			c := vs[v].path[depth]
			it, ok := groups[c]
			if !ok {
				it = &item{first: vs[v].pos, group: true}
				groups[c] = it
				items = append(items, it)
			}
			it.verts = append(it.verts, v)
			it.sum += key(v)
			continue
		}
		items = append(items, &item{key: key(v), first: vs[v].pos, verts: []int{v}})
	}
	for _, it := range items {
		if it.group {
			it.key = float64(it.sum / float64(len(it.verts)))
			it.verts = sortLevel(vs, it.verts, depth+1, key)
		}
	}
	slices.SortStableFunc(items, func(a, b *item) int {
		if c := cmp.Compare(a.key, b.key); c != 0 {
			return c
		}
		return cmp.Compare(a.first, b.first)
	})
	out := make([]int, 0, len(members))
	for _, it := range items {
		out = append(out, it.verts...)
	}
	return out
}

// reorder sorts layer l by the barycenter of its neighbours in the previous
// (down) or next (!down) layer.
func reorder(vs []vertex, l []int, down bool) {
	key := func(v int) float64 {
		nb := vs[v].out
		if down {
			nb = vs[v].in
		}
		if len(nb) == 0 {
			return float64(vs[v].pos)
		}
		s := 0.0
		for _, u := range nb {
			s += float64(vs[u].pos)
		}
		return float64(s / float64(len(nb)))
	}
	groupSort(vs, l, key, false)
	for i, v := range l {
		vs[v].pos = i
	}
}

// crossings counts edge crossings between adjacent layers.
func crossings(vs []vertex, layers [][]int) int {
	total := 0
	for r := 0; r+1 < len(layers); r++ {
		type pair struct{ a, b int }
		var es []pair
		for _, u := range layers[r] {
			for _, v := range vs[u].out {
				es = append(es, pair{vs[u].pos, vs[v].pos})
			}
		}
		slices.SortFunc(es, func(x, y pair) int {
			if c := cmp.Compare(x.a, y.a); c != 0 {
				return c
			}
			return cmp.Compare(x.b, y.b)
		})
		// Count pairs i<j with b_i > b_j using a Fenwick tree.
		size := len(layers[r+1]) + 1
		tree := make([]int, size+1)
		seen := 0
		for _, e := range es {
			// number of earlier b values > e.b = seen - prefix(e.b)
			s := 0
			for i := e.b + 1; i > 0; i -= i & -i {
				s += tree[i]
			}
			total += seen - s
			for i := e.b + 1; i <= size; i += i & -i {
				tree[i]++
			}
			seen++
		}
	}
	return total
}

// place moves the vertices of a layer toward their neighbours' mean x while
// keeping the minimum separation, by isotonic regression (pool adjacent
// violators). mode 0 uses predecessors, 1 successors, 2 both.
func place(vs []vertex, l []int, mode int, sep func(a, b int) float64) {
	if len(l) == 0 {
		return
	}
	n := len(l)
	z := make([]float64, n)
	w := make([]float64, n)
	off := make([]float64, n)
	for i, v := range l {
		if i > 0 {
			off[i] = off[i-1] + sep(l[i-1], v)
		}
		var nb []int
		switch mode {
		case 0:
			nb = vs[v].in
		case 1:
			nb = vs[v].out
		default:
			nb = append(append([]int(nil), vs[v].in...), vs[v].out...)
		}
		d, wt := vs[v].x, 0.25
		if len(nb) > 0 {
			s := 0.0
			for _, u := range nb {
				s += vs[u].x
			}
			d = float64(s / float64(len(nb)))
			wt = float64(len(nb))
			if vs[v].dummy {
				wt *= dummyWeight
			}
		}
		z[i] = d - off[i]
		w[i] = wt
	}
	type block struct {
		sw, swz float64
		n       int
	}
	var bs []block
	for i := 0; i < n; i++ {
		bs = append(bs, block{w[i], float64(w[i] * z[i]), 1})
		for len(bs) > 1 {
			a, b := bs[len(bs)-2], bs[len(bs)-1]
			if float64(a.swz/a.sw) <= float64(b.swz/b.sw) {
				break
			}
			bs = bs[:len(bs)-2]
			bs = append(bs, block{a.sw + b.sw, a.swz + b.swz, a.n + b.n})
		}
	}
	i := 0
	for _, b := range bs {
		y := float64(b.swz / b.sw)
		for k := 0; k < b.n; k++ {
			vs[l[i]].x = y + off[i]
			i++
		}
	}
}

// clusterBoxes computes cluster rectangles from member node boxes, innermost
// clusters first.
func clusterBoxes(nodes []Node, clusters []Cluster, centers []scene.Point, cfg Config) []scene.Rect {
	boxes := make([]scene.Rect, len(clusters))
	has := make([]bool, len(clusters))
	depth := make([]int, len(clusters))
	idx := make([]int, len(clusters))
	for i := range clusters {
		depth[i] = len(clusterChain(i, clusters))
		idx[i] = i
	}
	slices.SortStableFunc(idx, func(a, b int) int { return depth[b] - depth[a] })
	add := func(c int, r scene.Rect) {
		if has[c] {
			boxes[c] = boxes[c].Union(r)
		} else {
			boxes[c], has[c] = r, true
		}
	}
	for i, nd := range nodes {
		if nd.Cluster >= 0 && nd.Cluster < len(clusters) {
			add(nd.Cluster, scene.Rect{X: centers[i].X - float64(nd.W/2), Y: centers[i].Y - float64(nd.H/2), W: nd.W, H: nd.H})
		}
	}
	for _, c := range idx {
		if !has[c] {
			continue
		}
		r := boxes[c].Inset(cfg.ClusterPad)
		r.Y -= clusters[c].TitleH
		r.H += clusters[c].TitleH
		if minW := clusters[c].TitleW + float64(2*cfg.ClusterPad); r.W < minW {
			r.X -= float64((minW - r.W) / 2)
			r.W = minW
		}
		boxes[c] = r
		if p := clusters[c].Parent; p >= 0 {
			add(p, r)
		}
	}
	return boxes
}
