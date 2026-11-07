// Package architecture parses and lays out Mermaid architecture diagrams
// (architecture-beta): services and junctions in groups, joined by edges
// between their sides.
package architecture

import (
	"regexp"
	"strings"

	"github.com/lea/mermaid/internal/boxgraph"
	"github.com/lea/mermaid/internal/diagram"
	"github.com/lea/mermaid/internal/guard"
	"github.com/lea/mermaid/internal/layout/sugiyama"
	"github.com/lea/mermaid/internal/scene"
	"github.com/lea/mermaid/internal/source"
	"github.com/lea/mermaid/internal/textgraph"
)

// Node is a service or a junction.
type Node struct {
	ID, Label, Icon string
	Junction        bool
	Group           int // -1 for none
}

// Group frames services.
type Group struct {
	ID, Label, Icon string
	Parent          int
}

// Edge joins two sides: From's side FromSide (L, R, T or B) to To's side
// ToSide.
type Edge struct {
	From, To           int
	FromSide, ToSide   byte
	FromArrow, ToArrow bool
	Label              string
}

// Diagram is a parsed architecture diagram.
type Diagram struct {
	Title  string
	Nodes  []Node
	Groups []Group
	Edges  []Edge
	nodes  map[string]int
	groups map[string]int
}

const idPat = `([\p{L}\p{N}_][\p{L}\p{N}_\-]*)`

var (
	groupRe    = regexp.MustCompile(`^group\s+` + idPat + `\s*(?:\(([^)]*)\))?\s*(?:\[([^\]]*)\])?\s*(?:in\s+` + idPat + `)?$`)
	serviceRe  = regexp.MustCompile(`^service\s+` + idPat + `\s*(?:\(([^)]*)\))?\s*(?:\[([^\]]*)\])?\s*(?:in\s+` + idPat + `)?$`)
	junctionRe = regexp.MustCompile(`^junction\s+` + idPat + `\s*(?:in\s+` + idPat + `)?$`)
	edgeRe     = regexp.MustCompile(`^` + idPat + `(\{group\})?\s*:\s*([LRTB])\s*(<?)-(?:\[([^\]]*)\])?-(>?)\s*([LRTB])\s*:\s*` + idPat + `(\{group\})?$`)
)

// Parse parses an architecture diagram.
func Parse(g *guard.Guard, lines []source.Line) (*Diagram, error) {
	d := &Diagram{nodes: map[string]int{}, groups: map[string]int{}}
	groupOf := func(id string, no int) (int, error) {
		if id == "" {
			return -1, nil
		}
		k, ok := d.groups[id]
		if !ok {
			return 0, source.Errorf(no, "unknown group %q", id)
		}
		return k, nil
	}
	count := func() error { return g.Count("services, groups and edges", len(d.Nodes)+len(d.Groups)+len(d.Edges)) }
	for _, l := range lines[1:] {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		t := strings.TrimSpace(l.Text)
		if r, ok := source.CutKeyword(t, "title"); ok {
			d.Title = source.Label(r)
			continue
		}
		if strings.HasPrefix(t, "accTitle") || strings.HasPrefix(t, "accDescr") {
			// NOTE(mermaid-compat): accessibility texts do not affect the image.
			continue
		}
		if m := groupRe.FindStringSubmatch(t); m != nil {
			if _, dup := d.groups[m[1]]; dup {
				return nil, source.Errorf(l.No, "duplicate group %q", m[1])
			}
			parent, err := groupOf(m[4], l.No)
			if err != nil {
				return nil, err
			}
			d.groups[m[1]] = len(d.Groups)
			d.Groups = append(d.Groups, Group{ID: m[1], Icon: m[2], Label: label(m[3], m[1]), Parent: parent})
			if err := count(); err != nil {
				return nil, err
			}
			continue
		}
		if m := serviceRe.FindStringSubmatch(t); m != nil {
			grp, err := groupOf(m[4], l.No)
			if err != nil {
				return nil, err
			}
			if err := d.add(Node{ID: m[1], Icon: m[2], Label: label(m[3], m[1]), Group: grp}, l.No); err != nil {
				return nil, err
			}
			if err := count(); err != nil {
				return nil, err
			}
			continue
		}
		if m := junctionRe.FindStringSubmatch(t); m != nil {
			grp, err := groupOf(m[2], l.No)
			if err != nil {
				return nil, err
			}
			if err := d.add(Node{ID: m[1], Junction: true, Group: grp}, l.No); err != nil {
				return nil, err
			}
			if err := count(); err != nil {
				return nil, err
			}
			continue
		}
		if m := edgeRe.FindStringSubmatch(t); m != nil {
			from, ok1 := d.nodes[m[1]]
			to, ok2 := d.nodes[m[8]]
			if !ok1 || !ok2 {
				missing := m[1]
				if ok1 {
					missing = m[8]
				}
				return nil, source.Errorf(l.No, "edge to unknown service %q", missing)
			}
			d.Edges = append(d.Edges, Edge{From: from, To: to, FromSide: m[3][0], ToSide: m[7][0],
				FromArrow: m[4] == "<", ToArrow: m[6] == ">", Label: source.Label(m[5])})
			if err := count(); err != nil {
				return nil, err
			}
			continue
		}
		return nil, source.Errorf(l.No, "unrecognized statement %q", t)
	}
	if len(d.Nodes) == 0 {
		return nil, source.Errorf(lines[len(lines)-1].No, "architecture diagram has no services")
	}
	return d, nil
}

func (d *Diagram) add(n Node, no int) error {
	if _, dup := d.nodes[n.ID]; dup {
		return source.Errorf(no, "duplicate service %q", n.ID)
	}
	d.nodes[n.ID] = len(d.Nodes)
	d.Nodes = append(d.Nodes, n)
	return nil
}

func label(l, id string) string {
	if l == "" {
		return source.Label(id)
	}
	return source.Label(l)
}

// DiagramTitle returns the title given with the title statement.
func (d *Diagram) DiagramTitle() string { return d.Title }

// graph converts the diagram to a box graph for the text renderer.
//
// NOTE(mermaid-compat): text output places services with the layered
// layout: the whole diagram flows left to right or top to bottom, as most
// of its edges do, and each edge is oriented by its sides within that
// flow. Images use the grid of gridLayout. Icons are named («database»)
// instead of drawn.
func (d *Diagram) graph() *boxgraph.Graph {
	horizontal := 0
	for _, e := range d.Edges {
		if e.FromSide == 'L' || e.FromSide == 'R' {
			horizontal++
		} else {
			horizontal--
		}
	}
	g := &boxgraph.Graph{Dir: sugiyama.TB}
	if horizontal > 0 {
		g.Dir = sugiyama.LR
	}
	for _, n := range d.Nodes {
		b := boxgraph.Node{Header: []string{n.Label}, Shape: boxgraph.Round, Cluster: n.Group}
		if n.Icon != "" {
			b.Header = []string{"«" + n.Icon + "»", n.Label}
		}
		if n.Junction {
			b = boxgraph.Node{Header: []string{"•"}, Shape: boxgraph.Round, Cluster: n.Group}
		}
		g.Nodes = append(g.Nodes, b)
	}
	for _, grp := range d.Groups {
		title := grp.Label
		if grp.Icon != "" {
			title = "«" + grp.Icon + "» " + title
		}
		g.Clusters = append(g.Clusters, boxgraph.Cluster{Title: title, Parent: grp.Parent, Dashed: true})
	}
	for _, e := range d.Edges {
		be := boxgraph.Edge{From: e.From, To: e.To, Label: e.Label}
		if e.FromArrow {
			be.Start = scene.Arrow
		}
		if e.ToArrow {
			be.End = scene.Arrow
		}
		// "a:L -- R:b" puts b left of a, and "a:T -- B:b" puts b above a:
		// the edge then runs from b to a in the flow.
		if e.FromSide == 'L' || e.FromSide == 'T' {
			be.From, be.To = be.To, be.From
			be.Start, be.End = be.End, be.Start
		}
		g.Edges = append(g.Edges, be)
	}
	return g
}

// Layout implements diagram.Layouter.
func (d *Diagram) Layout(env *diagram.Env) (*scene.Scene, error) {
	return d.gridLayout(env)
}

// Text renders the diagram with box-drawing characters.
func (d *Diagram) Text(g *guard.Guard, maxCols int) ([]string, error) {
	return textgraph.Text(g, d.graph().TextGraph(), maxCols)
}
