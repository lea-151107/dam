package flowchart

import (
	"github.com/lea/mermaid/internal/guard"
	"github.com/lea/mermaid/internal/scene"
	"github.com/lea/mermaid/internal/textgraph"
	"github.com/lea/mermaid/internal/textgrid"
)

// Text renders the flowchart with box-drawing characters.
func (g *Graph) Text(gd *guard.Guard, maxCols int) ([]string, error) {
	return textgraph.Text(gd, g.textGraph(), maxCols)
}

// textGraph converts the flowchart to the text renderer's model. A node
// that stands for a subgraph is not drawn; edges to it are laid out with
// the subgraph's first member, as in the image, and drawn to its frame.
func (g *Graph) textGraph() *textgraph.Graph {
	tg := &textgraph.Graph{Dir: g.Dir}
	li := make([]int, len(g.Nodes))
	for i, n := range g.Nodes {
		li[i] = -1
		if g.ClusterRef[i] >= 0 && g.FirstMember[g.ClusterRef[i]] >= 0 {
			continue
		}
		var corners [4]rune
		switch n.Shape {
		case Round, Stadium, Circle, DoubleCircle, Cylinder:
			corners = textgrid.Rounded
		case Rhombus, Hexagon:
			corners = textgrid.Slanted
		}
		li[i] = len(tg.Nodes)
		tg.Nodes = append(tg.Nodes, textgraph.Node{Label: n.Label, Corners: corners, Cluster: n.Cluster})
	}
	for i, sg := range g.Subgraphs {
		tg.Clusters = append(tg.Clusters, textgraph.Cluster{Title: sg.Title, Parent: sg.Parent,
			Dir: sg.Dir, HasDir: sg.HasDir && !g.linkedOutside(i)})
	}
	endpoint := func(i int) int {
		if li[i] >= 0 {
			return li[i]
		}
		return li[g.FirstMember[g.ClusterRef[i]]]
	}
	// Markers are drawn as in the image: arrows ▲▼◀▶, circles ○ and
	// crosses ×; the key keeps edges with different markers from sharing
	// lines.
	head := func(m uint8) textgraph.Head {
		if m == 0 {
			return textgraph.Head{}
		}
		return textgraph.Head{Runes: textgraph.HeadFor(scene.Marker(m)).Runes, Key: m}
	}
	for _, e := range g.Edges {
		var style textgraph.LineStyle
		switch {
		case e.Kind == Dotted || e.Style.Dashed:
			style = textgraph.Dashed
		case e.Kind == Thick:
			style = textgraph.Heavy
		}
		te := textgraph.NewEdge(endpoint(e.From), endpoint(e.To))
		te.Label, te.Start, te.End, te.MinLen, te.Style = e.Label, head(uint8(e.Start)), head(uint8(e.End)), e.MinLen, style
		te.Hidden, te.OwnCluster = e.Kind == Invisible, ownCluster(g, e)
		// An end that stands for a subgraph stops at its frame, as in the
		// image, unless the edge links a node with its own subgraph.
		if te.From != te.To {
			if li[e.From] < 0 {
				te.FromCluster = g.ClusterRef[e.From]
			}
			if li[e.To] < 0 {
				te.ToCluster = g.ClusterRef[e.To]
			}
		}
		tg.Edges = append(tg.Edges, te)
	}
	return tg
}

// ownCluster returns the subgraph an edge refers to when one endpoint is a
// subgraph reference, or -1.
func ownCluster(g *Graph, e Edge) int {
	if c := g.ClusterRef[e.To]; c >= 0 {
		return c
	}
	return g.ClusterRef[e.From]
}
