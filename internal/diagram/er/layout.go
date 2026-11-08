package er

import (
	"github.com/lea-151107/dam/internal/boxgraph"
	"github.com/lea-151107/dam/internal/diagram"
	"github.com/lea-151107/dam/internal/guard"
	"github.com/lea-151107/dam/internal/scene"
	"github.com/lea-151107/dam/internal/textgraph"
)

// Layout implements diagram.Layouter.
func (d *Diagram) Layout(env *diagram.Env) (*scene.Scene, error) {
	return boxgraph.Layout(env, d.graph())
}

// Text renders the diagram with box-drawing characters; the multiplicities
// are written next to the relation labels (1, 0..1, 1..*, 0..*).
func (d *Diagram) Text(g *guard.Guard, maxCols int) ([]string, error) {
	return textgraph.Text(g, d.graph().TextGraph(), maxCols)
}
