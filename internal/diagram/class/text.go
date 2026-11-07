package class

import (
	"strings"

	"github.com/lea/mermaid/internal/guard"
	"github.com/lea/mermaid/internal/textgraph"
)

// Text renders the class diagram with box-drawing characters: each class
// is a box with its annotations and name, then its attributes and methods
// in sections below rules; relations show their markers (△ inheritance,
// ◆ composition, ◇ aggregation) and dependencies are dashed.
func (d *Diagram) Text(g *guard.Guard, maxCols int) ([]string, error) {
	tg := &textgraph.Graph{Dir: d.Dir}
	for _, c := range d.Classes {
		var head []string
		for _, a := range c.Annotations {
			head = append(head, "«"+a+"»")
		}
		head = append(head, c.Label)
		n := textgraph.Node{Label: strings.Join(head, "\n"), Cluster: c.Namespace}
		if len(c.Attributes) > 0 || len(c.Methods) > 0 {
			n.Sections = append(n.Sections, strings.Join(c.Attributes, "\n"))
		}
		if len(c.Methods) > 0 {
			n.Sections = append(n.Sections, strings.Join(c.Methods, "\n"))
		}
		tg.Nodes = append(tg.Nodes, n)
	}
	for _, ns := range d.Namespaces {
		tg.Clusters = append(tg.Clusters, textgraph.Cluster{Title: ns, Parent: -1})
	}
	for _, r := range d.Relations {
		label := strings.Join(nonEmpty(r.FromCard, r.Label, r.ToCard), " ")
		e := textgraph.NewEdge(r.From, r.To)
		e.Label, e.Start, e.End = label, textgraph.HeadFor(r.StartM), textgraph.HeadFor(r.EndM)
		if r.Dashed {
			e.Style = textgraph.Dashed
		}
		tg.Edges = append(tg.Edges, e)
	}
	return textgraph.Text(g, tg, maxCols)
}

func nonEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
