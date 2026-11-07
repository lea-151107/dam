// Package textgraph draws node-and-edge diagrams with box-drawing
// characters. Diagram packages describe their graph with Graph (boxes,
// clusters and edges); the nodes are laid out with internal/layout/sugiyama
// on a character grid and the edges are routed on the grid itself (see
// route.go).
package textgraph

import (
	"math"
	"sort"
	"strings"

	"github.com/lea/mermaid/internal/guard"
	"github.com/lea/mermaid/internal/layout/sugiyama"
	"github.com/lea/mermaid/internal/scene"
	"github.com/lea/mermaid/internal/textgrid"
)

// Graph is a diagram to draw.
type Graph struct {
	Dir      sugiyama.Dir
	Nodes    []Node
	Edges    []Edge
	Clusters []Cluster
}

// Node is a box. Its label is centered; further sections (class members)
// follow below separator lines, left-aligned.
type Node struct {
	Label    string
	Sections []string
	// Corners are the box corners (zero value: square).
	Corners [4]rune
	// Cluster is the innermost cluster containing the node, or -1.
	Cluster int
	// Glyph, when set, draws the node as this single character without a
	// border (start and end states).
	Glyph rune
}

// Head is the mark at one end of an edge.
type Head struct {
	// Runes are drawn in front of the box, by the direction in which the
	// edge enters the box: up, down, left, right.
	Runes [4]rune
	// Key tells heads apart for line sharing: edges into (or out of) the
	// same node share lines only when their heads have the same key. Zero
	// means no head.
	Key uint8
}

// Arrow is the usual arrow head.
var Arrow = Head{Runes: [4]rune{'▲', '▼', '◀', '▶'}, Key: 1}

// HeadFor returns the head that draws marker m: arrows as ▲▼◀▶,
// inheritance as △▽◁▷, composition ◆, aggregation ◇, circle ○ and
// cross ×. Each marker gets its own key.
func HeadFor(m scene.Marker) Head {
	var r [4]rune
	switch m {
	case scene.NoMarker:
		return Head{}
	case scene.Triangle:
		r = [4]rune{'△', '▽', '◁', '▷'}
	case scene.Diamond:
		r = [4]rune{'◆', '◆', '◆', '◆'}
	case scene.HollowDiamond:
		r = [4]rune{'◇', '◇', '◇', '◇'}
	case scene.Circle:
		r = [4]rune{'○', '○', '○', '○'}
	case scene.Cross:
		r = [4]rune{'×', '×', '×', '×'}
	default:
		r = Arrow.Runes
	}
	return Head{Runes: r, Key: uint8(m)}
}

// LineStyle is how an edge's line is drawn.
type LineStyle uint8

// Line styles.
const (
	Solid LineStyle = iota
	Dashed
	Heavy
)

// Edge connects two nodes.
type Edge struct {
	From, To   int
	Label      string
	Start, End Head
	MinLen     int
	Style      LineStyle
	// Hidden edges take part in the layout but are not drawn.
	Hidden bool
	// OwnCluster is the cluster that one end stands for when the edge
	// connects a node with the cluster containing it (flowchart "A -->
	// subgraph"); with From == To such an edge is drawn straight from the
	// node to the cluster border. -1 otherwise.
	OwnCluster int
	// FromCluster and ToCluster, when not -1, make that end of the edge
	// stop at the frame of the cluster (a composite state) instead of at
	// node From or To, which then only places the edge in the layout.
	FromCluster, ToCluster int
}

// NewEdge returns a solid edge from node from to node to with no heads,
// label or cluster ends.
func NewEdge(from, to int) Edge {
	return Edge{From: from, To: to, MinLen: 1, OwnCluster: -1, FromCluster: -1, ToCluster: -1}
}

// Cluster is a framed group of nodes with a title on its top border.
type Cluster struct {
	Title  string
	Parent int
	// Dir, when HasDir, is the direction of the cluster's contents.
	Dir    sugiyama.Dir
	HasDir bool
}

// attempt is one spacing/wrapping setting.
type attempt struct {
	nodeSep, rankSep float64
	wrap             int
}

// clusterPad is the space inside a cluster frame. Two cells keep a free
// column (row) between the frame and the boxes, so that edges can leave a
// box on the side facing the frame.
const clusterPad = 2

var attempts = [...]attempt{{4, 4, 0}, {2, 3, 30}, {2, 3, 20}, {1, 2, 12}, {1, 2, 8}}

// Text draws gr no wider than maxCols, compacting and wrapping as needed.
func Text(gd *guard.Guard, gr *Graph, maxCols int) ([]string, error) {
	lastW := 0
	work := 0
	for _, a := range attempts {
		lines, w, err := gr.text(gd, a, maxCols, &work)
		if err != nil {
			return nil, err
		}
		if lines != nil && textgrid.MaxWidth(lines) <= maxCols {
			return lines, nil
		}
		lastW = w
	}
	return nil, guard.Limit("columns", lastW, maxCols)
}

// nodeText is a node's wrapped text.
type nodeText struct {
	label    []string
	sections [][]string
}

func (nt nodeText) size() (w, h int) {
	w = textgrid.MaxWidth(nt.label) + 4
	h = len(nt.label) + 2
	for _, s := range nt.sections {
		w = max(w, textgrid.MaxWidth(s)+4)
		h += len(s) + 1
	}
	return w, h
}

// text draws gr with the settings of a. When a box alone is already wider
// than maxCols it returns no lines, only that width: routing the edges
// (the expensive part) would be wasted on a drawing that is discarded
// anyway.
func (gr *Graph) text(gd *guard.Guard, a attempt, maxCols int, work *int) ([]string, int, error) {
	horiz := gr.Dir.Horizontal()
	texts := make([]nodeText, len(gr.Nodes))
	nodes := make([]sugiyama.Node, len(gr.Nodes))
	for i, n := range gr.Nodes {
		if n.Glyph != 0 {
			nodes[i] = sugiyama.Node{W: 3, H: 3, Cluster: n.Cluster}
			continue
		}
		nt := nodeText{label: textgrid.Wrap(n.Label, a.wrap)}
		for _, s := range n.Sections {
			nt.sections = append(nt.sections, textgrid.Wrap(s, a.wrap))
		}
		texts[i] = nt
		w, h := nt.size()
		nodes[i] = sugiyama.Node{W: float64(w), H: float64(h), Cluster: n.Cluster}
	}
	var clusters []sugiyama.Cluster
	for _, c := range gr.Clusters {
		clusters = append(clusters, sugiyama.Cluster{Parent: c.Parent, TitleW: float64(textgrid.Width(c.Title) + 2), TitleH: 1,
			Dir: c.Dir, HasDir: c.HasDir})
	}
	// Make every box big enough to give each edge end its own border cell:
	// labelled edges need one each, unlabelled ones share one per group of
	// lines that may merge, and a self loop needs two. Without this a busy
	// node would run out of cells and edges would be lost.
	// Ends usually arrive on the side facing the previous rank and leave
	// from the opposite one, so each of those sides is sized for its own
	// count.
	needIn := make([]int, len(nodes))
	needOut := make([]int, len(nodes))
	groups := make([]map[int]bool, len(nodes))
	addEnd := func(n, key int, labelled bool, need []int) {
		if labelled {
			need[n]++
			return
		}
		if groups[n] == nil {
			groups[n] = map[int]bool{}
		}
		if !groups[n][key] {
			groups[n][key] = true
			need[n]++
		}
	}
	for _, e := range gr.Edges {
		if e.Hidden {
			continue
		}
		f, t := e.From, e.To
		if f == t {
			needIn[f]++
			needOut[f]++
			continue
		}
		addEnd(f, treeKey(f, false, e.Start.Key), e.Label != "", needOut)
		addEnd(t, treeKey(t, true, e.End.Key), e.Label != "", needIn)
	}
	for l := range nodes {
		if gr.Nodes[l].Glyph != 0 {
			continue
		}
		side := float64(max(needIn[l], needOut[l]) + 2)
		if horiz {
			nodes[l].H = max(nodes[l].H, side)
		} else {
			nodes[l].W = max(nodes[l].W, side)
		}
	}
	edges := make([]sugiyama.Edge, len(gr.Edges))
	elabels := make([][]string, len(gr.Edges))
	for i, e := range gr.Edges {
		se := sugiyama.Edge{From: e.From, To: e.To, MinLen: max(e.MinLen, 1)}
		if e.Label != "" {
			elabels[i] = textgrid.Wrap(e.Label, max(a.wrap, 0))
			se.LabelW, se.LabelH, se.HasLabel = float64(textgrid.MaxWidth(elabels[i])+2), float64(len(elabels[i])), true
		}
		edges[i] = se
	}
	rankSep := a.rankSep
	if horiz {
		rankSep *= 2
	}
	res, err := sugiyama.Layout(gd, nodes, edges, clusters, sugiyama.Config{
		Dir: gr.Dir, NodeSep: a.nodeSep, RankSep: rankSep, ClusterPad: clusterPad,
	})
	if err != nil {
		return nil, 0, err
	}
	// The boxes alone span this many columns; however the edges are
	// routed the drawing is not narrower, so a span wider than maxCols
	// skips routing, the expensive part. (The layout's total width is no
	// such bound: room it reserves for edge labels may stay unused.)
	// Boxes are placed as boxOf below does: the same integer arithmetic
	// gives the exact span of the drawn boxes.
	lo, hi := math.MaxInt, math.MinInt
	for l, nd := range nodes {
		w := int(nd.W)
		left := int(math.Round(res.Nodes[l].X)) - w/2
		if gr.Nodes[l].Glyph != 0 {
			// A glyph node takes 3x3 cells in the layout but draws only
			// its middle one.
			left, w = left+w/2, 1
		}
		lo, hi = min(lo, left), max(hi, left+w)
	}
	if span := hi - lo; len(nodes) > 0 && span > maxCols {
		return nil, span, nil
	}
	// The margin leaves room to route edges around the outermost nodes.
	const margin = 4
	if !(float64(res.Width*res.Height) <= textgrid.MaxCells) {
		return nil, 0, guard.Limit("text cells", textgrid.MaxCells+1, textgrid.MaxCells)
	}
	grid, err := textgrid.New(int(math.Ceil(res.Width))+2*margin, int(math.Ceil(res.Height))+2*margin)
	if err != nil {
		return nil, 0, err
	}
	rt := newRouter(gd, grid.W, grid.H, work)
	round := func(p scene.Point) (int, int) {
		return int(math.Round(p.X)) + margin, int(math.Round(p.Y)) + margin
	}
	boxOf := func(l int) box {
		c := res.Nodes[l]
		w, h := int(nodes[l].W), int(nodes[l].H)
		cx, cy := round(c)
		return box{cx - w/2, cy - h/2, w, h}
	}
	clusterBox := func(c int) box {
		r := res.Clusters[c]
		x, y := round(scene.Point{X: r.X, Y: r.Y})
		x2, y2 := round(scene.Point{X: r.X + r.W, Y: r.Y + r.H})
		return box{x, y, x2 - x + 1, y2 - y + 1}
	}
	// titleEnd is the last column of each cluster's title; ends on the top
	// frame must not cover it.
	titleEnd := make([]int, len(gr.Clusters))
	// Clusters.
	for i, r := range res.Clusters {
		if r.W <= 0 {
			continue
		}
		x, y := round(scene.Point{X: r.X, Y: r.Y})
		x2, y2 := round(scene.Point{X: r.X + r.W, Y: r.Y + r.H})
		grid.HLine(x, x2, y)
		grid.HLine(x, x2, y2)
		grid.VLine(x, y, y2)
		grid.VLine(x2, y, y2)
		title := ""
		if t := strings.ReplaceAll(gr.Clusters[i].Title, "\n", " "); t != "" {
			title = " " + t + " "
		}
		grid.Text(x+2, y, title)
		titleEnd[i] = x + 1 + textgrid.Width(title)
		rt.blockCluster(x, y, x2, y2, x+2, titleEnd[i])
	}
	for l := range nodes {
		rt.blockBox(boxOf(l))
	}

	type head struct {
		x, y int
		r    rune
	}
	var heads []head
	// glyphEnd reports whether an end of e is at a glyph node.
	glyphEnd := func(e Edge, start bool) bool {
		if start {
			return e.FromCluster < 0 && gr.Nodes[e.From].Glyph != 0
		}
		return e.ToCluster < 0 && gr.Nodes[e.To].Glyph != 0
	}
	// Edges between a node and the cluster containing it go straight to
	// the cluster border.
	for _, e := range gr.Edges {
		c := e.OwnCluster
		if e.Hidden || c < 0 || e.From != e.To {
			continue
		}
		r := res.Clusters[c]
		b := boxOf(e.From)
		bx, by := round(scene.Point{X: r.X, Y: r.Y})
		bx2, by2 := round(scene.Point{X: r.X + r.W, Y: r.Y + r.H})
		var path []cell
		d := dirDown
		switch gr.Dir {
		case sugiyama.TB:
			for y := b.y + b.h - 1; y <= by2; y++ {
				path = append(path, cell{b.cx(), y})
			}
		case sugiyama.BT:
			d = dirUp
			for y := b.y; y >= by; y-- {
				path = append(path, cell{b.cx(), y})
			}
		case sugiyama.LR:
			d = dirRight
			for x := b.x + b.w - 1; x <= bx2; x++ {
				path = append(path, cell{x, b.cy()})
			}
		default:
			d = dirLeft
			for x := b.x; x >= bx; x-- {
				path = append(path, cell{x, b.cy()})
			}
		}
		for k := 0; k+1 < len(path); k++ {
			grid.Join(path[k].x, path[k].y, path[k+1].x, path[k+1].y)
		}
		// The arrow head is drawn only when there is a free cell between
		// the node and the border; otherwise the borders are joined.
		if len(path) > 2 {
			p := path[len(path)-2]
			heads = append(heads, head{p.x, p.y, Arrow.Runes[d]})
		}
		rt.commit(path, 0, 0, 0, 0)
	}

	// Route the remaining edges in this order:
	//  1. straight forward edges: they become trunks others branch off;
	//  2. other forward edges (along the chart's direction), widest first:
	//     in a fan-out or fan-in the widest edge lays down a long
	//     cross-axis stretch that the others then join like a bus;
	//  3. back edges and edges within a rank, shortest first: they go
	//     around what is already there.
	order := make([]int, 0, len(gr.Edges))
	for i, e := range gr.Edges {
		if !e.Hidden && !(e.OwnCluster >= 0 && e.From == e.To) {
			order = append(order, i)
		}
	}
	type rank struct{ back, key int }
	keyOf := func(i int) rank {
		a, b := boxOf(edges[i].From), boxOf(edges[i].To)
		main, cross := b.cy()-a.cy(), abs(a.cx()-b.cx())
		if horiz {
			main, cross = b.cx()-a.cx(), abs(a.cy()-b.cy())
		}
		if gr.Dir == sugiyama.BT || gr.Dir == sugiyama.RL {
			main = -main
		}
		switch {
		case main > 0 && cross == 0:
			return rank{0, main} // straight: the trunk others branch off
		case main > 0:
			return rank{1, -cross}
		}
		return rank{2, abs(main) + cross}
	}
	sort.SliceStable(order, func(a, b int) bool {
		ka, kb := keyOf(order[a]), keyOf(order[b])
		if ka.back != kb.back {
			return ka.back < kb.back
		}
		return ka.key < kb.key
	})
	type routed struct {
		i                    int
		path                 []cell
		joinedFrom, joinedTo bool
	}
	var drawn []routed
	var labelAt []labelPlace
	// endsAt lists the ends of node box b, or of cluster c's frame (minus
	// its title) when c >= 0.
	endsAt := func(b box, c, toward int, other box) []edgeEnd {
		es := rt.ends(b, toward, other)
		if c < 0 {
			return es
		}
		out := es[:0:0]
		for _, e := range es {
			if !(e.port.y == b.y && e.port.x <= titleEnd[c]) {
				out = append(out, e)
			}
		}
		return out
	}
	for _, i := range order {
		e := gr.Edges[i]
		fb, tb := boxOf(edges[i].From), boxOf(edges[i].To)
		if e.FromCluster >= 0 {
			fb = clusterBox(e.FromCluster)
		}
		if e.ToCluster >= 0 {
			tb = clusterBox(e.ToCluster)
		}
		var from, to []edgeEnd
		inKey := treeKey(edges[i].To, true, e.End.Key)
		outKey := treeKey(edges[i].From, false, e.Start.Key)
		if edges[i].From == edges[i].To && e.FromCluster < 0 && e.ToCluster < 0 {
			// Self loop: out of the right side, back in through the top.
			from = sideOnly(rt.ends(fb, dirRight, fb), dirRight)
			to = sideOnly(rt.ends(tb, dirUp, tb), dirUp)
			inKey, outKey = 0, 0
		} else {
			from = append(endsAt(fb, e.FromCluster, toward(gr.Dir, fb, tb), tb), rt.joinEnds(outKey)...)
			to = append(endsAt(tb, e.ToCluster, toward(gr.Dir, tb, fb), fb), rt.joinEnds(inKey)...)
		}
		win := [4]int{
			min(fb.x, tb.x) - routeMargin, min(fb.y, tb.y) - routeMargin,
			max(fb.x+fb.w, tb.x+tb.w) + routeMargin, max(fb.y+fb.h, tb.y+tb.h) + routeMargin,
		}
		var guide []cell
		for _, p := range res.Edges[i] {
			x, y := round(p)
			guide = append(guide, cell{x, y})
		}
		guide = guideCells(guide)
		for _, c := range guide {
			win[0], win[1] = min(win[0], c.x-routeMargin), min(win[1], c.y-routeMargin)
			win[2], win[3] = max(win[2], c.x+routeMargin), max(win[3], c.y+routeMargin)
		}
		// The middle of the gap between the boxes along the main axis.
		mid := -1
		if horiz {
			if lo, hi := min(fb.x+fb.w, tb.x+tb.w), max(fb.x, tb.x); hi > lo {
				mid = (lo + hi) / 2
			}
		} else if lo, hi := min(fb.y+fb.h, tb.y+tb.h), max(fb.y, tb.y); hi > lo {
			mid = (lo + hi) / 2
		}
		path, err := rt.route(from, to, guide, win, horiz, mid)
		if err != nil {
			return nil, 0, err
		}
		if path == nil {
			if path, err = rt.route(from, to, guide, [4]int{0, 0, grid.W - 1, grid.H - 1}, horiz, mid); err != nil {
				return nil, 0, err
			}
		}
		if path == nil {
			// Boxed in by other lines: allow overlapping them rather than
			// lose the edge. Lines still never pass through boxes.
			rt.relaxed = true
			if edges[i].From == edges[i].To && e.FromCluster < 0 && e.ToCluster < 0 {
				from, to = sideOnly(rt.ends(fb, dirRight, fb), dirRight), sideOnly(rt.ends(tb, dirUp, tb), dirUp)
			} else {
				from, to = endsAt(fb, e.FromCluster, toward(gr.Dir, fb, tb), tb), endsAt(tb, e.ToCluster, toward(gr.Dir, tb, fb), fb)
			}
			path, err = rt.route(from, to, guide, [4]int{0, 0, grid.W - 1, grid.H - 1}, horiz, mid)
			rt.relaxed = false
			if err != nil {
				return nil, 0, err
			}
		}
		if path == nil {
			continue // every border cell is covered: nothing sensible to draw
		}
		n := len(path)
		// An end that joined another line of its tree shares that line's
		// arrow head; only ends at a box get their own.
		joinedFrom, joinedTo := rt.isLine(path[0]), rt.isLine(path[n-1])
		// The label goes on first, so that only the line beyond it is
		// offered to other edges.
		inFrom, outTo := 0, n
		if len(elabels[i]) > 0 {
			lx, ly := round(res.Labels[i])
			pl := rt.placeLabel(path, elabels[i], cell{lx, ly}, e.Start.Key != 0 && !joinedFrom, e.End.Key != 0 && !joinedTo, edges[i].From == edges[i].To)
			labelAt = append(labelAt, pl)
			if pl.first < 0 {
				inFrom, outTo = n, -1 // beside the line: share none of it
			} else {
				// Leave a line cell between the label and any junction.
				inFrom, outTo = pl.last+2, pl.first-2
			}
		}
		rt.commit(path, inKey, outKey, inFrom, outTo)
		// A head stands in front of a box border; at a glyph, which has
		// no border, it takes the port cell next to the glyph.
		if e.End.Key != 0 && !joinedTo {
			p, q := path[n-2], path[n-1]
			at := p
			if glyphEnd(e, false) {
				at = q
			}
			heads = append(heads, head{at.x, at.y, e.End.Runes[dirOf(p, q)]})
			rt.mark(at.x, at.y, cellBlocked)
		}
		if e.Start.Key != 0 && !joinedFrom {
			p, q := path[1], path[0]
			at := p
			if glyphEnd(e, true) {
				at = q
			}
			heads = append(heads, head{at.x, at.y, e.Start.Runes[dirOf(p, q)]})
			rt.mark(at.x, at.y, cellBlocked)
		}
		drawn = append(drawn, routed{i, path, joinedFrom, joinedTo})
	}
	for _, d := range drawn {
		// A head stands in front of the border; the line does not touch
		// the box there, so the border shows no junction under it.
		e := gr.Edges[d.i]
		lo, hi := 0, len(d.path)-1
		if e.Start.Key != 0 && !d.joinedFrom && !glyphEnd(e, true) {
			lo = 1
		}
		if e.End.Key != 0 && !d.joinedTo && !glyphEnd(e, false) {
			hi--
		}
		for k := lo; k < hi; k++ {
			grid.Join(d.path[k].x, d.path[k].y, d.path[k+1].x, d.path[k+1].y)
		}
	}
	// Dashed and heavy lines: straight cells only, so that junctions with
	// other edges still show.
	for _, d := range drawn {
		var h, v rune
		switch gr.Edges[d.i].Style {
		case Dashed:
			h, v = '┄', '┆'
		case Heavy:
			h, v = '━', '┃'
		default:
			continue
		}
		for _, c := range d.path {
			grid.Restyle(c.x, c.y, h, v)
		}
	}
	// Nodes on top of edges; Box keeps the line bits on its border, which
	// shows where each edge leaves.
	for l, n := range gr.Nodes {
		b := boxOf(l)
		if n.Glyph != 0 {
			// The ports are on the (undrawn) border; clear them so that
			// lines stop next to the glyph.
			grid.Set(b.cx(), b.cy(), n.Glyph)
			continue
		}
		grid.Box(b.x, b.y, b.w, b.h, n.Corners)
		nt := texts[l]
		top := b.y + 1
		if len(nt.sections) == 0 {
			top = b.y + (b.h-len(nt.label))/2
		}
		for k, line := range nt.label {
			grid.Text(b.x+(b.w-textgrid.Width(line))/2, top+k, line)
		}
		y := top + len(nt.label)
		for _, s := range nt.sections {
			grid.HLine(b.x, b.x+b.w-1, y)
			for k, line := range s {
				grid.Text(b.x+2, y+1+k, line)
			}
			y += len(s) + 1
		}
	}
	for _, hd := range heads {
		grid.Set(hd.x, hd.y, hd.r)
	}
	for _, pl := range labelAt {
		for k, line := range pl.lines {
			// Clear the row first so a line under the label does not show
			// between wide characters.
			for x, xe := pl.x, pl.x+textgrid.Width(line); x < xe; x++ {
				grid.Set(x, pl.y+k, ' ')
			}
			grid.Text(pl.x, pl.y+k, line)
		}
	}
	lines := textgrid.TrimLeft(grid.Lines())
	return lines, textgrid.MaxWidth(lines), nil
}

// toward returns the direction from box a toward box b along the chart's
// main axis, or along the cross axis when both are in the same rank.
func toward(dir sugiyama.Dir, a, b box) int {
	if dir.Horizontal() {
		switch {
		case b.cx() > a.cx()+a.w/2:
			return dirRight
		case b.cx() < a.cx()-a.w/2:
			return dirLeft
		case b.cy() >= a.cy():
			return dirDown
		}
		return dirUp
	}
	switch {
	case b.cy() > a.cy()+a.h/2:
		return dirDown
	case b.cy() < a.cy()-a.h/2:
		return dirUp
	case b.cx() >= a.cx():
		return dirRight
	}
	return dirLeft
}

func sideOnly(es []edgeEnd, d int) []edgeEnd {
	out := es[:0:0]
	for _, e := range es {
		if e.dir == d {
			out = append(out, e)
		}
	}
	return out
}

// dirOf is the direction of the step from p to its neighbour q.
func dirOf(p, q cell) int {
	switch {
	case q.y < p.y:
		return dirUp
	case q.y > p.y:
		return dirDown
	case q.x < p.x:
		return dirLeft
	}
	return dirRight
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// maxRouteWork bounds the search steps of the edge router in one Text call
// (all attempts together). The test and golden diagrams use at most about
// a million; a diagram that needs more than eight times that would take
// seconds to draw as text, so it fails with ErrTooLarge instead (the
// image output is the better fallback for it).
const maxRouteWork = 1 << 23
