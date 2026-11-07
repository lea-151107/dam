package mermaid

import (
	"github.com/lea/mermaid/internal/diagram"
	"github.com/lea/mermaid/internal/diagram/architecture"
	"github.com/lea/mermaid/internal/diagram/block"
	"github.com/lea/mermaid/internal/diagram/c4"
	"github.com/lea/mermaid/internal/diagram/class"
	"github.com/lea/mermaid/internal/diagram/er"
	"github.com/lea/mermaid/internal/diagram/flowchart"
	"github.com/lea/mermaid/internal/diagram/gantt"
	"github.com/lea/mermaid/internal/diagram/gitgraph"
	"github.com/lea/mermaid/internal/diagram/journey"
	"github.com/lea/mermaid/internal/diagram/kanban"
	"github.com/lea/mermaid/internal/diagram/mindmap"
	"github.com/lea/mermaid/internal/diagram/packet"
	"github.com/lea/mermaid/internal/diagram/pie"
	"github.com/lea/mermaid/internal/diagram/quadrant"
	"github.com/lea/mermaid/internal/diagram/radar"
	"github.com/lea/mermaid/internal/diagram/requirement"
	"github.com/lea/mermaid/internal/diagram/sankey"
	"github.com/lea/mermaid/internal/diagram/sequence"
	"github.com/lea/mermaid/internal/diagram/state"
	"github.com/lea/mermaid/internal/diagram/timeline"
	"github.com/lea/mermaid/internal/diagram/treemap"
	"github.com/lea/mermaid/internal/diagram/xychart"
	"github.com/lea/mermaid/internal/guard"
	"github.com/lea/mermaid/internal/source"
)

// kind describes one supported diagram type. Adding a diagram type means
// adding a DiagramType constant and one entry here; everything else
// (Detect, String, Render, RenderText) reads this table.
type kind struct {
	typ  DiagramType
	name string
	// keywords are the header words that select this type.
	keywords []string
	parse    func(g *guard.Guard, lines []source.Line) (diagram.Layouter, error)
	// text reports whether the parse result implements textDiagram. It is
	// a field rather than a type assertion so that RenderText can answer
	// ErrUnsupported before parsing (an unsupported type must not report
	// a syntax or size error). TestRegistry checks that it agrees.
	text bool
}

// kinds is indexed by DiagramType-1.
var kinds = [...]kind{
	{Flowchart, "flowchart", []string{"graph", "flowchart", "flowchart-v2", "flowchart-elk"}, wrap(flowchart.Parse), true},
	{Sequence, "sequence", []string{"sequenceDiagram"}, wrap(sequence.Parse), true},
	{Class, "class", []string{"classDiagram", "classDiagram-v2"}, wrap(class.Parse), true},
	{State, "state", []string{"stateDiagram", "stateDiagram-v2"}, wrap(state.Parse), true},
	{Pie, "pie", []string{"pie"}, wrap(pie.Parse), true},
	{Gantt, "gantt", []string{"gantt"}, wrap(gantt.Parse), true},
	{GitGraph, "gitGraph", []string{"gitGraph"}, wrap(gitgraph.Parse), true},
	{Mindmap, "mindmap", []string{"mindmap"}, wrap(mindmap.Parse), true},
	{ER, "er", []string{"erDiagram"}, wrap(er.Parse), true},
	{Timeline, "timeline", []string{"timeline"}, wrap(timeline.Parse), true},
	{Journey, "journey", []string{"journey"}, wrap(journey.Parse), true},
	{Quadrant, "quadrant", []string{"quadrantChart"}, wrap(quadrant.Parse), true},
	{XYChart, "xychart", []string{"xychart-beta", "xychart"}, wrap(xychart.Parse), true},
	{Requirement, "requirement", []string{"requirementDiagram"}, wrap(requirement.Parse), true},
	{C4, "c4", []string{"C4Context", "C4Container", "C4Component", "C4Dynamic", "C4Deployment"}, wrap(c4.Parse), true},
	{Kanban, "kanban", []string{"kanban"}, wrap(kanban.Parse), true},
	{Block, "block", []string{"block-beta", "block"}, wrap(block.Parse), true},
	{Packet, "packet", []string{"packet-beta", "packet"}, wrap(packet.Parse), true},
	{Sankey, "sankey", []string{"sankey-beta", "sankey"}, wrap(sankey.Parse), true},
	{Radar, "radar", []string{"radar-beta", "radar"}, wrap(radar.Parse), true},
	{Treemap, "treemap", []string{"treemap-beta", "treemap"}, wrap(treemap.Parse), true},
	{Architecture, "architecture", []string{"architecture-beta", "architecture"}, wrap(architecture.Parse), true},
}

// wrap adapts a parser that returns its own diagram type.
func wrap[D diagram.Layouter](p func(*guard.Guard, []source.Line) (D, error)) func(*guard.Guard, []source.Line) (diagram.Layouter, error) {
	return func(g *guard.Guard, lines []source.Line) (diagram.Layouter, error) {
		d, err := p(g, lines)
		if err != nil {
			return nil, err
		}
		return d, nil
	}
}

// kindOf returns the entry for t, or nil for Unknown and out-of-range values.
func kindOf(t DiagramType) *kind {
	if t < 1 || int(t) > len(kinds) {
		return nil
	}
	return &kinds[t-1]
}

// keywordType returns the type selected by a header keyword.
func keywordType(kw string) DiagramType {
	for i := range kinds {
		for _, k := range kinds[i].keywords {
			if k == kw {
				return kinds[i].typ
			}
		}
	}
	return Unknown
}
