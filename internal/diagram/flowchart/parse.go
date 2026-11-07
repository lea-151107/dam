// Package flowchart parses and lays out Mermaid flowcharts (graph / flowchart).
package flowchart

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/lea/mermaid/internal/guard"
	"github.com/lea/mermaid/internal/layout/sugiyama"
	"github.com/lea/mermaid/internal/scene"
	"github.com/lea/mermaid/internal/source"
	"github.com/lea/mermaid/internal/theme"
)

// Shape is a node shape.
type Shape uint8

// Shapes.
const (
	Rect Shape = iota
	Round
	Stadium
	Subroutine
	Cylinder
	Circle
	DoubleCircle
	Rhombus
	Hexagon
	Parallelogram
	ParallelogramAlt
	Trapezoid
	TrapezoidAlt
	Asymmetric
	// Shapes that only the Mermaid 11 syntax A@{ shape: ... } selects.
	Document
	Documents
	LinedDocument
	TaggedDocument
	Delay
	HCylinder
	LinedCylinder
	CurvedTrapezoid
	DividedRect
	Triangle
	FlippedTriangle
	WindowPane
	FilledCircle
	SmallCircle
	FramedCircle
	CrossedCircle
	NotchedPentagon
	SlopedRect
	StackedRect
	BowTieRect
	TextBlock
	Hourglass
	BraceLeft
	BraceRight
	Braces
	Bolt
	NotchedRect
	TaggedRect
	LinedRect
	Flag
)

// shapeNames maps the shape names and aliases of Mermaid 11
// (A@{ shape: name }) to shapes.
var shapeNames = map[string]Shape{
	"rect": Rect, "proc": Rect, "process": Rect, "rectangle": Rect,
	"rounded": Round, "event": Round,
	"stadium": Stadium, "pill": Stadium, "terminal": Stadium,
	"fr-rect": Subroutine, "subproc": Subroutine, "subprocess": Subroutine, "framed-rectangle": Subroutine, "subroutine": Subroutine,
	"cyl": Cylinder, "db": Cylinder, "database": Cylinder, "cylinder": Cylinder,
	"circle": Circle, "circ": Circle,
	"dbl-circ": DoubleCircle, "double-circle": DoubleCircle,
	"diam": Rhombus, "decision": Rhombus, "diamond": Rhombus, "question": Rhombus,
	"hex": Hexagon, "hexagon": Hexagon, "prepare": Hexagon,
	"lean-r": Parallelogram, "lean-right": Parallelogram, "in-out": Parallelogram,
	"lean-l": ParallelogramAlt, "lean-left": ParallelogramAlt, "out-in": ParallelogramAlt,
	"trap-b": Trapezoid, "priority": Trapezoid, "trapezoid-bottom": Trapezoid, "trapezoid": Trapezoid,
	"trap-t": TrapezoidAlt, "manual": TrapezoidAlt, "trapezoid-top": TrapezoidAlt, "inv-trapezoid": TrapezoidAlt,
	"odd": Asymmetric,
	"doc": Document, "document": Document,
	"docs": Documents, "documents": Documents, "st-doc": Documents, "stacked-document": Documents,
	"lin-doc": LinedDocument, "lined-document": LinedDocument,
	"tag-doc": TaggedDocument, "tagged-document": TaggedDocument,
	"delay": Delay, "half-rounded-rectangle": Delay,
	"h-cyl": HCylinder, "das": HCylinder, "horizontal-cylinder": HCylinder,
	"lin-cyl": LinedCylinder, "disk": LinedCylinder, "lined-cylinder": LinedCylinder,
	"curv-trap": CurvedTrapezoid, "curved-trapezoid": CurvedTrapezoid, "display": CurvedTrapezoid,
	"div-rect": DividedRect, "div-proc": DividedRect, "divided-process": DividedRect, "divided-rectangle": DividedRect,
	"tri": Triangle, "extract": Triangle, "triangle": Triangle,
	"flip-tri": FlippedTriangle, "flipped-triangle": FlippedTriangle, "manual-file": FlippedTriangle,
	"win-pane": WindowPane, "internal-storage": WindowPane, "window-pane": WindowPane,
	"f-circ": FilledCircle, "filled-circle": FilledCircle, "junction": FilledCircle,
	"sm-circ": SmallCircle, "small-circle": SmallCircle, "start": SmallCircle,
	"fr-circ": FramedCircle, "framed-circle": FramedCircle, "stop": FramedCircle,
	"cross-circ": CrossedCircle, "crossed-circle": CrossedCircle, "summary": CrossedCircle,
	"notch-pent": NotchedPentagon, "loop-limit": NotchedPentagon, "notched-pentagon": NotchedPentagon,
	"sl-rect": SlopedRect, "manual-input": SlopedRect, "sloped-rectangle": SlopedRect,
	"st-rect": StackedRect, "procs": StackedRect, "processes": StackedRect, "stacked-rectangle": StackedRect,
	"bow-rect": BowTieRect, "stored-data": BowTieRect, "bow-tie-rectangle": BowTieRect,
	"text":      TextBlock,
	"hourglass": Hourglass, "collate": Hourglass,
	"brace": BraceLeft, "brace-l": BraceLeft, "comment": BraceLeft,
	"brace-r": BraceRight,
	"braces":  Braces,
	"bolt":    Bolt, "com-link": Bolt, "lightning-bolt": Bolt,
	"notch-rect": NotchedRect, "card": NotchedRect, "notched-rectangle": NotchedRect,
	"tag-rect": TaggedRect, "tag-proc": TaggedRect, "tagged-process": TaggedRect, "tagged-rectangle": TaggedRect,
	"lin-rect": LinedRect, "lin-proc": LinedRect, "lined-process": LinedRect, "lined-rectangle": LinedRect, "shaded-process": LinedRect,
	"flag": Flag, "paper-tape": Flag,
}

// unsupportedShapes are the Mermaid 11 shape names that this library does
// not draw. They give ErrUnsupported rather than a syntax error.
var unsupportedShapes = map[string]bool{"fork": true, "join": true, "bang": true, "cloud": true}

// showsLabel reports whether a shape draws its label; the small marker
// shapes of Mermaid 11 do not.
func showsLabel(s Shape) bool {
	switch s {
	case FilledCircle, SmallCircle, FramedCircle, CrossedCircle, Bolt:
		return false
	}
	return true
}

// LineKind is an edge stroke style.
type LineKind uint8

// Line kinds.
const (
	Normal LineKind = iota
	Thick
	Dotted
	Invisible
)

// Node is a flowchart vertex.
type Node struct {
	ID      string
	Label   string
	Shape   Shape
	Classes []string
	Style   theme.Style
	Cluster int // subgraph index or -1
	// explicit is set once a shape or label was given.
	explicit bool
	line     int
}

// Edge is a link between two nodes.
type Edge struct {
	From, To   int
	Label      string
	Kind       LineKind
	Start, End scene.Marker
	MinLen     int
	Style      theme.Style
	// ID is the Mermaid 11 edge id ("A e1@--> B"), or "".
	ID string
	// Linear draws the edge as a polyline instead of a smooth curve
	// (e1@{ curve: linear }).
	Linear bool
}

// Subgraph is a cluster of nodes.
type Subgraph struct {
	ID     string
	Title  string
	Parent int
	// Dir is the direction set inside the subgraph, when HasDir.
	Dir    sugiyama.Dir
	HasDir bool
}

// Graph is a parsed flowchart.
type Graph struct {
	Dir       sugiyama.Dir
	Nodes     []*Node
	Edges     []Edge
	Subgraphs []Subgraph
	// ClusterRef[i] is the subgraph that node i stands for, or -1.
	ClusterRef []int
	// FirstMember[c] is the first real node inside subgraph c (at any
	// depth), or -1 when the subgraph is empty.
	FirstMember []int
	index       map[string]int
	classDefs   map[string]theme.Style
}

// maxSubgraphDepth bounds subgraph nesting.
const maxSubgraphDepth = 32

type parser struct {
	g     *Graph
	guard *guard.Guard
	stack []int
	// stackLine holds the source line of each open subgraph, for errors.
	stackLine []int
	line      int
	// edgeIDs maps Mermaid 11 edge ids to edge indices.
	edgeIDs map[string]int
}

// Parse parses flowchart source lines (the first line is the header).
func Parse(gd *guard.Guard, lines []source.Line) (*Graph, error) {
	g := &Graph{index: map[string]int{}, classDefs: map[string]theme.Style{}}
	_, rest := source.Header(lines)
	dir, rest := cutWord(rest)
	if d, more, ok := strings.Cut(dir, ";"); ok {
		// "graph TD;A-->B": the statements after the direction.
		dir, rest = d, more+" "+rest
	}
	g.Dir = sugiyama.TB
	if dir != "" {
		d, ok := parseDir(strings.TrimSuffix(dir, ";"))
		if !ok {
			return nil, source.Errorf(lines[0].No, "unknown direction %q", dir)
		}
		g.Dir = d
	}
	p := &parser{g: g, guard: gd, edgeIDs: map[string]int{}}
	var stmts []source.Line
	if rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), ";")); rest != "" {
		stmts = append(stmts, source.Line{No: lines[0].No, Text: rest})
	}
	stmts = append(stmts, lines[1:]...)
	for _, l := range stmts {
		for _, st := range splitStatements(l.Text) {
			if err := gd.Tick(); err != nil {
				return nil, err
			}
			p.line = l.No
			if err := p.statement(strings.TrimSpace(st)); err != nil {
				return nil, err
			}
		}
	}
	if len(p.stack) > 0 {
		return nil, source.Errorf(p.stackLine[len(p.stackLine)-1], "subgraph %q is not closed with end", g.Subgraphs[p.stack[len(p.stack)-1]].ID)
	}
	p.finish()
	return g, nil
}

func parseDir(s string) (sugiyama.Dir, bool) {
	switch strings.ToUpper(s) {
	case "TB", "TD":
		return sugiyama.TB, true
	case "BT":
		return sugiyama.BT, true
	case "LR":
		return sugiyama.LR, true
	case "RL":
		return sugiyama.RL, true
	}
	return 0, false
}

func cutWord(s string) (string, string) {
	s = strings.TrimSpace(s)
	i := strings.IndexFunc(s, unicode.IsSpace)
	if i < 0 {
		return s, ""
	}
	return s[:i], s[i:]
}

// splitStatements splits on ';' outside quotes and brackets.
func splitStatements(s string) []string {
	var out []string
	depth := 0
	inQ := false
	start := 0
	for i, r := range s {
		switch {
		case r == '"':
			inQ = !inQ
		case inQ:
		case r == '[' || r == '(' || r == '{':
			depth++
		case r == ']' || r == ')' || r == '}':
			depth = max(depth-1, 0)
		case r == ';' && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	var res []string
	for _, o := range out {
		if strings.TrimSpace(o) != "" {
			res = append(res, o)
		}
	}
	return res
}

func (p *parser) errf(format string, args ...any) error {
	return source.Errorf(p.line, format, args...)
}

func (p *parser) cluster() int {
	if len(p.stack) == 0 {
		return -1
	}
	return p.stack[len(p.stack)-1]
}

func (p *parser) statement(st string) error {
	if st == "" {
		return nil
	}
	kw, rest := cutWord(st)
	switch kw {
	case "subgraph":
		return p.subgraph(strings.TrimSpace(rest))
	case "end":
		if strings.TrimSpace(rest) == "" {
			if len(p.stack) == 0 {
				return p.errf("end without subgraph")
			}
			p.stack = p.stack[:len(p.stack)-1]
			p.stackLine = p.stackLine[:len(p.stackLine)-1]
			return nil
		}
	case "direction":
		d, ok := parseDir(strings.TrimSpace(rest))
		if !ok {
			return p.errf("unknown direction %q", strings.TrimSpace(rest))
		}
		// Outside a subgraph it changes nothing (the header sets the
		// graph's direction).
		if c := p.cluster(); c >= 0 {
			p.g.Subgraphs[c].Dir, p.g.Subgraphs[c].HasDir = d, true
		}
		return nil
	case "classDef":
		names, style := cutWord(rest)
		if names == "" {
			return p.errf("classDef needs a name")
		}
		st := theme.ParseStyle(style)
		for _, n := range strings.Split(names, ",") {
			p.g.classDefs[strings.TrimSpace(n)] = p.g.classDefs[strings.TrimSpace(n)].Merge(st)
		}
		return nil
	case "class":
		ids, cls := cutWord(rest)
		cls = strings.TrimSpace(cls)
		if ids == "" || cls == "" {
			return p.errf("class needs node ids and a class name")
		}
		for _, id := range strings.Split(ids, ",") {
			n, err := p.node(strings.TrimSpace(id))
			if err != nil {
				return err
			}
			p.g.Nodes[n].Classes = append(p.g.Nodes[n].Classes, cls)
		}
		return nil
	case "style":
		id, style := cutWord(rest)
		if id == "" {
			return p.errf("style needs a node id")
		}
		n, err := p.node(id)
		if err != nil {
			return err
		}
		p.g.Nodes[n].Style = p.g.Nodes[n].Style.Merge(theme.ParseStyle(style))
		return nil
	case "linkStyle":
		return p.linkStyle(rest)
	case "click", "accTitle", "accDescr", "accTitle:", "accDescr:":
		// NOTE(mermaid-compat): interaction and accessibility directives do
		// not affect the image.
		return nil
	}
	// "e1@{ ... }" sets attributes of the edge with id e1.
	if id, body, ok := strings.Cut(st, "@{"); ok {
		if i, isEdge := p.edgeIDs[strings.TrimSpace(id)]; isEdge {
			if !strings.HasSuffix(strings.TrimSpace(body), "}") {
				return p.errf("attribute block is not closed with }")
			}
			return p.edgeAttrs(&p.g.Edges[i], strings.TrimSuffix(strings.TrimSpace(body), "}"))
		}
	}
	return p.chain(st)
}

func (p *parser) subgraph(rest string) error {
	if len(p.stack) >= maxSubgraphDepth {
		return source.TooLarge(p.line, "subgraphs nested too deeply")
	}
	id, title := rest, ""
	if i := strings.IndexAny(rest, "[\""); i >= 0 {
		if rest[i] == '[' {
			id = strings.TrimSpace(rest[:i])
			if !strings.HasSuffix(rest, "]") {
				return p.errf("subgraph title is missing ]")
			}
			title = source.Label(rest[i+1 : len(rest)-1])
		} else if i == 0 {
			title = source.Label(rest)
			id = title
		}
	}
	if id == "" {
		id = "subgraph" + strconv.Itoa(len(p.g.Subgraphs))
	}
	if title == "" {
		title = source.Label(id)
	}
	p.g.Subgraphs = append(p.g.Subgraphs, Subgraph{ID: id, Title: title, Parent: p.cluster()})
	p.stack = append(p.stack, len(p.g.Subgraphs)-1)
	p.stackLine = append(p.stackLine, p.line)
	return p.guard.Count("subgraphs", len(p.g.Subgraphs))
}

func (p *parser) linkStyle(rest string) error {
	which, style := cutWord(rest)
	st := theme.ParseStyle(style)
	if which == "default" {
		for i := range p.g.Edges {
			p.g.Edges[i].Style = p.g.Edges[i].Style.Merge(st)
		}
		return nil
	}
	for _, s := range strings.Split(which, ",") {
		i, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || i < 0 || i >= len(p.g.Edges) {
			return p.errf("linkStyle refers to unknown link %q", s)
		}
		p.g.Edges[i].Style = p.g.Edges[i].Style.Merge(st)
	}
	return nil
}

// node returns the index of node id, creating it if necessary. Every node
// is created here so that the element limit is always enforced.
func (p *parser) node(id string) (int, error) {
	if i, ok := p.g.index[id]; ok {
		if c := p.cluster(); c >= 0 && p.g.Nodes[i].Cluster < 0 {
			p.g.Nodes[i].Cluster = c
		}
		return i, nil
	}
	if id == "" {
		return 0, p.errf("expected a node id")
	}
	p.g.Nodes = append(p.g.Nodes, &Node{ID: id, Label: id, Cluster: p.cluster(), line: p.line})
	p.g.index[id] = len(p.g.Nodes) - 1
	if err := p.guard.Count("nodes", len(p.g.Nodes)); err != nil {
		return 0, err
	}
	return len(p.g.Nodes) - 1, nil
}

// chain parses "A[x] & B --> C -- label --> D".
func (p *parser) chain(st string) error {
	s := &scanner{src: st}
	prev, err := p.group(s)
	if err != nil {
		return err
	}
	for {
		s.space()
		if s.eof() {
			return nil
		}
		edgeID := p.edgeID(s)
		l, ok, err := p.link(s)
		if err != nil {
			return err
		}
		if !ok {
			return p.errf("unexpected %q", s.rest())
		}
		next, err := p.group(s)
		if err != nil {
			return err
		}
		if edgeID != "" && len(prev)*len(next) != 1 {
			return p.errf("edge id %q names more than one edge", edgeID)
		}
		for _, a := range prev {
			for _, b := range next {
				e := l
				e.From, e.To = a, b
				if edgeID != "" {
					if _, dup := p.edgeIDs[edgeID]; dup {
						return p.errf("duplicate edge id %q", edgeID)
					}
					e.ID = edgeID
					p.edgeIDs[edgeID] = len(p.g.Edges)
				}
				p.g.Edges = append(p.g.Edges, e)
				if err := p.guard.Count("edges", len(p.g.Edges)); err != nil {
					return err
				}
			}
		}
		prev = next
	}
}

func (p *parser) group(s *scanner) ([]int, error) {
	var out []int
	for {
		s.space()
		n, err := p.nodeRef(s)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
		s.space()
		if !s.consume("&") {
			return out, nil
		}
	}
}

type opener struct {
	open   string
	closes []string
	shapes []Shape
}

var openers = []opener{
	{"(((", []string{")))"}, []Shape{DoubleCircle}},
	{"((", []string{"))"}, []Shape{Circle}},
	{"([", []string{"])"}, []Shape{Stadium}},
	{"(", []string{")"}, []Shape{Round}},
	{"[[", []string{"]]"}, []Shape{Subroutine}},
	{"[(", []string{")]"}, []Shape{Cylinder}},
	{"[/", []string{"/]", `\]`}, []Shape{Parallelogram, Trapezoid}},
	{`[\`, []string{`\]`, "/]"}, []Shape{ParallelogramAlt, TrapezoidAlt}},
	{"[", []string{"]"}, []Shape{Rect}},
	{"{{", []string{"}}"}, []Shape{Hexagon}},
	{"{", []string{"}"}, []Shape{Rhombus}},
	{">", []string{"]"}, []Shape{Asymmetric}},
}

func (p *parser) nodeRef(s *scanner) (int, error) {
	id := s.ident()
	if id == "" {
		if s.eof() {
			return 0, p.errf("expected a node")
		}
		return 0, p.errf("expected a node at %q", s.rest())
	}
	n, err := p.node(id)
	if err != nil {
		return 0, err
	}
	nd := p.g.Nodes[n]
	if s.consume("@{") {
		if err := p.nodeAttrs(s, nd); err != nil {
			return 0, err
		}
		return n, nil
	}
	for _, o := range openers {
		if !s.consume(o.open) {
			continue
		}
		label, shape, err := p.shapeBody(s, o)
		if err != nil {
			return 0, err
		}
		nd.Label, nd.Shape, nd.explicit = label, shape, true
		break
	}
	if s.consume(":::") {
		cls := s.ident()
		if cls == "" {
			return 0, p.errf("expected a class name after :::")
		}
		nd.Classes = append(nd.Classes, cls)
	}
	return n, nil
}

func (p *parser) shapeBody(s *scanner, o opener) (string, Shape, error) {
	rest := s.rest()
	start := 0
	if strings.HasPrefix(strings.TrimLeft(rest, " "), `"`) {
		q := strings.Index(rest, `"`)
		end := strings.Index(rest[q+1:], `"`)
		if end < 0 {
			return "", 0, p.errf("unterminated string")
		}
		start = q + 1 + end + 1
	}
	best, bi := -1, 0
	for i, c := range o.closes {
		k := strings.Index(rest[start:], c)
		if k >= 0 && (best < 0 || k+start < best) {
			best, bi = k+start, i
		}
	}
	if best < 0 {
		return "", 0, p.errf("missing %q", o.closes[0])
	}
	label := source.Label(rest[:best])
	s.pos += best + len(o.closes[bi])
	return label, o.shapes[bi], nil
}

// link parses an edge operator and its optional label.
func (p *parser) link(s *scanner) (Edge, bool, error) {
	e := Edge{MinLen: 1}
	save := s.pos
	if c := s.peek(); c == '<' {
		e.Start = scene.Arrow
		s.pos++
	} else if (c == 'o' || c == 'x') && strings.ContainsAny(s.peekAt(1), "-=") {
		e.Start = markerOf(c)
		s.pos++
	}
	body := s.run("-=.~")
	if len(body) < 2 {
		s.pos = save
		return e, false, nil
	}
	end, hasEnd := s.endMarker()
	if closing, ok := map[string]string{"--": "--", "==": "==", "-.": ".-"}[body]; ok && !hasEnd {
		// "A -- text --> B", and without spaces "A--text-->B": Mermaid's
		// lexer starts link text at the opening -- whether or not a space
		// follows. Without a closing -- the link has no text ("A -- B").
		rest := s.rest()
		k := strings.Index(rest, closing)
		switch {
		case k >= 0:
			e.Label = source.Label(rest[:k])
			s.pos += k
			body2 := s.run("-=.")
			end, hasEnd = s.endMarker()
			body += body2
		case s.peek() == ' ':
			return e, false, p.errf("link text is not closed")
		default:
			// A--B: two dashes are only the start of a link; Mermaid
			// rejects them alone.
			hint := map[string]string{"--": "--- or -->", "==": "=== or ==>", "-.": "-.- or -.->"}[body]
			return e, false, p.errf("incomplete link %q (use %s)", body, hint)
		}
	}
	e.End = end
	_ = hasEnd
	switch {
	case strings.Contains(body, "~"):
		e.Kind = Invisible
	case strings.Contains(body, "."):
		e.Kind = Dotted
		e.MinLen = max(1, strings.Count(body, "."))
	case strings.Contains(body, "="):
		e.Kind = Thick
		e.MinLen = max(1, strings.Count(body, "=")-countIf(hasEnd, 1, 2))
	default:
		e.MinLen = max(1, strings.Count(body, "-")-countIf(hasEnd, 1, 2))
	}
	if e.Label != "" {
		e.MinLen = 1
	}
	s.space()
	if s.consume("|") {
		k := strings.Index(s.rest(), "|")
		if k < 0 {
			return e, false, p.errf("link label is missing closing |")
		}
		e.Label = source.Label(s.rest()[:k])
		s.pos += k + 1
	}
	return e, true, nil
}

func countIf(c bool, a, b int) int {
	if c {
		return a
	}
	return b
}

func markerOf(c byte) scene.Marker {
	switch c {
	case '>', '<':
		return scene.Arrow
	case 'o':
		return scene.Circle
	case 'x':
		return scene.Cross
	}
	return scene.NoMarker
}

// finish resolves node classes and edges that point at subgraphs.
func (p *parser) finish() {
	g := p.g
	g.ClusterRef = make([]int, len(g.Nodes))
	sgIndex := map[string]int{}
	for i, sg := range g.Subgraphs {
		if _, ok := sgIndex[sg.ID]; !ok {
			sgIndex[sg.ID] = i
		}
	}
	for i, n := range g.Nodes {
		g.ClusterRef[i] = -1
		if c, ok := sgIndex[n.ID]; ok && !n.explicit {
			g.ClusterRef[i] = c
		}
		st := g.classDefs["default"]
		for _, c := range n.Classes {
			st = st.Merge(g.classDefs[c])
		}
		n.Style = st.Merge(n.Style)
	}
	g.FirstMember = make([]int, len(g.Subgraphs))
	for c := range g.FirstMember {
		g.FirstMember[c] = -1
	}
	for i, n := range g.Nodes {
		if g.ClusterRef[i] >= 0 {
			continue
		}
		for k, depth := n.Cluster, 0; k >= 0 && depth <= maxSubgraphDepth; k, depth = g.Subgraphs[k].Parent, depth+1 {
			if g.FirstMember[k] < 0 {
				g.FirstMember[k] = i
			}
		}
	}
}

// scanner is a byte cursor over one statement.
type scanner struct {
	src string
	pos int
}

func (s *scanner) eof() bool    { return s.pos >= len(s.src) }
func (s *scanner) rest() string { return s.src[s.pos:] }
func (s *scanner) peek() byte {
	if s.eof() {
		return 0
	}
	return s.src[s.pos]
}
func (s *scanner) peekAt(k int) string {
	if s.pos+k >= len(s.src) {
		return ""
	}
	return s.src[s.pos+k : s.pos+k+1]
}
func (s *scanner) space() {
	for !s.eof() && (s.src[s.pos] == ' ' || s.src[s.pos] == '\t') {
		s.pos++
	}
}
func (s *scanner) consume(t string) bool {
	if strings.HasPrefix(s.rest(), t) {
		s.pos += len(t)
		return true
	}
	return false
}
func (s *scanner) run(chars string) string {
	start := s.pos
	for !s.eof() && strings.IndexByte(chars, s.src[s.pos]) >= 0 {
		s.pos++
	}
	return s.src[start:s.pos]
}

// endMarker consumes '>', 'o' or 'x' at the end of a link. 'o' and 'x' only
// count when not followed by an identifier character.
func (s *scanner) endMarker() (scene.Marker, bool) {
	c := s.peek()
	switch c {
	case '>':
		s.pos++
		return scene.Arrow, true
	case 'o', 'x':
		// As in Mermaid's lexer, o and x right after the dashes end the
		// link even when a name follows: A---oB is a circle edge to B
		// (and dev---ops one to "ps"; Mermaid's documentation warns).
		s.pos++
		return markerOf(c), true
	}
	return scene.NoMarker, false
}

func isIDStart(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return isIDRune(r)
}

func isIDRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || (r > 127 && !unicode.IsSpace(r) && !unicode.IsPunct(r))
}

// ident reads a node id. '-' and '.' are allowed inside ids when followed by
// an id character, so that "A-->B" splits correctly.
func (s *scanner) ident() string {
	start := s.pos
	for !s.eof() {
		r, size := utf8.DecodeRuneInString(s.rest())
		switch {
		case isIDRune(r):
			s.pos += size
		case (r == '-' || r == '.') && s.pos > start && isIDStart(s.peekAt(1)):
			s.pos += size
		default:
			return s.src[start:s.pos]
		}
	}
	return s.src[start:s.pos]
}

// edgeID consumes a Mermaid 11 edge id ("e1@" before a link) and returns
// it, or returns "" and consumes nothing.
func (p *parser) edgeID(s *scanner) string {
	save := s.pos
	id := s.ident()
	if id != "" && s.consume("@") && !s.eof() && strings.ContainsRune("-=.~<ox", rune(s.peek())) {
		return id
	}
	s.pos = save
	return ""
}

// attrBody consumes the rest of an attribute block after "@{" up to the
// closing brace (outside quotes) and returns its contents.
func (p *parser) attrBody(s *scanner) (string, error) {
	rest := s.rest()
	var quote byte
	for i := 0; i < len(rest); i++ {
		switch c := rest[i]; {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '}':
			s.pos += i + 1
			return rest[:i], nil
		}
	}
	return "", p.errf("attribute block is not closed with }")
}

// nodeAttrs applies "A@{ shape: name, label: text }".
func (p *parser) nodeAttrs(s *scanner, nd *Node) error {
	body, err := p.attrBody(s)
	if err != nil {
		return err
	}
	attrs, err := source.ParseAttrs(body)
	if err != nil {
		return p.errf("attributes: %v", err)
	}
	for _, k := range sortedKeys(attrs) {
		v := fmt.Sprint(attrs[k])
		switch k {
		case "shape":
			sh, ok := shapeNames[v]
			if !ok && unsupportedShapes[v] {
				return source.Unsupported(p.line, "shape %q is not drawn", v)
			}
			if !ok {
				return p.errf("unknown shape %q", v)
			}
			nd.Shape, nd.explicit = sh, true
		case "label":
			nd.Label, nd.explicit = source.Label(v), true
		case "icon", "img", "form", "pos", "w", "h", "constraint":
			return source.Unsupported(p.line, "node attribute %q (icons and images) is not supported", k)
		default:
			return p.errf("unknown node attribute %q", k)
		}
	}
	return nil
}

// edgeAttrs applies "e1@{ curve: linear }" to the edge with that id.
func (p *parser) edgeAttrs(e *Edge, body string) error {
	attrs, err := source.ParseAttrs(body)
	if err != nil {
		return p.errf("attributes: %v", err)
	}
	for _, k := range sortedKeys(attrs) {
		v := fmt.Sprint(attrs[k])
		switch k {
		case "curve":
			switch v {
			case "linear", "step", "stepBefore", "stepAfter":
				// NOTE(mermaid-compat): the step curves are drawn as
				// straight segments.
				e.Linear = true
			case "basis", "bumpX", "bumpY", "cardinal", "catmullRom", "monotoneX", "monotoneY", "natural":
				// NOTE(mermaid-compat): all smooth curves are drawn alike.
				e.Linear = false
			default:
				return p.errf("unknown curve %q", v)
			}
		case "animate", "animation":
			// NOTE(mermaid-compat): animation has no effect on an image.
		default:
			return p.errf("unknown edge attribute %q", k)
		}
	}
	return nil
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
