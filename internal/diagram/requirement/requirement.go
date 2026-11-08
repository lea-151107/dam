// Package requirement parses and lays out Mermaid requirement diagrams.
package requirement

import (
	"regexp"
	"strings"

	"github.com/lea-151107/dam/internal/boxgraph"
	"github.com/lea-151107/dam/internal/diagram"
	"github.com/lea-151107/dam/internal/guard"
	"github.com/lea-151107/dam/internal/layout/sugiyama"
	"github.com/lea-151107/dam/internal/richtext"
	"github.com/lea-151107/dam/internal/scene"
	"github.com/lea-151107/dam/internal/source"
	"github.com/lea-151107/dam/internal/textgraph"
	"github.com/lea-151107/dam/internal/theme"
)

// Node is a requirement or an element.
type Node struct {
	ID      string
	Kind    string // requirement type, or "element"
	Fields  [][2]string
	Classes []string
	Style   theme.Style
}

// Relation is "from - kind -> to".
type Relation struct {
	From, To int
	Kind     string
}

// Diagram is a parsed requirement diagram.
type Diagram struct {
	Dir       sugiyama.Dir
	Nodes     []*Node
	Relations []Relation
	index     map[string]int
	classDefs map[string]theme.Style
}

// kinds maps the block keywords to the stereotype shown in the box.
var kinds = map[string]string{
	"requirement": "Requirement", "functionalRequirement": "Functional Requirement",
	"interfaceRequirement": "Interface Requirement", "performanceRequirement": "Performance Requirement",
	"physicalRequirement": "Physical Requirement", "designConstraint": "Design Constraint",
	"element": "Element",
}

// fields maps field keys to their display names, in display order.
var fields = []struct{ key, name string }{
	{"id", "Id"}, {"text", "Text"}, {"risk", "Risk"}, {"verifymethod", "Verification"},
	{"type", "Type"}, {"docref", "Doc Ref"},
}

var relKinds = map[string]bool{"contains": true, "copies": true, "derives": true, "satisfies": true,
	"verifies": true, "refines": true, "traces": true}

const namePat = `([\p{L}\p{N}_\-.]+|"[^"]*")`

var (
	blockRe    = regexp.MustCompile(`^(\w+)\s+` + namePat + `\s*(?::::\s*([\w-]+))?\s*\{$`)
	fieldRe    = regexp.MustCompile(`^(\w+)\s*:\s*(.*)$`)
	relRe      = regexp.MustCompile(`^` + namePat + `\s*-\s*(\w+)\s*->\s*` + namePat + `$`)
	revRelRe   = regexp.MustCompile(`^` + namePat + `\s*<-\s*(\w+)\s*-\s*` + namePat + `$`)
	classDefRe = regexp.MustCompile(`^classDef\s+(\S+)\s+(.*)$`)
	classRe    = regexp.MustCompile(`^class\s+(\S+)\s+([\w-]+)$`)
	// classShorthandRe matches "name:::class" alone on a line.
	classShorthandRe = regexp.MustCompile(`^` + namePat + `\s*:::\s*[\w-]+$`)
	styleRe          = regexp.MustCompile(`^style\s+(\S+)\s+(.*)$`)
)

// Parse parses a requirement diagram.
func Parse(g *guard.Guard, lines []source.Line) (*Diagram, error) {
	d := &Diagram{Dir: sugiyama.TB, index: map[string]int{}, classDefs: map[string]theme.Style{}}
	open := -1
	openLine := 0
	node := func(id string) (int, error) {
		id = strings.Trim(id, `"`)
		if i, ok := d.index[id]; ok {
			return i, nil
		}
		d.Nodes = append(d.Nodes, &Node{ID: id, Kind: "element"})
		d.index[id] = len(d.Nodes) - 1
		return len(d.Nodes) - 1, g.Count("requirements and elements", len(d.Nodes))
	}
	for _, l := range lines[1:] {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		t := strings.TrimSpace(l.Text)
		if open >= 0 {
			if t == "}" {
				open = -1
				continue
			}
			m := fieldRe.FindStringSubmatch(t)
			if m == nil {
				return nil, source.Errorf(l.No, "expected key: value")
			}
			key := strings.ToLower(m[1])
			known := false
			for _, f := range fields {
				known = known || f.key == key
			}
			if !known {
				return nil, source.Errorf(l.No, "unknown field %q", m[1])
			}
			n := d.Nodes[open]
			n.Fields = append(n.Fields, [2]string{key, source.Label(strings.TrimSpace(m[2]))})
			if err := g.Count("fields", len(n.Fields)); err != nil {
				return nil, err
			}
			continue
		}
		if strings.HasPrefix(t, "accTitle") || strings.HasPrefix(t, "accDescr") || strings.HasPrefix(t, "title") {
			// NOTE(mermaid-compat): accessibility texts do not affect the
			// image; titles come from front matter.
			continue
		}
		if rest, ok := source.CutKeyword(t, "direction"); ok {
			switch strings.ToUpper(rest) {
			case "TB", "TD":
				d.Dir = sugiyama.TB
			case "BT":
				d.Dir = sugiyama.BT
			case "LR":
				d.Dir = sugiyama.LR
			case "RL":
				d.Dir = sugiyama.RL
			default:
				return nil, source.Errorf(l.No, "unknown direction %q", rest)
			}
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
				i, err := node(id)
				if err != nil {
					return nil, err
				}
				d.Nodes[i].Classes = append(d.Nodes[i].Classes, m[2])
			}
			continue
		}
		if m := styleRe.FindStringSubmatch(t); m != nil {
			i, err := node(m[1])
			if err != nil {
				return nil, err
			}
			d.Nodes[i].Style = d.Nodes[i].Style.Merge(theme.ParseStyle(m[2]))
			continue
		}
		if m := blockRe.FindStringSubmatch(t); m != nil {
			if _, ok := kinds[m[1]]; !ok {
				return nil, source.Errorf(l.No, "unknown block %q", m[1])
			}
			i, err := node(m[2])
			if err != nil {
				return nil, err
			}
			d.Nodes[i].Kind = m[1]
			if m[3] != "" {
				d.Nodes[i].Classes = append(d.Nodes[i].Classes, m[3])
			}
			open, openLine = i, l.No
			continue
		}
		var from, kind, to string
		if m := relRe.FindStringSubmatch(t); m != nil {
			from, kind, to = m[1], m[2], m[3]
		} else if m := revRelRe.FindStringSubmatch(t); m != nil {
			from, kind, to = m[3], m[2], m[1]
		} else {
			if classShorthandRe.MatchString(t) {
				// NOTE(mermaid-compat): "name:::class" on its own line
				// (Mermaid 11) is valid; "class name class" is supported.
				return nil, source.Unsupported(l.No, "name:::class statements (use class name class)")
			}
			return nil, source.Errorf(l.No, "unrecognized statement %q", t)
		}
		if !relKinds[kind] {
			return nil, source.Errorf(l.No, "unknown relationship %q", kind)
		}
		f, err := node(from)
		if err != nil {
			return nil, err
		}
		to2, err := node(to)
		if err != nil {
			return nil, err
		}
		d.Relations = append(d.Relations, Relation{From: f, To: to2, Kind: kind})
		if err := g.Count("relations", len(d.Relations)); err != nil {
			return nil, err
		}
	}
	if open >= 0 {
		return nil, source.Errorf(openLine, "%q is not closed with }", d.Nodes[open].ID)
	}
	for _, n := range d.Nodes {
		st := d.classDefs["default"]
		for _, c := range n.Classes {
			st = st.Merge(d.classDefs[c])
		}
		n.Style = st.Merge(n.Style)
	}
	return d, nil
}

// graph converts the diagram to a box graph.
func (d *Diagram) graph() *boxgraph.Graph {
	g := &boxgraph.Graph{Dir: d.Dir}
	for _, n := range d.Nodes {
		b := boxgraph.Node{Header: []string{"«" + kinds[n.Kind] + "»", string(richtext.Marker(richtext.Bold)) + n.ID}, Cluster: -1,
			Fill: n.Style.Fill, Stroke: n.Style.Stroke, Text: n.Style.Color}
		var rows [][]string
		for _, f := range fields {
			for _, v := range n.Fields {
				if v[0] == f.key {
					rows = append(rows, []string{f.name + ":", v[1]})
				}
			}
		}
		if len(rows) > 0 {
			b.Sections = [][][]string{rows}
		}
		g.Nodes = append(g.Nodes, b)
	}
	for _, r := range d.Relations {
		e := boxgraph.Edge{From: r.From, To: r.To, Label: "«" + r.Kind + "»", End: scene.Arrow}
		if r.Kind == "contains" {
			// Containment has a circle at the container instead.
			e.Start, e.End = scene.Circle, scene.NoMarker
		}
		g.Edges = append(g.Edges, e)
	}
	return g
}

// Layout implements diagram.Layouter.
func (d *Diagram) Layout(env *diagram.Env) (*scene.Scene, error) {
	return boxgraph.Layout(env, d.graph())
}

// Text renders the diagram with box-drawing characters.
func (d *Diagram) Text(g *guard.Guard, maxCols int) ([]string, error) {
	return textgraph.Text(g, d.graph().TextGraph(), maxCols)
}
