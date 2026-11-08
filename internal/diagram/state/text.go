package state

import (
	"strings"

	"github.com/lea-151107/dam/internal/guard"
	"github.com/lea-151107/dam/internal/textgraph"
	"github.com/lea-151107/dam/internal/textgrid"
)

// Text renders the state diagram with box-drawing characters: states are
// rounded boxes (descriptions below a rule), start and end states ● and ◉,
// choices ◇, forks and joins ━, composite states and concurrent regions
// frames; notes are joined by dashed lines.
func (d *Diagram) Text(g *guard.Guard, maxCols int) ([]string, error) {
	has := d.hasMembers()
	tg := &textgraph.Graph{Dir: d.Dir}
	li := make([]int, len(d.States))
	for i, s := range d.States {
		li[i] = -1
		if s.Composite >= 0 && has[s.Composite] {
			continue
		}
		n := textgraph.Node{Cluster: s.Parent}
		switch s.Kind {
		case Start:
			n.Glyph = '●'
		case End:
			n.Glyph = '◉'
		case Choice:
			n.Glyph = '◇'
		case Fork, Join:
			n.Glyph = '━'
			if d.Dir.Horizontal() {
				n.Glyph = '┃'
			}
		case Note:
			n.Label = s.Label
		default:
			n.Label, n.Corners = s.Label, textgrid.Rounded
			if len(s.Desc) > 0 {
				n.Sections = []string{strings.Join(s.Desc, "\n")}
			}
		}
		li[i] = len(tg.Nodes)
		tg.Nodes = append(tg.Nodes, n)
	}
	for i, c := range d.Composites {
		title := ""
		if !c.Region {
			title = d.States[c.State].Label
		}
		parent := c.Parent
		if !has[i] {
			parent = noCluster
		}
		dirOf := c
		if c.Region && c.Parent >= 0 {
			dirOf = d.Composites[c.Parent]
		}
		tg.Clusters = append(tg.Clusters, textgraph.Cluster{Title: title, Parent: parent, Dir: dirOf.Dir, HasDir: dirOf.HasDir})
	}
	// A transition to or from a composite state goes to its first member,
	// as in the image layout.
	first := func(c int) int {
		for i, s := range d.States {
			if li[i] < 0 {
				continue
			}
			for p := s.Parent; p >= 0; p = d.Composites[p].Parent {
				if p == c {
					return li[i]
				}
			}
		}
		return -1
	}
	// endpoint returns the layout node of a state and, for a composite
	// with members, the cluster whose frame the edge stops at.
	endpoint := func(s int) (int, int) {
		if li[s] >= 0 {
			return li[s], -1
		}
		c := d.States[s].Composite
		return first(c), c
	}
	for _, tr := range d.Transitions {
		f, fc := endpoint(tr.From)
		t, tc := endpoint(tr.To)
		if f < 0 || t < 0 {
			continue
		}
		e := textgraph.NewEdge(f, t)
		e.Label, e.FromCluster, e.ToCluster = tr.Label, fc, tc
		if tr.Note {
			e.Style = textgraph.Dashed
		} else {
			e.End = textgraph.Arrow
		}
		tg.Edges = append(tg.Edges, e)
	}
	return textgraph.Text(g, tg, maxCols)
}
