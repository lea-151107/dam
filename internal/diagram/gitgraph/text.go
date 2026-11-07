package gitgraph

import (
	"strings"

	"github.com/lea/mermaid/internal/guard"
	"github.com/lea/mermaid/internal/textgrid"
)

// Text renders the git graph with box-drawing characters: one column per
// branch and one row per commit, top to bottom (bottom to top for BT),
// with the commit id, branch and tags to the right. A branch leaves its
// lane right after the commit it starts from; a merge joins right before
// the merge commit.
//
// NOTE(mermaid-compat): text output is always vertical, also for LR.
func (d *Diagram) Text(g *guard.Guard, maxCols int) ([]string, error) {
	var last []string
	// Drop the branch names and tags when the full labels do not fit.
	for _, full := range []bool{true, false} {
		lines, err := d.text(g, full)
		if err != nil {
			return nil, err
		}
		if textgrid.MaxWidth(lines) <= maxCols {
			return lines, nil
		}
		last = lines
	}
	return nil, guard.Limit("columns", textgrid.MaxWidth(last), maxCols)
}

func (d *Diagram) text(g *guard.Guard, full bool) ([]string, error) {
	n := len(d.Commits)
	if n == 0 {
		return nil, nil
	}
	lanes := d.lanes()
	width := 0
	for _, l := range lanes {
		width = max(width, 2*l+1)
	}
	rowOf := func(i int) int {
		if d.Orient == BT {
			return 2 * (n - 1 - i)
		}
		return 2 * i
	}
	labels := make([]string, n)
	labelW := 0
	for i, c := range d.Commits {
		parts := []string{c.ID}
		if full {
			parts = append(parts, "("+d.Branches[c.Branch].Name+")")
			for _, t := range c.Tags {
				parts = append(parts, "["+t+"]")
			}
		}
		labels[i] = textgrid.Sanitize(strings.Join(parts, " "))
		labelW = max(labelW, textgrid.Width(labels[i]))
	}
	grid, err := textgrid.New(width+1+labelW, 2*n-1)
	if err != nil {
		return nil, err
	}
	// A line from a parent to a child: along the parent's lane up to the
	// row before the child, across to the child's lane, then into it.
	// Shared stretches merge into junctions.
	for i, c := range d.Commits {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		x1, y1 := 2*lanes[c.Branch], rowOf(i)
		for k, p := range c.Parents {
			x0, y0 := 2*lanes[d.Commits[p].Branch], rowOf(p)
			// A merge comes in just before the merge commit; a branch-off
			// (the first commit of a branch, whose parent is on another
			// lane) leaves right after its parent, so that the drawing
			// shows where the branch started.
			step := 1
			if y1 < y0 {
				step = -1
			}
			bend := y1 - step
			if k == 0 && !c.Merge && x0 != x1 {
				bend = y0 + step
			}
			line(grid, x0, y0, x0, bend)
			line(grid, x0, bend, x1, bend)
			line(grid, x1, bend, x1, y1)
		}
	}
	for i, c := range d.Commits {
		r := '●'
		switch {
		case c.Type == Highlight:
			r = '■'
		case c.Type == Reverse:
			r = '⊗'
		case c.Merge:
			r = '◉'
		}
		y := rowOf(i)
		grid.Set(2*lanes[c.Branch], y, r)
		grid.Text(width+1, y, labels[i])
	}
	return grid.Lines(), nil
}

// line joins the cells of a horizontal or vertical stretch one step at a
// time (Grid.Join connects neighbouring cells only).
func line(grid *textgrid.Grid, x0, y0, x1, y1 int) {
	dx, dy := sign(x1-x0), sign(y1-y0)
	for x, y := x0, y0; x != x1 || y != y1; x, y = x+dx, y+dy {
		grid.Join(x, y, x+dx, y+dy)
	}
}

func sign(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}
