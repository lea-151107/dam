package architecture

import (
	"container/heap"
	"errors"
	"math"
	"sort"

	"github.com/lea-151107/dam/internal/diagram"
	"github.com/lea-151107/dam/internal/guard"
	"github.com/lea-151107/dam/internal/scene"
)

// Edge routing: an orthogonal path from a1 (just outside the start side)
// to b1 (just outside the end side) that does not cross any service. The
// search runs on the grid formed by the services' edges, lines a margin
// away from them, and the two end points, with a cost for length and for
// each bend (Dijkstra on (point, direction) states).

// Routing costs, in layout units.
const (
	bendCost = 40.0
	// hugFactor multiplies the length of a segment that runs along a
	// service's border, so that paths keep to the channels between them.
	hugFactor = 8.0
	// maxRouteGrid bounds the grid points of one search; larger diagrams
	// fail with ErrTooLarge (a path through the services would mislead).
	maxRouteGrid = 40000
	// maxRouteWork bounds the search steps of one Render call, over all
	// its edges and the layout attempts that fit MaxWidth: about a second
	// at most. A diagram of 20 services and 60 edges uses about a quarter
	// of it with MaxWidth; much larger ones fail with ErrTooLarge and are
	// better drawn as text.
	maxRouteWork = 8_000_000
)

// router holds the search buffers, reused by the edges of a layout; a
// state's entries are valid only when its stamp matches the current
// search.
type router struct {
	env   *diagram.Env
	dist  []float64
	prev  []int32
	stamp []uint32
	gen   uint32
	steps int // not yet passed to env.Spend
}

// spend counts n steps against the Render call's budget, in batches.
func (r *router) spend(n int) error {
	r.steps += n
	if r.steps < 1024 {
		return nil
	}
	n, r.steps = r.steps, 0
	return r.env.Spend(n, maxRouteWork, "architecture routing steps")
}

// routeOrth returns the corner points of a path from a1 to b1 (both
// included) that leaves a1 along da and enters b1 against db, avoiding the
// obstacles, or nil when there is none or the grid would be too large.
//
// used holds the paths of the edges routed before: running along one of
// them costs like hugging a service, so that edges keep apart where they
// can and a shared stretch does not hide where they part.
func (rt *router) routeOrth(a1, b1, da, db scene.Point, obstacles []scene.Rect, margin float64, used *occupancy) ([]scene.Point, error) {
	g := rt.env.G
	xs := []float64{a1.X, b1.X}
	ys := []float64{a1.Y, b1.Y}
	for _, r := range obstacles {
		xs = append(xs, r.X-margin, r.X, r.X+r.W, r.X+r.W+margin)
		ys = append(ys, r.Y-margin, r.Y, r.Y+r.H, r.Y+r.H+margin)
	}
	xs, ys = uniq(xs), uniq(ys)
	nx, ny := len(xs), len(ys)
	if nx*ny > maxRouteGrid {
		return nil, guard.Limit("architecture routing grid points", nx*ny, maxRouteGrid)
	}
	// covered[i*ny+j]: the cell between xs[i..i+1] and ys[j..j+1] lies
	// inside a service.
	covered := make([]bool, nx*ny)
	idx := func(v []float64, x float64) int { return sort.SearchFloat64s(v, x) }
	for _, r := range obstacles {
		i0, i1 := idx(xs, r.X), idx(xs, r.X+r.W)
		j0, j1 := idx(ys, r.Y), idx(ys, r.Y+r.H)
		for i := i0; i < i1; i++ {
			for j := j0; j < j1; j++ {
				covered[i*ny+j] = true
			}
		}
	}
	cov := func(i, j int) bool { return i >= 0 && j >= 0 && i < nx-1 && j < ny-1 && covered[i*ny+j] }
	// segCost returns the cost of the unit step from (i, j) in direction
	// d, or -1 when it crosses a service.
	dirs := [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	var workErr error
	segCost := func(i, j, d int) float64 {
		var c1, c2 bool
		var length float64
		switch d {
		case 0: // right: cells above and below the segment
			c1, c2, length = cov(i, j-1), cov(i, j), xs[i+1]-xs[i]
		case 1:
			c1, c2, length = cov(i-1, j-1), cov(i-1, j), xs[i]-xs[i-1]
		case 2: // down: cells left and right of the segment
			c1, c2, length = cov(i-1, j), cov(i, j), ys[j+1]-ys[j]
		default:
			c1, c2, length = cov(i-1, j-1), cov(i, j-1), ys[j]-ys[j-1]
		}
		switch {
		case c1 && c2:
			return -1
		case c1 || c2:
			return float64(length * hugFactor)
		}
		ni, nj := i+dirs[d][0], j+dirs[d][1]
		hit, scanned := used.covers(scene.Point{X: xs[i], Y: ys[j]}, scene.Point{X: xs[ni], Y: ys[nj]})
		workErr = errors.Join(workErr, rt.spend(scanned))
		if hit {
			return float64(length * hugFactor)
		}
		return length
	}
	dirOf := func(p scene.Point) int {
		switch {
		case p.X > 0:
			return 0
		case p.X < 0:
			return 1
		case p.Y > 0:
			return 2
		}
		return 3
	}
	si, sj := idx(xs, a1.X), idx(ys, a1.Y)
	ti, tj := idx(xs, b1.X), idx(ys, b1.Y)
	startDir, endDir := dirOf(da), dirOf(scene.Point{X: -db.X, Y: -db.Y})
	n := nx * ny * 4
	if len(rt.dist) < n {
		rt.dist, rt.prev, rt.stamp = make([]float64, n), make([]int32, n), make([]uint32, n)
	}
	rt.gen++
	gen := rt.gen
	dist := func(s int) float64 {
		if rt.stamp[s] != gen {
			return math.Inf(1)
		}
		return rt.dist[s]
	}
	set := func(s int, c float64, p int32) { rt.stamp[s], rt.dist[s], rt.prev[s] = gen, c, p }
	state := func(i, j, d int) int { return (i*ny+j)*4 + d }
	pq := &queue{}
	s0 := state(si, sj, startDir)
	set(s0, 0, -1)
	heap.Push(pq, item{s0, 0})
	goal, best := -1, math.Inf(1)
	for pq.Len() > 0 {
		it := heap.Pop(pq).(item)
		if it.cost > dist(it.s) {
			continue
		}
		if workErr != nil {
			return nil, workErr
		}
		if err := rt.spend(1); err != nil {
			return nil, err
		}
		if it.cost >= best {
			break // states pop in cost order: nothing cheaper is left
		}
		if err := g.Tick(); err != nil {
			return nil, err
		}
		cell, d := it.s/4, it.s%4
		i, j := cell/ny, cell%ny
		if i == ti && j == tj {
			cost := it.cost
			if d != endDir {
				cost += bendCost // the turn into b
			}
			if cost < best {
				goal, best = it.s, cost
			}
			continue
		}
		for nd, dv := range dirs {
			if nd^1 == d {
				continue // no U-turns
			}
			ni, nj := i+dv[0], j+dv[1]
			if ni < 0 || nj < 0 || ni >= nx || nj >= ny {
				continue
			}
			c := segCost(i, j, nd)
			if c < 0 {
				continue
			}
			if nd != d {
				c += bendCost
			}
			ns := state(ni, nj, nd)
			if nc := it.cost + c; nc < dist(ns) {
				set(ns, nc, int32(it.s))
				heap.Push(pq, item{ns, nc})
			}
		}
	}
	if goal < 0 {
		return nil, nil
	}
	var rev []scene.Point
	for s := goal; s >= 0; s = int(rt.prev[s]) {
		cell := s / 4
		rev = append(rev, scene.Point{X: xs[cell/ny], Y: ys[cell%ny]})
	}
	pts := make([]scene.Point, len(rev))
	for k := range rev {
		pts[k] = rev[len(rev)-1-k]
	}
	return straighten(pts), nil
}

// straighten drops points in the middle of straight runs.
func straighten(pts []scene.Point) []scene.Point {
	pts = scene.Dedupe(pts)
	if len(pts) < 3 {
		return pts
	}
	out := []scene.Point{pts[0]}
	for k := 1; k < len(pts)-1; k++ {
		a, b, c := out[len(out)-1], pts[k], pts[k+1]
		if (a.X == b.X && b.X == c.X) || (a.Y == b.Y && b.Y == c.Y) {
			continue
		}
		out = append(out, b)
	}
	return append(out, pts[len(pts)-1])
}

func uniq(v []float64) []float64 {
	sort.Float64s(v)
	out := v[:0]
	for k, x := range v {
		if k == 0 || x != out[len(out)-1] {
			out = append(out, x)
		}
	}
	return out
}

type item struct {
	s    int
	cost float64
}

// queue is a min-heap of search states; ties go to the lower state index,
// so the search is deterministic.
type queue []item

func (q queue) Len() int { return len(q) }
func (q queue) Less(a, b int) bool {
	if q[a].cost != q[b].cost {
		return q[a].cost < q[b].cost
	}
	return q[a].s < q[b].s
}
func (q queue) Swap(a, b int) { q[a], q[b] = q[b], q[a] }
func (q *queue) Push(x any)   { *q = append(*q, x.(item)) }
func (q *queue) Pop() any {
	old := *q
	it := old[len(old)-1]
	*q = old[:len(old)-1]
	return it
}

// occupancy records the straight runs of routed paths: horizontal runs by
// their y, vertical runs by their x.
type occupancy struct {
	horiz, vert map[float64][][2]float64
}

func newOccupancy() *occupancy {
	return &occupancy{horiz: map[float64][][2]float64{}, vert: map[float64][][2]float64{}}
}

// add records the runs of a path.
func (o *occupancy) add(pts []scene.Point) {
	for k := 1; k < len(pts); k++ {
		a, b := pts[k-1], pts[k]
		switch {
		case a.Y == b.Y:
			o.horiz[a.Y] = append(o.horiz[a.Y], [2]float64{math.Min(a.X, b.X), math.Max(a.X, b.X)})
		case a.X == b.X:
			o.vert[a.X] = append(o.vert[a.X], [2]float64{math.Min(a.Y, b.Y), math.Max(a.Y, b.Y)})
		}
	}
}

// covers reports whether the straight segment a-b runs along a recorded
// run for part of its length, and how many runs it looked at.
func (o *occupancy) covers(a, b scene.Point) (bool, int) {
	runs, lo, hi := o.horiz[a.Y], math.Min(a.X, b.X), math.Max(a.X, b.X)
	if a.X == b.X {
		runs, lo, hi = o.vert[a.X], math.Min(a.Y, b.Y), math.Max(a.Y, b.Y)
	}
	for k, r := range runs {
		if r[0] < hi && lo < r[1] {
			return true, k + 1
		}
	}
	return false, len(runs)
}
