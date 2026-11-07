// Package c4 parses and lays out Mermaid C4 diagrams (C4Context,
// C4Container, C4Component, C4Dynamic and C4Deployment).
package c4

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/lea/mermaid/internal/boxgraph"
	"github.com/lea/mermaid/internal/diagram"
	"github.com/lea/mermaid/internal/guard"
	"github.com/lea/mermaid/internal/layout/sugiyama"
	"github.com/lea/mermaid/internal/richtext"
	"github.com/lea/mermaid/internal/scene"
	"github.com/lea/mermaid/internal/source"
	"github.com/lea/mermaid/internal/textgraph"
	"github.com/lea/mermaid/internal/theme"
)

// Element is a person, system, container, component or deployment node.
type Element struct {
	Alias, Label string
	Kind         string // person, system, container, component, node
	Variant      string // "", "db", "queue"
	External     bool
	Techn, Descr string
	Boundary     int // innermost boundary, or -1
	Fill, Text   *color.NRGBA
	Stroke       *color.NRGBA
}

// Boundary groups elements.
type Boundary struct {
	Alias, Label, Type string
	Parent             int
}

// Rel is a relationship.
type Rel struct {
	From, To     int
	Label, Techn string
	Both         bool
}

// Diagram is a parsed C4 diagram.
type Diagram struct {
	Title      string
	Elements   []*Element
	Boundaries []Boundary
	Rels       []Rel
	index      map[string]int
}

// elementOf maps an element macro, named Kind[Db|Queue][_Ext], to its
// kind, variant and whether it is external.
func elementOf(name string) (kind, variant string, ext, ok bool) {
	base, ext := strings.CutSuffix(name, "_Ext")
	for _, k := range []string{"Person", "System", "Container", "Component"} {
		rest, found := strings.CutPrefix(base, k)
		if !found {
			continue
		}
		switch rest {
		case "":
			return strings.ToLower(k), "", ext, true
		case "Db":
			return strings.ToLower(k), "db", ext, k != "Person"
		case "Queue":
			return strings.ToLower(k), "queue", ext, k != "Person"
		}
	}
	switch name {
	case "Deployment_Node", "Node", "Node_L", "Node_R":
		return "node", "", false, true
	}
	return "", "", false, false
}

// Parse parses a C4 diagram.
func Parse(g *guard.Guard, lines []source.Line) (*Diagram, error) {
	d := &Diagram{index: map[string]int{}}
	var stack []int // open boundaries (deployment nodes are boundaries too)
	var stackLine []int
	scope := func() int {
		if len(stack) == 0 {
			return -1
		}
		return stack[len(stack)-1]
	}
	for _, l := range lines[1:] {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		t := strings.TrimSpace(l.Text)
		if t == "}" {
			if len(stack) == 0 {
				return nil, source.Errorf(l.No, "unexpected }")
			}
			stack, stackLine = stack[:len(stack)-1], stackLine[:len(stackLine)-1]
			continue
		}
		if r, ok := source.CutKeyword(t, "title"); ok {
			d.Title = source.Label(r)
			continue
		}
		if strings.HasPrefix(t, "accTitle") || strings.HasPrefix(t, "accDescr") {
			// NOTE(mermaid-compat): accessibility texts do not affect the image.
			continue
		}
		name, args, opens, err := call(t)
		if err != nil {
			return nil, source.Errorf(l.No, "%v", err)
		}
		arg := func(i int) string {
			if i < len(args.pos) {
				return args.pos[i]
			}
			return ""
		}
		switch {
		case strings.HasSuffix(name, "Boundary") || name == "Deployment_Node" || strings.HasPrefix(name, "Node"):
			if len(args.pos) < 2 {
				return nil, source.Errorf(l.No, "%s needs an alias and a label", name)
			}
			typ := arg(2)
			if typ == "" {
				typ = strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(name, "_Boundary"), "Boundary"))
			}
			if name == "Boundary" {
				typ = arg(2)
			}
			d.Boundaries = append(d.Boundaries, Boundary{Alias: arg(0), Label: source.Label(arg(1)), Type: source.Label(typ), Parent: scope()})
			if err := g.Count("elements", len(d.Elements)+len(d.Boundaries)); err != nil {
				return nil, err
			}
			if opens {
				stack = append(stack, len(d.Boundaries)-1)
				stackLine = append(stackLine, l.No)
			}
			continue
		case strings.HasPrefix(name, "Rel") || strings.HasPrefix(name, "BiRel"):
			from, to, label, techn := arg(0), arg(1), arg(2), arg(3)
			if name == "RelIndex" {
				// RelIndex(index, from, to, label, techn)
				from, to, label, techn = arg(1), arg(2), arg(3), arg(4)
			}
			if from == "" || to == "" {
				return nil, source.Errorf(l.No, "%s needs two elements", name)
			}
			d.Rels = append(d.Rels, Rel{From: d.element(from), To: d.element(to), Label: source.Label(label),
				Techn: source.Label(techn), Both: strings.HasPrefix(name, "BiRel")})
			if err := g.Count("relationships", len(d.Rels)); err != nil {
				return nil, err
			}
			continue
		case name == "UpdateElementStyle":
			e := d.Elements[d.element(arg(0))]
			for k, v := range args.named {
				c, ok := theme.ParseColor(v)
				if !ok {
					continue
				}
				c.A = 255
				switch k {
				case "bgColor":
					e.Fill = &c
				case "fontColor":
					e.Text = &c
				case "borderColor":
					e.Stroke = &c
				}
			}
			continue
		case strings.HasPrefix(name, "Update"):
			// NOTE(mermaid-compat): relationship styles, layout settings and
			// other Update* calls do not change this renderer's output.
			continue
		}
		kind, variant, ext, ok := elementOf(name)
		if !ok {
			return nil, source.Errorf(l.No, "unknown C4 statement %q", name)
		}
		if len(args.pos) < 2 {
			return nil, source.Errorf(l.No, "%s needs an alias and a label", name)
		}
		i := d.element(arg(0))
		e := d.Elements[i]
		e.Label, e.Kind, e.Variant, e.External, e.Boundary = source.Label(arg(1)), kind, variant, ext, scope()
		if kind == "person" || kind == "system" {
			e.Descr = source.Label(arg(2))
		} else {
			e.Techn, e.Descr = source.Label(arg(2)), source.Label(arg(3))
		}
		if kind == "node" {
			// Deployment nodes written without braces are boxes.
			e.Techn, e.Descr = source.Label(arg(2)), source.Label(arg(3))
		}
		if err := g.Count("elements", len(d.Elements)+len(d.Boundaries)); err != nil {
			return nil, err
		}
	}
	if len(stack) > 0 {
		return nil, source.Errorf(stackLine[len(stackLine)-1], "boundary %q is not closed with }", d.Boundaries[stack[len(stack)-1]].Alias)
	}
	return d, nil
}

// element returns the index of the element with alias a, creating a
// placeholder when a relationship names it before it is declared.
func (d *Diagram) element(a string) int {
	if i, ok := d.index[a]; ok {
		return i
	}
	d.Elements = append(d.Elements, &Element{Alias: a, Label: a, Kind: "system", Boundary: -1})
	d.index[a] = len(d.Elements) - 1
	return len(d.Elements) - 1
}

type callArgs struct {
	pos   []string
	named map[string]string
}

// call parses `Name(arg, "quoted, arg", $key="value") {`.
func call(t string) (name string, args callArgs, opens bool, err error) {
	t, opens = strings.CutSuffix(strings.TrimSpace(t), "{")
	t = strings.TrimSpace(t)
	open := strings.IndexByte(t, '(')
	if open <= 0 || !strings.HasSuffix(t, ")") {
		return "", args, false, fmt.Errorf("expected Name(arguments)")
	}
	name = strings.TrimSpace(t[:open])
	body := t[open+1 : len(t)-1]
	args.named = map[string]string{}
	var cur strings.Builder
	quoted := false
	flush := func() {
		a := strings.TrimSpace(cur.String())
		cur.Reset()
		if k, v, ok := strings.Cut(a, "="); ok && strings.HasPrefix(k, "$") {
			args.named[strings.TrimSpace(k[1:])] = strings.Trim(strings.TrimSpace(v), `"`)
			return
		}
		args.pos = append(args.pos, strings.Trim(a, `"`))
	}
	for _, r := range body {
		switch {
		case r == '"':
			quoted = !quoted
			cur.WriteRune(r)
		case r == ',' && !quoted:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	if quoted {
		return "", args, false, fmt.Errorf("unterminated string")
	}
	if strings.TrimSpace(cur.String()) != "" || len(args.pos) > 0 {
		flush()
	}
	return name, args, opens, nil
}

// DiagramTitle returns the title given with the title statement.
func (d *Diagram) DiagramTitle() string { return d.Title }

// colors of the C4 model notation.
var (
	personFill    = color.NRGBA{0x08, 0x42, 0x7b, 0xff}
	systemFill    = color.NRGBA{0x11, 0x68, 0xbd, 0xff}
	containerFill = color.NRGBA{0x43, 0x8d, 0xd5, 0xff}
	componentFill = color.NRGBA{0x85, 0xbb, 0xf0, 0xff}
	externalFill  = color.NRGBA{0x99, 0x99, 0x99, 0xff}
	white         = color.NRGBA{0xff, 0xff, 0xff, 0xff}
	dark          = color.NRGBA{0x00, 0x00, 0x00, 0xff}
)

// graph converts the diagram to a box graph: elements are filled boxes
// with a stereotype, the bold name, [technology] and the description;
// boundaries are dashed clusters.
func (d *Diagram) graph(th theme.Theme) *boxgraph.Graph {
	g := &boxgraph.Graph{Dir: sugiyama.TB}
	for _, e := range d.Elements {
		stereo := e.Kind
		if e.External {
			stereo = "external_" + stereo
		}
		if e.Variant != "" {
			stereo += " " + e.Variant
		}
		header := []string{"«" + stereo + "»", string(richtext.Marker(richtext.Bold)) + e.Label}
		if e.Techn != "" {
			header = append(header, "["+e.Techn+"]")
		}
		if e.Descr != "" {
			header = append(header, e.Descr)
		}
		fill, text := systemFill, white
		switch {
		case e.External:
			fill = externalFill
		case e.Kind == "person":
			fill = personFill
		case e.Kind == "container":
			fill = containerFill
		case e.Kind == "component":
			fill, text = componentFill, dark
		case e.Kind == "node":
			fill, text = th.NodeFill, th.Text
		}
		if e.Fill != nil {
			fill = *e.Fill
		}
		if e.Text != nil {
			text = *e.Text
		}
		stroke := theme.Mix(fill, dark, 0.25)
		if e.Stroke != nil {
			stroke = *e.Stroke
		}
		n := boxgraph.Node{Header: header, Shape: boxgraph.Round, Cluster: e.Boundary,
			Fill: &fill, Text: &text, Stroke: &stroke}
		g.Nodes = append(g.Nodes, n)
	}
	for _, b := range d.Boundaries {
		title := b.Label
		if b.Type != "" {
			title += " [" + b.Type + "]"
		}
		g.Clusters = append(g.Clusters, boxgraph.Cluster{Title: title, Parent: b.Parent, Dashed: true})
	}
	for _, r := range d.Rels {
		label := r.Label
		if r.Techn != "" {
			label += "\n[" + r.Techn + "]"
		}
		e := boxgraph.Edge{From: r.From, To: r.To, Label: label, End: scene.Arrow}
		if r.Both {
			e.Start = scene.Arrow
		}
		g.Edges = append(g.Edges, e)
	}
	return g
}

// Layout implements diagram.Layouter.
func (d *Diagram) Layout(env *diagram.Env) (*scene.Scene, error) {
	return boxgraph.Layout(env, d.graph(env.T))
}

// Text renders the diagram with box-drawing characters.
func (d *Diagram) Text(g *guard.Guard, maxCols int) ([]string, error) {
	return textgraph.Text(g, d.graph(theme.Theme{}).TextGraph(), maxCols)
}
