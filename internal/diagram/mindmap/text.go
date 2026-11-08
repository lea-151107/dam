package mindmap

import (
	"strings"

	"github.com/lea-151107/dam/internal/guard"
	"github.com/lea-151107/dam/internal/textgrid"
)

// Text renders the mind map as an indented tree (├─ └─), wrapping long
// labels to the width left after the indentation.
//
// NOTE(mermaid-compat): node shapes are not shown in text output.
func (d *Diagram) Text(g *guard.Guard, maxCols int) ([]string, error) {
	if len(d.Nodes) == 0 {
		return nil, nil
	}
	var out []string
	var walk func(n int, prefix, cont string) error
	walk = func(n int, prefix, cont string) error {
		if err := g.Tick(); err != nil {
			return err
		}
		room := maxCols - textgrid.Width(prefix)
		if room < minLabelCols {
			return guard.Limit("columns", textgrid.Width(prefix)+minLabelCols, maxCols)
		}
		lines := textgrid.Wrap(strings.ReplaceAll(d.Nodes[n].Label, "\n", " "), room)
		if len(lines) == 0 {
			lines = []string{""}
		}
		out = append(out, prefix+lines[0])
		for _, l := range lines[1:] {
			out = append(out, cont+l)
		}
		kids := d.Nodes[n].Children
		for k, c := range kids {
			p, q := cont+"├─ ", cont+"│  "
			if k == len(kids)-1 {
				p, q = cont+"└─ ", cont+"   "
			}
			if err := walk(c, p, q); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(0, "", ""); err != nil {
		return nil, err
	}
	return out, nil
}

// minLabelCols is the narrowest room left for a label; deeper trees in
// fewer columns are too large.
const minLabelCols = 4
