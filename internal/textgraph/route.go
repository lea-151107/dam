package textgraph

import (
	"slices"

	"github.com/lea/mermaid/internal/guard"
	"github.com/lea/mermaid/internal/textgrid"
)

// The text renderer routes edges on the character grid itself instead of
// tracing the layout's polylines. Tracing them made unrelated edges share
// cells: all edges of a node met at the middle of its border (an arrow head
// on top of an outgoing line, "▲┬"), parallel segments between two ranks
// landed on the same row and merged, and labels sat where the layout put the
// label node rather than on the line actually drawn.
//
// The router gives every edge end its own border cell, never lets two lines
// run along the same cell (they may only cross at right angles) and finds a
// cheap orthogonal path with Dijkstra's algorithm.

// Cell flags of the routing map.
const (
	cellBlocked   uint16 = 1 << iota // node box, arrow head, label, cluster title
	cellH                            // a horizontal line passes straight through
	cellV                            // a vertical line passes straight through
	cellBend                         // a line turns or ends here: nothing may pass
	cellBorderH                      // horizontal cluster border: cross vertically only
	cellBorderV                      // vertical cluster border: cross horizontally only
	cellNearBox                      // next to a node box: allowed but discouraged
	cellNearLine                     // next to a drawn line: allowed but discouraged
	cellNearLabel                    // around a label: no other label may touch it
)

// Directions, indexed so that dir^1 is the opposite direction.
const (
	dirUp = iota
	dirDown
	dirLeft
	dirRight
)

var dirDX = [4]int{0, 0, -1, 1}
var dirDY = [4]int{-1, 1, 0, 0}

func horizontalDir(d int) bool { return d >= dirLeft }

// Path costs. A bend costs as much as a few steps so that routes stay
// straight, and a crossing more than a short detour.
const (
	costStep     = 2
	costBend     = 6
	costCross    = 12
	costBorder   = 3
	costNearBox  = 3
	costNearLine = 2
	costSide     = 14 // leaving or entering through a perpendicular side
	costBackSide = 20
	// routeMargin is how far beyond the end boxes a search first looks;
	// the whole grid is searched if that fails.
	routeMargin = 8
	// costOffGuide is charged per cell of distance from the layout's
	// polyline (up to maxOffGuide). The layout reserved room along it with
	// dummy nodes, so staying near it keeps long edges out of the way
	// instead of letting them circle the whole drawing.
	costOffGuide = 1
	maxOffGuide  = 10
	// costOffMid is charged per cell of distance from the middle of the gap
	// between the two boxes, for each step across the chart's main axis
	// (up to maxOffMid). Cross-axis stretches then sit mid-gap, leaving the
	// rows next to the boxes free for the other edges that end there.
	costOffMid = 1
	maxOffMid  = 4
)

type cell struct{ x, y int }

// router holds the routing map of one text rendering.
type router struct {
	joinBuf []edgeEnd // reused by joinEnds
	g       *guard.Guard
	// work counts search steps; it is shared by the attempts of one Text
	// call and bounded by maxRouteWork.
	work *int
	w, h int
	occ  []uint16
	port []bool // border cells already used as an edge end
	// trees maps a tree key (see treeKey) to the straight line cells of the
	// edges in that tree. An edge may join a line of its own tree instead
	// of reaching the box itself: edges into the same node with the same
	// arrow merge before the arrow ("─┬─▶"), edges out of the same node
	// fan out from a shared line. Lines of other edges are only crossed.
	trees map[int][]cell
	// owner is the line (committed path, numbered from 1) through a cell,
	// or -1 where lines of different edges cross; ownerIdx is the cell's
	// index on that path.
	owner, ownerIdx []int32
	// inMin and outMax bound, per line, the indexes where other edges may
	// merge in (in-tree) or branch off (out-tree). On one line every branch
	// must come before every merge, or the drawing would show a path that
	// does not exist: a source merging in would seem to reach the targets
	// branching off after it.
	inMin, outMax []int

	// relaxed lets a search overlap other lines and reuse ports; it is the
	// last resort before dropping an edge.
	relaxed bool

	// Search buffers, reused across searches (see route).
	gen             uint32
	dist, prev      []int32
	stamp, offStamp []uint32
	off             []uint8
	goal            []int32
	goalStamp       []uint32
	queue           []int
	bq              bucketQueue
}

func newRouter(g *guard.Guard, w, h int, work *int) *router {
	r := &router{g: g, work: work, w: w, h: h, occ: make([]uint16, w*h), port: make([]bool, w*h),
		trees: map[int][]cell{}, owner: make([]int32, w*h), ownerIdx: make([]int32, w*h),
		inMin: []int{0}, outMax: []int{0}}
	return r
}

// treeKey identifies the edges that may share lines: those entering node
// (in) or leaving it (!in) with the same marker at that node.
func treeKey(node int, in bool, marker uint8) int {
	k := (node+1)<<9 | int(marker)<<1
	if in {
		k |= 1
	}
	return k
}

// joinEnds lists the places where an edge may join the lines of tree key:
// straight cells of that tree, entered at a right angle. The result is
// valid until the next call.
func (r *router) joinEnds(key int) []edgeEnd {
	// Scanning a tree costs work too: with many edges sharing one tree
	// (a node with hundreds of children) it was most of the time spent,
	// outside the search-step limit. route checks the total.
	*r.work += len(r.trees[key])
	// The result is reused by the next call: callers copy it at once.
	out := r.joinBuf[:0]
	defer func() { r.joinBuf = out[:0] }()
	for _, c := range r.trees[key] {
		f := r.at(c.x, c.y)
		k := c.y*r.w + c.x
		if f&cellBend != 0 || f&(cellH|cellV) == cellH|cellV || r.owner[k] < 0 {
			continue
		}
		line, idx := r.owner[k], int(r.ownerIdx[k])
		if (key&1 == 1 && idx < r.inMin[line]) || (key&1 == 0 && idx > r.outMax[line]) {
			continue
		}
		dirs := [2]int{dirLeft, dirRight}
		if f&cellH != 0 {
			dirs = [2]int{dirUp, dirDown}
		}
		for _, d := range dirs {
			o := cell{c.x + dirDX[d], c.y + dirDY[d]}
			if r.passable(o, d) && r.at(o.x, o.y)&(cellH|cellV) == 0 {
				out = append(out, edgeEnd{port: c, out: o, dir: d, cost: costBend})
			}
		}
	}
	return out
}

// isLine reports whether c carries a line (a join point rather than a
// box border).
func (r *router) isLine(c cell) bool { return r.at(c.x, c.y)&(cellH|cellV) != 0 }

func (r *router) in(x, y int) bool { return x >= 0 && y >= 0 && x < r.w && y < r.h }

func (r *router) at(x, y int) uint16 {
	if !r.in(x, y) {
		return cellBlocked
	}
	return r.occ[y*r.w+x]
}

func (r *router) mark(x, y int, f uint16) {
	if r.in(x, y) {
		r.occ[y*r.w+x] |= f
	}
}

// box is a node rectangle on the grid.
type box struct{ x, y, w, h int }

func (b box) cx() int { return b.x + b.w/2 }
func (b box) cy() int { return b.y + b.h/2 }

// blockBox marks a node box and discourages the cells around it.
func (r *router) blockBox(b box) {
	for y := b.y - 1; y <= b.y+b.h; y++ {
		for x := b.x - 1; x <= b.x+b.w; x++ {
			if x >= b.x && x < b.x+b.w && y >= b.y && y < b.y+b.h {
				r.mark(x, y, cellBlocked)
			} else {
				r.mark(x, y, cellNearBox)
			}
		}
	}
}

// blockCluster marks a cluster frame: its sides may only be crossed, and its
// corners and title not at all.
func (r *router) blockCluster(x0, y0, x1, y1, titleX0, titleX1 int) {
	for x := x0; x <= x1; x++ {
		r.mark(x, y0, cellBorderH)
		r.mark(x, y1, cellBorderH)
	}
	for y := y0; y <= y1; y++ {
		r.mark(x0, y, cellBorderV)
		r.mark(x1, y, cellBorderV)
	}
	for _, c := range [4]cell{{x0, y0}, {x1, y0}, {x0, y1}, {x1, y1}} {
		r.mark(c.x, c.y, cellBlocked)
	}
	for x := titleX0; x <= titleX1; x++ {
		r.mark(x, y0, cellBlocked)
	}
}

// edgeEnd is a candidate edge end: the border cell of the box, the free
// cell just outside it, the direction pointing out of the box, and the
// extra cost of using it.
type edgeEnd struct {
	port, out cell
	dir       int
	cost      int
}

// ends lists the usable ends of b for an edge to other. toward is the direction of the other end
// of the edge along the main axis (dirDown for a forward edge in a top-down
// chart); sides facing it are free, perpendicular ones cost costSide and the
// opposite one costBackSide. Cells farther from the middle cost a little
// more, so ends spread out from the centre.
func (r *router) ends(b box, toward int, other box) []edgeEnd {
	// The preferred cell on each side lies toward the other box, so that
	// edges leaving one node for several others take ports in the order of
	// their targets and do not cross right away.
	wantX := b.cx() + max(-(b.w/2-1), min(b.w/2-1, (other.cx()-b.cx())/2))
	wantY := b.cy() + max(-(b.h/2-1), min(b.h/2-1, (other.cy()-b.cy())/2))
	var out []edgeEnd
	add := func(px, py, d int, offset int) {
		port := cell{px, py}
		o := cell{px + dirDX[d], py + dirDY[d]}
		if !r.in(o.x, o.y) {
			return
		}
		if r.relaxed {
			if r.at(o.x, o.y)&cellBlocked != 0 {
				return
			}
		} else {
			if r.port[py*r.w+px] {
				return
			}
			if f := r.at(o.x, o.y); f&(cellBlocked|cellH|cellV|cellBend|cellBorderH|cellBorderV) != 0 {
				return
			}
		}
		c := 0
		switch {
		case d == toward:
		case d == toward^1:
			c = costBackSide
		default:
			c = costSide
		}
		if offset < 0 {
			offset = -offset
		}
		out = append(out, edgeEnd{port: port, out: o, dir: d, cost: c + offset})
	}
	for x := b.x + 1; x < b.x+b.w-1; x++ {
		add(x, b.y, dirUp, x-wantX)
		add(x, b.y+b.h-1, dirDown, x-wantX)
	}
	for y := b.y + 1; y < b.y+b.h-1; y++ {
		add(b.x, y, dirLeft, 2*(y-wantY))
		add(b.x+b.w-1, y, dirRight, 2*(y-wantY))
	}
	return out
}

// route finds a path from one of the from ends to one of the to ends. The
// returned cells run from the source's port to the target's port, both
// included. It returns nil if there is no path.
// mid is the main-axis coordinate of the middle of the gap (a row when
// horizontal is false, a column otherwise), or -1 for none.
func (r *router) route(from, to []edgeEnd, guide []cell, window [4]int, horizontal bool, mid int) ([]cell, error) {
	if len(from) == 0 || len(to) == 0 {
		return nil, nil
	}
	if *r.work > maxRouteWork {
		return nil, guard.Limit("text routing steps", *r.work, maxRouteWork)
	}
	x0, y0, x1, y1 := max(0, window[0]), max(0, window[1]), min(r.w-1, window[2]), min(r.h-1, window[3])
	// The search buffers cover the whole grid and are reused: a state is
	// valid only if its stamp matches this search's generation.
	n := r.w * r.h * 4
	sink := n
	if r.dist == nil {
		r.dist = make([]int32, n+1)
		r.prev = make([]int32, n+1)
		r.stamp = make([]uint32, n+1)
		r.off = make([]uint8, r.w*r.h)
		r.offStamp = make([]uint32, r.w*r.h)
		r.goal = make([]int32, n+1)
		r.goalStamp = make([]uint32, n+1)
	}
	r.gen++
	gen := r.gen
	dist, prev, stamp := r.dist, r.prev, r.stamp
	state := func(c cell, d int) int { return (c.y*r.w+c.x)*4 + d }
	inWin := func(c cell) bool { return c.x >= x0 && c.x <= x1 && c.y >= y0 && c.y <= y1 }
	r.guideDistance(guide, x0, y0, x1, y1)
	if *r.work > maxRouteWork {
		return nil, guard.Limit("text routing steps", *r.work, maxRouteWork)
	}
	offCost := func(c cell) int32 {
		if len(guide) == 0 {
			return 0
		}
		k := c.y*r.w + c.x
		d := maxOffGuide
		if r.offStamp[k] == gen {
			d = int(r.off[k])
		}
		return int32(costOffGuide * d)
	}
	// goal[state] is the end reached by arriving in that state (valid where
	// goalStamp matches). gx0…gy1 bound the goal cells for the A* estimate.
	gx0, gy0, gx1, gy1 := r.w, r.h, -1, -1
	for i, e := range to {
		if !inWin(e.out) {
			continue
		}
		gs := state(e.out, e.dir^1)
		if r.goalStamp[gs] != gen {
			r.goalStamp[gs], r.goal[gs] = gen, int32(i)
		}
		gx0, gy0, gx1, gy1 = min(gx0, e.out.x), min(gy0, e.out.y), max(gx1, e.out.x), max(gy1, e.out.y)
	}
	if gx1 < 0 {
		return nil, nil
	}
	// estimate is a lower bound of the remaining cost (A*): the steps to
	// the box around all goals.
	estimate := func(c cell) int32 {
		dx := max(gx0-c.x, 0, c.x-gx1)
		dy := max(gy0-c.y, 0, c.y-gy1)
		return int32(costStep * (dx + dy))
	}
	// The queue is a bucket queue (Dial's algorithm) on cost+estimate:
	// costs are small integers, and buckets are popped in FIFO order, which
	// keeps the search deterministic.
	bq := &r.bq
	bq.reset()
	push := func(s int, cost int32, from int32, c cell) {
		if stamp[s] == gen && dist[s] <= cost {
			return
		}
		stamp[s] = gen
		dist[s] = cost
		prev[s] = from
		pri := cost
		if s != sink {
			pri += estimate(c)
		}
		bq.push(int(pri), int32(s))
	}
	starts := map[int]edgeEnd{}
	for _, e := range from {
		if !inWin(e.out) {
			continue
		}
		s := state(e.out, e.dir)
		if _, dup := starts[s]; !dup {
			starts[s] = e
		}
		push(s, int32(e.cost+costStep+r.extra(e.out, e.dir))+offCost(e.out), -1, e.out)
	}
	found := false
	for {
		pri, s32, ok := bq.pop()
		if !ok {
			break
		}
		s := int(s32)
		ci := s / 4
		c := cell{ci % r.w, ci / r.w}
		cost := dist[s]
		if s != sink && int(cost+estimate(c)) != pri {
			continue // stale entry
		}
		if s == sink && int(cost) != pri {
			continue
		}
		if err := r.g.Tick(); err != nil {
			return nil, err
		}
		if *r.work++; *r.work > maxRouteWork {
			return nil, guard.Limit("text routing steps", *r.work, maxRouteWork)
		}
		if s == sink {
			found = true
			break
		}
		d := s % 4
		if r.goalStamp[s] == gen {
			push(sink, cost+int32(to[r.goal[s]].cost), int32(s), c)
		}
		f := r.occ[ci]
		for nd := 0; nd < 4; nd++ {
			if nd == d^1 {
				continue
			}
			bend := nd != d
			if bend && !r.relaxed && f&(cellH|cellV|cellBorderH|cellBorderV) != 0 {
				// Crossing cells and cluster borders are passed straight.
				continue
			}
			nc := cell{c.x + dirDX[nd], c.y + dirDY[nd]}
			if !inWin(nc) || !r.passable(nc, nd) {
				continue
			}
			nextCost := cost + costStep + int32(r.extra(nc, nd)) + offCost(nc)
			if mid >= 0 && horizontalDir(nd) != horizontal {
				// A step across the main axis: cheapest mid-gap.
				m := nc.y
				if horizontal {
					m = nc.x
				}
				nextCost += int32(costOffMid * min(maxOffMid, abs(m-mid)))
			}
			if bend {
				nextCost += costBend
			}
			push(state(nc, nd), nextCost, int32(s), nc)
		}
	}
	if !found {
		return nil, nil
	}
	// Walk back from the sink.
	last := int(prev[sink])
	path := []cell{to[r.goal[last]].port}
	s := last
	for s >= 0 {
		ci := s / 4
		path = append(path, cell{ci % r.w, ci / r.w})
		if prev[s] < 0 {
			path = append(path, starts[s].port)
		}
		s = int(prev[s])
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path, nil
}

// guideDistance records, for the cells of the window within maxOffGuide
// steps of the guide, their distance from it (in r.off, valid where
// r.offStamp equals the current generation).
func (r *router) guideDistance(guide []cell, x0, y0, x1, y1 int) {
	if len(guide) == 0 {
		return
	}
	gen := r.gen
	queue := r.queue[:0]
	for _, c := range guide {
		if c.x < x0 || c.y < y0 || c.x > x1 || c.y > y1 {
			continue
		}
		k := c.y*r.w + c.x
		if r.offStamp[k] != gen {
			r.offStamp[k], r.off[k] = gen, 0
			queue = append(queue, k)
		}
	}
	for h := 0; h < len(queue); h++ {
		k := queue[h]
		if int(r.off[k]) >= maxOffGuide {
			continue
		}
		x, y := k%r.w, k/r.w
		for d := 0; d < 4; d++ {
			nx, ny := x+dirDX[d], y+dirDY[d]
			if nx < x0 || ny < y0 || nx > x1 || ny > y1 {
				continue
			}
			nk := ny*r.w + nx
			if r.offStamp[nk] != gen {
				r.offStamp[nk], r.off[nk] = gen, r.off[k]+1
				queue = append(queue, nk)
			}
		}
	}
	r.queue = queue
	*r.work += len(queue)
}

// guideCells rasterises a polyline into the cells it passes.
func guideCells(pts []cell) []cell {
	var out []cell
	for i := 0; i+1 < len(pts); i++ {
		a, b := pts[i], pts[i+1]
		n := max(abs(b.x-a.x), abs(b.y-a.y), 1)
		for k := 0; k <= n; k++ {
			out = append(out, cell{a.x + (b.x-a.x)*k/n, a.y + (b.y-a.y)*k/n})
		}
	}
	return out
}

// passable reports whether a line moving in direction d may enter c.
func (r *router) passable(c cell, d int) bool {
	f := r.at(c.x, c.y)
	if r.relaxed {
		return f&cellBlocked == 0
	}
	if f&(cellBlocked|cellBend) != 0 {
		return false
	}
	if horizontalDir(d) {
		return f&(cellH|cellBorderH) == 0
	}
	return f&(cellV|cellBorderV) == 0
}

// extra is the cost of entering c in direction d beyond a plain step.
func (r *router) extra(c cell, d int) int {
	f := r.at(c.x, c.y)
	e := 0
	if f&(cellH|cellV) != 0 {
		e += costCross
	}
	if f&(cellBorderH|cellBorderV) != 0 {
		e += costBorder
	}
	if f&cellNearBox != 0 {
		e += costNearBox
	}
	if f&cellNearLine != 0 {
		e += costNearLine
	}
	return e
}

// commit records a drawn path so later edges avoid it, and adds its
// straight cells to the trees inKey (cells from index inFrom on) and outKey
// (cells up to index outTo); 0 means none. A labelled edge offers only the
// part beyond its label, so that a label never ends up on a shared line. The two ports
// are reserved; cells where the path turns or joins another line become
// impassable.
func (r *router) commit(path []cell, inKey, outKey, inFrom, outTo int) {
	if len(path) == 0 {
		return
	}
	// A merge at index k of a line allows branches only before k; a branch
	// at k allows merges only after it.
	n := len(path)
	if p := path[n-1]; r.isLine(p) {
		if k := p.y*r.w + p.x; r.owner[k] > 0 {
			l := r.owner[k]
			r.outMax[l] = min(r.outMax[l], int(r.ownerIdx[k])-1)
		}
	}
	if p := path[0]; r.isLine(p) {
		if k := p.y*r.w + p.x; r.owner[k] > 0 {
			l := r.owner[k]
			r.inMin[l] = max(r.inMin[l], int(r.ownerIdx[k])+1)
		}
	}
	line := int32(len(r.inMin))
	r.inMin = append(r.inMin, inFrom)
	r.outMax = append(r.outMax, outTo)
	for _, p := range [2]cell{path[0], path[len(path)-1]} {
		if r.isLine(p) {
			r.mark(p.x, p.y, cellBend) // a junction with the joined line
		}
	}
	for i := 2; i < len(path)-2; i++ {
		c := path[i]
		k := c.y*r.w + c.x
		if r.owner[k] != 0 {
			r.owner[k] = -1 // crossing
			continue
		}
		r.owner[k], r.ownerIdx[k] = line, int32(i)
		if inKey != 0 {
			r.trees[inKey] = append(r.trees[inKey], c)
		}
		if outKey != 0 {
			r.trees[outKey] = append(r.trees[outKey], c)
		}
	}
	for _, p := range [2]cell{path[0], path[len(path)-1]} {
		if r.in(p.x, p.y) {
			r.port[p.y*r.w+p.x] = true
		}
	}
	for i := 1; i < len(path)-1; i++ {
		a, c, b := path[i-1], path[i], path[i+1]
		switch {
		case a.y == c.y && c.y == b.y:
			r.mark(c.x, c.y, cellH)
		case a.x == c.x && c.x == b.x:
			r.mark(c.x, c.y, cellV)
		default:
			r.mark(c.x, c.y, cellBend)
		}
	}
	// Spread later lines away from this one, so that parallel lines leave
	// room for labels and are easy to follow.
	for _, c := range path {
		for d := 0; d < 4; d++ {
			r.mark(c.x+dirDX[d], c.y+dirDY[d], cellNearLine)
		}
	}
	// The cells next to the ports are where arrow heads go and where the
	// line meets the box; keep other lines out of them.
	// The line right behind them too, so that no crossing reads as part
	// of the arrow ("◀┼").
	for _, i := range [4]int{1, 2, len(path) - 3, len(path) - 2} {
		if i > 0 && i < len(path)-1 {
			r.mark(path[i].x, path[i].y, cellBend)
		}
	}
}

// bucketQueue is a monotone priority queue for small integer priorities:
// one FIFO bucket per priority.
type bucketQueue struct {
	buckets [][]int32
	heads   []int
	cur     int
}

func (q *bucketQueue) reset() {
	for i := range q.buckets {
		q.buckets[i] = q.buckets[i][:0]
		q.heads[i] = 0
	}
	q.cur = 0
}

func (q *bucketQueue) push(pri int, s int32) {
	for pri >= len(q.buckets) {
		q.buckets = append(q.buckets, nil)
		q.heads = append(q.heads, 0)
	}
	q.buckets[pri] = append(q.buckets[pri], s)
	// A consistent estimate never pushes below the current priority, but
	// guard anyway so nothing is skipped.
	q.cur = min(q.cur, pri)
}

func (q *bucketQueue) pop() (pri int, s int32, ok bool) {
	for q.cur < len(q.buckets) {
		b := q.buckets[q.cur]
		if h := q.heads[q.cur]; h < len(b) {
			q.heads[q.cur]++
			return q.cur, b[h], true
		}
		q.cur++
	}
	return 0, 0, false
}

// bendAt reports whether the path turns at index i.
func bendAt(path []cell, i int) bool {
	if i <= 0 || i >= len(path)-1 {
		return false
	}
	a, b := path[i-1], path[i+1]
	return a.x != b.x && a.y != b.y
}

// labelPlace is where an edge label is written: its top-left cell.
type labelPlace struct {
	x, y  int
	lines []string
	// first and last are the indexes of the path cells under the label,
	// or -1 when it sits beside the path.
	first, last int
}

// placeLabel puts an edge label on or next to its own path, preferring
// the spot nearest hint (where the layout reserved room for it):
//   - on a horizontal stretch, written over the line ("──はい──"), or on a
//     vertical one, written across it, keeping line cells visible at both
//     ends of the text,
//   - beside a straight stretch of the path, or
//   - failing all, in the free space nearest the path.
//
// The label never covers its own path elsewhere (ports, bends, arrow heads,
// other stretches), never touches another label on the same row, and never
// touches another edge's line. The cells it takes are blocked for later
// edges.
//
// A self loop (loop true) is small and all its stretches are short, so its
// label always goes outside the loop rather than on it.
func (r *router) placeLabel(path []cell, lines []string, hint cell, startHead, endHead, loop bool) labelPlace {
	w := 0
	for _, l := range lines {
		w = max(w, textgrid.Width(l))
	}
	n := len(lines)
	// The path is not committed yet, so its cells look free in the map;
	// own records them.
	own := make([]bool, r.w*r.h)
	for _, c := range path {
		if r.in(c.x, c.y) {
			own[c.y*r.w+c.x] = true
		}
	}
	free := func(x, y int) bool {
		return r.in(x, y) && !own[y*r.w+x] && r.at(x, y)&^(cellNearBox|cellNearLine) == 0
	}
	// taken[(y+1)*(r.w+1)+x+1] counts the cells that are not free in the
	// rectangle from (0, 0) to (x, y), so that a candidate rectangle is
	// checked in constant time instead of cell by cell.
	stride := r.w + 1
	taken := make([]int32, stride*(r.h+1))
	for y := 0; y < r.h; y++ {
		var row int32
		for x := 0; x < r.w; x++ {
			if !free(x, y) {
				row++
			}
			taken[(y+1)*stride+x+1] = taken[y*stride+x+1] + row
		}
	}
	// fits reports whether text at (x, y) is entirely on free cells, except
	// for the cells listed in on (the line it is written over).
	fits := func(x, y int, on map[cell]bool) bool {
		// Cells off the grid are never free, nor in on.
		if x < 0 || y < 0 || x+w > r.w || y+n > r.h {
			return false
		}
		c := taken[(y+n)*stride+x+w] - taken[y*stride+x+w] - taken[(y+n)*stride+x] + taken[y*stride+x]
		for k := range on {
			if k.x >= x && k.x < x+w && k.y >= y && k.y < y+n && !free(k.x, k.y) {
				c--
			}
		}
		return c == 0
	}
	// apart reports whether text at (x, y) keeps a blank cell between
	// itself and every other label on the same rows; clear, that the cells
	// just left and right of it carry no other edge's line ("┘Two").
	apart := func(x, y int) bool {
		for yy := y; yy < y+n; yy++ {
			for xx := x - 1; xx <= x+w; xx++ {
				if r.at(xx, yy)&cellNearLabel != 0 {
					return false
				}
			}
		}
		return true
	}
	// Own arrow heads count too: "▼x" would read as part of the arrow.
	var heads []cell
	if startHead && len(path) > 1 {
		heads = append(heads, path[1])
	}
	if endHead && len(path) > 1 {
		heads = append(heads, path[len(path)-2])
	}
	clear := func(x, y int) bool {
		for yy := y; yy < y+n; yy++ {
			for _, xx := range [2]int{x - 1, x + w} {
				if r.at(xx, yy)&(cellH|cellV|cellBend) != 0 || slices.Contains(heads, cell{xx, yy}) {
					return false
				}
			}
		}
		return true
	}
	// The loop's bounding box, which its label must stay out of.
	bx0, by0, bx1, by1 := path[0].x, path[0].y, path[0].x, path[0].y
	for _, c := range path {
		bx0, by0, bx1, by1 = min(bx0, c.x), min(by0, c.y), max(bx1, c.x), max(by1, c.y)
	}
	outside := func(x, y int) bool {
		return x+w-1 < bx0 || x > bx1 || y+n-1 < by0 || y > by1
	}
	best, bestD := labelPlace{}, -1
	try := func(x, y int, on map[cell]bool) {
		if loop && (on != nil || !outside(x, y)) {
			return
		}
		if !fits(x, y, on) || !apart(x, y) || !clear(x, y) {
			return
		}
		cx, cy := x+w/2, y+n/2
		d := abs(cx-hint.x) + abs(cy-hint.y)
		if bestD < 0 || d < bestD {
			best, bestD = labelPlace{x: x, y: y, lines: lines}, d
		}
	}
	// Runs of the path without its ports and arrow heads. The text never
	// takes the first or last cell of a run, so bends and the line behind
	// an arrow head stay visible.
	lo, hi := 1, len(path)-1
	if startHead {
		lo++
	}
	if endHead {
		hi--
	}
	inner := path[min(lo, len(path)):max(min(lo, len(path)), hi)]
	type run struct {
		cells []cell
		horiz bool
		at    int // index of cells[0] in path
	}
	var runs []run
	for i := 0; i+1 < len(inner); {
		// A run is a maximal straight stretch; the cell at a bend belongs
		// to both runs around it.
		horiz := inner[i+1].y == inner[i].y
		j := i + 1
		for j < len(inner) && ((horiz && inner[j].y == inner[i].y) || (!horiz && inner[j].x == inner[i].x)) {
			j++
		}
		runs = append(runs, run{inner[i:j], horiz, lo + i})
		i = j - 1
	}
	for _, rn := range runs {
		first, last := rn.cells[0], rn.cells[len(rn.cells)-1]
		// Line cells kept visible at each end of the text. Text written over
		// a horizontal line next to a corner hides the line ("┌x─┐" in a
		// self loop), so there two cells stay where the run turns; text
		// across a vertical line leaves it readable above and below, so
		// one cell is enough.
		mFirst, mLast := 1, 1
		if rn.horiz && bendAt(path, rn.at) {
			mFirst = 2
		}
		if rn.horiz && bendAt(path, rn.at+len(rn.cells)-1) {
			mLast = 2
		}
		if rn.horiz && n == 1 {
			lo, hi := first.x, last.x
			mLo, mHi := mFirst, mLast
			if lo > hi {
				lo, hi, mLo, mHi = hi, lo, mLast, mFirst
			}
			for x := lo + mLo; x+w <= hi-mHi+1; x++ {
				on := map[cell]bool{}
				ok := true
				for xx := x; xx < x+w; xx++ {
					on[cell{xx, first.y}] = true
					if r.at(xx, first.y)&cellV != 0 {
						ok = false // another line crosses here
					}
				}
				if ok {
					try(x, first.y, on)
				}
			}
		}
		if !rn.horiz {
			col := first.x
			lo, hi := first.y, last.y
			mLo, mHi := mFirst, mLast
			if lo > hi {
				lo, hi, mLo, mHi = hi, lo, mLast, mFirst
			}
			x0 := col - (w-1)/2
			for y := lo + mLo; y+n <= hi-mHi+1; y++ {
				on := map[cell]bool{}
				ok := true
				for yy := y; yy < y+n; yy++ {
					on[cell{col, yy}] = true
					if r.at(col, yy)&cellH != 0 {
						ok = false
					}
				}
				if ok {
					try(x0, y, on)
				}
			}
		}
	}
	if bestD < 0 {
		// Beside a straight stretch, away from its ends: right or left of
		// a vertical one, above or below a horizontal one.
		for _, rn := range runs {
			for k := 1; k+1 < len(rn.cells); k++ {
				c := rn.cells[k]
				if rn.horiz {
					try(c.x-w/2, c.y-n, nil)
					try(c.x-w/2, c.y+1, nil)
				} else {
					try(c.x+1, c.y-n/2, nil)
					try(c.x-w, c.y-n/2, nil)
				}
			}
		}
	}
	if bestD < 0 {
		// Anywhere near the path: search outward from its middle.
		mid := path[len(path)/2]
		for rad := 1; rad <= 12 && bestD < 0; rad++ {
			for dy := -rad; dy <= rad; dy++ {
				for dx := -rad; dx <= rad; dx++ {
					if max(abs(dx), abs(dy)) == rad {
						try(mid.x+dx-w/2, mid.y+dy, nil)
					}
				}
			}
		}
	}
	if bestD < 0 {
		best = labelPlace{x: hint.x - w/2, y: hint.y - n/2, lines: lines}
	}
	// Labels may sit on neighbouring rows, but never touch on the same row
	// ("はいいいえ" would read as one word).
	for yy := best.y; yy < best.y+n; yy++ {
		for xx := best.x - 1; xx <= best.x+w; xx++ {
			if xx >= best.x && xx < best.x+w {
				r.mark(xx, yy, cellBlocked)
			}
			r.mark(xx, yy, cellNearLabel)
		}
	}
	best.first, best.last = -1, -1
	for i, c := range path {
		if c.x >= best.x && c.x < best.x+w && c.y >= best.y && c.y < best.y+n {
			if best.first < 0 {
				best.first = i
			}
			best.last = i
		}
	}
	return best
}
