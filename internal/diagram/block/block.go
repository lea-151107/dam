// Package block parses and lays out Mermaid block diagrams (block-beta):
// blocks placed in a grid of columns, with nested blocks and edges.
package block

import (
	"errors"
	"fmt"
	"image/color"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/lea/mermaid/internal/diagram"
	"github.com/lea/mermaid/internal/guard"
	"github.com/lea/mermaid/internal/scene"
	"github.com/lea/mermaid/internal/source"
	"github.com/lea/mermaid/internal/textgrid"
	"github.com/lea/mermaid/internal/theme"
)

// Shape is a block outline.
type Shape uint8

// Shapes.
const (
	Rect Shape = iota
	Round
	Stadium
	Circle
	Hexagon
	Rhombus
	Cylinder
	Subroutine
	Asymmetric
	Parallelogram
	Trapezoid
	Arrow // block arrow; see Item.ArrowDir
	Space
	Group // nested block
)

// Item is a cell of a grid: a block, an empty space or a nested group.
type Item struct {
	ID       string
	Label    string
	Shape    Shape
	Span     int
	ArrowDir string // right, left, up, down, x, y
	Columns  int    // for groups: -1 = one row
	Items    []*Item
	Classes  []string
	Style    theme.Style
}

// Edge joins two blocks.
type Edge struct {
	From, To string
	Label    string
	Arrow    bool
	line     int // source line, for errors
}

// Diagram is a parsed block diagram.
type Diagram struct {
	Root      *Item
	Edges     []Edge
	byID      map[string]*Item
	classDefs map[string]theme.Style
}

// maxDepth bounds block nesting; maxSpan bounds spans and columns.
const (
	maxDepth = 16
	maxSpan  = 64
)

var (
	edgeRe     = regexp.MustCompile(`^(\S+)\s*(?:--\s*"([^"]*)"\s*)?(-->|---)\s*(\S+)$`)
	classDefRe = regexp.MustCompile(`^classDef\s+(\S+)\s+(.*)$`)
	classRe    = regexp.MustCompile(`^class\s+(\S+)\s+([\w-]+)$`)
	styleRe    = regexp.MustCompile(`^style\s+(\S+)\s+(.*)$`)
)

// shapes are the label brackets, longest first.
var shapes = []struct {
	open, close string
	shape       Shape
}{
	{"((", "))", Circle}, {"([", "])", Stadium}, {"[(", ")]", Cylinder}, {"[[", "]]", Subroutine},
	{"{{", "}}", Hexagon}, {"[/", "/]", Parallelogram}, {"[/", `\]`, Trapezoid},
	{"(", ")", Round}, {"[", "]", Rect}, {"{", "}", Rhombus}, {">", "]", Asymmetric},
}

// Parse parses a block diagram.
func Parse(g *guard.Guard, lines []source.Line) (*Diagram, error) {
	d := &Diagram{Root: &Item{Shape: Group, Columns: -1, Span: 1}, byID: map[string]*Item{}, classDefs: map[string]theme.Style{}}
	stack := []*Item{d.Root}
	var stackLine []int
	count := 0
	for _, l := range lines[1:] {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		t := strings.TrimSpace(l.Text)
		cur := stack[len(stack)-1]
		switch {
		case strings.HasPrefix(t, "accTitle") || strings.HasPrefix(t, "accDescr"):
			// NOTE(mermaid-compat): accessibility texts do not affect the image.
			continue
		case t == "end":
			if len(stack) == 1 {
				return nil, source.Errorf(l.No, "end without block")
			}
			stack, stackLine = stack[:len(stack)-1], stackLine[:len(stackLine)-1]
			continue
		}
		if r, ok := source.CutKeyword(t, "columns"); ok {
			n, err := strconv.Atoi(r)
			if r == "auto" {
				n, err = -1, nil
			}
			if (err == nil && n > maxSpan) || overflow(err, r) {
				return nil, source.TooLarge(l.No, "%s columns exceed limit %d", r, maxSpan)
			}
			if err != nil || n == 0 {
				return nil, source.Errorf(l.No, "invalid columns %q", r)
			}
			cur.Columns = n
			continue
		}
		if m := classDefRe.FindStringSubmatch(t); m != nil {
			for _, n := range strings.Split(m[1], ",") {
				d.classDefs[n] = d.classDefs[n].Merge(theme.ParseStyle(m[2]))
			}
			continue
		}
		if m := classRe.FindStringSubmatch(t); m != nil {
			for _, id := range strings.Split(m[1], ",") {
				if it := d.byID[id]; it != nil {
					it.Classes = append(it.Classes, m[2])
				}
			}
			continue
		}
		if m := styleRe.FindStringSubmatch(t); m != nil {
			it := d.byID[m[1]]
			if it == nil {
				return nil, source.Errorf(l.No, "style for unknown block %q", m[1])
			}
			it.Style = it.Style.Merge(theme.ParseStyle(m[2]))
			continue
		}
		if m := edgeRe.FindStringSubmatch(t); m != nil {
			d.Edges = append(d.Edges, Edge{From: m[1], To: m[4], Label: source.Label(m[2]), Arrow: m[3] == "-->", line: l.No})
			if err := g.Count("edges", len(d.Edges)); err != nil {
				return nil, err
			}
			continue
		}
		if r, ok := source.CutKeyword(t, "block"); ok || t == "block" || strings.HasPrefix(t, "block:") {
			if !ok {
				r = strings.TrimPrefix(t, "block")
			}
			if len(stack) > maxDepth {
				return nil, source.TooLarge(l.No, "blocks nested too deeply")
			}
			grp := &Item{Shape: Group, Columns: -1, Span: 1}
			parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(r), ":"), ":")
			if len(parts) > 0 && parts[0] != "" {
				grp.ID = parts[0]
				d.byID[grp.ID] = grp
			}
			if len(parts) > 1 {
				n, err := strconv.Atoi(parts[1])
				if (err == nil && n > maxSpan) || overflow(err, parts[1]) {
					return nil, source.TooLarge(l.No, "span %s exceeds limit %d", parts[1], maxSpan)
				}
				if err != nil || n < 1 {
					return nil, source.Errorf(l.No, "invalid span %q", parts[1])
				}
				grp.Span = n
			}
			cur.Items = append(cur.Items, grp)
			stack = append(stack, grp)
			stackLine = append(stackLine, l.No)
			count++
			if err := g.Count("blocks", count); err != nil {
				return nil, err
			}
			continue
		}
		items, err := d.items(t)
		if errors.Is(err, guard.ErrTooLarge) {
			return nil, source.TooLarge(l.No, "%s", strings.TrimPrefix(err.Error(), guard.ErrTooLarge.Error()+": "))
		}
		if err != nil {
			return nil, source.Errorf(l.No, "%v", err)
		}
		cur.Items = append(cur.Items, items...)
		count += len(items)
		if err := g.Count("blocks", count); err != nil {
			return nil, err
		}
	}
	if len(stack) > 1 {
		return nil, source.Errorf(stackLine[len(stackLine)-1], "block is not closed with end")
	}
	for _, e := range d.Edges {
		for _, id := range []string{e.From, e.To} {
			if d.byID[id] == nil {
				return nil, source.Errorf(e.line, "edge to unknown block %q", id)
			}
		}
	}
	for _, it := range d.byID {
		st := d.classDefs["default"]
		for _, c := range it.Classes {
			st = st.Merge(d.classDefs[c])
		}
		it.Style = st.Merge(it.Style)
	}
	return d, nil
}

// items parses a line of blocks: `a b["Label"]:2 space:3 c(("x"))`.
func (d *Diagram) items(t string) ([]*Item, error) {
	var out []*Item
	for t = strings.TrimSpace(t); t != ""; t = strings.TrimSpace(t) {
		it, rest, err := d.item(t)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
		t = rest
	}
	return out, nil
}

func (d *Diagram) item(t string) (*Item, string, error) {
	end := strings.IndexFunc(t, func(r rune) bool {
		return r == ' ' || r == '[' || r == '(' || r == '{' || r == '>' || r == '<' || r == ':'
	})
	if end < 0 {
		end = len(t)
	}
	id := t[:end]
	t = t[end:]
	it := &Item{ID: id, Label: source.Label(id), Span: 1}
	if id == "space" {
		it.Shape = Space
	}
	if id == "" {
		return nil, "", fmt.Errorf("expected a block id at %q", t)
	}
	if strings.HasPrefix(t, "<[") {
		// Block arrow: id<["Label"]>(right)
		k := strings.Index(t, "]>")
		if k < 0 {
			return nil, "", fmt.Errorf("block arrow is not closed with ]>")
		}
		it.Shape, it.Label = Arrow, source.Label(t[2:k])
		t = t[k+2:]
		if strings.HasPrefix(t, "(") {
			c := strings.IndexByte(t, ')')
			if c < 0 {
				return nil, "", fmt.Errorf("arrow direction is not closed with )")
			}
			it.ArrowDir = strings.TrimSpace(t[1:c])
			t = t[c+1:]
		}
		switch it.ArrowDir {
		case "", "right", "left", "up", "down", "x", "y":
		default:
			return nil, "", fmt.Errorf("unknown arrow direction %q", it.ArrowDir)
		}
	} else {
		for _, s := range shapes {
			if !strings.HasPrefix(t, s.open) {
				continue
			}
			body := t[len(s.open):]
			// A quoted label may contain the closing bracket.
			start := 0
			if strings.HasPrefix(body, `"`) {
				if q := strings.IndexByte(body[1:], '"'); q >= 0 {
					start = q + 2
				}
			}
			k := strings.Index(body[start:], s.close)
			if k < 0 {
				continue
			}
			it.Shape, it.Label = s.shape, source.Label(body[:start+k])
			t = body[start+k+len(s.close):]
			break
		}
	}
	if strings.HasPrefix(t, ":") {
		n := 1
		for n < len(t) && t[n] >= '0' && t[n] <= '9' {
			n++
		}
		span, err := strconv.Atoi(t[1:n])
		if (err == nil && span > maxSpan) || overflow(err, t[1:n]) {
			return nil, "", fmt.Errorf("%w: span %s exceeds limit %d", guard.ErrTooLarge, t[1:n], maxSpan)
		}
		if err != nil || span < 1 {
			return nil, "", fmt.Errorf("invalid span %q", t[:n])
		}
		it.Span, t = span, t[n:]
	}
	if it.Shape != Space {
		d.byID[id] = it
	}
	return it, t, nil
}

// Visual constants in layout units.
const (
	padX     = 14.0
	padY     = 10.0
	gap      = 10.0
	groupPad = 10.0
	lineW    = 1.3
	radius   = 5.0
	arrowSz  = 10.0
	minCell  = 40.0
)

// cell is a laid out item: its size, and for groups the grid inside.
type cell struct {
	it    *Item
	lines []string
	w, h  float64
	kids  []*cell
	cols  int
	colW  float64
	rowH  []float64
	place [][2]int // column and row of each kid
}

// columns returns the number of grid columns of a group.
func columns(it *Item) int {
	if it.Columns > 0 {
		return it.Columns
	}
	n := 0
	for _, k := range it.Items {
		n += k.Span
	}
	return max(n, 1)
}

// size computes the natural size of an item (bottom-up).
func size(env *diagram.Env, it *Item, depth int) (*cell, error) {
	if err := env.G.Tick(); err != nil {
		return nil, err
	}
	c := &cell{it: it}
	if it.Shape != Group {
		if it.Shape != Space {
			c.lines = env.Label(it.Label)
		}
		tw, th := env.M.Size(c.lines)
		th = max(th, env.M.LineHeight())
		c.w, c.h = tw+2*padX, th+2*padY
		switch it.Shape {
		case Circle:
			d := max(c.w, c.h)
			c.w, c.h = d, d
		case Rhombus:
			c.w, c.h = tw+th+2*padY, tw+th+2*padY
		case Hexagon, Parallelogram, Trapezoid:
			c.w += float64(c.h / 2)
		case Cylinder:
			c.h += 12
		case Arrow:
			c.w += 2 * arrowSz
			if it.ArrowDir == "up" || it.ArrowDir == "down" || it.ArrowDir == "y" {
				c.w, c.h = c.h+2*arrowSz, c.w
			}
		case Space:
			c.w, c.h = minCell, minCell
		}
		return c, nil
	}
	c.cols = columns(it)
	col, row := 0, 0
	var rowH []float64
	for _, k := range it.Items {
		kc, err := size(env, k, depth+1)
		if err != nil {
			return nil, err
		}
		span := min(k.Span, c.cols)
		if col+span > c.cols {
			col, row = 0, row+1
		}
		for len(rowH) <= row {
			rowH = append(rowH, 0)
		}
		rowH[row] = max(rowH[row], kc.h)
		c.colW = max(c.colW, float64((kc.w-float64(float64(span-1)*gap))/float64(span)))
		c.kids = append(c.kids, kc)
		c.place = append(c.place, [2]int{col, row})
		col += span
		if col >= c.cols {
			col, row = 0, row+1
		}
	}
	c.rowH = rowH
	c.w = float64(float64(c.cols)*c.colW) + float64(float64(c.cols-1)*gap)
	c.h = 0
	for _, h := range rowH {
		c.h += h
	}
	c.h += float64(float64(max(len(rowH)-1, 0)) * gap)
	if depth > 0 {
		c.w += 2 * groupPad
		c.h += 2 * groupPad
	}
	return c, nil
}

// placeAll assigns rectangles to the items of group c drawn in r.
func placeAll(c *cell, r scene.Rect, depth int, out map[*Item]scene.Rect) {
	out[c.it] = r
	if c.it.Shape != Group {
		return
	}
	inner := r
	if depth > 0 {
		inner = scene.Rect{X: r.X + groupPad, Y: r.Y + groupPad, W: r.W - 2*groupPad, H: r.H - 2*groupPad}
	}
	colW := float64((inner.W - float64(float64(c.cols-1)*gap)) / float64(c.cols))
	ys := make([]float64, len(c.rowH))
	y := inner.Y
	for i, h := range c.rowH {
		ys[i] = y
		y += h + gap
	}
	for i, k := range c.kids {
		col, row := c.place[i][0], c.place[i][1]
		span := min(k.it.Span, c.cols)
		x := inner.X + float64(float64(col)*(colW+gap))
		w := float64(float64(span)*colW) + float64(float64(span-1)*gap)
		rr := scene.Rect{X: x, Y: ys[row], W: w, H: c.rowH[row]}
		if k.it.Shape != Group {
			// Blocks keep their height and fill their span's width.
			rr = scene.Rect{X: x, Y: ys[row] + float64((c.rowH[row]-k.h)/2), W: w, H: k.h}
			if k.it.Shape == Circle || k.it.Shape == Rhombus {
				rr = scene.Rect{X: x + float64((w-k.w)/2), Y: rr.Y, W: k.w, H: k.h}
			}
		}
		placeAll(k, rr, depth+1, out)
	}
}

// Layout implements diagram.Layouter.
func (d *Diagram) Layout(env *diagram.Env) (*scene.Scene, error) {
	root, err := size(env, d.Root, 0)
	if err != nil {
		return nil, err
	}
	rects := map[*Item]scene.Rect{}
	placeAll(root, scene.Rect{W: root.w, H: root.h}, 0, rects)
	t := env.T
	sc := &scene.Scene{}
	var draw func(c *cell, depth int)
	draw = func(c *cell, depth int) {
		r := rects[c.it]
		fill, stroke, text := t.NodeFill, t.NodeStroke, t.Text
		if c.it.Style.Fill != nil {
			fill = *c.it.Style.Fill
		}
		if c.it.Style.Stroke != nil {
			stroke = *c.it.Style.Stroke
		}
		if c.it.Style.Color != nil {
			text = *c.it.Style.Color
		} else {
			text = theme.Readable(text, fill)
		}
		switch c.it.Shape {
		case Group:
			if depth > 0 {
				sc.Fill(scene.RoundRect(r, radius), theme.Mix(t.Base, t.ClusterFill, 0.9), t.ClusterStroke, lineW)
			}
			for _, k := range c.kids {
				draw(k, depth+1)
			}
			return
		case Space:
			return
		}
		sc.Add(&scene.Shape{Path: outline(c.it, r), Fill: fill, Stroke: scene.Stroke{Color: stroke, Width: lineW}})
		if c.it.Shape == Cylinder {
			k := 0.5522847498
			ry := 6.0
			front := scene.NewPath().MoveTo(r.X, r.Y+ry)
			front.CubeTo(r.X, r.Y+ry+float64(ry*k), r.X+float64(r.W/2)-float64(r.W/2*k), r.Y+float64(2*ry), r.X+float64(r.W/2), r.Y+float64(2*ry))
			front.CubeTo(r.X+float64(r.W/2)+float64(r.W/2*k), r.Y+float64(2*ry), r.X+r.W, r.Y+ry+float64(ry*k), r.X+r.W, r.Y+ry)
			sc.Line(front, stroke, lineW, nil)
		}
		if c.it.Shape == Subroutine {
			for _, x := range []float64{r.X + 8, r.X + r.W - 8} {
				sc.Line(scene.Polyline([]scene.Point{{X: x, Y: r.Y}, {X: x, Y: r.Y + r.H}}), stroke, lineW, nil)
			}
		}
		cy := r.Y + float64(r.H/2)
		if c.it.Shape == Cylinder {
			cy += 6
		}
		sc.Label(r.X+float64(r.W/2), cy, c.lines, scene.Center, text)
	}
	draw(root, 0)
	// Edges: straight between the borders, with the label at the middle.
	for _, e := range d.Edges {
		a, b := rects[d.byID[e.From]], rects[d.byID[e.To]]
		pts := []scene.Point{a.Center(), b.Center()}
		pts = scene.ClipEndRect(scene.ClipStartRect(pts, a), b)
		if len(pts) < 2 || pts[0] == pts[1] {
			continue
		}
		line := pts
		if e.Arrow {
			line = scene.Shorten(pts, scene.MarkerLen(scene.Arrow, arrowSz))
		}
		sc.Line(scene.Polyline(line), t.Line, lineW, nil)
		if e.Arrow {
			sc.DrawMarker(scene.Arrow, pts[1], pts[0], arrowSz, t.Line, t.Base, lineW)
		}
		if e.Label != "" {
			mid := scene.MidPoint(pts)
			lines := env.Label(e.Label)
			tw, th := env.M.Size(lines)
			sc.Fill(scene.RectPath(scene.Rect{X: mid.X - float64(tw/2) - 3, Y: mid.Y - float64(th/2) - 1, W: tw + 6, H: th + 2}), t.LabelBg, color.NRGBA{}, 0)
			sc.Label(mid.X, mid.Y, lines, scene.Center, t.Text)
		}
	}
	return diagram.Finish(sc, env, root.w, root.h), nil
}

// outline returns the path of a block's shape in r.
func outline(it *Item, r scene.Rect) *scene.Path {
	x, y, w, h := r.X, r.Y, r.W, r.H
	c := r.Center()
	switch it.Shape {
	case Round:
		return scene.RoundRect(r, radius*1.5)
	case Stadium:
		return scene.RoundRect(r, float64(h/2))
	case Circle:
		return scene.Ellipse(c, float64(w/2), float64(h/2))
	case Rhombus:
		return scene.Polygon([]scene.Point{{X: c.X, Y: y}, {X: x + w, Y: c.Y}, {X: c.X, Y: y + h}, {X: x, Y: c.Y}})
	case Hexagon:
		k := float64(h / 4)
		return scene.Polygon([]scene.Point{{X: x + k, Y: y}, {X: x + w - k, Y: y}, {X: x + w, Y: c.Y}, {X: x + w - k, Y: y + h}, {X: x + k, Y: y + h}, {X: x, Y: c.Y}})
	case Parallelogram:
		k := float64(h / 2)
		return scene.Polygon([]scene.Point{{X: x + k, Y: y}, {X: x + w, Y: y}, {X: x + w - k, Y: y + h}, {X: x, Y: y + h}})
	case Trapezoid:
		k := float64(h / 2)
		return scene.Polygon([]scene.Point{{X: x + k, Y: y}, {X: x + w - k, Y: y}, {X: x + w, Y: y + h}, {X: x, Y: y + h}})
	case Asymmetric:
		return scene.Polygon([]scene.Point{{X: x, Y: y}, {X: x + w, Y: y}, {X: x + w, Y: y + h}, {X: x, Y: y + h}, {X: x + float64(h/4), Y: c.Y}})
	case Cylinder:
		ry := 6.0
		k := 0.5522847498
		p := scene.NewPath().MoveTo(x, y+ry).LineTo(x, y+h-ry)
		p.CubeTo(x, y+h-ry+float64(ry*k), c.X-float64(w/2*k), y+h, c.X, y+h)
		p.CubeTo(c.X+float64(w/2*k), y+h, x+w, y+h-ry+float64(ry*k), x+w, y+h-ry)
		p.LineTo(x+w, y+ry)
		p.CubeTo(x+w, y+ry-float64(ry*k), c.X+float64(w/2*k), y, c.X, y)
		p.CubeTo(c.X-float64(w/2*k), y, x, y+ry-float64(ry*k), x, y+ry)
		return p.Close()
	case Arrow:
		return arrowPath(it.ArrowDir, r)
	}
	return scene.RectPath(r)
}

// arrowPath draws a block arrow pointing in dir ("x" and "y" point both
// ways).
func arrowPath(dir string, r scene.Rect) *scene.Path {
	horizontal := dir != "up" && dir != "down" && dir != "y"
	// Work in a frame where the arrow points right, then map.
	L, T := r.W, r.H
	if !horizontal {
		L, T = r.H, r.W
	}
	head := math.Min(arrowSz*1.5, float64(L/3))
	shaft := float64(T * 0.25)
	pts := []scene.Point{{X: 0, Y: shaft}, {X: L - head, Y: shaft}, {X: L - head, Y: 0}, {X: L, Y: float64(T / 2)},
		{X: L - head, Y: T}, {X: L - head, Y: T - shaft}, {X: 0, Y: T - shaft}}
	if dir == "x" || dir == "y" {
		pts = []scene.Point{{X: head, Y: shaft}, {X: L - head, Y: shaft}, {X: L - head, Y: 0}, {X: L, Y: float64(T / 2)},
			{X: L - head, Y: T}, {X: L - head, Y: T - shaft}, {X: head, Y: T - shaft}, {X: head, Y: T}, {X: 0, Y: float64(T / 2)}, {X: head, Y: 0}}
	}
	out := make([]scene.Point, len(pts))
	for i, p := range pts {
		switch dir {
		case "left":
			out[i] = scene.Point{X: r.X + L - p.X, Y: r.Y + p.Y}
		case "up":
			out[i] = scene.Point{X: r.X + p.Y, Y: r.Y + L - p.X}
		case "down", "y":
			out[i] = scene.Point{X: r.X + p.Y, Y: r.Y + p.X}
		default:
			out[i] = scene.Point{X: r.X + p.X, Y: r.Y + p.Y}
		}
	}
	return scene.Polygon(out)
}

// Text renders the grid with box-drawing characters and lists the edges
// below it.
//
// NOTE(mermaid-compat): edges are listed as "a → b" instead of drawn.
func (d *Diagram) Text(g *guard.Guard, maxCols int) ([]string, error) {
	for _, wrap := range []int{0, 24, 16, 10, 6} {
		lines, err := d.text(g, wrap, maxCols)
		if err != nil {
			return nil, err
		}
		if textgrid.MaxWidth(lines) <= maxCols {
			return lines, nil
		}
		if wrap == 6 {
			return nil, guard.Limit("columns", textgrid.MaxWidth(lines), maxCols)
		}
	}
	return nil, nil
}

// tcell is a cell of the text grid.
type tcell struct {
	it         *Item
	lines      []string
	w, h       int
	kids       []*tcell
	cols, colW int
	rowH       []int
	place      [][2]int
}

func tsize(g *guard.Guard, it *Item, wrap, depth int) (*tcell, error) {
	if err := g.Tick(); err != nil {
		return nil, err
	}
	c := &tcell{it: it}
	if it.Shape != Group {
		if it.Shape != Space {
			c.lines = textgrid.Wrap(it.Label, wrap)
		}
		c.w, c.h = textgrid.MaxWidth(c.lines)+4, max(len(c.lines), 1)+2
		return c, nil
	}
	c.cols = columns(it)
	col, row := 0, 0
	for _, k := range it.Items {
		kc, err := tsize(g, k, wrap, depth+1)
		if err != nil {
			return nil, err
		}
		span := min(k.Span, c.cols)
		if col+span > c.cols {
			col, row = 0, row+1
		}
		for len(c.rowH) <= row {
			c.rowH = append(c.rowH, 0)
		}
		c.rowH[row] = max(c.rowH[row], kc.h)
		c.colW = max(c.colW, (kc.w-(span-1)+span-1)/span)
		c.kids = append(c.kids, kc)
		c.place = append(c.place, [2]int{col, row})
		col += span
		if col >= c.cols {
			col, row = 0, row+1
		}
	}
	c.w = c.cols*c.colW + c.cols - 1
	for _, h := range c.rowH {
		c.h += h
	}
	// Nested groups multiply widths (columns × the widest child): stop
	// before the numbers overflow.
	if c.w > textgrid.MaxCells || c.h > textgrid.MaxCells {
		return nil, guard.Limit("text cells", textgrid.MaxCells+1, textgrid.MaxCells)
	}
	if depth > 0 {
		c.w += 4
		c.h += 2
	}
	return c, nil
}

func (d *Diagram) text(g *guard.Guard, wrap, maxCols int) ([]string, error) {
	root, err := tsize(g, d.Root, wrap, 0)
	if err != nil {
		return nil, err
	}
	grid, err := textgrid.New(root.w+1, root.h+1)
	if err != nil {
		return nil, err
	}
	var draw func(c *tcell, x, y, w, h, depth int)
	draw = func(c *tcell, x, y, w, h, depth int) {
		switch c.it.Shape {
		case Space:
			return
		case Group:
			if depth > 0 {
				grid.Box(x, y, w, h, textgrid.Rounded)
				x, y, w, h = x+2, y+1, w-4, h-2
			}
			colW := (w - (c.cols - 1)) / c.cols
			ys := make([]int, len(c.rowH))
			yy := y
			for i, rh := range c.rowH {
				ys[i] = yy
				yy += rh
			}
			for i, k := range c.kids {
				col, row := c.place[i][0], c.place[i][1]
				span := min(k.it.Span, c.cols)
				kw := span*colW + span - 1
				kh := c.rowH[row]
				if k.it.Shape != Group {
					kh = k.h
				}
				draw(k, x+col*(colW+1), ys[row], kw, kh, depth+1)
			}
			return
		}
		var corners [4]rune
		if c.it.Shape == Round || c.it.Shape == Stadium || c.it.Shape == Circle || c.it.Shape == Cylinder {
			corners = textgrid.Rounded
		}
		grid.Box(x, y, w, h, corners)
		for k, l := range c.lines {
			grid.Text(x+(w-textgrid.Width(l))/2, y+1+k, l)
		}
	}
	draw(root, 0, 0, root.w, root.h, 0)
	out := grid.Lines()
	if len(d.Edges) > 0 {
		out = append(out, "")
		for _, e := range d.Edges {
			arrow := " → "
			if !e.Arrow {
				arrow = " ─ "
			}
			s := label(d.byID[e.From]) + arrow + label(d.byID[e.To])
			if e.Label != "" {
				s += ": " + e.Label
			}
			out = append(out, textgrid.Wrap(s, maxCols)...)
		}
	}
	return out, nil
}

func label(it *Item) string { return strings.ReplaceAll(it.Label, "\n", " ") }

// overflow reports whether err is strconv.Atoi's error for a positive
// number too large for int, which is a size limit (also on 32-bit
// platforms, where int ends at 2^31), not a syntax error.
func overflow(err error, s string) bool {
	return errors.Is(err, strconv.ErrRange) && !strings.HasPrefix(s, "-")
}
